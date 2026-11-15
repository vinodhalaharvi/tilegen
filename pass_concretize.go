package main

import (
	"fmt"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
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
		{Name: "package-layout", Pattern: sexp.Pat("(package ?name ?items...)"), Then: concretizePackage, Produces: "package", Doc: "places a package per (layout ...)"},
		{Name: "json-tag", Pattern: sexp.Pat("(field ?name ?type ?opts...)"), Then: concretizeField, Produces: "field", Doc: "adds json tags per (json-tags ...)"},
		{Name: "context-first", Pattern: sexp.Pat("(method ?name ?parts...)"), Then: concretizeMethod, Produces: "method", Doc: "prepends ctx context.Context per (context-first ...)"},
		{Name: "backend", Pattern: sexp.Pat("(impl ?iface ?parts...)"), Then: concretizeImpl, Produces: "impl", Doc: "records the storage backend chosen by (storage ...)"},
		// implement's (field ...) forms are dependencies, not data: no json
		// tags, so this tile covers the whole form and stops.
		{Name: "implement", Pattern: sexp.Pat("(implement ?iface ?parts...)"), Produces: "implement", Doc: "keeps implement dependencies free of json tags", Then: func(m *Munch, b sexp.Bindings, n *sexp.Node) ([]*sexp.Node, error) {
			return []*sexp.Node{n}, nil
		}},
	},
}

func concretizePackage(m *Munch, b sexp.Bindings, n *sexp.Node) ([]*sexp.Node, error) {
	name := b.Atom("name")
	m.C.curPkg = name
	dir := name
	if m.C.Cfg.Layout == "internal" {
		dir = "internal/" + name
	}
	// A package may say where it goes. The layout is a house style for
	// packages that do not care; one that does, says so, and "." is how a
	// spec generates into the root of an existing module.
	var rest []*sexp.Node
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
	out := sexp.L(sexp.Sym("package"), sexp.Sym(name), sexp.L(sexp.Sym("dir"), sexp.Str(dir)))
	out.List = append(out.List, items...)
	return []*sexp.Node{out}, nil
}

func concretizeField(m *Munch, b sexp.Bindings, n *sexp.Node) ([]*sexp.Node, error) {
	name := b.Atom("name")
	out := sexp.L(sexp.Sym("field"), sexp.Sym(name), b.One("type"))
	out.List = append(out.List, b.Rest("opts")...)
	if n.Find("tag") != nil || m.C.Cfg.JSONTags == "none" || !exported(name) {
		return []*sexp.Node{out}, nil // an explicit tag in the spec always wins
	}
	key := snake(name)
	if m.C.Cfg.JSONTags == "camel" {
		key = camel(name)
	}
	tag := "json:" + strconv.Quote(key)
	out.List = append(out.List, sexp.L(sexp.Sym("tag"), sexp.Str(tag)))
	return []*sexp.Node{out}, nil
}

func concretizeMethod(m *Munch, b sexp.Bindings, n *sexp.Node) ([]*sexp.Node, error) {
	out := sexp.L(sexp.Sym("method"), b.One("name"))
	hasParams := false
	for _, part := range b.Rest("parts") {
		if part.Head() == "params" {
			hasParams = true
			part = withCtx(part, m.C.Cfg.ContextFirst)
		}
		out.List = append(out.List, part)
	}
	if !hasParams && m.C.Cfg.ContextFirst {
		out.List = append(out.List, withCtx(sexp.L(sexp.Sym("params")), true))
	}
	return []*sexp.Node{out}, nil
}

func withCtx(params *sexp.Node, on bool) *sexp.Node {
	args := params.Args()
	if !on || (len(args) > 0 && args[0].List[1].Atom == "context.Context") {
		return params
	}
	out := sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("ctx"), sexp.Sym("context.Context")))
	out.List = append(out.List, args...)
	return out
}

func concretizeImpl(m *Munch, b sexp.Bindings, n *sexp.Node) ([]*sexp.Node, error) {
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
	out := sexp.L(append([]*sexp.Node{sexp.Sym("impl"), b.One("iface")}, b.Rest("parts")...)...)
	out.List = append(out.List, sexp.L(sexp.Sym("backend"), sexp.Sym(be.Name)))
	out.List = append(out.List, coverNodes(cov)...) // visible in -dump
	return []*sexp.Node{out}, nil
}
