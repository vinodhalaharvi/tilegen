package compile

import (
	"fmt"
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

func NewPlanGraph() *PlanGraph { return &PlanGraph{Nodes: map[string]*PlanNode{}} }

func (g *PlanGraph) Add(n *PlanNode) *PlanNode {
	if prev, ok := g.Nodes[n.ID]; ok {
		prev.Deps = append(prev.Deps, n.Deps...)
		return prev
	}
	g.Nodes[n.ID] = n
	g.Order = append(g.Order, n.ID)
	return n
}

func (g *PlanGraph) Dep(from, to string) {
	if n, ok := g.Nodes[from]; ok && from != to {
		n.Deps = append(n.Deps, to)
	}
}

func FileNode(rel string) string  { return "file:" + rel }
func TaskNode(id string) string   { return "task:" + id }
func ToolNode(cmd string) string  { return "tool:" + cmd }
func tileNode(name string) string { return "tile:" + name }

// BuildPlanGraph projects a finished plan into a graph.
func BuildPlanGraph(r *Report, c *Ctx) *PlanGraph {
	g := NewPlanGraph()

	// The tiles that were chosen, and what each need delegated to.
	for _, id := range SortedKeys(c.Cover) {
		addCovering(g, c.Cover[id])
	}

	// Every file the plan makes or keeps, and the tile responsible.
	for _, rel := range r.planned {
		n := g.Add(&PlanNode{ID: FileNode(rel), Kind: "file", Name: rel})
		if by := r.producedBy[rel]; by != "" {
			n.By = by
			g.Add(&PlanNode{ID: tileNode(by), Kind: "tile", Name: by})
			g.Dep(n.ID, tileNode(by))
		}
	}

	// Tool steps: a backend's generate command needs the files tilegen
	// wrote for it, and the files the tool writes need the step.
	for _, st := range ToolSteps(c) {
		step := g.Add(&PlanNode{ID: ToolNode(st.Cmd), Kind: "tool", Name: st.Cmd, By: st.Tile,
			Note: "run after tilegen, before building"})
		for _, rel := range r.order {
			if st.Reads(rel) {
				g.Dep(step.ID, FileNode(rel))
			}
		}
		for _, dir := range st.Writes { // what the tool writes
			out := g.Add(&PlanNode{ID: "dir:" + dir, Kind: "file", Name: dir + "/", By: st.Tile,
				Note: "written by " + st.Cmd})
			g.Dep(out.ID, step.ID)
		}
	}

	// LLM tasks: each needs its own file and everything it was given as
	// context, which is exactly what the prompt carries.
	for _, t := range r.TaskList {
		n := g.Add(&PlanNode{ID: TaskNode(t.ID), Kind: "task", Name: t.ID, Note: t.Intent})
		if t.File != "" {
			g.Dep(n.ID, FileNode(t.File))
		}
		for _, f := range t.ContextFiles {
			if _, ok := g.Nodes[FileNode(f)]; ok {
				g.Dep(n.ID, FileNode(f))
				continue
			}
			for _, be := range usedBackends(c) { // e.g. internal/db/models.go
				for _, dir := range be.Packages {
					if strings.HasPrefix(f, dir+"/") {
						g.Dep(n.ID, "dir:"+dir)
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
	n := g.Add(&PlanNode{ID: "need:" + cov.Need.ID, Kind: "need", Name: cov.Need.ID,
		By: cov.Offer.Tile, Note: fmt.Sprintf("%s, chosen by %s (score %d)", cov.Need.Capability, cov.By, cov.Score)})
	g.Add(&PlanNode{ID: tileNode(cov.Offer.Tile), Kind: "tile", Name: cov.Offer.Tile})
	g.Dep(n.ID, tileNode(cov.Offer.Tile))
	for _, kid := range cov.Children {
		addCovering(g, kid)
		g.Dep(n.ID, "need:"+kid.Need.ID)
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
