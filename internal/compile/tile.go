package compile

import (
	"errors"
	"fmt"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

// A Rule is a tile: a tree pattern plus the code that replaces the matched
// subtree. Then may return zero, one, or many nodes (they are spliced into
// the parent), and decides itself which leftover subtrees to munch further
// by calling m.Sub - exactly like a compiler tile whose leaves are covered
// by other tiles.
type Rule struct {
	Name    string
	Pattern *sexp.Node
	Then    func(m *Munch, b sexp.Bindings, n *sexp.Node) ([]*sexp.Node, error)

	// Registry metadata (see registry.go), shown by `tilegen tiles`.
	Produces string // the capability it yields: struct, store, go/file, llm/task, ...
	Doc      string
	Cost     Cost
}

// A Pass is an ordered list of tiles. Order matters: put the biggest,
// most specific patterns first (maximal munch).
type Pass struct {
	Name  string
	Rules []Rule
}

// Munch carries shared state through one run of a pass.
type Munch struct {
	pass *Pass
	C    *Ctx
}

// Run applies the pass to a sequence of top-level forms.
func (p *Pass) Run(c *Ctx, nodes []*sexp.Node) ([]*sexp.Node, error) {
	m := &Munch{pass: p, C: c}
	return m.Sub(nodes...)
}

// Sub munches each node and splices the results.
func (m *Munch) Sub(nodes ...*sexp.Node) ([]*sexp.Node, error) {
	var out []*sexp.Node
	for _, n := range nodes {
		res, err := m.one(n)
		if err != nil {
			return nil, err
		}
		out = append(out, res...)
	}
	return out, nil
}

func (m *Munch) one(n *sexp.Node) ([]*sexp.Node, error) {
	if !n.IsList {
		return []*sexp.Node{n}, nil
	}
	for _, r := range m.pass.Rules {
		b := sexp.Bindings{}
		if !sexp.Match(r.Pattern, n, b) {
			continue
		}
		res, err := r.Then(m, b, n)
		if err != nil {
			var te *TileError
			if errors.As(err, &te) {
				return nil, err // already positioned by an inner tile
			}
			return nil, &TileError{Pos: n.Pos, Pass: m.pass.Name, Rule: r.Name, Err: err}
		}
		for _, x := range res {
			sexp.InheritPos(x, n.Pos)
		}
		return res, nil
	}
	// No tile here: keep the node (identity tile) and munch its children.
	kids := []*sexp.Node{n.List[0]}
	for _, k := range n.List[1:] {
		res, err := m.one(k)
		if err != nil {
			return nil, err
		}
		kids = append(kids, res...)
	}
	cp := *n
	cp.List = kids
	return []*sexp.Node{&cp}, nil
}

// TileError pins a failure to the source position of the form being tiled.
type TileError struct {
	Pos        sexp.Pos
	Pass, Rule string
	Err        error
}

func (e *TileError) Error() string {
	return fmt.Sprintf("%s: [%s/%s] %v", e.Pos, e.Pass, e.Rule, e.Err)
}
func (e *TileError) Unwrap() error { return e.Err }
