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
//
// The last rule is the catch-all tile. Just as a compiler guarantees
// coverage with a one-node tile for every operator, anything no other tile
// covers becomes an LLM task instead of being silently dropped.
var Select = &Pass{
	Name: "select",
	Rules: []Rule{
		{Name: "project", Pattern: Pat("(project ?name ?items...)"), Then: selectProject},
		{Name: "package", Pattern: Pat("(package ?name ?items...)"), Then: selectPackage},
		{Name: "impl", Pattern: Pat("(impl ?iface ?parts...)"), Then: selectImpl},
		{Name: "struct", Pattern: Pat("(struct ?name ?items...)"), Then: rename("go/struct")},
		{Name: "interface", Pattern: Pat("(interface ?name ?items...)"), Then: rename("go/interface")},
		{Name: "llm", Pattern: Pat("(llm ?intent ?more...)"), Then: selectLLM},
		{Name: "catch-all", Pattern: Pat("_"), Then: selectUncovered},
	},
}

func rename(head string) func(*Munch, Bindings, *Node) ([]*Node, error) {
	return func(m *Munch, b Bindings, n *Node) ([]*Node, error) {
		return []*Node{L(append([]*Node{Sym(head)}, n.Args()...)...)}, nil
	}
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
	for _, p := range pkgs {
		c.Local[p.List[1].Atom] = c.Module + "/" + p.Text("dir")
		c.PkgDirs[p.List[1].Atom] = p.Text("dir")
	}
	if c.Cfg.Storage == "postgres" {
		c.Local["db"] = c.Module + "/internal/db"
	}

	out := []*Node{mod}
	res, err := m.Sub(pkgs...) // the project tile's leaves
	if err != nil {
		return nil, err
	}
	out = append(out, res...)
	for _, r := range res {
		if r.Head() == "sql/table" {
			out = append(out, sqlcConfig(c))
			break
		}
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
		case "go/struct", "go/interface", "go/func", "go/assert", "go/var":
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
	var hint func(method string) string
	var out []*Node
	switch backend {
	case "memory":
		decl.List = append(decl.List,
			L(Sym("field"), Sym("mu"), Sym("sync.Mutex")),
			L(Sym("field"), Sym("m"), Str(fmt.Sprintf("map[%s]*%s", idType, entity))))
		ctor.List = append(ctor.List, L(Sym("params")), L(Sym("returns"), Sym(ptr)),
			L(Sym("body"), Str(fmt.Sprintf("return &%s{m: make(map[%s]*%s)}", impl, idType, entity))))
		hint = func(meth string) string {
			return map[string]string{
				"Get":    "Look up id in s.m while holding s.mu. Return ErrNotFound when absent.",
				"List":   "Collect every value in s.m while holding s.mu and sort them by ID.",
				"Save":   "Store the value in s.m keyed by its ID while holding s.mu, replacing any existing one.",
				"Delete": "Remove id from s.m while holding s.mu. Deleting a missing ID is not an error.",
			}[meth]
		}
	case "postgres":
		decl.List = append(decl.List, L(Sym("field"), Sym("q"), Sym("*db.Queries")))
		ctor.List = append(ctor.List, L(Sym("params"), L(Sym("q"), Sym("*db.Queries"))),
			L(Sym("returns"), Sym(ptr)), L(Sym("body"), Str(fmt.Sprintf("return &%s{q: q}", impl))))
		sql, err := sqlFor(c, entity, st, n.Find("ops"))
		if err != nil {
			return nil, err
		}
		out = append(out, sql...)
		hint = func(meth string) string {
			q := meth + entity
			if meth == "List" {
				q = "List" + plural(entity)
			}
			extra := map[string]string{
				"Get":  " Map pgx.ErrNoRows to ErrNotFound.",
				"Save": fmt.Sprintf(" Build db.Save%sParams from the value's fields.", entity),
			}[meth]
			return fmt.Sprintf("Call the sqlc-generated s.q.%s and convert between db.%s rows and *%s.%s", q, entity, entity, extra)
		}
	default:
		return nil, fmt.Errorf("unknown backend %q", backend)
	}

	contextFiles := []string{genFile}
	asDecl := func(head string, n *Node) *Node { return L(append([]*Node{Sym(head)}, n.Args()...)...) }
	contextFiles = append(contextFiles, foreignGenFiles(c, declTypes([]*Node{asDecl("go/interface", iface), asDecl("go/struct", st)}))...)
	if backend == "postgres" {
		contextFiles = append(contextFiles, "db/query.sql")
	}

	decls := []*Node{decl, ctor}
	var tasks []*Node
	for _, meth := range iface.FindAll("method") {
		name := meth.List[1].Atom
		id := pkg.Name + "." + impl + "." + name
		fn := L(Sym("go/func"), Sym(name), L(Sym("recv"), Sym("s"), Sym(ptr)))
		if p := meth.Find("params"); p != nil {
			fn.List = append(fn.List, p)
		}
		if r := meth.Find("returns"); r != nil {
			fn.List = append(fn.List, r)
		}
		fn.List = append(fn.List, L(Sym("body"), Str(holeBody(id))))
		decls = append(decls, fn)

		intent := hint(name)
		if intent == "" {
			intent = fmt.Sprintf("Implement %s so that %s satisfies %s.", name, impl, ifaceName)
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
				// goimports adds it
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
		case "go/func":
			sig(d)
		}
	}
	return out
}

var (
	stdOnce  sync.Once
	stdNames map[string]bool
)

// isStd asks the go command itself which standard packages exist.
func isStd(name string) bool {
	stdOnce.Do(func() {
		stdNames = map[string]bool{}
		out, err := exec.Command("go", "list", "std").Output()
		if err != nil {
			return
		}
		for _, p := range strings.Fields(string(out)) {
			if !strings.Contains(p, "internal") && !strings.HasPrefix(p, "vendor/") {
				stdNames[path.Base(p)] = true
			}
		}
	})
	return stdNames[name]
}
