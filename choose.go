package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Selection: for every store, the backends whose declared illegal-when
// needs it does not have are legal; with (storage auto) the cheapest legal
// one wins, by declared cost times the weights below. An explicit
// (storage NAME) still decides, but choosing an illegal backend is an
// error at the spec. Legality depends only on the spec, never on which
// tools happen to be installed, so every machine makes the same choice.

// knownNeeds are the needs a store can declare, like (durable).
var knownNeeds = []string{"durable"}

// defaultWeights turn a cost into a score. A policy file will set these.
var defaultWeights = map[string]int{"llm-work": 4, "maintenance": 3, "dependency": 1, "runtime": 1, "uncertainty": 5}

var weightOrder = []string{"llm-work", "maintenance", "dependency", "runtime", "uncertainty"}

// Choice is the backend chosen for one store, and why.
type Choice struct {
	Store   string // orders.OrderStore
	Needs   []string
	By      string // "auto" or "config"
	Chosen  *Backend
	Ranked  []Scored // legal backends, cheapest first
	Illegal []Rejected
	Pos     Pos
}

type Scored struct {
	B     *Backend
	Score int
	Terms string // llm 3·4 + maint 2·3 + ...
}

type Rejected struct {
	B      *Backend
	Reason string
}

func score(c Cost) (int, string) {
	total := 0
	var terms []string
	short := strings.Split(Cost(c).Short(), ", ")
	for i, t := range c {
		w := defaultWeights[t.Dim]
		total += t.Value * w
		terms = append(terms, fmt.Sprintf("%s·%d", short[i], w))
	}
	return total, strings.Join(terms, " + ")
}

// chooseAll picks a backend for every store in the project and records the
// set of backends in use.
func chooseAll(project *Node, c *Ctx) error {
	c.Choices = map[string]*Choice{}
	used := map[string]*Backend{}
	var errs []error
	for _, pkg := range project.FindAll("package") {
		for _, e := range pkg.FindAll("entity") {
			store := e.Find("store")
			if store == nil {
				continue
			}
			ch := &Choice{Store: pkg.List[1].Atom + "." + e.List[1].Atom + "Store", Pos: store.Pos, By: "auto"}
			for _, op := range store.Args() {
				if op.IsList && len(op.List) == 1 && contains(knownNeeds, op.Head()) {
					ch.Needs = append(ch.Needs, op.Head())
				}
			}
			for _, name := range backendNames() {
				b := backends[name]
				if reason := illegalFor(b, ch.Needs); reason != "" {
					ch.Illegal = append(ch.Illegal, Rejected{b, reason})
					continue
				}
				if len(b.Cost) == 0 {
					continue // no declared cost: explicit use only
				}
				s, terms := score(b.Cost)
				ch.Ranked = append(ch.Ranked, Scored{b, s, terms})
			}
			sort.SliceStable(ch.Ranked, func(i, j int) bool { return ch.Ranked[i].Score < ch.Ranked[j].Score })

			if want := c.Cfg.Storage; want != "auto" {
				ch.By = "config"
				b := lookupBackend(want)
				if reason := illegalFor(b, ch.Needs); reason != "" {
					errs = append(errs, fmt.Errorf("%s: (storage %s) is illegal for %s: %s; legal: %s, or use (storage auto)",
						store.Pos, want, ch.Store, reason, legalList(ch)))
					continue
				}
				ch.Chosen = b
			} else if len(ch.Ranked) > 0 {
				ch.Chosen = ch.Ranked[0].B
			} else {
				errs = append(errs, fmt.Errorf("%s: no storage backend is legal for %s (needs: %s)", store.Pos, ch.Store, strings.Join(ch.Needs, ", ")))
				continue
			}
			c.Choices[ch.Store] = ch
			used[ch.Chosen.Name] = ch.Chosen
		}
	}
	c.Used = nil
	for _, name := range sortedKeys(used) {
		c.Used = append(c.Used, used[name])
	}
	return errors.Join(errs...)
}

func illegalFor(b *Backend, needs []string) string {
	for _, n := range needs {
		if reason, ok := b.IllegalWhen[n]; ok {
			return reason
		}
	}
	return ""
}

func legalList(ch *Choice) string {
	var parts []string
	for _, s := range ch.Ranked {
		parts = append(parts, fmt.Sprintf("%s (score %d)", s.B.Name, s.Score))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

// checkReserved rejects project packages that a chosen backend needs for
// its own code (postgres-sqlc puts sqlc's output in package db).
func checkReserved(project *Node, c *Ctx) error {
	var errs []error
	for _, pkg := range project.FindAll("package") {
		name := pkg.List[1]
		for _, b := range c.Used {
			if dir, ok := b.Packages[name.Atom]; ok {
				errs = append(errs, fmt.Errorf("%s: package name %q is reserved: the %s backend puts its code in %s", name.Pos, name.Atom, b.Tile, dir))
			}
		}
	}
	return errors.Join(errs...)
}

// choiceNodes records a choice in the tree, so -dump shows it.
func choiceNodes(ch *Choice) []*Node {
	var out []*Node
	for _, s := range ch.Ranked {
		head := "considered"
		if s.B == ch.Chosen {
			head = "chosen"
		}
		out = append(out, L(Sym(head), Sym(s.B.Tile), L(Sym("score"), Sym(fmt.Sprint(s.Score))), L(Sym("by"), Sym(ch.By))))
	}
	if ch.By == "config" && !contains(tileNames(ch.Ranked), ch.Chosen.Tile) {
		out = append(out, L(Sym("chosen"), Sym(ch.Chosen.Tile), L(Sym("by"), Sym("config"))))
	}
	for _, r := range ch.Illegal {
		out = append(out, L(Sym("illegal"), Sym(r.B.Tile), Str(r.Reason)))
	}
	return out
}

func tileNames(s []Scored) []string {
	var out []string
	for _, x := range s {
		out = append(out, x.B.Tile)
	}
	return out
}

// explain prints one choice for a person.
func explain(ch *Choice) string {
	var b strings.Builder
	needs := "none"
	if len(ch.Needs) > 0 {
		needs = strings.Join(ch.Needs, ", ")
	}
	fmt.Fprintf(&b, "%s   needs: %s   chosen by: %s\n", ch.Store, needs, ch.By)
	for _, s := range ch.Ranked {
		mark := "        "
		if s.B == ch.Chosen {
			mark = "chosen  "
		}
		fmt.Fprintf(&b, "  %s%-15s score %-4d %s\n", mark, s.B.Tile, s.Score, s.Terms)
	}
	if ch.By == "config" && !contains(tileNames(ch.Ranked), ch.Chosen.Tile) {
		fmt.Fprintf(&b, "  chosen  %-15s (named by the config; no declared cost)\n", ch.Chosen.Tile)
	}
	for _, r := range ch.Illegal {
		fmt.Fprintf(&b, "  illegal %-15s %s\n", r.B.Tile, r.Reason)
	}
	return b.String()
}
