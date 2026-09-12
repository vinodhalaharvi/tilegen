package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

// The plan graph: what depends on what, and how the compiler arrived
// there. Every edge is inferred from the plan tilegen already builds, so
// there is nothing extra for a tile to declare and nothing to get wrong:
//
//   - a generated file depends on the tile that staged it;
//   - a tool step (sqlc generate) depends on its inputs, and whatever the
//     tool writes depends on the step;
//   - an LLM task depends on its file and on its context files;
//   - a covering's children are edges the solver already computed.
//
// It is a projection, not a second source of truth. Nothing here decides
// anything; `tilegen plan` prints it, and later work (parallel fills,
// invalidation, caching) can build on it.

// PlanNode is one thing the plan makes or does.
type PlanNode struct {
	ID   string   // "file:orders/orders_gen.go", "tool:sqlc generate", "task:orders.Get"
	Kind string   // file | tool | task | tile
	Name string   // the part a person reads
	By   string   // the tile or command responsible
	Deps []string // node IDs this one needs first
	Note string   // why, for explain
}

// PlanGraph is the whole projection.
type PlanGraph struct {
	Nodes map[string]*PlanNode
	Order []string // insertion order, for stable output
}

func newPlanGraph() *PlanGraph { return &PlanGraph{Nodes: map[string]*PlanNode{}} }

func (g *PlanGraph) add(n *PlanNode) *PlanNode {
	if prev, ok := g.Nodes[n.ID]; ok {
		prev.Deps = append(prev.Deps, n.Deps...)
		return prev
	}
	g.Nodes[n.ID] = n
	g.Order = append(g.Order, n.ID)
	return n
}

func (g *PlanGraph) dep(from, to string) {
	if n, ok := g.Nodes[from]; ok && from != to {
		n.Deps = append(n.Deps, to)
	}
}

func fileNode(rel string) string  { return "file:" + rel }
func taskNode(id string) string   { return "task:" + id }
func toolNode(cmd string) string  { return "tool:" + cmd }
func tileNode(name string) string { return "tile:" + name }

// buildPlanGraph projects a finished plan into a graph.
func buildPlanGraph(r *Report, c *Ctx) *PlanGraph {
	g := newPlanGraph()

	// The tiles that were chosen, and what each need delegated to.
	for _, id := range sortedKeys(c.Cover) {
		addCovering(g, c.Cover[id])
	}

	// Every file the plan makes or keeps, and the tile responsible.
	for _, rel := range r.planned {
		n := g.add(&PlanNode{ID: fileNode(rel), Kind: "file", Name: rel})
		if by := r.producedBy[rel]; by != "" {
			n.By = by
			g.add(&PlanNode{ID: tileNode(by), Kind: "tile", Name: by})
			g.dep(n.ID, tileNode(by))
		}
	}

	// Tool steps: a backend's generate command needs the files tilegen
	// wrote for it, and the files the tool writes need the step.
	for _, be := range usedBackends(c) {
		if be.Generate == "" {
			continue
		}
		step := g.add(&PlanNode{ID: toolNode(be.Generate), Kind: "tool", Name: be.Generate, By: be.Tile,
			Note: "run after tilegen, before building"})
		for _, rel := range r.order {
			if strings.HasPrefix(rel, "db/") || rel == "sqlc.yaml" {
				g.dep(step.ID, fileNode(rel))
			}
		}
		for _, dir := range be.Packages { // what the tool writes
			out := g.add(&PlanNode{ID: "dir:" + dir, Kind: "file", Name: dir + "/", By: be.Tile,
				Note: "written by " + be.Generate})
			g.dep(out.ID, step.ID)
		}
	}

	// LLM tasks: each needs its own file and everything it was given as
	// context, which is exactly what the prompt carries.
	for _, t := range r.TaskList {
		n := g.add(&PlanNode{ID: taskNode(t.ID), Kind: "task", Name: t.ID, Note: t.Intent})
		if t.File != "" {
			g.dep(n.ID, fileNode(t.File))
		}
		for _, f := range t.ContextFiles {
			if _, ok := g.Nodes[fileNode(f)]; ok {
				g.dep(n.ID, fileNode(f))
				continue
			}
			for _, be := range usedBackends(c) { // e.g. internal/db/models.go
				for _, dir := range be.Packages {
					if strings.HasPrefix(f, dir+"/") {
						g.dep(n.ID, "dir:"+dir)
					}
				}
			}
		}
	}
	for _, n := range g.Nodes {
		n.Deps = dedupe(n.Deps)
	}
	return g
}

func addCovering(g *PlanGraph, cov *Covering) {
	n := g.add(&PlanNode{ID: "need:" + cov.Need.ID, Kind: "need", Name: cov.Need.ID,
		By: cov.Offer.Tile, Note: fmt.Sprintf("%s, chosen by %s (score %d)", cov.Need.Capability, cov.By, cov.Score)})
	g.add(&PlanNode{ID: tileNode(cov.Offer.Tile), Kind: "tile", Name: cov.Offer.Tile})
	g.dep(n.ID, tileNode(cov.Offer.Tile))
	for _, kid := range cov.Children {
		addCovering(g, kid)
		g.dep(n.ID, "need:"+kid.Need.ID)
	}
}

func dedupe(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

// Levels returns the nodes in dependency order, grouped so that everything
// in one level depends only on earlier levels: Kahn's algorithm. A cycle
// is reported with the nodes involved rather than silently dropped.
func (g *PlanGraph) Levels() ([][]string, error) {
	indeg := map[string]int{}
	out := map[string][]string{}
	for _, id := range g.Order {
		indeg[id] += 0
		for _, d := range g.Nodes[id].Deps {
			if _, ok := g.Nodes[d]; !ok {
				continue // a dependency outside the plan, e.g. a file on disk
			}
			indeg[id]++
			out[d] = append(out[d], id)
		}
	}
	var levels [][]string
	done := 0
	ready := []string{}
	for _, id := range g.Order {
		if indeg[id] == 0 {
			ready = append(ready, id)
		}
	}
	for len(ready) > 0 {
		sort.Strings(ready)
		levels = append(levels, ready)
		done += len(ready)
		var next []string
		for _, id := range ready {
			for _, to := range out[id] {
				indeg[to]--
				if indeg[to] == 0 {
					next = append(next, to)
				}
			}
		}
		ready = next
	}
	if done != len(g.Order) {
		var stuck []string
		for _, id := range g.Order {
			if indeg[id] > 0 {
				stuck = append(stuck, id)
			}
		}
		sort.Strings(stuck)
		return levels, fmt.Errorf("the plan has a dependency cycle among %d node(s): %s", len(stuck), strings.Join(stuck, ", "))
	}
	return levels, nil
}

// Dot renders the graph for graphviz.
func (g *PlanGraph) Dot() string {
	shape := map[string]string{"file": "box", "tool": "component", "task": "note", "need": "ellipse", "tile": "diamond"}
	var b strings.Builder
	b.WriteString("digraph tilegen {\n  rankdir=LR;\n  node [fontname=\"sans-serif\" fontsize=10];\n")
	for _, id := range g.Order {
		n := g.Nodes[id]
		fmt.Fprintf(&b, "  %s [label=%s shape=%s];\n", strconv.Quote(id), strconv.Quote(n.Name), shape[n.Kind])
	}
	for _, id := range g.Order {
		for _, d := range g.Nodes[id].Deps {
			if _, ok := g.Nodes[d]; ok {
				fmt.Fprintf(&b, "  %s -> %s;\n", strconv.Quote(d), strconv.Quote(id))
			}
		}
	}
	b.WriteString("}\n")
	return b.String()
}

// tilegen plan [-dot] [SPEC] prints the plan graph.
func planCmd(args []string, stdout, log io.Writer) error {
	fs := flag.NewFlagSet("tilegen plan", flag.ExitOnError)
	var o Options
	fs.StringVar(&o.Config, "config", "", "config .sexp file (default: a (config ...) form in the spec)")
	fs.StringVar(&o.Out, "out", "", "the generated project (default: the workspace's (out ...), else ./out)")
	fs.StringVar(&o.PolicyFile, "policy", "", "policy .sexp file")
	dot := fs.Bool("dot", false, "print graphviz DOT instead of levels")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: tilegen plan [-dot] [SPEC]\n\nWhat the plan makes, in dependency order. Writes nothing.\n\n")
		fs.PrintDefaults()
	}
	pos := parseAnywhere(fs, args)
	o.Spec = "spec"
	if len(pos) > 0 {
		o.Spec = pos[0]
	}
	cp, err := compile(o, log)
	if err != nil {
		return err
	}
	r, err := Emit(cp.nodes, cp.o.Out, cp.c, false) // plan only: nothing is written
	if err != nil {
		return err
	}
	g := buildPlanGraph(r, cp.c)
	if *dot {
		fmt.Fprint(stdout, g.Dot())
		return nil
	}
	levels, err := g.Levels()
	for i, level := range levels {
		fmt.Fprintf(stdout, "level %d\n", i)
		for _, id := range level {
			n := g.Nodes[id]
			line := fmt.Sprintf("  %-6s %s", n.Kind, n.Name)
			switch {
			case n.By != "" && n.Note != "":
				line += fmt.Sprintf("   (%s: %s)", n.By, n.Note)
			case n.By != "":
				line += fmt.Sprintf("   (%s)", n.By)
			case n.Note != "":
				line += "   " + shortNote(n.Note)
			}
			fmt.Fprintln(stdout, line)
		}
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "\n%d node(s) in %d level(s); everything in a level is independent of the rest of it.\n", len(g.Order), len(levels))
	return nil
}

func shortNote(s string) string {
	if len(s) <= 70 {
		return s
	}
	if i := strings.IndexByte(s[40:], '.'); i > 0 && 40+i < 70 {
		return s[:40+i+1]
	}
	return s[:67] + "..."
}
