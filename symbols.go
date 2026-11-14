//go:build !js

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/tools/go/packages"
)

// readSymbols is how the rest of tilegen asks for a module's symbols. An
// answer is kept for a while, so looking at a module and then asking
// about parts of it fetches it once.
var readSymbols = rememberSymbols(moduleSymbols, 15*time.Minute)

func rememberSymbols(read func(mod, version string) (*ModuleSymbols, error), keep time.Duration) func(mod, version string) (*ModuleSymbols, error) {
	type entry struct {
		m    *ModuleSymbols
		when time.Time
	}
	var mu sync.Mutex
	kept := map[string]entry{}
	return func(mod, version string) (*ModuleSymbols, error) {
		key := mod + "@" + version
		mu.Lock()
		e, ok := kept[key]
		mu.Unlock()
		if ok && time.Since(e.when) < keep {
			return e.m, nil
		}
		m, err := read(mod, version)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		kept[key] = entry{m, time.Now()}
		mu.Unlock()
		return m, nil
	}
}

var errorIface = types.Universe.Lookup("error").Type().Underlying().(*types.Interface)

func isErrorType(t types.Type) bool {
	return types.Implements(t, errorIface) || types.Implements(types.NewPointer(t), errorIface)
}

// packageSymbols reads one type-checked package.
func packageSymbols(p *packages.Package) PackageSymbols {
	out := PackageSymbols{Path: p.PkgPath, Name: p.Types.Name(), Doc: packageDoc(p)}
	docs := symbolDocs(p)
	qual := func(other *types.Package) string {
		if other == p.Types {
			return ""
		}
		return other.Name()
	}
	str := func(t types.Type) string { return types.TypeString(t, qual) }
	fn := func(name, doc string, sig *types.Signature) FuncSym {
		f := FuncSym{Name: name, Doc: doc, Variadic: sig.Variadic(), TypeParams: typeParams(sig.TypeParams(), qual)}
		for i := 0; i < sig.Params().Len(); i++ {
			v := sig.Params().At(i)
			t := str(v.Type())
			if sig.Variadic() && i == sig.Params().Len()-1 {
				t = "..." + str(v.Type().(*types.Slice).Elem())
			}
			f.Params = append(f.Params, ParamSym{Name: v.Name(), Type: t})
		}
		for i := 0; i < sig.Results().Len(); i++ {
			f.Results = append(f.Results, str(sig.Results().At(i).Type()))
		}
		return f
	}

	scope := p.Types.Scope()
	var ifaces, concrete []*types.TypeName
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}
		switch o := obj.(type) {
		case *types.TypeName:
			if types.IsInterface(o.Type()) {
				ifaces = append(ifaces, o)
			} else {
				concrete = append(concrete, o)
			}
		case *types.Func:
			out.Funcs = append(out.Funcs, fn(o.Name(), docs[o.Name()], o.Type().(*types.Signature)))
		case *types.Var:
			out.Values = append(out.Values, ValueSym{Name: o.Name(), Doc: docs[o.Name()], Kind: "var", Type: str(o.Type()), IsError: isErrorType(o.Type())})
		case *types.Const:
			out.Values = append(out.Values, ValueSym{Name: o.Name(), Doc: docs[o.Name()], Kind: "const", Type: str(o.Type()), Value: o.Val().String()})
		}
	}

	generic := func(o *types.TypeName) bool {
		n, ok := types.Unalias(o.Type()).(*types.Named)
		return ok && n.TypeParams().Len() > 0
	}
	satisfies := func(t *types.TypeName, i *types.TypeName) bool {
		iface := i.Type().Underlying().(*types.Interface)
		if generic(t) || generic(i) || iface.NumMethods() == 0 || !iface.IsMethodSet() {
			return false
		}
		return types.Implements(t.Type(), iface) || types.Implements(types.NewPointer(t.Type()), iface)
	}

	for _, o := range ifaces {
		iface := o.Type().Underlying().(*types.Interface)
		s := InterfaceSym{Name: o.Name(), Doc: docs[o.Name()], Constraint: !iface.IsMethodSet()}
		if n, ok := types.Unalias(o.Type()).(*types.Named); ok {
			s.TypeParams = typeParams(n.TypeParams(), qual)
		}
		for i := 0; i < iface.NumMethods(); i++ {
			if m := iface.Method(i); m.Exported() {
				s.Methods = append(s.Methods, fn(m.Name(), docs[o.Name()+"."+m.Name()], m.Type().(*types.Signature)))
			}
		}
		for _, t := range concrete {
			if satisfies(t, o) {
				s.ImplementedBy = append(s.ImplementedBy, t.Name())
			}
		}
		out.Interfaces = append(out.Interfaces, s)
	}

	for _, o := range concrete {
		s := TypeSym{Name: o.Name(), Doc: docs[o.Name()], IsError: !generic(o) && isErrorType(o.Type())}
		if o.IsAlias() {
			s.Alias = str(types.Unalias(o.Type()))
		}
		if n, ok := types.Unalias(o.Type()).(*types.Named); ok {
			s.TypeParams = typeParams(n.TypeParams(), qual)
		}
		switch u := o.Type().Underlying().(type) {
		case *types.Struct:
			s.Kind = "struct"
			for i := 0; i < u.NumFields(); i++ {
				if f := u.Field(i); f.Exported() {
					s.Fields = append(s.Fields, ParamSym{Name: f.Name(), Type: str(f.Type())})
				}
			}
		case *types.Signature:
			s.Kind, s.Underlying = "func", str(u)
		case *types.Map:
			s.Kind, s.Underlying = "map", str(u)
		case *types.Slice:
			s.Kind, s.Underlying = "slice", str(u)
		case *types.Array:
			s.Kind, s.Underlying = "array", str(u)
		case *types.Chan:
			s.Kind, s.Underlying = "chan", str(u)
		case *types.Pointer:
			s.Kind, s.Underlying = "pointer", str(u)
		case *types.Basic:
			s.Kind = u.Name()
		default:
			s.Kind, s.Underlying = "other", str(u)
		}
		if !o.IsAlias() {
			ms := types.NewMethodSet(types.NewPointer(o.Type()))
			for i := 0; i < ms.Len(); i++ {
				if m, ok := ms.At(i).Obj().(*types.Func); ok && m.Exported() {
					s.Methods = append(s.Methods, fn(m.Name(), docs[o.Name()+"."+m.Name()], ms.At(i).Type().(*types.Signature)))
				}
			}
		}
		for _, i := range ifaces {
			if satisfies(o, i) {
				s.Implements = append(s.Implements, i.Name())
			}
		}
		out.Types = append(out.Types, s)
	}

	// A constructor is a function that hands back the type, or a pointer to it.
	owner := map[string]int{}
	for i, t := range out.Types {
		owner[t.Name] = i
	}
	for _, name := range scope.Names() {
		f, ok := scope.Lookup(name).(*types.Func)
		if !ok || !f.Exported() {
			continue
		}
		res := f.Type().(*types.Signature).Results()
		seen := map[string]bool{}
		for i := 0; i < res.Len(); i++ {
			t := res.At(i).Type()
			if ptr, ok := t.(*types.Pointer); ok {
				t = ptr.Elem()
			}
			n, ok := types.Unalias(t).(*types.Named)
			if !ok || n.Obj().Pkg() != p.Types || seen[n.Obj().Name()] {
				continue
			}
			seen[n.Obj().Name()] = true
			if at, ok := owner[n.Obj().Name()]; ok {
				out.Types[at].Constructors = append(out.Types[at].Constructors, f.Name())
			}
		}
	}
	return out
}

func typeParams(l *types.TypeParamList, qual types.Qualifier) string {
	if l == nil || l.Len() == 0 {
		return ""
	}
	parts := make([]string, l.Len())
	for i := range parts {
		parts[i] = l.At(i).Obj().Name() + " " + types.TypeString(l.At(i).Constraint(), qual)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// firstSentence is the opening of a doc comment: the contract, not the prose.
func firstSentence(doc *ast.CommentGroup) string {
	if doc == nil {
		return ""
	}
	text := strings.Join(strings.Fields(doc.Text()), " ")
	if i := strings.Index(text, ". "); i >= 0 {
		text = text[:i+1]
	}
	if len(text) > 200 {
		text = text[:197] + "..."
	}
	return text
}

func packageDoc(p *packages.Package) string {
	for _, f := range p.Syntax {
		if s := firstSentence(f.Doc); s != "" {
			return s
		}
	}
	return ""
}

// symbolDocs maps Name, and Type.Method, to the first sentence of its doc.
func symbolDocs(p *packages.Package) map[string]string {
	out := map[string]string{}
	add := func(name string, doc *ast.CommentGroup) {
		if s := firstSentence(doc); s != "" && name != "" {
			out[name] = s
		}
	}
	for _, f := range p.Syntax {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					add(d.Name.Name, d.Doc)
				} else if len(d.Recv.List) == 1 {
					add(receiverName(d.Recv.List[0].Type)+"."+d.Name.Name, d.Doc)
				}
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					switch s := sp.(type) {
					case *ast.TypeSpec:
						doc := s.Doc
						if doc == nil {
							doc = d.Doc
						}
						add(s.Name.Name, doc)
						if it, ok := s.Type.(*ast.InterfaceType); ok && it.Methods != nil {
							for _, m := range it.Methods.List {
								for _, n := range m.Names {
									add(s.Name.Name+"."+n.Name, m.Doc)
								}
							}
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							doc := s.Doc
							if doc == nil {
								doc = d.Doc
							}
							add(n.Name, doc)
						}
					}
				}
			}
		}
	}
	return out
}

func receiverName(e ast.Expr) string {
	for {
		switch t := e.(type) {
		case *ast.StarExpr:
			e = t.X
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		case *ast.ParenExpr:
			e = t.X
		case *ast.Ident:
			return t.Name
		default:
			return ""
		}
	}
}

// maxSymbolPackages bounds how many of a module's packages are read.
const maxSymbolPackages = 12

// goRun runs the go tool in dir and returns what it printed.
func goRun(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.LastIndex(msg, "\n"); i >= 0 && len(msg) > 400 {
			msg = msg[i+1:]
		}
		return nil, fmt.Errorf("go %s: %s", strings.Join(args, " "), msg)
	}
	return out, nil
}

// moduleSymbols resolves a module at a version (or "latest" when it is
// empty) and reads the packages a program would import from it. It runs
// the go tool, which fetches the module if it is not already on this
// machine; nothing from the module is run.
func moduleSymbols(mod, version string) (*ModuleSymbols, error) {
	if err := module.CheckPath(mod); err != nil {
		return nil, fmt.Errorf("%q is not a module path: %v", mod, err)
	}
	if version == "" {
		version = "latest"
	}
	if strings.ContainsAny(version, " \t\n@") || strings.HasPrefix(version, "-") {
		return nil, fmt.Errorf("%q is not a version", version)
	}
	if _, err := exec.LookPath("go"); err != nil {
		return nil, errors.New("reading a module's symbols needs the go tool on PATH, and it is not there")
	}
	dir, err := os.MkdirTemp("", "tilegen-symbols-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module tilegen.invalid/symbols\n"), 0o644); err != nil {
		return nil, err
	}
	if _, err := goRun(dir, "get", mod+"@"+version); err != nil {
		return nil, fmt.Errorf("cannot fetch %s@%s: %w", mod, version, err)
	}
	raw, err := goRun(dir, "list", "-m", "-json", mod)
	if err != nil {
		return nil, err
	}
	var info struct{ Version string }
	if err := json.Unmarshal(raw, &info); err != nil || info.Version == "" {
		return nil, fmt.Errorf("go list did not say which version of %s it resolved", mod)
	}
	return readModule(dir, mod, info.Version, nil, append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off"))
}

// readModule reads a module's packages from a directory whose go.mod
// requires it (or is its own). With only set, it reads exactly those
// import paths. A nil env is this process's: nothing in dir is changed.
func readModule(dir, mod, version string, only []string, env []string) (*ModuleSymbols, error) {
	out := &ModuleSymbols{Module: mod, Version: version}
	paths := only
	if len(paths) == 0 {
		listed, err := packages.Load(&packages.Config{Mode: packages.NeedName, Dir: dir, Env: env}, mod+"/...")
		if err != nil {
			return nil, fmt.Errorf("cannot list the packages of %s: %v", mod, err)
		}
		for _, p := range listed {
			if importable(mod, p.PkgPath, p.Name) {
				paths = append(paths, p.PkgPath)
			}
		}
		// The module's own package first, then the nearest ones.
		sort.Slice(paths, func(i, j int) bool {
			di, dj := strings.Count(paths[i], "/"), strings.Count(paths[j], "/")
			if di != dj {
				return di < dj
			}
			return paths[i] < paths[j]
		})
		if len(paths) > maxSymbolPackages {
			out.NotRead = append(out.NotRead, paths[maxSymbolPackages:]...)
			paths = paths[:maxSymbolPackages]
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("%s has no package a program can import", mod)
	}
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo, Dir: dir, Env: env}
	pkgs, err := packages.Load(cfg, paths...)
	if err != nil {
		return nil, fmt.Errorf("cannot load %s: %v", mod, err)
	}
	sort.Slice(pkgs, func(i, j int) bool { return indexOf(paths, pkgs[i].PkgPath) < indexOf(paths, pkgs[j].PkgPath) })
	for _, p := range pkgs {
		if p.Types == nil || len(p.Errors) > 0 {
			why := "it does not type-check"
			if len(p.Errors) > 0 {
				why = p.Errors[0].Msg
			}
			out.NotRead = append(out.NotRead, p.PkgPath+" ("+why+")")
			continue
		}
		out.Packages = append(out.Packages, packageSymbols(p))
	}
	if len(out.Packages) == 0 {
		return nil, fmt.Errorf("no package of %s type-checks: %s", mod, strings.Join(out.NotRead, "; "))
	}
	return out, nil
}

func indexOf(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return len(list)
}

// importable reports whether a program outside the module could import
// the package and would want to: not internal, not a command, not a test
// helper or an example.
func importable(mod, path, name string) bool {
	if name == "main" || name == "" || strings.HasSuffix(name, "_test") {
		return false
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(path, mod), "/")
	for _, el := range strings.Split(rel, "/") {
		switch el {
		case "internal", "testdata", "examples", "example", "test", "tests", "testing", "mock", "mocks", "bench", "benchmark", "benchmarks", "tools", "scripts":
			return false
		}
		if strings.HasPrefix(el, "_") || strings.HasPrefix(el, ".") {
			return false
		}
	}
	return true
}

// moduleSymbolsForCmd is the command's own reader: it filters what it
// reads, so it never shares an answer with anyone.
var moduleSymbolsForCmd = moduleSymbols

// tilegen symbols MODULE[@VERSION]...
func symbolsCmd(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("symbols", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the symbols as JSON")
	kind := fs.String("kind", "", "only these kinds, comma-separated: "+strings.Join(symbolKinds, ", "))
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: tilegen symbols [-json] [-kind KINDS] MODULE[@VERSION]...\n\nWhat a Go module exports, read by the type checker: each interface with its whole\nmethod set and the types that satisfy it, each type with its methods and\nconstructors, and the sentinel errors. The module is fetched with the go tool if it\nis not on this machine; nothing from it is run.")
		fs.PrintDefaults()
	}
	mods := parseAnywhere(fs, args)
	if len(mods) == 0 {
		fs.Usage()
		return errors.New("no module given")
	}
	kinds, err := parseKinds(*kind)
	if err != nil {
		return err
	}
	var all []*ModuleSymbols
	for _, arg := range mods {
		mod, version, _ := strings.Cut(arg, "@")
		m, err := moduleSymbolsForCmd(mod, version)
		if err != nil {
			return err
		}
		m.only(kinds)
		all = append(all, m)
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(all)
	}
	for _, m := range all {
		fmt.Fprintln(stdout, strings.TrimRight(m.text(1<<30), "\n"))
	}
	return nil
}

func parseKinds(s string) ([]string, error) {
	var out []string
	for _, k := range strings.Split(s, ",") {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if !contains(symbolKinds, k) {
			return nil, fmt.Errorf("-kind takes %s; got %q%s", strings.Join(symbolKinds, ", "), k, didYouMean(k, symbolKinds))
		}
		out = append(out, k)
	}
	return out, nil
}
