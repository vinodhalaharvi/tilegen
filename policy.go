package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// (policy
//   (weights (llm-work 4) (maintenance 3) (dependency 1) (runtime 1) (uncertainty 5))
//   (prefer postgres-sqlc)                       ; break near-ties in its favour
//   (avoid pgx "we standardised on sqlc"))       ; never choose it; explicit use is an error
//
// The policy is how a team states what it values, without editing any
// tile: tiles declare costs, the policy prices them. It lives beside the
// spec (any .sexp file in a spec folder, or -policy FILE) and changes only
// which tile wins, never what a tile generates.

// Policy prices tiles for one project or team.
type Policy struct {
	Weights map[string]int
	Prefer  map[string]bool   // tile names: win ties, and near-ties within Margin
	Avoid   map[string]string // tile name -> why; never chosen automatically
	Margin  int               // how far behind a preferred tile may be and still win
	Pos     Pos
}

// DefaultPolicy is the built-in pricing, used when no policy is given.
func DefaultPolicy() *Policy {
	w := map[string]int{}
	for d, v := range defaultWeights {
		w[d] = v
	}
	return &Policy{Weights: w, Prefer: map[string]bool{}, Avoid: map[string]string{}, Margin: 0}
}

// ParsePolicy reads a (policy ...) form.
func ParsePolicy(n *Node) (*Policy, error) {
	p := DefaultPolicy()
	p.Pos = n.Pos
	var errs []error
	bad := func(at *Node, f string, a ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", at.Pos, fmt.Sprintf(f, a...)))
	}
	seen := map[string]bool{}
	for _, it := range n.Args() {
		switch h := it.Head(); h {
		case "weights":
			if seen[h] {
				bad(it, "duplicate (weights ...)")
			}
			seen[h] = true
			for _, w := range it.Args() {
				if len(w.List) != 2 || w.List[1].IsList {
					bad(w, "expected (dimension N), got %s", short(w))
					continue
				}
				dim := w.Head()
				if _, ok := defaultWeights[dim]; !ok {
					bad(w, "unknown cost dimension %q%s (want %s)", dim, didYouMean(dim, weightOrder), strings.Join(weightOrder, ", "))
					continue
				}
				v, err := strconv.Atoi(w.List[1].Atom)
				if err != nil || v < 0 || v > 100 {
					bad(w, "weight for %s must be 0 to 100, got %s", dim, w.List[1].Atom)
					continue
				}
				p.Weights[dim] = v
			}
		case "prefer", "avoid":
			for _, t := range it.Args() {
				if t.IsList {
					bad(t, "expected a tile name, got %s", short(t))
					continue
				}
				if h == "prefer" {
					p.Prefer[t.Atom] = true
				} else {
					p.Avoid[t.Atom] = ""
				}
			}
			if h == "avoid" && len(it.Args()) == 2 && !it.Args()[1].IsList && it.Args()[1].Quoted {
				p.Avoid[it.Args()[0].Atom] = it.Args()[1].Atom // (avoid pgx "reason")
				delete(p.Avoid, it.Args()[1].Atom)
			}
		case "margin":
			if len(it.List) != 2 || it.List[1].IsList {
				bad(it, "expected (margin N), got %s", short(it))
				continue
			}
			v, err := strconv.Atoi(it.List[1].Atom)
			if err != nil || v < 0 || v > 1000 {
				bad(it, "margin must be 0 to 1000, got %s", it.List[1].Atom)
				continue
			}
			p.Margin = v
		default:
			bad(it, "policy takes (weights ...), (prefer ...), (avoid ...) and (margin N), got %s%s",
				short(it), didYouMean(h, []string{"weights", "prefer", "avoid", "margin"}))
		}
	}
	return p, errors.Join(errs...)
}

// LoadPolicy reads a file holding a single (policy ...) form.
func LoadPolicy(path string) (*Policy, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	forms, err := Parse(path, string(src))
	if err != nil {
		return nil, err
	}
	if len(forms) != 1 || forms[0].Head() != "policy" {
		return nil, fmt.Errorf("%s: expected a single (policy ...) form", path)
	}
	return ParsePolicy(forms[0])
}

// checkTileNames reports prefer or avoid entries that name no tile.
func (p *Policy) checkTileNames() error {
	known := map[string]bool{}
	var names []string
	for _, t := range Registry() {
		known[t.Name] = true
		names = append(names, t.Name)
	}
	sort.Strings(names)
	var errs []error
	for _, m := range []struct {
		what string
		set  []string
	}{{"prefer", sortedKeys(p.Prefer)}, {"avoid", sortedKeys(p.Avoid)}} {
		for _, n := range m.set {
			if !known[n] {
				errs = append(errs, fmt.Errorf("%s: (%s %s): no such tile%s", p.Pos, m.what, n, didYouMean(n, names)))
			}
		}
	}
	return errors.Join(errs...)
}

// score prices a cost under this policy.
func (p *Policy) score(c Cost) (int, string) {
	total := 0
	var terms []string
	short := strings.Split(c.Short(), ", ")
	for i, t := range c {
		w := p.Weights[t.Dim]
		total += t.Value * w
		terms = append(terms, fmt.Sprintf("%s·%d", short[i], w))
	}
	return total, strings.Join(terms, " + ")
}

// describe summarizes a policy for explain.
func (p *Policy) describe() string {
	var w []string
	for _, d := range weightOrder {
		w = append(w, fmt.Sprintf("%s %d", d, p.Weights[d]))
	}
	s := "weights: " + strings.Join(w, ", ")
	if len(p.Prefer) > 0 {
		s += fmt.Sprintf("\nprefer: %s (margin %d)", strings.Join(sortedKeys(p.Prefer), ", "), p.Margin)
	}
	if len(p.Avoid) > 0 {
		var parts []string
		for _, n := range sortedKeys(p.Avoid) {
			if why := p.Avoid[n]; why != "" {
				n += " (" + why + ")"
			}
			parts = append(parts, n)
		}
		s += "\navoid: " + strings.Join(parts, ", ")
	}
	return s
}
