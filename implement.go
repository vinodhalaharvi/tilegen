package main

import (
	"fmt"
	"go/build"
	"go/importer"
	"go/token"
	"go/types"
	"os/exec"
	"path"
	"strings"
	"sync"
)

// (implement Iface (as TypeName)
//   (field store ShareStore)        ; dependencies, injected by the constructor
//   (doc "...")
//   (constraint "..."))
//
// implements any interface declared in the package, including generated
// store interfaces: a struct, a constructor, one hole per method (embedded
// interfaces included), a compile-time assertion, and LLM tasks. The file
// is scaffolded once and reconciled after that, like every other.

func (v *validator) implement(n *Node, ifaces, types map[string]bool) {
	b := v.shape(n, "(implement ?iface ?parts...)")
	if b == nil {
		return
	}
	if in := b.One("iface"); in.IsList || !ifaces[in.Atom] {
		v.bad(in, "implement: %s is not an interface declared in this package%s (have: %s)", in.Flat(), didYouMean(in.Atom, sortedKeys(ifaces)), strings.Join(sortedKeys(ifaces), ", "))
	}
	as := 0
	fields := map[string]bool{}
	for _, p := range b.Rest("parts") {
		switch p.Head() {
		case "as":
			if pb := v.shape(p, "(as ?name)"); pb != nil {
				v.declName(pb.One("name"), types)
				as++
			}
		case "field":
			if fb := v.shape(p, "(field ?name ?type)"); fb != nil {
				v.ident(fb.One("name"), "field name", false)
				v.typ(fb.One("type"))
				if fields[fb.Atom("name")] {
					v.bad(p, "duplicate field %q", fb.Atom("name"))
				}
				fields[fb.Atom("name")] = true
			}
		case "doc", "constraint":
			v.shape(p, "(_ ?text)")
		default:
			v.bad(p, "implement takes (as Name), (field name Type), (doc ...) and (constraint ...), got %s%s", short(p), didYouMean(p.Head(), []string{"as", "field", "doc", "constraint"}))
		}
	}
	if as != 1 {
		v.bad(n, "implement needs exactly one (as TypeName)")
	}
}

// packageInterfaces lists the interfaces a package declares, including the
// NameStore interfaces its entities' stores will generate.
func packageInterfaces(items []*Node) map[string]bool {
	out := map[string]bool{}
	for _, it := range items {
		switch {
		case it.Head() == "interface" && len(it.List) > 1:
			out[it.List[1].Atom] = true
		case it.Head() == "entity" && len(it.List) > 1 && it.Find("store") != nil:
			out[it.List[1].Atom+"Store"] = true
		}
	}
	return out
}

func selectImplement(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	c, pkg := m.C, m.C.Pkg
	ifaceName, impl := b.Atom("iface"), n.Text("as")
	iface := pkg.Ifaces[ifaceName]
	if iface == nil {
		return nil, fmt.Errorf("implement: interface %s not found in package %s", ifaceName, pkg.Name)
	}
	methods, unresolved, err := interfaceMethods(pkg, iface, map[string]bool{})
	if err != nil {
		return nil, err
	}
	file := path.Join(pkg.Dir, snake(impl)+".go")
	genFile := path.Join(pkg.Dir, pkg.Name+"_gen.go")

	doc := n.Text("doc")
	if doc == "" {
		doc = fmt.Sprintf("%s implements %s.", impl, ifaceName)
	}
	decl := L(Sym("go/struct"), Sym(impl), L(Sym("doc"), Str(doc)))
	params := L(Sym("params"))
	var inits, deps []string
	var types []*Node
	for _, f := range n.FindAll("field") {
		name, typ := f.List[1], f.List[2]
		decl.List = append(decl.List, L(Sym("field"), name, typ))
		params.List = append(params.List, L(name, typ))
		inits = append(inits, name.Atom+": "+name.Atom)
		deps = append(deps, fmt.Sprintf("s.%s (%s)", name.Atom, typ.Atom))
		types = append(types, typ)
	}
	ctor := L(Sym("go/func"), Sym("New"+impl),
		L(Sym("doc"), Str(fmt.Sprintf("New%s returns a new %s using the given dependencies.", impl, impl))),
		params, L(Sym("returns"), Sym("*"+impl)),
		L(Sym("body"), Str(fmt.Sprintf("return &%s{%s}", impl, strings.Join(inits, ", ")))))

	var constraints []string
	for _, k := range n.FindAll("constraint") {
		constraints = append(constraints, k.List[1].Atom)
	}
	for _, meth := range methods {
		types = append(types, declTypes([]*Node{L(append([]*Node{Sym("go/func")}, meth.Args()...)...)})...)
	}
	contextFiles := append([]string{genFile}, foreignGenFiles(c, types)...)
	hint := func(meth *Node) string {
		h := customHint(meth)
		if h == "" {
			h = fmt.Sprintf("Implement %s so that %s satisfies %s.", meth.List[1].Atom, impl, ifaceName)
		}
		if len(deps) > 0 {
			h += " Dependencies: " + strings.Join(deps, ", ") + "."
		}
		return h
	}
	stubs, tasks := stubsAndTasks(pkg, impl, ifaceName, file, methods, hint, contextFiles, constraints)
	for _, u := range unresolved {
		c.warn(n.Pos, "cannot list the methods of %s (embedded in %s); the compile-time check will report any %s lacks", u, ifaceName, impl)
	}
	f, err := goFile(c, file, "keep", "", append([]*Node{decl, ctor}, stubs...))
	if err != nil {
		return nil, err
	}
	return append([]*Node{f, L(Sym("go/assert"), Sym(ifaceName), Sym(impl))}, tasks...), nil
}

// interfaceMethods returns an interface's full method set, following
// embedded interfaces: project interfaces directly, standard-library ones
// through Go's type checker. Embeds it cannot resolve are returned by name.
func interfaceMethods(pkg *PkgScope, iface *Node, seen map[string]bool) (methods []*Node, unresolved []string, err error) {
	name := iface.List[1].Atom
	if seen[name] {
		return nil, nil, fmt.Errorf("interface %s embeds itself", name)
	}
	seen[name] = true
	have := map[string]bool{}
	add := func(ms ...*Node) {
		for _, m := range ms {
			if n := m.List[1].Atom; !have[n] {
				have[n] = true
				methods = append(methods, m)
			}
		}
	}
	for _, it := range iface.Args() {
		switch it.Head() {
		case "method":
			add(it)
		case "embed":
			t := it.List[1].Atom
			if local := pkg.Ifaces[t]; local != nil {
				ms, un, err := interfaceMethods(pkg, local, seen)
				if err != nil {
					return nil, nil, err
				}
				add(ms...)
				unresolved = append(unresolved, un...)
				continue
			}
			if q, n, ok := strings.Cut(t, "."); ok && isStd(q) {
				if ms, err := stdInterfaceMethods(q, n); err == nil {
					add(ms...)
					continue
				}
			}
			unresolved = append(unresolved, t)
		}
	}
	return methods, unresolved, nil
}

var (
	stdIfaceMu    sync.Mutex
	stdIfaceCache = map[string][]*Node{}
)

// stdInterfaceMethods asks the Go type checker, reading the standard
// library's own source, for the methods of an interface like io.Writer.
// Nothing is hardcoded: whatever the installed Go version says, goes.
func stdInterfaceMethods(pkgName, typeName string) ([]*Node, error) {
	stdIfaceMu.Lock()
	defer stdIfaceMu.Unlock()
	key := pkgName + "." + typeName
	if ms, ok := stdIfaceCache[key]; ok {
		return ms, nil
	}
	importPath, err := stdPath(pkgName)
	if err != nil {
		return nil, err
	}
	if build.Default.GOROOT == "" { // e.g. tilegen built with -trimpath
		if out, err := exec.Command("go", "env", "GOROOT").Output(); err == nil {
			build.Default.GOROOT = strings.TrimSpace(string(out))
		}
	}
	p, err := importer.ForCompiler(token.NewFileSet(), "source", nil).Import(importPath)
	if err != nil {
		return nil, err
	}
	obj, ok := p.Scope().Lookup(typeName).(*types.TypeName)
	if !ok {
		return nil, fmt.Errorf("%s has no type %s", importPath, typeName)
	}
	it, ok := obj.Type().Underlying().(*types.Interface)
	if !ok {
		return nil, fmt.Errorf("%s is not an interface", key)
	}
	qual := func(p *types.Package) string { return p.Name() }
	var ms []*Node
	for i := 0; i < it.NumMethods(); i++ { // the complete method set, embeds included
		fn := it.Method(i)
		sig := fn.Type().(*types.Signature)
		params := L(Sym("params"))
		for j := 0; j < sig.Params().Len(); j++ {
			v := sig.Params().At(j)
			name := v.Name()
			if name == "" || name == "_" || name == "s" {
				name = fmt.Sprintf("p%d", j)
			}
			t := types.TypeString(v.Type(), qual)
			if sig.Variadic() && j == sig.Params().Len()-1 {
				t = "..." + types.TypeString(v.Type().(*types.Slice).Elem(), qual)
			}
			params.List = append(params.List, L(Sym(name), Str(t)))
		}
		returns := L(Sym("returns"))
		for j := 0; j < sig.Results().Len(); j++ {
			returns.List = append(returns.List, Str(types.TypeString(sig.Results().At(j).Type(), qual)))
		}
		ms = append(ms, L(Sym("method"), Sym(fn.Name()),
			L(Sym("doc"), Str(fmt.Sprintf("%s implements %s.%s.", fn.Name(), key, fn.Name()))), params, returns))
	}
	stdIfaceCache[key] = ms
	return ms, nil
}
