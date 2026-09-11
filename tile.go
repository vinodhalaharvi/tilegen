package main

import (
	"errors"
	"fmt"
	"strings"
)

// Patterns are S-expressions with variables:
//
//	?x     matches exactly one node and binds it to x
//	?xs... matches the remaining nodes of a list (must be last)
//	_      matches anything, binds nothing
//	sym    matches the identical atom
//
// Example: (entity ?name ?items...) matches (entity Order (field ID int64)).

// Bindings maps pattern variables to the nodes they matched.
type Bindings map[string][]*Node

func (b Bindings) One(k string) *Node {
	if v := b[k]; len(v) > 0 {
		return v[0]
	}
	return nil
}

func (b Bindings) Atom(k string) string {
	if n := b.One(k); n != nil && !n.IsList {
		return n.Atom
	}
	return ""
}

func (b Bindings) Rest(k string) []*Node { return b[k] }

// Pat parses a pattern. It panics on malformed patterns because patterns
// are written by tilegen's authors, not its users.
func Pat(src string) *Node {
	ns, err := Parse("<pattern>", src)
	if err != nil || len(ns) != 1 {
		panic(fmt.Sprintf("bad pattern %q: %v", src, err))
	}
	return ns[0]
}

// Match reports whether n matches pat, recording variables in b.
func Match(pat, n *Node, b Bindings) bool {
	if !pat.IsList {
		switch {
		case pat.Atom == "_":
			return true
		case strings.HasPrefix(pat.Atom, "?"):
			b[pat.Atom[1:]] = []*Node{n}
			return true
		default:
			return !n.IsList && n.Atom == pat.Atom
		}
	}
	if !n.IsList {
		return false
	}
	for i, p := range pat.List {
		if !p.IsList && strings.HasPrefix(p.Atom, "?") && strings.HasSuffix(p.Atom, "...") {
			b[strings.TrimSuffix(p.Atom[1:], "...")] = n.List[i:]
			return true
		}
		if i >= len(n.List) || !Match(p, n.List[i], b) {
			return false
		}
	}
	return len(pat.List) == len(n.List)
}

// A Rule is a tile: a tree pattern plus the code that replaces the matched
// subtree. Then may return zero, one, or many nodes (they are spliced into
// the parent), and decides itself which leftover subtrees to munch further
// by calling m.Sub - exactly like a compiler tile whose leaves are covered
// by other tiles.
type Rule struct {
	Name    string
	Pattern *Node
	Then    func(m *Munch, b Bindings, n *Node) ([]*Node, error)

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
func (p *Pass) Run(c *Ctx, nodes []*Node) ([]*Node, error) {
	m := &Munch{pass: p, C: c}
	return m.Sub(nodes...)
}

// Sub munches each node and splices the results.
func (m *Munch) Sub(nodes ...*Node) ([]*Node, error) {
	var out []*Node
	for _, n := range nodes {
		res, err := m.one(n)
		if err != nil {
			return nil, err
		}
		out = append(out, res...)
	}
	return out, nil
}

func (m *Munch) one(n *Node) ([]*Node, error) {
	if !n.IsList {
		return []*Node{n}, nil
	}
	for _, r := range m.pass.Rules {
		b := Bindings{}
		if !Match(r.Pattern, n, b) {
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
			inheritPos(x, n.Pos)
		}
		return res, nil
	}
	// No tile here: keep the node (identity tile) and munch its children.
	kids := []*Node{n.List[0]}
	for _, k := range n.List[1:] {
		res, err := m.one(k)
		if err != nil {
			return nil, err
		}
		kids = append(kids, res...)
	}
	cp := *n
	cp.List = kids
	return []*Node{&cp}, nil
}

// TileError pins a failure to the source position of the form being tiled.
type TileError struct {
	Pos        Pos
	Pass, Rule string
	Err        error
}

func (e *TileError) Error() string {
	return fmt.Sprintf("%s: [%s/%s] %v", e.Pos, e.Pass, e.Rule, e.Err)
}
func (e *TileError) Unwrap() error { return e.Err }
