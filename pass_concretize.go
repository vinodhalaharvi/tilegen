package main

import (
	"fmt"
	"path"
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
		{Name: "package-layout", Pattern: Pat("(package ?name ?items...)"), Then: concretizePackage, Produces: "package", Doc: "places a package per (layout ...)"},
		{Name: "json-tag", Pattern: Pat("(field ?name ?type ?opts...)"), Then: concretizeField, Produces: "field", Doc: "adds json tags per (json-tags ...)"},
		{Name: "context-first", Pattern: Pat("(method ?name ?parts...)"), Then: concretizeMethod, Produces: "method", Doc: "prepends ctx context.Context per (context-first ...)"},
		{Name: "backend", Pattern: Pat("(impl ?iface ?parts...)"), Then: concretizeImpl, Produces: "impl", Doc: "records the storage backend chosen by (storage ...)"},
		// implement's (field ...) forms are dependencies, not data: no json
		// tags, so this tile covers the whole form and stops.
		{Name: "implement", Pattern: Pat("(implement ?iface ?parts...)"), Produces: "implement", Doc: "keeps implement dependencies free of json tags", Then: func(m *Munch, b Bindings, n *Node) ([]*Node, error) {
			return []*Node{n}, nil
		}},
	},
}

func concretizePackage(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	name := b.Atom("name")
	m.C.curPkg = name
	dir := name
	if m.C.Cfg.Layout == "internal" {
		dir = "internal/" + name
	}
	// A package may say where it goes. The layout is a house style for
	// packages that do not care; one that does, says so, and "." is how a
	// spec generates into the root of an existing module.
	var rest []*Node
	for _, it := range b.Rest("items") {
		if it.Head() == "dir" && len(it.List) == 2 {
			dir = path.Clean(it.List[1].Atom)
			continue
		}
		rest = append(rest, it)
	}
	items, err := m.Sub(rest...) // the leaves of this tile
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
	id := m.C.curPkg + "." + b.Atom("iface")
	cov := m.C.Cover[id]
	if cov == nil {
		return nil, fmt.Errorf("no tile was chosen for %s", id)
	}
	be, ok := cov.Offer.Impl.(*Backend)
	if !ok {
		return nil, fmt.Errorf("the tile chosen for %s does not implement stores", id)
	}
	out := L(append([]*Node{Sym("impl"), b.One("iface")}, b.Rest("parts")...)...)
	out.List = append(out.List, L(Sym("backend"), Sym(be.Name)))
	out.List = append(out.List, coverNodes(cov)...) // visible in -dump
	return []*Node{out}, nil
}
