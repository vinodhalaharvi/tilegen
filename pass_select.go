package main

import (
	"fmt"
	"os/exec"
	"path"
	"sort"
	"strings"
	"sync"
)

// Select is instruction selection proper: it covers the concrete IR with
// tiles whose outputs are target forms - the "machine instructions" of
// this compiler:
//
//	(gomod ...)        -> go.mod        via golang.org/x/mod/modfile
//	(go/file ...)      -> .go files     via go/parser, go/format, x/tools/imports
//	(sql/table ...)    -> db/schema.sql (sqlc input)
//	(sql/query ...)    -> db/query.sql  (sqlc input)
//	(sqlc/config ...)  -> sqlc.yaml
//	(llm/task ...)     -> tilegen.tasks.json
//	(text/file ...)    -> starter files such as README.md, kept once written
//	(git/repo ...)     -> git init or clone, commit, gh repo create, topics (-git)
//
// The last rule is the catch-all tile. Just as a compiler guarantees
// coverage with a one-node tile for every operator, anything no other tile
// covers becomes an LLM task instead of being silently dropped.
var Select = &Pass{
	Name: "select",
	Rules: []Rule{
		{Name: "project", Pattern: Pat("(project ?name ?items...)"), Then: selectProject, Produces: "gomod", Doc: "writes go.mod and resolves imports"},
		{Name: "package", Pattern: Pat("(package ?name ?items...)"), Then: selectPackage, Produces: "go/file", Doc: "collects a package into its generated file"},
		{Name: "impl", Pattern: Pat("(impl ?iface ?parts...)"), Then: selectImpl, Produces: "store", Doc: "implements a store through the registered backend"},
		{Name: "struct", Pattern: Pat("(struct ?name ?items...)"), Then: rename("go/struct"), Produces: "go/struct", Doc: "a struct, as written", Cost: Cost{{Dim: "llm-work", Value: 0}}},
		{Name: "interface", Pattern: Pat("(interface ?name ?items...)"), Then: selectInterface, Produces: "go/interface", Doc: "an interface, as written", Cost: Cost{{Dim: "llm-work", Value: 0}}},
		{Name: "implement", Pattern: Pat("(implement ?iface ?parts...)"), Then: selectImplement, Produces: "go/file", Doc: "implements any interface: struct, constructor, stubs, check", Cost: Cost{{Dim: "llm-work", Value: 5}, {Dim: "maintenance", Value: 2}}},
		{Name: "enum", Pattern: Pat("(enum ?name ?items...)"), Then: selectEnum, Produces: "go/type", Doc: "a string type with constants, Valid() and Parse()", Cost: Cost{{Dim: "llm-work", Value: 0}, {Dim: "maintenance", Value: 0}}},
		{Name: "llm", Pattern: Pat("(llm ?intent ?more...)"), Then: selectLLM, Produces: "llm/task", Doc: "an explicit hole: pure intent for the LLM", Cost: Cost{{Dim: "llm-work", Value: 8}, {Dim: "uncertainty", Value: 6}}},
		{Name: "catch-all", Pattern: Pat("_"), Then: selectUncovered, Produces: "llm/task", Doc: "covers anything no other tile covers: the LLM as the tile of last resort", Cost: Cost{{Dim: "llm-work", Value: 10}, {Dim: "uncertainty", Value: 10}}},
	},
}

func rename(head string) func(*Munch, Bindings, *Node) ([]*Node, error) {
	return func(m *Munch, b Bindings, n *Node) ([]*Node, error) {
		return []*Node{L(append([]*Node{Sym(head)}, n.Args()...)...)}, nil
	}
}

// selectInterface emits (go/interface ...), dropping the (op ...) tags that
// store methods carry between passes.
func selectInterface(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	out := L(Sym("go/interface"))
	for _, a := range n.Args() {
		if a.Head() == "method" {
			cp := L()
			for _, part := range a.List {
				if part.Head() != "op" {
					cp.List = append(cp.List, part)
				}
			}
			a = cp
		}
		out.List = append(out.List, a)
	}
	return []*Node{out}, nil
}

// methodOp returns a store method's op kind and field; custom methods and
// plain interface methods report "custom".
func methodOp(meth *Node) (kind, field string) {
	op := meth.Find("op")
	if op == nil || len(op.List) < 2 {
		return "custom", ""
	}
	if len(op.List) > 2 {
		field = op.List[2].Atom
	}
	return op.List[1].Atom, field
}

// firstParam is the first non-context parameter's name.
func firstParam(meth *Node) string {
	for _, p := range meth.Find("params").Args() {
		if p.List[1].Atom != "context.Context" {
			return p.List[0].Atom
		}
	}
	return ""
}

func memoryHint(meth *Node) string {
	kind, f := methodOp(meth)
	p := firstParam(meth)
	switch kind {
	case "get":
		return "Look up id in s.m while holding s.mu. Return ErrNotFound when absent."
	case "list":
		return "Collect every value in s.m while holding s.mu and sort them by ID."
	case "save":
		return "Store the value in s.m keyed by its ID while holding s.mu, replacing any existing one."
	case "delete":
		return "Remove id from s.m while holding s.mu. Deleting a missing ID is not an error."
	case "count":
		return "Return len(s.m) while holding s.mu."
	case "list-by":
		return fmt.Sprintf("Collect the values in s.m whose %s equals %s, sorted by ID, while holding s.mu.", f, p)
	case "get-by":
		return fmt.Sprintf("Return the value in s.m with the lowest ID whose %s equals %s, while holding s.mu. Return ErrNotFound when there is none.", f, p)
	case "count-by":
		return fmt.Sprintf("Count the values in s.m whose %s equals %s, while holding s.mu.", f, p)
	case "exists-by":
		return fmt.Sprintf("Report whether any value in s.m has %s equal to %s, while holding s.mu.", f, p)
	case "delete-by":
		return fmt.Sprintf("Remove every value in s.m whose %s equals %s, while holding s.mu.", f, p)
	}
	return customHint(meth)
}

func postgresHint(meth *Node, entity string) string { return sqlcHint(meth, entity, "pgx.ErrNoRows") }

// sqlcHint describes a method sqlc has already written the query for. The
// sentinel differs by driver: pgx has its own, database/sql has
// sql.ErrNoRows, and naming the wrong one sends the reader to a package
// the project does not import.
func sqlcHint(meth *Node, entity, noRows string) string {
	kind, f := methodOp(meth)
	q := queryName(kind, entity, f)
	switch kind {
	case "get", "get-by":
		return fmt.Sprintf("Call the sqlc-generated s.q.%s and convert the db.%s row to *%s. Map %s to ErrNotFound.", q, entity, entity, noRows)
	case "list", "list-by":
		return fmt.Sprintf("Call the sqlc-generated s.q.%s and convert each db.%s row to *%s.", q, entity, entity)
	case "save":
		return fmt.Sprintf("Call the sqlc-generated s.q.%s with db.%sParams built from the value's fields.", q, q)
	case "count", "count-by", "exists-by":
		return fmt.Sprintf("Call the sqlc-generated s.q.%s and return its result.", q)
	case "delete", "delete-by":
		return fmt.Sprintf("Call the sqlc-generated s.q.%s.", q)
	}
	return customHint(meth) + " tilegen generates no SQL for custom methods: use the existing queries in s.q, or add a store op to the spec if it needs a new one."
}

func customHint(meth *Node) string {
	if d := meth.Text("doc"); d != "" {
		return "Implement it as documented: " + d
	}
	return ""
}

func selectProject(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	c := m.C
	c.Module = n.Text("module")
	c.Requires, c.Local, c.PkgDirs = map[string]string{}, map[string]string{}, map[string]string{}
	mod := L(Sym("gomod"), L(Sym("module"), Str(c.Module)), L(Sym("go"), Str(n.Text("go"))))
	for _, req := range n.FindAll("require") {
		for _, r := range req.Args() {
			alias, modPath, ver := r.List[0].Atom, r.List[1].Atom, r.List[2].Atom
			imp := modPath
			if len(r.List) == 4 {
				imp = r.List[3].Atom
			}
			c.Requires[alias] = imp
			mod.List = append(mod.List, L(Sym("require"), Str(modPath), Str(ver)))
		}
	}
	pkgs := n.FindAll("package")
	c.Enums = map[string][]string{}
	for _, p := range pkgs {
		for _, e := range p.FindAll("enum") {
			c.Enums[p.List[1].Atom+"."+e.List[1].Atom] = enumValues(e)
		}
	}
	for _, p := range pkgs {
		c.Local[p.List[1].Atom] = c.Module + "/" + p.Text("dir")
		c.PkgDirs[p.List[1].Atom] = p.Text("dir")
	}
	for _, be := range usedBackends(c) {
		for name, dir := range be.Packages {
			c.Local[name] = c.Module + "/" + dir
		}
		for q, imp := range be.Imports {
			c.Requires[q] = imp
		}
	}
	for _, o := range c.Used {
		if tr, ok := o.Impl.(*BusTransport); ok && tr.Import[0] != "" {
			c.Requires[tr.Import[0]] = tr.Import[1]
		}
	}

	out := []*Node{mod}
	res, err := m.Sub(pkgs...) // the project tile's leaves
	if err != nil {
		return nil, err
	}
	out = append(out, res...)
	for _, be := range usedBackends(c) {
		if be.ProjectForms != nil {
			out = append(out, be.ProjectForms(c)...)
		}
	}
	if repo := n.Find("repo"); repo != nil {
		out = append(out, selectRepo(c, n, repo)...)
	}
	return out, nil
}

func selectPackage(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	name := b.Atom("name")
	scope := &PkgScope{Name: name, Dir: n.Text("dir"), Structs: map[string]*Node{}, Ifaces: map[string]*Node{}}
	var items []*Node
	doc := ""
	for _, it := range b.Rest("items") {
		switch it.Head() {
		case "dir":
			continue
		case "doc":
			doc = it.Args()[0].Atom
			continue
		case "struct":
			scope.Structs[it.List[1].Atom] = it
		case "interface":
			scope.Ifaces[it.List[1].Atom] = it
		}
		items = append(items, it)
	}
	m.C.Pkg = scope
	res, err := m.Sub(items...)
	if err != nil {
		return nil, err
	}

	var decls, rest []*Node
	seen := map[string]bool{}
	for _, r := range res {
		switch r.Head() {
		case "go/struct", "go/interface", "go/func", "go/assert", "go/var", "go/type", "go/consts":
			if key := r.Flat(); !seen[key] { // two stores may both want ErrNotFound
				seen[key] = true
				decls = append(decls, r)
			}
		default:
			rest = append(rest, r)
		}
	}
	if doc == "" {
		doc = fmt.Sprintf("Package %s was scaffolded by tilegen.", name)
	}
	gen, err := goFile(m.C, path.Join(scope.Dir, name+"_gen.go"), "generated", doc, decls)
	if err != nil {
		return nil, err
	}
	return append([]*Node{gen}, rest...), nil
}

// selectImpl is the big tile. It covers (impl ...) plus, through the
// package symbol table, the interface and struct it refers to, and emits
// a whole implementation file, a compile-time assertion, SQL for sqlc,
// and one LLM task per method body.
func selectImpl(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	c, pkg := m.C, m.C.Pkg
	ifaceName, entity, backend := b.Atom("iface"), n.Text("for"), n.Text("backend")
	iface, st := pkg.Ifaces[ifaceName], pkg.Structs[entity]
	if iface == nil || st == nil {
		return nil, fmt.Errorf("impl %s: interface or struct %s not found in package %s", ifaceName, entity, pkg.Name)
	}
	idType := ""
	for _, f := range st.FindAll("field") {
		if f.List[1].Atom == "ID" {
			idType = f.List[2].Atom
		}
	}
	var constraints []string
	for _, k := range n.FindAll("constraint") {
		constraints = append(constraints, k.List[1].Atom)
	}

	impl := strings.ToUpper(backend[:1]) + backend[1:] + ifaceName // MemoryOrderStore
	file := path.Join(pkg.Dir, snake(impl)+".go")
	genFile := path.Join(pkg.Dir, pkg.Name+"_gen.go")
	ptr := "*" + impl

	decl := L(Sym("go/struct"), Sym(impl), L(Sym("doc"), Str(fmt.Sprintf("%s implements %s using %s storage.", impl, ifaceName, backend))))
	ctor := L(Sym("go/func"), Sym("New"+impl), L(Sym("doc"), Str(fmt.Sprintf("New%s returns a ready-to-use %s.", impl, impl))))
	be := lookupBackend(backend)
	if be == nil {
		return nil, fmt.Errorf("unknown storage backend %q%s (registered: %s)", backend, didYouMean(backend, backendNames()), strings.Join(backendNames(), ", "))
	}
	parts, err := be.Implement(StoreInput{C: c, Pkg: pkg, Entity: entity, Struct: st, Iface: ifaceName, Impl: impl, IDType: idType, Ops: n.Find("ops")})
	if err != nil {
		return nil, err
	}
	decl.List = append(decl.List, parts.Fields...)
	ctor.List = append(ctor.List, parts.Params, L(Sym("returns"), Sym(ptr)), L(Sym("body"), Str(parts.Body)))
	out := parts.Extra

	contextFiles := []string{genFile}
	asDecl := func(head string, n *Node) *Node { return L(append([]*Node{Sym(head)}, n.Args()...)...) }
	contextFiles = append(contextFiles, foreignGenFiles(c, declTypes([]*Node{asDecl("go/interface", iface), asDecl("go/struct", st)}))...)
	contextFiles = append(contextFiles, parts.Context...)

	stubs, tasks := stubsAndTasks(pkg, impl, ifaceName, file, iface.FindAll("method"), parts.Hint, contextFiles, constraints)
	decls := append([]*Node{decl, ctor}, stubs...)

	f, err := goFile(c, file, "keep", "", decls)
	if err != nil {
		return nil, err
	}
	out = append([]*Node{f,
		L(Sym("go/assert"), Sym(ifaceName), Sym(impl)),
		L(Sym("go/var"), Sym("ErrNotFound"), Str(fmt.Sprintf("errors.New(%q)", pkg.Name+": not found")),
			L(Sym("doc"), Str("ErrNotFound is returned when a requested value does not exist."))),
	}, out...)
	return append(out, tasks...), nil
}

// foreignGenFiles returns the generated files of other project packages
// whose types appear in ts, so an LLM filling a hole sees orders.Order's
// definition when a billing method takes one.
func foreignGenFiles(c *Ctx, ts []*Node) []string {
	seen := map[string]bool{}
	for _, t := range ts {
		qs, _ := typeQualifiers(t.Atom)
		for _, q := range qs {
			if dir, ok := c.PkgDirs[q]; ok && q != c.Pkg.Name {
				seen[path.Join(dir, q+"_gen.go")] = true
			}
		}
	}
	return sortedKeys(seen)
}

// stubsAndTasks makes one hole-bodied method and one LLM task per method,
// for any tile that implements an interface.
func stubsAndTasks(pkg *PkgScope, impl, iface, file string, methods []*Node, hint func(*Node) string,
	contextFiles, constraints []string) (stubs, tasks []*Node) {
	return stubsAndTasksAs("s", pkg, impl, iface, file, methods, hint, contextFiles, constraints)
}

// stubsAndTasksAs is stubsAndTasks with the receiver name a tile wants, so
// a handler's methods read the same as the generated ones beside them.
func stubsAndTasksAs(recv string, pkg *PkgScope, impl, iface, file string, methods []*Node, hint func(*Node) string,
	contextFiles, constraints []string) (stubs, tasks []*Node) {
	ptr := "*" + impl
	for _, meth := range methods {
		name := meth.List[1].Atom
		id := pkg.Name + "." + impl + "." + name
		fn := L(Sym("go/func"), Sym(name), L(Sym("recv"), Sym(recv), Sym(ptr)))
		if p := meth.Find("params"); p != nil {
			fn.List = append(fn.List, p)
		}
		if r := meth.Find("returns"); r != nil {
			fn.List = append(fn.List, r)
		}
		fn.List = append(fn.List, L(Sym("body"), Str(holeBody(id))))
		stubs = append(stubs, fn)

		intent := hint(meth)
		if intent == "" {
			intent = fmt.Sprintf("Implement %s so that %s satisfies %s.", name, impl, iface)
		}
		task := L(Sym("llm/task"),
			L(Sym("id"), Str(id)),
			L(Sym("file"), Str(file)),
			L(Sym("symbol"), Str(fmt.Sprintf("(%s).%s", ptr, name))),
			L(Sym("contract"), Str(signature(meth))),
			L(Sym("intent"), Str(intent)))
		for _, f := range contextFiles {
			task.List = append(task.List, L(Sym("context-file"), Str(f)))
		}
		for _, k := range constraints {
			task.List = append(task.List, L(Sym("constraint"), Str(k)))
		}
		tasks = append(tasks, task)
	}
	return stubs, tasks
}

// holeBody is the marker the emitter later finds with go/ast to report the
// exact line of every remaining hole.
func holeBody(id string) string { return fmt.Sprintf("panic(%q)", holePrefix+id) }

const holePrefix = "tilegen:hole "

func selectLLM(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	m.C.seq++
	task := L(Sym("llm/task"),
		L(Sym("id"), Str(fmt.Sprintf("%s.llm-%d", m.C.Pkg.Name, m.C.seq))),
		L(Sym("intent"), Str(b.Atom("intent"))),
		L(Sym("context-file"), Str(path.Join(m.C.Pkg.Dir, m.C.Pkg.Name+"_gen.go"))))
	for _, x := range b.Rest("more") {
		task.List = append(task.List, x)
	}
	return []*Node{task}, nil
}

func selectUncovered(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	if m.C.Strict {
		return nil, fmt.Errorf("no tile covers %s", short(n))
	}
	m.C.warn(n.Pos, "no tile covers %s; handing it to the LLM", short(n))
	m.C.seq++
	pkg := "project"
	if m.C.Pkg != nil {
		pkg = m.C.Pkg.Name
	}
	return []*Node{L(Sym("llm/task"),
		L(Sym("id"), Str(fmt.Sprintf("%s.uncovered-%d", pkg, m.C.seq))),
		L(Sym("intent"), Str("No tile covers this spec form. Implement what it describes by hand, in package "+pkg+".")),
		L(Sym("sexpr"), Str(n.Flat())))}, nil
}

// goFile wraps declarations into a (go/file ...) and resolves the imports
// its types need. Declared requires and project packages get explicit
// imports; standard-library qualifiers are verified here and left for
// goimports (x/tools/imports) to insert, so we never hand-maintain a list.
func goFile(c *Ctx, file, mode, doc string, decls []*Node) (*Node, error) {
	f := L(Sym("go/file"), Str(file), L(Sym("mode"), Sym(mode)), L(Sym("package"), Sym(c.Pkg.Name)))
	if doc != "" {
		f.List = append(f.List, L(Sym("doc"), Str(doc)))
	}
	imports := map[string]string{}
	for _, t := range declTypes(decls) {
		qs, err := typeQualifiers(t.Atom)
		if err != nil {
			return nil, &TileError{Pos: t.Pos, Pass: "select", Rule: "imports", Err: err}
		}
		for _, q := range qs {
			switch imp, local := c.Requires[q], c.Local[q]; {
			case imp != "":
				imports[q] = imp
			case local != "" && q != c.Pkg.Name:
				imports[q] = local
			case isStd(q):
				// Say it outright rather than leave it to goimports:
				// tilegen knows the path, and must work where the Go
				// toolchain is not installed.
				sp, err := stdPath(q)
				if err != nil {
					return nil, &TileError{Pos: t.Pos, Pass: "select", Rule: "imports", Err: err}
				}
				imports[q] = sp
			default:
				return nil, &TileError{Pos: t.Pos, Pass: "select", Rule: "imports",
					Err: fmt.Errorf("unknown package %q in type %s: add (require (%s <module> <version>)) or use a project package", q, t.Atom, q)}
			}
		}
	}
	names := make([]string, 0, len(imports))
	for q := range imports {
		names = append(names, q)
	}
	sort.Strings(names)
	for _, q := range names {
		f.List = append(f.List, L(Sym("import"), Sym(q), Str(imports[q])))
	}
	f.List = append(f.List, decls...)
	return f, nil
}

// declTypes collects every type expression mentioned by go/* declarations.
func declTypes(decls []*Node) []*Node {
	var out []*Node
	sig := func(n *Node) {
		for _, p := range n.Find("params").Args() {
			out = append(out, p.List[1])
		}
		if r := n.Find("returns"); r != nil {
			out = append(out, r.Args()...)
		}
		if r := n.Find("recv"); r != nil {
			out = append(out, r.List[2])
		}
	}
	for _, d := range decls {
		switch d.Head() {
		case "go/struct":
			for _, f := range d.FindAll("field") {
				out = append(out, f.List[2])
			}
		case "go/interface":
			for _, m := range d.FindAll("method") {
				sig(m)
			}
			for _, e := range d.FindAll("embed") {
				out = append(out, e.List[1])
			}
		case "go/func":
			sig(d)
			// Types a body references but no signature names, declared by
			// the tile with (uses T): the ID type a handler parses, say.
			for _, u := range d.FindAll("uses") {
				out = append(out, u.Args()...)
			}
		}
	}
	return out
}

var (
	stdOnce  sync.Once
	stdPaths map[string][]string // package name -> import paths, e.g. rand -> crypto/rand, math/rand
)

// isStd asks the go command itself which standard packages exist.
func isStd(name string) bool {
	loadStd()
	return len(stdPaths[name]) > 0
}

// stdPath resolves a standard-library package name to its import path.
func stdPath(name string) (string, error) {
	loadStd()
	switch ps := stdPaths[name]; len(ps) {
	case 0:
		return "", fmt.Errorf("%s is not a standard-library package", name)
	case 1:
		return ps[0], nil
	default:
		return "", fmt.Errorf("%s is ambiguous in the standard library (%s)", name, strings.Join(ps, ", "))
	}
}

// loadStd indexes the standard library by package name. The list is
// baked in (stdlist.go, from gen_std.go), because tilegen must work where
// the Go toolchain is not installed: the HTTP service runs in an image
// with no `go` binary, and shelling out there failed silently, leaving
// every standard package unrecognised.
//
// A newer toolchain than the one tilegen was built with may have packages
// the list lacks, so `go list std` is still consulted when it is there,
// and anything it adds is merged in.
func loadStd() {
	stdOnce.Do(func() {
		stdPaths = map[string][]string{}
		add := func(p string) {
			if !strings.Contains(p, "internal") && !strings.HasPrefix(p, "vendor/") {
				name := path.Base(p)
				if !contains(stdPaths[name], p) {
					stdPaths[name] = append(stdPaths[name], p)
				}
			}
		}
		for _, p := range stdList {
			add(p)
		}
		if out, err := exec.Command("go", "list", "std").Output(); err == nil {
			for _, p := range strings.Fields(string(out)) {
				add(p)
			}
		}
	})
}
