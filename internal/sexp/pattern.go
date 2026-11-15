package sexp

import (
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
