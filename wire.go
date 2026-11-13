package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"path"
	"sort"
	"strings"
	"unicode"
)

// (project orders
//   (app)
//   ...)
//
// asks for the program as well as its parts: cmd/orders/wire_gen.go, which
// builds every store, bus, server and helper in dependency order, and a
// main.go to start it.
//
// The wiring is worked out from the Go this run produces, not from what
// each tile says about itself. Every constructor (func New...) in a
// package is read back; a parameter another constructor of the project
// can supply is supplied by it, and one nothing can supply becomes a field
// of Deps, for main to fill. So a tile added later, or a constructor
// someone edits in a scaffolded file, is wired without tilegen knowing
// anything about it.
//
// wire_gen.go is regenerated and never edited. main.go is scaffolded once
// and is the caller's: it opens what Deps asks for.

// constructor is one func New... read back from a package.
type constructor struct {
	Pkg, Name string
	Params    []wireParam
	Result    string // the type it returns, without the star: PostgresOrderStore
	Pointer   bool
	Err       bool     // it also returns an error
	Provides  []string // the interfaces of its package the result is asserted to satisfy
	Methods   map[string]string
}

type wireParam struct {
	Name, Type string // as written in its package: OrderStore, *db.Queries
	Import     string // the import path behind a qualified type, if any
}

// wirePackage reads the constructors, assertions and methods of one
// package from the files the plan will leave in dir.
func wirePackage(r *Report, pkg, dir string) ([]*constructor, error) {
	fset := token.NewFileSet()
	var ctors []*constructor
	asserts := map[string][]string{}          // PostgresOrderStore -> [OrderStore]
	methods := map[string]map[string]string{} // type -> method -> signature
	show := func(n ast.Node) string {
		var b bytes.Buffer
		printer.Fprint(&b, fset, n)
		return b.String()
	}
	for _, rel := range r.planned {
		if path.Dir(rel) != dir || !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		src, err := r.read(rel)
		if err != nil {
			continue
		}
		f, err := parser.ParseFile(fset, rel, src, 0)
		if err != nil {
			return nil, fmt.Errorf("wiring: %s: %v", rel, err)
		}
		imports := map[string]string{}
		for _, im := range f.Imports {
			p := strings.Trim(im.Path.Value, `"`)
			name := path.Base(p)
			if im.Name != nil {
				name = im.Name.Name
			} else if strings.HasPrefix(name, "v") && len(name) <= 3 { // .../pgx/v5 is package pgx
				name = path.Base(path.Dir(p))
			}
			imports[name] = p
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.GenDecl: // var _ OrderStore = (*PostgresOrderStore)(nil)
				for _, s := range d.Specs {
					vs, ok := s.(*ast.ValueSpec)
					if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "_" || vs.Type == nil || len(vs.Values) != 1 {
						continue
					}
					if call, ok := vs.Values[0].(*ast.CallExpr); ok && token.IsIdentifier(show(vs.Type)) {
						impl := strings.Trim(show(call.Fun), "(*)")
						asserts[impl] = append(asserts[impl], show(vs.Type)) // interfaces of this package only
					}
				}
			case *ast.FuncDecl:
				if d.Recv != nil && len(d.Recv.List) == 1 {
					recv := strings.TrimPrefix(show(d.Recv.List[0].Type), "*")
					if methods[recv] == nil {
						methods[recv] = map[string]string{}
					}
					methods[recv][d.Name.Name] = strings.TrimPrefix(show(d.Type), "func")
					continue
				}
				if d.Recv != nil || !strings.HasPrefix(d.Name.Name, "New") || d.Type.TypeParams != nil ||
					d.Type.Results == nil || len(d.Type.Results.List) == 0 || len(d.Type.Results.List) > 2 {
					continue
				}
				res := show(d.Type.Results.List[0].Type)
				c := &constructor{Pkg: pkg, Name: d.Name.Name, Result: strings.TrimPrefix(res, "*"), Pointer: strings.HasPrefix(res, "*")}
				if !token.IsIdentifier(c.Result) || !unicode.IsUpper([]rune(c.Result)[0]) {
					continue // returns something that is not a type of this package
				}
				if len(d.Type.Results.List) == 2 {
					if show(d.Type.Results.List[1].Type) != "error" {
						continue
					}
					c.Err = true
				}
				for _, p := range d.Type.Params.List {
					typ := show(p.Type)
					wp := wireParam{Type: typ}
					if q, _, ok := strings.Cut(strings.TrimLeft(typ, "*[]"), "."); ok {
						wp.Import = imports[q]
					}
					if len(p.Names) == 0 {
						c.Params = append(c.Params, wp)
					}
					for _, n := range p.Names {
						wp.Name = n.Name
						c.Params = append(c.Params, wp)
					}
				}
				ctors = append(ctors, c)
			}
		}
	}
	for _, c := range ctors {
		c.Provides, c.Methods = asserts[c.Result], methods[c.Result]
	}
	sort.Slice(ctors, func(i, j int) bool { return ctors[i].Name < ctors[j].Name })
	return ctors, nil
}

// wired is one constructed value in the App.
type wired struct {
	*constructor
	Field string   // its field of App
	Type  string   // the field's type, qualified
	Args  []string // the expression for each parameter
	Deps  []int    // the wired values it is built from
}

// wireProject plans the program: what is built from what, and what has to
// come from outside.
func wireProject(c *Ctx, r *Report) (*wirePlan, error) {
	plan := &wirePlan{Module: c.Module, imports: map[string]string{}}
	pkgs := sortedKeys(c.PkgDirs)
	byImport := map[string]string{} // import path -> package name
	var all []*wired
	for _, pkg := range pkgs {
		byImport[c.Module+"/"+c.PkgDirs[pkg]] = pkg
		ctors, err := wirePackage(r, pkg, c.PkgDirs[pkg])
		if err != nil {
			return nil, err
		}
		for _, ct := range ctors {
			w := &wired{constructor: ct, Field: strings.TrimPrefix(ct.Name, "New")}
			w.Type = "*" + pkg + "." + ct.Result
			if !ct.Pointer {
				w.Type = pkg + "." + ct.Result
			}
			if len(ct.Provides) == 1 { // held as what it is used as
				w.Type, w.Field = pkg+"."+ct.Provides[0], ct.Provides[0]
			}
			all = append(all, w)
		}
		plan.imports[pkg] = c.Module + "/" + c.PkgDirs[pkg]
	}
	// A field's name carries its package only where two would collide.
	count := map[string]int{}
	for _, w := range all {
		count[w.Field]++
	}
	for _, w := range all {
		if count[w.Field] > 1 || len(pkgs) > 1 {
			w.Field = camelUpper(w.Pkg) + w.Field
		}
	}
	// find returns what supplies typ, as asked for inside package pkg.
	find := func(pkg, typ, imp string) (int, error) {
		name := strings.TrimPrefix(typ, "*")
		if imp != "" {
			other, ok := byImport[imp]
			if !ok {
				return -1, nil
			}
			pkg, name = other, name[strings.Index(name, ".")+1:]
		}
		found := -1
		for i, w := range all {
			if w.Pkg != pkg || !(w.Result == name || contains(w.Provides, name)) {
				continue
			}
			if found >= 0 {
				return -1, fmt.Errorf("wiring: %s.%s and %s.%s both supply %s.%s; the program cannot tell which one a constructor wants",
					pkg, all[found].Name, pkg, w.Name, pkg, name)
			}
			found = i
		}
		return found, nil
	}
	for _, w := range all {
		for _, p := range w.Params {
			if p.Type == "context.Context" {
				w.Args = append(w.Args, "ctx")
				continue
			}
			i, err := find(w.Pkg, p.Type, p.Import)
			if err != nil {
				return nil, err
			}
			if i >= 0 {
				w.Args, w.Deps = append(w.Args, "a."+all[i].Field), append(w.Deps, i)
				continue
			}
			w.Args = append(w.Args, "d."+plan.dep(w, p))
		}
	}
	// Order: everything a value is built from comes before it.
	state := make([]int, len(all))
	var visit func(i int, trail []string) error
	visit = func(i int, trail []string) error {
		switch state[i] {
		case 2:
			return nil
		case 1:
			return fmt.Errorf("wiring: a cycle: %s", strings.Join(append(trail, all[i].Pkg+"."+all[i].Name), " needs "))
		}
		state[i] = 1
		for _, d := range all[i].Deps {
			if err := visit(d, append(trail, all[i].Pkg+"."+all[i].Name)); err != nil {
				return err
			}
		}
		state[i] = 2
		plan.Order = append(plan.Order, all[i])
		return nil
	}
	for i := range all {
		if err := visit(i, nil); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

// wirePlan is the program, planned.
type wirePlan struct {
	Module  string
	Order   []*wired
	Deps    []wireDep
	imports map[string]string // qualifier -> import path, for wire_gen.go
}

// wireDep is one thing the program needs from outside.
type wireDep struct {
	Field, Type string
	For         []string // the constructors that take it
}

// dep returns the Deps field that supplies p to w, adding it if new. Two
// constructors asking for the same named type share one field; a plain
// string or number belongs to the constructor that asked.
func (p *wirePlan) dep(w *wired, param wireParam) string {
	typ, base := param.Type, strings.TrimLeft(param.Type, "*[]")
	local := param.Import == "" && token.IsIdentifier(base) && unicode.IsUpper([]rune(base)[0])
	field := ""
	switch {
	case local: // a type of the constructor's own package: OutboxDB
		typ = strings.Replace(typ, base, w.Pkg+"."+base, 1)
		field = camelUpper(w.Pkg) + base
	case param.Import != "" && !(isStdImport(param.Import) && !strings.HasPrefix(param.Type, "*")): // *db.Queries, *nats.Conn, *sql.DB
		q, name, _ := strings.Cut(base, ".")
		p.imports[q] = param.Import
		field = name
		if name == "Conn" || name == "Client" || name == "DB" || name == "Pool" {
			field = camelUpper(q) + name // NatsConn, VaultClient
		}
	default: // a setting: string, int, time.Duration
		if q, _, ok := strings.Cut(base, "."); ok {
			p.imports[q] = param.Import
		}
		field = camelUpper(w.Pkg) + strings.TrimPrefix(w.Name, "New") + camelUpper(param.Name)
	}
	user := w.Pkg + "." + w.Name
	for i := range p.Deps {
		if p.Deps[i].Field == field && p.Deps[i].Type == typ {
			p.Deps[i].For = append(p.Deps[i].For, user)
			return field
		}
	}
	for _, d := range p.Deps { // same name, another type: keep them apart
		if d.Field == field {
			field = camelUpper(w.Pkg) + field
		}
	}
	p.Deps = append(p.Deps, wireDep{Field: field, Type: typ, For: []string{user}})
	return field
}

// text renders wire_gen.go.
func (p *wirePlan) text(app string) ([]byte, error) {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	serveHTTP, serveGRPC, runners := []*wired{}, []*wired{}, []*wired{}
	for _, x := range p.Order {
		switch {
		case strings.HasSuffix(x.Methods["Routes"], "*http.ServeMux"):
			serveHTTP = append(serveHTTP, x)
		case strings.Contains(x.Methods["Register"], "grpc.ServiceRegistrar"):
			serveGRPC = append(serveGRPC, x)
		case strings.HasPrefix(x.Methods["Run"], "(ctx context.Context, onError func(error)) error"):
			runners = append(runners, x)
		}
	}
	w("// %s\n\npackage main\n\nimport (\n\t\"context\"\n", generatedMarker)
	std := map[string]bool{"errors": true, "net/http": len(serveHTTP) > 0, "net": len(serveGRPC) > 0, "os": len(serveHTTP)+len(serveGRPC) > 0,
		"time": len(serveHTTP) > 0}
	for _, q := range sortedKeys(p.imports) {
		if isStdImport(p.imports[q]) {
			std[p.imports[q]] = true
		}
	}
	for _, s := range sortedKeys(std) {
		if std[s] {
			w("\t%q\n", s)
		}
	}
	w("\n")
	if len(serveGRPC) > 0 {
		w("\t\"google.golang.org/grpc\"\n")
	}
	for _, q := range sortedKeys(p.imports) {
		if !isStdImport(p.imports[q]) {
			w("\t%s %q\n", q, p.imports[q])
		}
	}
	w(")\n\n// Deps is what the program needs from outside itself: connections to open\n// and settings to read. Filling it is main's job; everything built on it\n// is build's.\ntype Deps struct {\n")
	if len(p.Deps) == 0 {
		w("\t// Nothing: every part of this program is built from the others.\n")
	}
	for _, d := range p.Deps {
		w("\t%s %s // for %s\n", d.Field, d.Type, strings.Join(d.For, ", "))
	}
	w("}\n\n// missing names the fields of Deps that were left empty.\nfunc (d Deps) missing() []string {\n\tvar out []string\n")
	for _, d := range p.Deps {
		switch {
		case strings.HasPrefix(d.Type, "*") || strings.HasPrefix(d.Type, "[]") || isLocalIface(d):
			w("\tif d.%s == nil {\n\t\tout = append(out, %q)\n\t}\n", d.Field, d.Field+" ("+d.Type+")")
		case d.Type == "string":
			w("\tif d.%s == \"\" {\n\t\tout = append(out, %q)\n\t}\n", d.Field, d.Field+" (string)")
		}
	}
	w("\treturn out\n}\n\n// App is the program, built: every part, wired to the parts it uses.\ntype App struct {\n")
	for _, x := range p.Order {
		w("\t%s %s\n", x.Field, x.Type)
	}
	w("}\n\n// build constructs every part in dependency order.\nfunc build(ctx context.Context, d Deps) (*App, error) {\n" +
		"\tif m := d.missing(); len(m) > 0 {\n\t\treturn nil, errors.New(\"not set in Deps: \" + strings.Join(m, \", \"))\n\t}\n\ta := &App{}\n")
	needErr := false
	for _, x := range p.Order {
		call := fmt.Sprintf("%s.%s(%s)", x.Pkg, x.Name, strings.Join(x.Args, ", "))
		if x.Err {
			needErr = true
			w("\tif a.%s, err = %s; err != nil {\n\t\treturn nil, fmt.Errorf(\"%s.%s: %%w\", err)\n\t}\n", x.Field, call, x.Pkg, x.Name)
		} else {
			w("\ta.%s = %s\n", x.Field, call)
		}
	}
	w("\treturn a, nil\n}\n\n")

	w("// Serve runs the program until ctx is done or a part of it fails: it\n// starts what runs in the background and serves what listens.\n")
	w("func (a *App) Serve(ctx context.Context) error {\n\tctx, cancel := context.WithCancel(ctx)\n\tdefer cancel()\n\tfailed := make(chan error, %d)\n", len(runners)+len(serveGRPC)+1)
	for _, x := range runners {
		w("\tgo func() {\n\t\terr := a.%s.Run(ctx, func(err error) { log.Printf(\"%s: %%v\", err) })\n\t\tif ctx.Err() == nil {\n\t\t\tfailed <- err\n\t\t}\n\t}()\n", x.Field, x.Pkg+"."+x.Result)
	}
	if len(serveGRPC) > 0 {
		w("\tgrpcAddr := os.Getenv(\"GRPC_ADDR\")\n\tif grpcAddr == \"\" {\n\t\tgrpcAddr = \":9090\"\n\t}\n\tlis, err := net.Listen(\"tcp\", grpcAddr)\n\tif err != nil {\n\t\treturn err\n\t}\n\tgs := grpc.NewServer()\n")
		for _, x := range serveGRPC {
			w("\ta.%s.Register(gs)\n", x.Field)
		}
		w("\tgo func() { failed <- gs.Serve(lis) }()\n\tdefer gs.GracefulStop()\n\tlog.Printf(\"gRPC on %%s\", lis.Addr())\n")
	}
	if len(serveHTTP) > 0 {
		w("\taddr := os.Getenv(\"ADDR\")\n\tif addr == \"\" {\n\t\taddr = \":8080\"\n\t}\n\tmux := http.NewServeMux()\n")
		for _, x := range serveHTTP {
			if len(serveHTTP) == 1 {
				w("\tmux.Handle(\"/\", a.%s.Routes())\n", x.Field)
			} else { // each package under its own name
				w("\tmux.Handle(\"/%[1]s/\", http.StripPrefix(\"/%[1]s\", a.%[2]s.Routes()))\n", x.Pkg, x.Field)
			}
		}
		w("\tsrv := &http.Server{Addr: addr, Handler: mux}\n\tgo func() {\n\t\tif err := srv.ListenAndServe(); err != http.ErrServerClosed {\n\t\t\tfailed <- err\n\t\t}\n\t}()\n" +
			"\tdefer func() {\n\t\tstop, done := context.WithTimeout(context.Background(), 10*time.Second)\n\t\tdefer done()\n\t\tsrv.Shutdown(stop)\n\t}()\n\tlog.Printf(\"HTTP on %%s\", addr)\n")
	}
	w("\tselect {\n\tcase <-ctx.Done():\n\t\treturn nil\n\tcase err := <-failed:\n\t\treturn err\n\t}\n}\n")

	src := b.String()
	// The imports that depend on what was written above.
	extra := "\t\"strings\"\n"
	if needErr || strings.Contains(src, "fmt.") {
		extra += "\t\"fmt\"\n"
	}
	if strings.Contains(src, "log.") {
		extra += "\t\"log\"\n"
	}
	src = strings.Replace(src, "import (\n", "import (\n"+extra, 1)
	if needErr {
		src = strings.Replace(src, "\ta := &App{}\n", "\ta := &App{}\n\tvar err error\n", 1)
	}
	out, err := format.Source([]byte(src))
	if err != nil {
		return nil, fmt.Errorf("wiring: tilegen rendered invalid Go for cmd/%s/wire_gen.go (tilegen bug): %v\n%s", app, err, numbered(src))
	}
	return out, nil
}

// isStdImport reports whether an import path is the standard library's:
// its first element has no dot.
func isStdImport(p string) bool {
	first, _, _ := strings.Cut(p, "/")
	return !strings.Contains(first, ".")
}

func isLocalIface(d wireDep) bool {
	return !strings.HasPrefix(d.Type, "*") && strings.Contains(d.Type, ".") && !strings.HasPrefix(d.Type, "time.")
}

// mainText is the scaffold of main.go: written once, then the caller's.
func (p *wirePlan) mainText(app string) []byte {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("// Scaffolded by tilegen. This file is yours: tilegen never overwrites it.\n\n// Command %s runs the %s service.\npackage main\n\n"+
		"import (\n\t\"context\"\n\t\"log\"\n\t\"os\"\n\t\"os/signal\"\n\t\"syscall\"\n)\n\n"+
		"func main() {\n\tctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)\n\tdefer stop()\n\n"+
		"\td, err := deps(ctx)\n\tif err != nil {\n\t\tlog.Fatal(err)\n\t}\n\tapp, err := build(ctx, d)\n\tif err != nil {\n\t\tlog.Fatal(err)\n\t}\n"+
		"\tif err := app.Serve(ctx); err != nil {\n\t\tlog.Fatal(err)\n\t}\n}\n\n", app, app)
	w("// deps opens what the program needs from outside itself. wire_gen.go says\n// what that is, in Deps, and build refuses to start with any of it missing.\n")
	w("func deps(ctx context.Context) (Deps, error) {\n\tvar d Deps\n")
	for _, x := range p.Order {
		if contains(x.Provides, "Secrets") && len(x.Params) == 0 {
			w("\t// %s.%s() reads the secrets to open these with.\n", x.Pkg, x.Name)
		}
	}
	for _, d := range p.Deps {
		w("\t// d.%s = ...   %s, for %s\n", d.Field, d.Type, strings.Join(d.For, ", "))
	}
	w("\treturn d, nil\n}\n")
	out, err := format.Source([]byte(b.String()))
	if err != nil {
		return []byte(b.String())
	}
	return out
}

// emitWiring stages cmd/<app>/wire_gen.go and, once, cmd/<app>/main.go.
func emitWiring(c *Ctx, r *Report) error {
	plan, err := wireProject(c, r)
	if err != nil {
		return err
	}
	dir := path.Join("cmd", c.App)
	gen, err := plan.text(c.App)
	if err != nil {
		return err
	}
	r.stageBy(path.Join(dir, "wire_gen.go"), "", gen)
	if main := path.Join(dir, "main.go"); r.exists(main) {
		r.Produced[main] = true
		r.note(main, "")
	} else {
		r.stageBy(main, "", plan.mainText(c.App))
	}
	return nil
}
