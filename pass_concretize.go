package main

import (
	"fmt"
	"strconv"
)

// Concretize makes every config-dependent decision explicit in the tree,
// so that after this pass nothing downstream needs to read the config
// except the storage backend choice recorded on each impl.
//
//	(field PlacedAt time.Time)              => (field PlacedAt time.Time (tag "json:\"placed_at\""))
//	(method Get (params (id T)) ...)        => (method Get (params (ctx context.Context) (id T)) ...)
//	(impl OrderStore ...)                   => (impl OrderStore ... (backend postgres))
//	(package orders ...)                    => (package orders (dir "internal/orders") ...)
var Concretize = &Pass{
	Name: "concretize",
	Rules: []Rule{
		{Name: "package-layout", Pattern: Pat("(package ?name ?items...)"), Then: concretizePackage},
		{Name: "json-tag", Pattern: Pat("(field ?name ?type ?opts...)"), Then: concretizeField},
		{Name: "context-first", Pattern: Pat("(method ?name ?parts...)"), Then: concretizeMethod},
		{Name: "backend", Pattern: Pat("(impl ?iface ?parts...)"), Then: concretizeImpl},
		// implement's (field ...) forms are dependencies, not data: no json
		// tags, so this tile covers the whole form and stops.
		{Name: "implement", Pattern: Pat("(implement ?iface ?parts...)"), Then: func(m *Munch, b Bindings, n *Node) ([]*Node, error) {
			return []*Node{n}, nil
		}},
	},
}

func concretizePackage(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	name := b.Atom("name")
	dir := name
	if m.C.Cfg.Layout == "internal" {
		dir = "internal/" + name
	}
	items, err := m.Sub(b.Rest("items")...) // the leaves of this tile
	if err != nil {
		return nil, err
	}
	out := L(Sym("package"), Sym(name), L(Sym("dir"), Str(dir)))
	out.List = append(out.List, items...)
	return []*Node{out}, nil
}

func concretizeField(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	name := b.Atom("name")
	out := L(Sym("field"), Sym(name), b.One("type"))
	out.List = append(out.List, b.Rest("opts")...)
	if n.Find("tag") != nil || m.C.Cfg.JSONTags == "none" || !exported(name) {
		return []*Node{out}, nil // an explicit tag in the spec always wins
	}
	key := snake(name)
	if m.C.Cfg.JSONTags == "camel" {
		key = camel(name)
	}
	tag := "json:" + strconv.Quote(key)
	out.List = append(out.List, L(Sym("tag"), Str(tag)))
	return []*Node{out}, nil
}

func concretizeMethod(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	out := L(Sym("method"), b.One("name"))
	hasParams := false
	for _, part := range b.Rest("parts") {
		if part.Head() == "params" {
			hasParams = true
			part = withCtx(part, m.C.Cfg.ContextFirst)
		}
		out.List = append(out.List, part)
	}
	if !hasParams && m.C.Cfg.ContextFirst {
		out.List = append(out.List, withCtx(L(Sym("params")), true))
	}
	return []*Node{out}, nil
}

func withCtx(params *Node, on bool) *Node {
	args := params.Args()
	if !on || (len(args) > 0 && args[0].List[1].Atom == "context.Context") {
		return params
	}
	out := L(Sym("params"), L(Sym("ctx"), Sym("context.Context")))
	out.List = append(out.List, args...)
	return out
}

func concretizeImpl(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	if n.Find("backend") != nil {
		return nil, fmt.Errorf("impl already has a backend")
	}
	out := L(append([]*Node{Sym("impl"), b.One("iface")}, b.Rest("parts")...)...)
	out.List = append(out.List, L(Sym("backend"), Sym(m.C.Cfg.Storage)))
	return []*Node{out}, nil
}
