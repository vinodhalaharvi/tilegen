package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Selection, properly.
//
// A spec node says what it needs; tiles say what they offer. Where more
// than one tile offers a capability, they compete: the legal ones are
// scored under the policy and the cheapest wins. Storage was the first
// capability; any other works the same way, with no new machinery.
//
// The covering is computed bottom-up, the way a compiler's instruction
// selector does it (Aho-Johnson, iburg): the cost of covering a node is
// the tile's own cost plus the cost of covering the children it delegates,
// plus the cost of any chain that converts its form to the one the parent
// wants. Doing it bottom-up matters as soon as a tile's cost depends on
// what its children cost; doing it greedily would then be locally right
// and globally wrong.
//
// Four properties hold, and each is tested:
//
//   - Totality: every need is either covered or reported. No silent gaps.
//   - Determinism: the same spec, policy and lock always give the same
//     covering. Scores are integers; ties break by tile name; no map
//     iteration decides anything.
//   - Legality before cost: an illegal tile is never scored, and legality
//     depends only on the spec, never on the machine.
//   - Optimality: among legal coverings, the one selected is cheapest
//     under the policy's weights.

// A Capability is something a node can need and tiles can offer:
// "store", "event-bus". Requirements are the shared vocabulary a node
// uses to say what it wants: (durable) means the same everywhere.
type Capability struct {
	Name string
	Doc  string
	// Requirements a node of this capability may declare.
	Requirements []string
}

var capabilities = map[string]*Capability{}

// RegisterCapability declares a capability. Offers name it; needs ask for it.
func RegisterCapability(c *Capability) {
	if _, dup := capabilities[c.Name]; dup {
		panic("tilegen: capability registered twice: " + c.Name)
	}
	capabilities[c.Name] = c
}

func capabilityNames() []string { return sortedKeys(capabilities) }

// knownRequirements is every requirement any capability accepts: the
// shared vocabulary, so (durable) means one thing across the language.
func knownRequirements() []string {
	set := map[string]bool{}
	for _, c := range capabilities {
		for _, r := range c.Requirements {
			set[r] = true
		}
	}
	return sortedKeys(set)
}

// An Offer is one tile's claim that it can satisfy a capability.
type Offer struct {
	Tile       string // the tile's name in the registry
	Capability string
	Doc        string
	Form       string            // the form its code yields; "" means domain
	Cost       Cost              // its own cost, before children and chains
	Requires   []string          // external tools, reported but never a legality test
	IllegalFor map[string]string // requirement -> why it cannot serve it

	// Children are the capabilities this offer itself needs covered. An
	// offer with children is what makes the covering a tree, and what
	// makes bottom-up necessary.
	Children func(n *Need) []*Need

	// Impl carries whatever the tile needs to generate; selection never
	// looks inside it.
	Impl any
}

var offers = map[string][]*Offer{} // capability -> offers, kept sorted by tile

// RegisterOffer adds a tile's offer of a capability. Registration order
// does not matter: Go runs init functions in filename order, so an offer
// may arrive before its capability. checkRegistry, called once at startup,
// catches an offer whose capability never appears.
func RegisterOffer(o *Offer) {
	for _, prev := range offers[o.Capability] {
		if prev.Tile == o.Tile {
			panic("tilegen: offer registered twice: " + o.Tile)
		}
	}
	list := append(offers[o.Capability], o)
	sort.Slice(list, func(i, j int) bool { return list[i].Tile < list[j].Tile })
	offers[o.Capability] = list
}

func offersOf(capability string) []*Offer { return offers[capability] }

// A Need is one node asking for a capability.
type Need struct {
	ID           string // orders.OrderStore: stable, and the lock's key
	Capability   string
	Requirements []string // from the shared vocabulary
	Want         string   // the form the parent wants; "" means domain
	Pos          Pos
	Node         *Node // the spec node, for tiles that need it
	Data         any   // whatever the tile that made this need attached
}

// A Covering is the chosen tile for a need, its children's coverings, and
// the chain that converts its form to the wanted one.
type Covering struct {
	Need     *Need
	Offer    *Offer
	Score    int // total: own cost + children + chain
	Own      int
	Terms    string
	Via      []Step
	Children []*Covering
	By       string // auto | config | lock
	Why      string // when the policy, rather than cost alone, decided
	Ranked   []Candidate
	Illegal  []Rejected
}

// A Candidate is a legal offer with its total cost, for explain.
type Candidate struct {
	Offer *Offer
	Score int
	Own   int
	Terms string
	Via   []Step
}

// Rejected is an offer that could not be chosen, and why.
type Rejected struct {
	Tile   string
	Reason string
}

// solve computes the cheapest legal covering of a need, bottom-up: the
// children first, then each offer's total, then the cheapest. It is the
// whole optimization, and it is deliberately small.
func solve(n *Need, c *Ctx, shape func(*Need) EntityShape) (*Covering, error) {
	cov := &Covering{Need: n, By: "auto"}
	cands := offersOf(n.Capability)
	if len(cands) == 0 {
		return nil, fmt.Errorf("%s: nothing offers %s, which %s needs", n.Pos, n.Capability, n.ID)
	}
	want := n.Want
	if want == "" {
		want = domainForm
	}
	byTile := map[string]*Covering{}
	for _, o := range cands {
		if why := offerIllegalFor(o, n.Requirements); why != "" {
			cov.Illegal = append(cov.Illegal, Rejected{o.Tile, why})
			continue
		}
		if why, avoided := c.Policy.Avoid[o.Tile]; avoided {
			reason := "avoided by the policy"
			if why != "" {
				reason += ": " + why
			}
			cov.Illegal = append(cov.Illegal, Rejected{o.Tile, reason})
			continue
		}
		via, chainCost, ok := convert(offerForm(o), want, shape(n), c.Policy)
		if !ok {
			cov.Illegal = append(cov.Illegal, Rejected{o.Tile,
				fmt.Sprintf("it produces %s, and no chain converts that to %s", offerForm(o), want)})
			continue
		}
		own, terms := c.Policy.score(o.Cost)
		total := own + chainCost

		// Bottom-up: cover this offer's own needs first, and add them in.
		var kids []*Covering
		broken := ""
		if o.Children != nil {
			for _, kid := range o.Children(n) {
				kc, err := solve(kid, c, shape)
				if err != nil {
					broken = err.Error()
					break
				}
				kids = append(kids, kc)
				total += kc.Score
			}
		}
		if broken != "" {
			cov.Illegal = append(cov.Illegal, Rejected{o.Tile, "one of its own needs cannot be covered: " + broken})
			continue
		}
		cov.Ranked = append(cov.Ranked, Candidate{Offer: o, Score: total, Own: own, Terms: terms, Via: via})
		byTile[o.Tile] = &Covering{Need: n, Offer: o, Score: total, Own: own, Terms: terms, Via: via, Children: kids}
	}
	// Cheapest first; ties break by tile name, so the result never
	// depends on registration or map order.
	sort.SliceStable(cov.Ranked, func(i, j int) bool {
		if cov.Ranked[i].Score != cov.Ranked[j].Score {
			return cov.Ranked[i].Score < cov.Ranked[j].Score
		}
		return cov.Ranked[i].Offer.Tile < cov.Ranked[j].Offer.Tile
	})
	if len(cov.Ranked) == 0 {
		return cov, fmt.Errorf("%s: no tile can cover %s (%s)%s", n.Pos, n.ID, n.Capability, rejections(cov.Illegal))
	}
	chosen, why := pickOffer(c.Policy, cov.Ranked)
	fill := byTile[chosen.Tile]
	cov.Offer, cov.Score, cov.Own, cov.Terms, cov.Via, cov.Children = fill.Offer, fill.Score, fill.Own, fill.Terms, fill.Via, fill.Children
	cov.Why = why
	return cov, nil
}

// pickOffer applies the policy's preferences to ranked candidates.
func pickOffer(p *Policy, ranked []Candidate) (*Offer, string) {
	best := ranked[0]
	for _, s := range ranked[1:] {
		if p.Prefer[s.Offer.Tile] && !p.Prefer[best.Offer.Tile] && s.Score <= best.Score+p.Margin {
			return s.Offer, fmt.Sprintf("preferred, and within the margin of %s (%d vs %d, margin %d)",
				best.Offer.Tile, s.Score, best.Score, p.Margin)
		}
	}
	if p.Prefer[best.Offer.Tile] {
		return best.Offer, "cheapest, and preferred"
	}
	return best.Offer, ""
}

func offerIllegalFor(o *Offer, reqs []string) string {
	for _, r := range reqs {
		if why, ok := o.IllegalFor[r]; ok {
			return why
		}
	}
	return ""
}

func offerForm(o *Offer) string {
	if o.Form == "" {
		return domainForm
	}
	return o.Form
}

func rejections(rs []Rejected) string {
	if len(rs) == 0 {
		return ""
	}
	var parts []string
	for _, r := range rs {
		parts = append(parts, fmt.Sprintf("\n  %s: %s", r.Tile, r.Reason))
	}
	return strings.Join(parts, "")
}

// validateRequirements reports requirements a capability does not accept.
func validateRequirements(capability string, reqs []string, at *Node, v *validator) {
	c := capabilities[capability]
	if c == nil {
		return
	}
	for _, r := range reqs {
		if !contains(c.Requirements, r) {
			v.bad(at, "%s does not take the requirement (%s)%s; it takes %s",
				capability, r, didYouMean(r, c.Requirements), strings.Join(c.Requirements, ", "))
		}
	}
}

// checkRegistry verifies the registry is coherent, whatever order things
// registered in: every capability has at least one offer (totality: a
// spec's need can always be covered or explained), and every offer names a
// capability that exists. run() calls it, so a mistake fails loudly and
// immediately rather than at the node that needed it.
func checkRegistry() error {
	var errs []error
	for _, name := range capabilityNames() {
		if len(offers[name]) == 0 {
			errs = append(errs, fmt.Errorf("capability %q has no offers: a spec needing it could never be covered", name))
		}
	}
	for _, capability := range sortedKeys(offers) {
		if capabilities[capability] == nil {
			var tiles []string
			for _, o := range offers[capability] {
				tiles = append(tiles, o.Tile)
			}
			errs = append(errs, fmt.Errorf("tile(s) %s offer %q, which is not a registered capability", strings.Join(tiles, ", "), capability))
		}
	}
	return errors.Join(errs...)
}
