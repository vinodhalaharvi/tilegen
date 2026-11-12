package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/vinodhalaharvi/dyna"
)

// The whole-project view. Selection covers one need at a time, so a cost
// that a project pays once is charged again for every need: five stores on
// postgres are priced as five servers. This view prices a project as a
// whole instead:
//
//   - operations is paid once per engine, at the highest any tile on that
//     engine declares: a second store on the same postgres runs no second
//     server;
//   - dependency is paid once per tile: a library is added once;
//   - each engine beyond the first within one capability costs the policy's
//     (extra-engine N): a second database is one more thing to back up,
//     migrate and understand.
//
// It is a dynamic program over the needs in name order. The state is the
// next need and what the project already runs; an alternative is one legal
// tile for that need, priced at what it adds. A need the config, the lock
// or a preference decided keeps its tile.
//
// Today this only reports: `tilegen explain -project`. Nothing it finds
// changes what tilegen generates.

// engines names the engine behind a tile where it is not the tile itself:
// two tiles on one engine share its server. "" means there is nothing to
// run or keep. A tile not listed is its own engine.
var engines = map[string]string{
	"postgres-sqlc":  "postgres",
	"postgres-pgx":   "postgres",
	"nats-core":      "nats",
	"nats-jetstream": "nats",
	"sqlite-sqlc":    "sqlite",
	"memory":         "",
	"local-bus":      "",
}

func engineOf(o *Offer) string {
	if e, ok := engines[o.Tile]; ok {
		return e
	}
	return o.Tile
}

// weighted is what one cost dimension of an offer comes to under a policy.
func weighted(o *Offer, p *Policy, dim string) int {
	total := 0
	for _, t := range o.Cost {
		if t.Dim == dim {
			total += t.Value * p.Weights[dim]
		}
	}
	return total
}

// running is what a project already runs and has already paid for.
type running struct {
	ops   map[string]int  // engine -> operations paid so far
	caps  map[string]bool // "capability/engine" in use
	tiles map[string]bool // tiles whose dependency is paid
}

func (r running) key() string {
	var parts []string
	for e, v := range r.ops {
		parts = append(parts, fmt.Sprintf("%s=%d", e, v))
	}
	for c := range r.caps {
		parts = append(parts, c)
	}
	for t := range r.tiles {
		parts = append(parts, "tile:"+t)
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// add prices a candidate for a project that already runs r, and returns
// what the project runs afterwards.
func (r running) add(cand Candidate, p *Policy) (int, running) {
	o := cand.Offer
	next := running{ops: map[string]int{}, caps: map[string]bool{}, tiles: map[string]bool{}}
	for k, v := range r.ops {
		next.ops[k] = v
	}
	for k := range r.caps {
		next.caps[k] = true
	}
	for k := range r.tiles {
		next.tiles[k] = true
	}
	cost := cand.Score
	if r.tiles[o.Tile] {
		cost -= weighted(o, p, "dependency")
	}
	next.tiles[o.Tile] = true
	engine := engineOf(o)
	if engine == "" {
		return cost, next
	}
	ops := weighted(o, p, "operations")
	paid, runs := r.ops[engine]
	if runs {
		cost -= min(ops, paid)
	}
	next.ops[engine] = max(ops, paid)
	mine := o.Capability + "/" + engine
	if !r.caps[mine] {
		for c := range r.caps {
			if strings.HasPrefix(c, o.Capability+"/") {
				cost += p.ExtraEngine
				break
			}
		}
	}
	next.caps[mine] = true
	return cost, next
}

// projectPick is one need's tile in a project-wide choice.
type projectPick struct {
	Need string
	Tile string
	Cost int // what it adds to the project
}

// ProjectView compares the choice made need by need with the cheapest
// choice for the project as a whole, both priced the whole-project way.
type ProjectView struct {
	ExtraEngine int
	Today, Best []projectPick
	TodayTotal  int
	BestTotal   int
	States      int
}

// projectState is the DP's state: the next need, and what already runs.
type projectState struct {
	i   int
	run string
}

type projectStep struct {
	need string
	tile string
	cost int
	done bool
}

// free reports whether cost alone chose this need's tile, so the
// whole-project view may choose again.
func free(cov *Covering) bool { return cov.By == "auto" && cov.Why == "" }

func projectView(c *Ctx) *ProjectView {
	names := sortedKeys(c.Cover)
	v := &ProjectView{ExtraEngine: c.Policy.ExtraEngine}

	// Store by store: the tiles already chosen, priced as one project.
	r := running{}
	for _, n := range names {
		cov := c.Cover[n]
		cost, next := r.add(Candidate{Offer: cov.Offer, Score: cov.Score}, c.Policy)
		v.Today = append(v.Today, projectPick{n, cov.Offer.Tile, cost})
		v.TodayTotal += cost
		r = next
	}

	// The whole project: every free need may take any of its legal tiles.
	runs := map[string]running{"": {}}
	coalg := func(s projectState) []dyna.Node[projectStep, projectState] {
		if s.i == len(names) {
			return []dyna.Node[projectStep, projectState]{{Label: projectStep{done: true}}}
		}
		cov := c.Cover[names[s.i]]
		cands := cov.Ranked
		if !free(cov) {
			cands = []Candidate{{Offer: cov.Offer, Score: cov.Score}}
		}
		var out []dyna.Node[projectStep, projectState]
		for _, cand := range cands {
			cost, next := runs[s.run].add(cand, c.Policy)
			key := next.key()
			runs[key] = next
			out = append(out, dyna.Node[projectStep, projectState]{
				Label: projectStep{need: names[s.i], tile: cand.Offer.Tile, cost: cost},
				Kids:  []projectState{{s.i + 1, key}},
			})
		}
		return out
	}
	// Cheapest total, with the choices that earned it. Candidates arrive
	// cheapest first and then by name, and a tie keeps the earlier one, so
	// the answer does not depend on map order.
	cheapest := dyna.Ungraded(dyna.TopK(1, func(l projectStep, kids []float64, _ float64, _ []float64) float64 {
		if l.done {
			return 0
		}
		return kids[0] - float64(l.cost)
	}))
	best, stats := dyna.Dyna(cheapest, coalg, projectState{})
	v.States = stats.States
	for e := best[0]; !e.Label.done; e = e.Kids[0] {
		v.Best = append(v.Best, projectPick{e.Label.need, e.Label.tile, e.Label.cost})
		v.BestTotal += e.Label.cost
	}
	if v.BestTotal == v.TodayTotal {
		v.Best = v.Today // nothing to gain: keep the choice already made
	}
	return v
}

func (v *ProjectView) same() bool {
	for i := range v.Today {
		if v.Today[i].Tile != v.Best[i].Tile {
			return false
		}
	}
	return true
}

func enginesIn(picks []projectPick) string {
	seen := map[string]bool{}
	for _, p := range picks {
		e, listed := engines[p.Tile]
		if !listed {
			e = p.Tile
		}
		if e != "" {
			seen[e] = true
		}
	}
	if len(seen) == 0 {
		return "none"
	}
	return strings.Join(sortedKeys(seen), ", ")
}

func (v *ProjectView) print(w io.Writer) {
	fmt.Fprintf(w, "whole project   a server and a library are paid once; each extra engine in a capability costs %d\n\n", v.ExtraEngine)
	if len(v.Today) == 0 {
		fmt.Fprintln(w, "  nothing to choose in this spec")
		return
	}
	width := len("engines")
	for _, p := range v.Today {
		width = max(width, len(p.Need))
	}
	row := func(a, b, c, mark string) {
		fmt.Fprintln(w, strings.TrimRight(fmt.Sprintf("  %-*s   %-22s %-22s %s", width, a, b, c, mark), " "))
	}
	row("", "need by need", "whole project", "")
	for i, p := range v.Today {
		b := v.Best[i]
		mark := ""
		if p.Tile != b.Tile {
			mark = "changes"
		}
		row(p.Need, fmt.Sprintf("%s %d", p.Tile, p.Cost), fmt.Sprintf("%s %d", b.Tile, b.Cost), mark)
	}
	row("total", fmt.Sprint(v.TodayTotal), fmt.Sprint(v.BestTotal), "")
	row("engines", enginesIn(v.Today), enginesIn(v.Best), "")
	fmt.Fprintln(w)
	if v.same() {
		fmt.Fprintln(w, "  The choice made need by need is also the cheapest for the project as a whole.")
		return
	}
	fmt.Fprintf(w, "  Choosing for the project as a whole saves %d. This is a report: tilegen still generates the need-by-need choice.\n", v.TodayTotal-v.BestTotal)
}
