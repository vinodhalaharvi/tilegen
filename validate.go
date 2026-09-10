package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// Ctx is shared state across passes.
type Ctx struct {
	Cfg      Config
	Strict   bool // uncovered forms are errors instead of LLM tasks
	Warnings []string

	// Filled in by the select pass while it walks the project.
	Module   string
	Requires map[string]string // qualifier -> import path
	Local    map[string]string // project package name -> import path
	Pkg      *PkgScope
	seq      int
}

// PkgScope is the symbol table of the package being tiled, so big tiles
// (like impl) can look up the interface and struct they implement.
type PkgScope struct {
	Name, Dir string
	Structs   map[string]*Node
	Ifaces    map[string]*Node
}

func (c *Ctx) warn(p Pos, format string, a ...any) {
	c.Warnings = append(c.Warnings, fmt.Sprintf("%s: %s", p, fmt.Sprintf(format, a...)))
}

var storeOps = []string{"get", "list", "save", "delete"}

// Validate checks the spec's shape before any lowering. Everything here
// reuses real tooling: x/mod for module paths and versions, go/parser for
// type expressions, go/token for identifiers. It reports all errors at once.
func Validate(forms []*Node, c *Ctx) error {
	v := &validator{c: c}
	projects := 0
	for _, f := range forms {
		if f.Head() == "project" {
			projects++
			v.project(f)
		} else {
			v.bad(f, "top-level forms must be (project ...), got %s", short(f))
		}
	}
	if projects != 1 {
		v.errs = append(v.errs, fmt.Errorf("spec must contain exactly one (project ...) form, found %d", projects))
	}
	return errors.Join(v.errs...)
}

type validator struct {
	c    *Ctx
	errs []error
}

func (v *validator) bad(n *Node, format string, a ...any) {
	v.errs = append(v.errs, fmt.Errorf("%s: %s", n.Pos, fmt.Sprintf(format, a...)))
}

func (v *validator) shape(n *Node, pat string) Bindings {
	b := Bindings{}
	if !Match(Pat(pat), n, b) {
		v.bad(n, "expected %s, got %s", pat, short(n))
		return nil
	}
	return b
}

func (v *validator) ident(n *Node, what string, mustExport bool) {
	switch {
	case n == nil:
	case n.IsList || !token.IsIdentifier(n.Atom):
		v.bad(n, "%s must be a Go identifier, got %s", what, n.Flat())
	case mustExport && !exported(n.Atom):
		v.bad(n, "%s %q must be exported (start with an upper-case letter)", what, n.Atom)
	}
}

func (v *validator) project(p *Node) {
	b := v.shape(p, "(project ?name ?items...)")
	if b == nil {
		return
	}
	v.ident(b.One("name"), "project name", false)
	counts := map[string]int{}
	pkgs := map[string]bool{}
	for _, it := range b.Rest("items") {
		counts[it.Head()]++
		switch it.Head() {
		case "module":
			if mb := v.shape(it, "(module ?path)"); mb != nil {
				if err := module.CheckPath(mb.Atom("path")); err != nil {
					v.bad(it, "invalid module path: %v", err)
				}
			}
		case "go":
			if gb := v.shape(it, "(go ?version)"); gb != nil && !modfile.GoVersionRE.MatchString(gb.Atom("version")) {
				v.bad(it, "invalid go version %q", gb.Atom("version"))
			}
		case "require":
			for _, r := range it.Args() {
				v.require(r)
			}
		case "package":
			v.pkg(it, pkgs)
		default:
			v.bad(it, "unknown project item %s (want module, go, require, package)", short(it))
		}
	}
	for _, k := range []string{"module", "go"} {
		if counts[k] != 1 {
			v.bad(p, "project needs exactly one (%s ...), found %d", k, counts[k])
		}
	}
	if counts["package"] == 0 {
		v.bad(p, "project needs at least one (package ...)")
	}
}

func (v *validator) require(r *Node) {
	b := v.shape(r, "(?alias ?path ?version ?importpath...)")
	if b == nil {
		return
	}
	v.ident(b.One("alias"), "require alias", false)
	path := b.Atom("path")
	if err := module.CheckPath(path); err != nil {
		v.bad(r, "invalid module path: %v", err)
	}
	if ver := b.Atom("version"); !semver.IsValid(ver) {
		v.bad(r, "invalid version %q (want semver like v1.6.0)", ver)
	}
	if imp := b.Rest("importpath"); len(imp) > 1 || (len(imp) == 1 && !strings.HasPrefix(imp[0].Atom, path)) {
		v.bad(r, "optional import path must be inside module %s", path)
	}
}

func (v *validator) pkg(p *Node, seen map[string]bool) {
	b := v.shape(p, "(package ?name ?items...)")
	if b == nil {
		return
	}
	name := b.One("name")
	v.ident(name, "package name", false)
	if name.Atom != strings.ToLower(name.Atom) {
		v.bad(name, "package name %q should be lower case", name.Atom)
	}
	if name.Atom == "db" && v.c.Cfg.Storage == "postgres" {
		v.bad(name, `package name "db" is reserved for sqlc output when storage is postgres`)
	}
	if seen[name.Atom] {
		v.bad(name, "duplicate package %q", name.Atom)
	}
	seen[name.Atom] = true
	types := map[string]bool{}
	for _, it := range b.Rest("items") {
		switch it.Head() {
		case "entity", "struct":
			v.structLike(it, types)
		case "interface":
			v.iface(it, types)
		case "doc", "llm":
			v.shape(it, "(_ ?text ?more...)")
		default:
			if v.c.Strict {
				v.bad(it, "no tile covers %s (strict mode)", short(it))
			}
			// Otherwise the select pass's catch-all turns it into an LLM task.
		}
	}
}

func (v *validator) declName(n *Node, types map[string]bool) {
	v.ident(n, "type name", true)
	if n != nil && !n.IsList {
		if types[n.Atom] {
			v.bad(n, "duplicate type %q", n.Atom)
		}
		types[n.Atom] = true
	}
}

func (v *validator) structLike(s *Node, types map[string]bool) {
	b := v.shape(s, "(_ ?name ?items...)")
	if b == nil {
		return
	}
	v.declName(b.One("name"), types)
	fields := map[string]bool{}
	for _, it := range b.Rest("items") {
		switch it.Head() {
		case "field":
			fb := v.shape(it, "(field ?name ?type ?opts...)")
			if fb == nil {
				continue
			}
			v.ident(fb.One("name"), "field name", false)
			if fields[fb.Atom("name")] {
				v.bad(it, "duplicate field %q", fb.Atom("name"))
			}
			fields[fb.Atom("name")] = true
			v.typ(fb.One("type"))
			for _, o := range fb.Rest("opts") {
				if o.Head() != "tag" && o.Head() != "doc" {
					v.bad(o, "field options are (tag \"...\") and (doc \"...\"), got %s", short(o))
				}
			}
		case "doc":
			v.shape(it, "(doc ?text)")
		case "store":
			if s.Head() != "entity" {
				v.bad(it, "(store ...) is only allowed inside (entity ...)")
				continue
			}
			if !fields["ID"] {
				v.bad(it, "(store ...) needs an ID field declared before it")
			}
			for _, op := range it.Args() {
				if op.IsList {
					v.shape(op, "(constraint ?text)")
				} else if !contains(storeOps, op.Atom) {
					v.bad(op, "unknown store op %q (want %s)", op.Atom, strings.Join(storeOps, ", "))
				}
			}
		default:
			v.bad(it, "unexpected %s in %s", short(it), s.Head())
		}
	}
}

func (v *validator) iface(i *Node, types map[string]bool) {
	b := v.shape(i, "(interface ?name ?items...)")
	if b == nil {
		return
	}
	v.declName(b.One("name"), types)
	for _, it := range b.Rest("items") {
		switch it.Head() {
		case "doc":
			v.shape(it, "(doc ?text)")
		case "method":
			v.method(it)
		default:
			v.bad(it, "unexpected %s in interface", short(it))
		}
	}
}

func (v *validator) method(m *Node) {
	b := v.shape(m, "(method ?name ?parts...)")
	if b == nil {
		return
	}
	v.ident(b.One("name"), "method name", false)
	for _, part := range b.Rest("parts") {
		switch part.Head() {
		case "params":
			for _, p := range part.Args() {
				if pb := v.shape(p, "(?name ?type)"); pb != nil {
					v.ident(pb.One("name"), "parameter name", false)
					v.typ(pb.One("type"))
				}
			}
		case "returns":
			for _, t := range part.Args() {
				v.typ(t)
			}
		case "doc":
			v.shape(part, "(doc ?text)")
		default:
			v.bad(part, "method parts are (params ...), (returns ...), (doc ...), got %s", short(part))
		}
	}
}

func (v *validator) typ(n *Node) {
	if n == nil {
		return
	}
	if n.IsList {
		v.bad(n, "type must be an atom or string, got %s (quote types that contain spaces or parens)", n.Flat())
		return
	}
	if _, err := typeQualifiers(n.Atom); err != nil {
		v.bad(n, "%v", err)
	}
}

// typeQualifiers parses a Go type expression with go/parser - no hand-written
// type grammar - and returns the package qualifiers it uses (uuid in uuid.UUID).
func typeQualifiers(src string) ([]string, error) {
	e, err := parser.ParseExpr(src)
	if err != nil || !isType(e) {
		return nil, fmt.Errorf("invalid Go type %q", src)
	}
	var qs []string
	ast.Inspect(e, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := s.X.(*ast.Ident); ok {
				qs = append(qs, id.Name)
			}
		}
		return true
	})
	return qs, nil
}

func isType(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.Ident, *ast.ArrayType, *ast.MapType, *ast.ChanType,
		*ast.FuncType, *ast.InterfaceType, *ast.StructType:
		return true
	case *ast.StarExpr:
		return isType(t.X)
	case *ast.ParenExpr:
		return isType(t.X)
	case *ast.SelectorExpr:
		_, ok := t.X.(*ast.Ident)
		return ok
	case *ast.IndexExpr: // generic instantiation: List[int]
		return isType(t.X)
	case *ast.IndexListExpr:
		return isType(t.X)
	}
	return false
}

func short(n *Node) string {
	s := n.Flat()
	if len(s) > 60 {
		s = s[:57] + "..."
	}
	return s
}
