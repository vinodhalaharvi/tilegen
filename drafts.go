package main

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// A draft is a tile declared for one compile, not built into tilegen:
//
//	(tile cockroachdb
//	  (offers store)
//	  (doc "distributed SQL that speaks the Postgres protocol")
//	  (module github.com/cockroachdb/cockroach-go/v2)
//	  (engine cockroachdb)
//	  (symbols crdbpgx.ExecuteTx)
//	  (satisfies (durable) "a write is replicated before it is acknowledged")
//	  (satisfies (cross-process) "any number of clients connect to the cluster")
//	  (illegal-when (no-broker) "it is a cluster to run")
//	  (cost (llm-work 4 "plain SQL through pgx")
//	        (maintenance 3) (dependency 2) (runtime 2)
//	        (operations 7 "at least three nodes, and their upgrades")))
//
// It says what a tool is for and what it costs, which is all selection
// needs. So a draft takes part in every decision and every explanation,
// and a team can ask "would the thing we already use win here?" before
// anyone writes a generator for it.
//
// It cannot generate code: that needs a tile compiled into tilegen. When
// a draft wins a need, tilegen reports the decision and stops there.
//
// A draft is held to a stricter rule than a built-in tile. It may be
// written by a person from memory or by an LLM, and nothing has run it.
// So it wins only what it claims: a requirement it does not mention is
// unverified, and it pays for being unproven.

// draftUnproven is the uncertainty every draft carries: nothing has
// generated or run it.
const draftUnproven = 2

// symbolRef is how a draft cites its module: pkg.Name or pkg.Type.Method,
// where pkg is a package's name or its import path.
var symbolRef = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*|[a-z0-9][A-Za-z0-9_.~-]*(/[A-Za-z0-9_.~-]+)+)\.[A-Z][A-Za-z0-9_]*(\.[A-Z][A-Za-z0-9_]*)?$`)

var draftName = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// draftDims are the cost dimensions a draft may price.
var draftDims = []string{"llm-work", "maintenance", "dependency", "runtime", "operations", "uncertainty"}

// parseDrafts reads (tile ...) forms into offers. Every problem is
// reported, so a draft can be fixed in one pass.
func parseDrafts(text string) ([]*Offer, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	forms, err := Parse("tiles.sexp", text)
	if err != nil {
		return nil, err
	}
	var out []*Offer
	var errs []error
	seen := map[string]bool{}
	for _, f := range forms {
		o, err := parseDraft(f)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if seen[o.Tile] {
			errs = append(errs, fmt.Errorf("%s: two drafts are named %s", f.Pos, o.Tile))
			continue
		}
		seen[o.Tile] = true
		out = append(out, o)
	}
	return out, errors.Join(errs...)
}

func parseDraft(f *Node) (*Offer, error) {
	var errs []error
	bad := func(n *Node, format string, a ...any) {
		errs = append(errs, fmt.Errorf("%s: "+format, append([]any{n.Pos}, a...)...))
	}
	if f.Head() != "tile" || len(f.List) < 2 || f.List[1].IsList {
		return nil, fmt.Errorf("%s: expected (tile NAME (offers CAPABILITY) ...), got %s", f.Pos, short(f))
	}
	o := &Offer{Tile: f.List[1].Atom, Draft: true, IllegalFor: map[string]string{}, Satisfies: map[string]string{}, Unverified: map[string]string{}}
	if !draftName.MatchString(o.Tile) {
		bad(f.List[1], "a tile's name is lower-case words joined by hyphens, like cockroach-db, got %q", o.Tile)
	}
	for _, capability := range capabilityNames() {
		for _, built := range offersOf(capability) {
			if built.Tile == o.Tile {
				bad(f.List[1], "tilegen already has a tile named %s; give the draft another name", o.Tile)
			}
		}
	}
	// (offers ...) first: what a requirement may be depends on it.
	var cap *Capability
	if offers := f.Find("offers"); offers == nil || len(offers.List) != 2 || offers.List[1].IsList {
		bad(f, "(tile %s ...) needs (offers CAPABILITY): one of %s", o.Tile, strings.Join(draftCapabilities(), ", "))
	} else if cap = capabilities[offers.List[1].Atom]; cap == nil || !contains(draftCapabilities(), cap.Name) {
		bad(offers, "a draft can offer %s; got %s%s", strings.Join(draftCapabilities(), ", "), offers.List[1].Atom, didYouMean(offers.List[1].Atom, draftCapabilities()))
		cap = nil
	} else {
		o.Capability = cap.Name
	}
	claim := func(it *Node, into map[string]string) {
		if len(it.List) != 3 || !it.List[1].IsList || len(it.List[1].List) != 1 || it.List[2].IsList || !it.List[2].Quoted || strings.TrimSpace(it.List[2].Atom) == "" {
			bad(it, `expected (%s (REQUIREMENT) "why"), got %s`, it.Head(), short(it))
			return
		}
		req := it.List[1].List[0].Atom
		if cap != nil && !contains(cap.Requirements, req) {
			bad(it.List[1], "%s is not something a %s can be asked for; those are %s%s", req, cap.Name, strings.Join(cap.Requirements, ", "), didYouMean(req, cap.Requirements))
			return
		}
		for _, m := range []map[string]string{o.Satisfies, o.IllegalFor, o.Unverified} {
			if _, dup := m[req]; dup {
				bad(it, "the draft says two things about %s; say one", req)
				return
			}
		}
		into[req] = it.List[2].Atom
	}
	counted := map[string]bool{}
	for _, it := range f.List[2:] {
		if h := it.Head(); h != "satisfies" && h != "illegal-when" && h != "unverified" {
			if counted[h] {
				bad(it, "duplicate (%s ...) in tile %s", h, o.Tile)
				continue
			}
			counted[h] = true
		}
		switch it.Head() {
		case "offers":
		case "doc":
			if len(it.List) != 2 || it.List[1].IsList {
				bad(it, `expected (doc "what it is"), got %s`, short(it))
				continue
			}
			o.Doc = it.List[1].Atom
		case "module":
			if len(it.List) != 2 || it.List[1].IsList {
				bad(it, "expected (module IMPORT/PATH), got %s", short(it))
				continue
			}
			o.Module = it.List[1].Atom
		case "engine":
			if len(it.List) != 2 || it.List[1].IsList || !draftName.MatchString(it.List[1].Atom) {
				bad(it, "expected (engine NAME), the server this tile runs on, got %s", short(it))
				continue
			}
			o.Engine = it.List[1].Atom
		case "symbols":
			if len(it.List) < 2 {
				bad(it, "expected (symbols pkg.Name ...), the exported symbols the claims rest on, got %s", short(it))
				continue
			}
			for _, ref := range it.Args() {
				if ref.IsList || !symbolRef.MatchString(ref.Atom) {
					bad(ref, "a symbol is written pkg.Name or pkg.Type.Method, with an exported name, like nats.Conn or nats.Conn.Publish; got %s", short(ref))
					continue
				}
				if contains(o.Symbols, ref.Atom) {
					bad(ref, "%s is cited twice", ref.Atom)
					continue
				}
				o.Symbols = append(o.Symbols, ref.Atom)
			}
		case "satisfies":
			claim(it, o.Satisfies)
		case "illegal-when":
			claim(it, o.IllegalFor)
		case "unverified":
			claim(it, o.Unverified)
		case "cost":
			for _, t := range it.Args() {
				if !t.IsList || len(t.List) < 2 || len(t.List) > 3 || t.List[1].IsList || (len(t.List) == 3 && (t.List[2].IsList || !t.List[2].Quoted)) {
					bad(t, `expected (DIMENSION N) or (DIMENSION N "why"), got %s`, short(t))
					continue
				}
				dim := t.Head()
				if !contains(draftDims, dim) {
					bad(t, "%s is not a cost dimension; those are %s%s", dim, strings.Join(draftDims, ", "), didYouMean(dim, draftDims))
					continue
				}
				v, err := strconv.Atoi(t.List[1].Atom)
				if err != nil || v < 0 || v > 9 {
					bad(t, "a cost is 0 to 9, got %s", t.List[1].Atom)
					continue
				}
				term := CostTerm{Dim: dim, Value: v}
				if len(t.List) == 3 {
					term.Source, term.Note = "draft", t.List[2].Atom
				}
				for _, have := range o.Cost {
					if have.Dim == dim {
						bad(t, "%s is priced twice", dim)
					}
				}
				o.Cost = append(o.Cost, term)
			}
		default:
			known := []string{"offers", "doc", "module", "engine", "symbols", "satisfies", "illegal-when", "unverified", "cost"}
			bad(it, "a tile contains %s; got %s%s", strings.Join(known, ", "), short(it), didYouMean(it.Head(), known))
		}
	}
	if strings.TrimSpace(o.Doc) == "" {
		bad(f, `(tile %s ...) needs a (doc "..."): one line on what it is`, o.Tile)
	}
	if len(o.Symbols) > 0 && o.Module == "" {
		bad(f, "(tile %s ...) cites symbols and names no (module ...) they could come from", o.Tile)
	}
	for _, need := range []string{"llm-work", "maintenance", "dependency"} {
		priced := false
		for _, t := range o.Cost {
			priced = priced || t.Dim == need
		}
		if !priced {
			bad(f, "(tile %s ...) needs a (cost ...) with llm-work, maintenance and dependency; %s is missing", o.Tile, need)
			break
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	// It wins only what it claims.
	for _, req := range cap.Requirements {
		if o.Satisfies[req] == "" && o.IllegalFor[req] == "" && o.Unverified[req] == "" {
			o.Unverified[req] = "the draft says nothing about it"
		}
	}
	// Unproven, whoever wrote it.
	unproven := false
	for i, t := range o.Cost {
		if t.Dim == "uncertainty" {
			o.Cost[i].Value, unproven = t.Value+draftUnproven, true
		}
	}
	if !unproven {
		o.Cost = append(o.Cost, CostTerm{Dim: "uncertainty", Value: draftUnproven, Source: "draft", Note: "a draft: nothing has generated or run it"})
	}
	return o, nil
}

// draftCapabilities are the capabilities a draft may offer: the ones a
// tool can be chosen for.
func draftCapabilities() []string {
	var out []string
	for _, n := range capabilityNames() {
		if n != "transport" {
			out = append(out, n)
		}
	}
	return out
}

// offersFor is every offer of a capability in this compile: tilegen's own,
// then the drafts given with the spec.
func (c *Ctx) offersFor(capability string) []*Offer {
	out := offersOf(capability)
	for _, o := range c.Extra {
		if o.Capability == capability {
			out = append(out[:len(out):len(out)], o)
		}
	}
	return out
}

// draftsChosen is the needs a draft won. tilegen can say so and why, and
// can go no further: a draft has no generator.
type draftsChosen struct {
	cp    *compiled
	needs []string // "orders.OrderStore: cockroachdb"
}

func (d *draftsChosen) Error() string {
	return "a draft tile was chosen, and a draft cannot generate code: " + strings.Join(d.needs, "; ") +
		". The decision stands; to get the project, build that tile into tilegen or untick what only it can do"
}
