package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Turning a spec into needs, and coverings back into what the passes and
// reports use. All the deciding happens in solve.go; this file builds
// needs, applies the lock and the config, and renders what was chosen.

// defaultWeights turn a cost into a score; a policy replaces them.
var defaultWeights = map[string]int{"llm-work": 4, "maintenance": 3, "dependency": 1, "runtime": 1, "operations": 3, "uncertainty": 5}

var weightOrder = []string{"llm-work", "maintenance", "dependency", "runtime", "operations", "uncertainty"}

func score(c Cost) (int, string) { return DefaultPolicy().score(c) }

// coverAll builds a need for every node that has one, and covers it.
func coverAll(project *Node, c *Ctx) error {
	ensureClassified()
	c.Cover = map[string]*Covering{}
	c.Used = nil
	enums := map[string]bool{}
	for _, pkg := range project.FindAll("package") {
		for _, e := range pkg.FindAll("enum") {
			enums[pkg.List[1].Atom+"."+e.List[1].Atom] = true
		}
	}
	shapes := map[string]EntityShape{}
	var needs []*Need
	for _, pkg := range project.FindAll("package") {
		name := pkg.List[1].Atom
		for _, n := range needsOf(pkg, name) {
			needs = append(needs, n)
			if e, ok := n.Data.(*Node); ok && e != nil {
				shapes[n.ID] = entityShape(e, name, enums)
			}
		}
	}
	shape := func(n *Need) EntityShape { return shapes[n.ID] }

	var errs []error
	used := map[string]*Offer{}
	for _, n := range needs {
		cov, err := solve(n, c, shape)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := applyChoice(n, cov, c); err != nil {
			errs = append(errs, err)
			continue
		}
		c.Cover[n.ID] = cov
		for _, o := range coveringOffers(cov) {
			// Keyed by capability too: one tile may offer several, with
			// a different Impl for each, and kafka as a transport is not
			// the same registration as kafka as an event bus.
			used[o.Capability+"/"+o.Tile] = o
		}
	}
	for _, key := range sortedKeys(used) {
		c.Used = append(c.Used, used[key])
	}
	return errors.Join(errs...)
}

// applyChoice lets the config and the lock override what cost chose.
func applyChoice(n *Need, cov *Covering, c *Ctx) error {
	if want := c.Cfg.tileFor(n.Capability); want != "" {
		for _, r := range cov.Illegal {
			if matchesTile(want, r.Tile) {
				return fmt.Errorf("%s: the config chose %s, which is illegal for %s: %s; legal: %s, or use auto",
					n.Pos, want, n.ID, r.Reason, legalTiles(cov))
			}
		}
		for _, cand := range cov.Ranked {
			if matchesTile(want, cand.Offer.Tile) {
				adopt(cov, cand, "config")
				return nil
			}
		}
		return fmt.Errorf("%s: the config chose %s, which offers no %s; legal: %s", n.Pos, want, n.Capability, legalTiles(cov))
	}
	if e, ok := c.Lock[n.ID]; ok && !c.Reselect {
		for _, r := range cov.Illegal {
			if r.Tile == e.Tile {
				c.warn(e.Pos, "%s: %s is now illegal for %s (%s); re-selected", lockFile, e.Tile, n.ID, r.Reason)
				return nil
			}
		}
		for _, cand := range cov.Ranked {
			if cand.Offer.Tile == e.Tile {
				adopt(cov, cand, "lock")
				return nil
			}
		}
		c.warn(e.Pos, "%s: tile %q offers no %s any more; re-selected%s", lockFile, e.Tile, n.Capability, didYouMean(e.Tile, tileNamesOf(cov)))
	}
	return nil
}

// matchesTile accepts a tile's registry name or a shorter alias the config
// may use (storage postgres -> postgres-sqlc).
func matchesTile(want, tile string) bool {
	return want == tile || aliasOf(tile) == want
}

func adopt(cov *Covering, cand Candidate, by string) {
	cov.Offer, cov.Score, cov.Own, cov.Terms, cov.Via, cov.By = cand.Offer, cand.Score, cand.Own, cand.Terms, cand.Via, by
	cov.Why = ""
}

func coveringOffers(cov *Covering) []*Offer {
	out := []*Offer{cov.Offer}
	for _, k := range cov.Children {
		out = append(out, coveringOffers(k)...)
	}
	return out
}

func legalTiles(cov *Covering) string {
	var parts []string
	for _, cand := range cov.Ranked {
		parts = append(parts, fmt.Sprintf("%s (score %d)", cand.Offer.Tile, cand.Score))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func tileNamesOf(cov *Covering) []string {
	var out []string
	for _, cand := range cov.Ranked {
		out = append(out, cand.Offer.Tile)
	}
	for _, r := range cov.Illegal {
		out = append(out, r.Tile)
	}
	sort.Strings(out)
	return out
}

// checkReserved rejects project packages a chosen tile needs for itself.
func checkReserved(project *Node, c *Ctx) error {
	var errs []error
	for _, pkg := range project.FindAll("package") {
		name := pkg.List[1]
		for _, o := range c.Used {
			b, ok := o.Impl.(*Backend)
			if !ok {
				continue
			}
			if dir, taken := b.Packages[name.Atom]; taken {
				errs = append(errs, fmt.Errorf("%s: package name %q is reserved: the %s tile puts its code in %s", name.Pos, name.Atom, o.Tile, dir))
			}
		}
	}
	return errors.Join(errs...)
}

// coverNodes records a covering in the tree, so -dump shows it.
func coverNodes(cov *Covering) []*Node {
	var out []*Node
	for _, cand := range cov.Ranked {
		head := "considered"
		if cand.Offer == cov.Offer {
			head = "chosen"
		}
		n := L(Sym(head), Sym(cand.Offer.Tile), L(Sym("score"), Sym(fmt.Sprint(cand.Score))))
		if len(cand.Via) > 0 {
			n.List = append(n.List, L(Sym("own"), Sym(fmt.Sprint(cand.Own))))
			for _, st := range cand.Via {
				n.List = append(n.List, L(Sym("via"), Sym(st.Chain.Name), Sym(fmt.Sprint(st.Score)), Str(st.Detail)))
			}
		}
		if head == "chosen" {
			n.List = append(n.List, L(Sym("by"), Sym(cov.By)))
		}
		out = append(out, n)
	}
	for _, r := range cov.Illegal {
		out = append(out, L(Sym("illegal"), Sym(r.Tile), Str(r.Reason)))
	}
	return out
}

// explainCovering prints one covering for a person.
func explainCovering(cov *Covering) string {
	var b strings.Builder
	reqs := "none"
	if len(cov.Need.Requirements) > 0 {
		reqs = strings.Join(cov.Need.Requirements, ", ")
	}
	fmt.Fprintf(&b, "%s   (%s)   needs: %s   chosen by: %s\n", cov.Need.ID, cov.Need.Capability, reqs, cov.By)
	for _, cand := range cov.Ranked {
		mark := "        "
		if cand.Offer == cov.Offer {
			mark = "chosen  "
		}
		fmt.Fprintf(&b, "  %s%-15s score %-4d %s", mark, cand.Offer.Tile, cand.Score, cand.Terms)
		if len(cand.Via) > 0 {
			fmt.Fprintf(&b, " = %d", cand.Own)
			for _, st := range cand.Via {
				fmt.Fprintf(&b, "\n  %-24s          + %s %d (%s)", "", st.Chain.Name, st.Score, st.Detail)
			}
		}
		b.WriteString("\n")
	}
	for _, r := range cov.Illegal {
		fmt.Fprintf(&b, "  illegal %-15s %s\n", r.Tile, r.Reason)
	}
	for _, cand := range cov.Ranked {
		for _, term := range cand.Offer.Cost {
			if src := term.provenance(); src != "" {
				fmt.Fprintf(&b, "  cost    %s %s: %d (%s)\n", cand.Offer.Tile, term.Dim, term.Value, src)
			}
		}
	}
	if cov.Why != "" {
		fmt.Fprintf(&b, "  policy: %s\n", cov.Why)
	}
	if cov.By == "lock" && len(cov.Ranked) > 0 && cov.Ranked[0].Offer != cov.Offer {
		fmt.Fprintf(&b, "  note: pinned by %s; auto would now pick %s (score %d). Run with -reselect to switch.\n",
			lockFile, cov.Ranked[0].Offer.Tile, cov.Ranked[0].Score)
	}
	for _, k := range cov.Children {
		b.WriteString(indent(explainCovering(k), "    "))
	}
	return b.String()
}
