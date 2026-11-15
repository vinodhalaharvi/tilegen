package compile

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/vinodhalaharvi/tilegen/internal/sexp"
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
	Requires map[string]string    // qualifier -> import path
	Local    map[string]string    // project package name -> import path
	PkgDirs  map[string]string    // project package name -> directory
	Enums    map[string][]string  // "pkg.Type" -> values, for SQL CHECK constraints
	Cover    map[string]*Covering // need ID -> the tile chosen for it
	Used     []*Offer             // every tile some need uses, by tile name
	curPkg   string               // package being concretized
	Lock     map[string]LockEntry // tilegen.lock: pinned backend choices
	Reselect bool                 // ignore the lock
	App      string               // (app): the command to wire under cmd/, or ""
	Extra    []*Offer             // drafts: tiles declared for this compile only
	Pins     map[string]*ProtoPin // tilegen.lock: pinned proto numbers
	PinsOut  map[string]*ProtoPin // the numbers this run handed out
	Policy   *Policy              // what this team values (policy.go)
	Pkg      *PkgScope
	seq      int
}

// PkgScope is the symbol table of the package being tiled, so big tiles
// (like impl) can look up the interface and struct they implement.
type PkgScope struct {
	Name, Dir string
	Structs   map[string]*sexp.Node
	Ifaces    map[string]*sexp.Node
	Events    map[string]*sexp.Node // the package's (event ...) forms, by name
}

func (c *Ctx) warn(p sexp.Pos, format string, a ...any) {
	c.Warnings = append(c.Warnings, fmt.Sprintf("%s: %s", p, fmt.Sprintf(format, a...)))
}

// The names each context knows, for did-you-mean suggestions.
var (
	projectItems = []string{"module", "go", "require", "package", "repo", "app", "doc"}
	packageForms = []string{"entity", "struct", "interface", "implement", "enum", "doc", "llm", "dir"}
)

func allStoreOps() []string {
	return append(append(append([]string{"method", "constraint"}, storeOps...), fieldOps...), KnownRequirements()...)
}

// Validate checks the spec's shape before any lowering. Everything here
// reuses real tooling: x/mod for module paths and versions, go/parser for
// type expressions, go/token for identifiers. It reports all errors at once.
func Validate(forms []*sexp.Node, c *Ctx) error {
	v := &Validator{c: c}
	projects := 0
	for _, f := range forms {
		if f.Head() == "project" {
			projects++
			v.project(f)
		} else {
			v.Bad(f, "top-level forms must be (project ...), got %s", Short(f))
		}
	}
	if projects != 1 {
		v.errs = append(v.errs, fmt.Errorf("spec must contain exactly one (project ...) form, found %d", projects))
	}
	return errors.Join(v.errs...)
}

type Validator struct {
	c         *Ctx
	errs      []error
	goVersion string // the project's (go ...), for tiles that need a newer Go

	pkgNames map[string]bool                  // every project package, known up front
	cur      string                           // package being validated
	deps     map[string]map[string]*sexp.Node // pkg -> referenced pkg -> first type mentioning it
	order    []string                         // package declaration order, for stable reports
}

func (v *Validator) Bad(n *sexp.Node, format string, a ...any) {
	v.errs = append(v.errs, fmt.Errorf("%s: %s", n.Pos, fmt.Sprintf(format, a...)))
}

func (v *Validator) Shape(n *sexp.Node, pat string) sexp.Bindings {
	b := sexp.Bindings{}
	if !sexp.Match(sexp.Pat(pat), n, b) {
		v.Bad(n, "expected %s, got %s", pat, Short(n))
		return nil
	}
	return b
}

func (v *Validator) ident(n *sexp.Node, what string, mustExport bool) {
	switch {
	case n == nil:
	case n.IsList || !token.IsIdentifier(n.Atom):
		v.Bad(n, "%s must be a Go identifier, got %s", what, n.Flat())
	case mustExport && !exported(n.Atom):
		v.Bad(n, "%s %q must be exported (start with an upper-case letter)", what, n.Atom)
	}
}

func (v *Validator) project(p *sexp.Node) {
	b := v.Shape(p, "(project ?name ?items...)")
	if b == nil {
		return
	}
	if n := b.One("name"); n.IsList || !repoNameRE.MatchString(n.Atom) {
		v.Bad(n, "project name must be letters, digits, '.', '_' or '-', got %s", n.Flat())
	}
	counts := map[string]int{}
	pkgs := map[string]bool{}
	v.pkgNames, v.deps = map[string]bool{}, map[string]map[string]*sexp.Node{}
	for _, it := range b.Rest("items") {
		if it.Head() == "package" && len(it.Args()) > 0 && !it.Args()[0].IsList {
			v.pkgNames[it.Args()[0].Atom] = true
		}
	}
	for _, it := range b.Rest("items") {
		counts[it.Head()]++
		switch it.Head() {
		case "module":
			if mb := v.Shape(it, "(module ?path)"); mb != nil {
				if err := module.CheckPath(mb.Atom("path")); err != nil {
					v.Bad(it, "invalid module path: %v", err)
				}
			}
		case "go":
			if gb := v.Shape(it, "(go ?version)"); gb != nil && !modfile.GoVersionRE.MatchString(gb.Atom("version")) {
				v.Bad(it, "invalid go version %q", gb.Atom("version"))
			} else if gb != nil {
				v.goVersion = gb.Atom("version")
			}
		case "require":
			for _, r := range it.Args() {
				v.require(r)
			}
		case "package":
			v.pkg(it, pkgs)
		case "repo":
			v.repo(it)
		case "app":
			if len(it.List) > 2 || (len(it.List) == 2 && (it.List[1].IsList || !appName.MatchString(it.List[1].Atom))) {
				v.Bad(it, "expected (app) or (app name), where name is the command to build under cmd/, got %s", Short(it))
			}
		case "doc":
			v.Shape(it, "(doc ?text)")
		default:
			v.Bad(it, "unknown project item %s%s (want module, go, require, package, repo, app, doc)", Short(it), DidYouMean(it.Head(), projectItems))
		}
	}
	switch {
	case counts["module"] == 0:
		v.Bad(p, "project needs (module ...), or (repo (github owner/name)) to derive it")
	case counts["module"] > 1:
		v.Bad(p, "project needs exactly one (module ...), found %d", counts["module"])
	}
	if counts["go"] != 1 {
		v.Bad(p, "project needs exactly one (go ...), found %d", counts["go"])
	}
	if counts["repo"] > 1 {
		v.Bad(p, "project has %d (repo ...) forms; want at most one", counts["repo"])
	}
	if counts["package"] == 0 {
		v.Bad(p, "project needs at least one (package ...)")
	}
	v.cycles()
}

// cycles reports import cycles between project packages at spec level, so
// the error points at the type that closes the loop instead of at
// generated files during go build.
func (v *Validator) cycles() {
	const (
		unvisited = iota
		active
		done
	)
	state := map[string]int{}
	var stack []string
	var visit func(pkg string) bool
	visit = func(pkg string) bool {
		state[pkg] = active
		stack = append(stack, pkg)
		for _, dep := range SortedKeys(v.deps[pkg]) {
			switch state[dep] {
			case active:
				i := len(stack) - 1
				for stack[i] != dep {
					i--
				}
				loop := append(append([]string{}, stack[i:]...), dep)
				v.Bad(v.deps[pkg][dep], "import cycle between project packages: %s (Go forbids import cycles; move the shared type into one package)",
					strings.Join(loop, " -> "))
				return true
			case unvisited:
				if visit(dep) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[pkg] = done
		return false
	}
	for _, pkg := range v.order {
		if state[pkg] == unvisited && visit(pkg) {
			return // one cycle at a time keeps the message readable
		}
	}
}

func SortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (v *Validator) require(r *sexp.Node) {
	b := v.Shape(r, "(?alias ?path ?version ?importpath...)")
	if b == nil {
		return
	}
	v.ident(b.One("alias"), "require alias", false)
	path := b.Atom("path")
	if err := module.CheckPath(path); err != nil {
		v.Bad(r, "invalid module path: %v", err)
	}
	if ver := b.Atom("version"); !semver.IsValid(ver) {
		v.Bad(r, "invalid version %q (want semver like v1.6.0)", ver)
	}
	if imp := b.Rest("importpath"); len(imp) > 1 || (len(imp) == 1 && !strings.HasPrefix(imp[0].Atom, path)) {
		v.Bad(r, "optional import path must be inside module %s", path)
	}
}

func (v *Validator) pkg(p *sexp.Node, seen map[string]bool) {
	b := v.Shape(p, "(package ?name ?items...)")
	if b == nil {
		return
	}
	name := b.One("name")
	v.ident(name, "package name", false)
	if name.Atom != strings.ToLower(name.Atom) {
		v.Bad(name, "package name %q should be lower case", name.Atom)
	}
	if seen[name.Atom] {
		v.Bad(name, "duplicate package %q", name.Atom)
	}
	seen[name.Atom] = true
	v.cur = name.Atom
	v.order = append(v.order, name.Atom)
	types := map[string]bool{}
	ifaces := packageInterfaces(b.Rest("items"))
	for _, it := range b.Rest("items") {
		if pt, ok := packageTiles[it.Head()]; ok {
			pt.Validate(v, it, types)
			continue
		}
		switch it.Head() {
		case "entity", "struct":
			v.structLike(it, types)
		case "interface":
			v.iface(it, types)
		case "implement":
			v.implement(it, ifaces, types)
		case "enum":
			v.enum(it, types)
		case "doc", "llm":
			v.Shape(it, "(_ ?text ?more...)")
		case "dir":
			// A package may say where it goes, overriding (layout ...).
			// "." generates into the root of the module at <out>.
			v.Shape(it, "(dir ?path)")
			if len(it.List) == 2 && !localDir(it.List[1].Atom) {
				v.Bad(it, "%s leaves the project: a package's folder is a path inside <out>, such as \"internal/orders\" or \".\"", Short(it))
			}
		default:
			if s := Closest(it.Head(), knownPackageForms()); s != "" {
				// A likely typo is an error: it must not quietly become an LLM task.
				v.Bad(it, "unknown form %s (did you mean %s?)", Short(it), s)
			} else if v.c.Strict {
				v.Bad(it, "no tile covers %s (strict mode)", Short(it))
			}
			// Otherwise the select pass's catch-all turns it into an LLM task.
		}
	}
}

func (v *Validator) declName(n *sexp.Node, types map[string]bool) {
	v.ident(n, "type name", true)
	if n != nil && !n.IsList {
		if types[n.Atom] {
			v.Bad(n, "duplicate type %q", n.Atom)
		}
		types[n.Atom] = true
	}
}

func (v *Validator) structLike(s *sexp.Node, types map[string]bool) {
	b := v.Shape(s, "(_ ?name ?items...)")
	if b == nil {
		return
	}
	v.declName(b.One("name"), types)
	fields := map[string]bool{}
	for _, it := range b.Rest("items") {
		switch it.Head() {
		case "field":
			fb := v.Shape(it, "(field ?name ?type ?opts...)")
			if fb == nil {
				continue
			}
			v.ident(fb.One("name"), "field name", false)
			if fields[fb.Atom("name")] {
				v.Bad(it, "duplicate field %q", fb.Atom("name"))
			}
			fields[fb.Atom("name")] = true
			v.typ(fb.One("type"))
			for _, o := range fb.Rest("opts") {
				if o.Head() != "tag" && o.Head() != "doc" {
					v.Bad(o, "field options are (tag \"...\") and (doc \"...\"), got %s%s", Short(o), DidYouMean(o.Head(), []string{"tag", "doc"}))
				}
			}
		case "doc":
			v.Shape(it, "(doc ?text)")
		case "store":
			if s.Head() != "entity" {
				v.Bad(it, "(store ...) is only allowed inside (entity ...)")
				continue
			}
			if !fields["ID"] {
				v.Bad(it, "(store ...) needs an ID field declared before it")
			}
			methods := map[string]bool{}
			for _, op := range it.Args() {
				var name string
				switch kind := op.Head(); {
				case !op.IsList:
					if !Contains(storeOps, op.Atom) {
						v.Bad(op, "unknown store op %q%s (want %s, or one of (%s F), (method ...), (constraint ...))",
							op.Atom, DidYouMean(op.Atom, allStoreOps()), strings.Join(storeOps, ", "), strings.Join(fieldOps, " F), ("))
						continue
					}
					name = opMethodName(op.Atom, "")
				case kind == "constraint":
					v.Shape(op, "(constraint ?text)")
					continue
				case Contains(KnownRequirements(), kind):
					if v.Shape(op, "(_)") != nil { // a requirement, like (durable)
						validateRequirements("store", []string{kind}, op, v)
					}
					continue
				case kind == "method":
					v.method(op)
					if len(op.List) > 1 && !op.List[1].IsList {
						name = op.List[1].Atom
					}
				case Contains(fieldOps, kind):
					b := v.Shape(op, "(_ ?field)")
					if b == nil {
						continue
					}
					if f := b.Atom("field"); !fields[f] {
						v.Bad(op, "(%s %s): %s is not a field declared before the store%s", kind, f, f, DidYouMean(f, SortedKeys(fields)))
						continue
					}
					name = opMethodName(kind, b.Atom("field"))
				default:
					v.Bad(op, "unknown store op %s%s (want %s, or one of (%s F), (method ...), (constraint ...))",
						Short(op), DidYouMean(kind, allStoreOps()), strings.Join(storeOps, ", "), strings.Join(fieldOps, " F), ("))
					continue
				}
				if name != "" && methods[name] {
					v.Bad(op, "duplicate store method %s", name)
				}
				methods[name] = true
			}
		default:
			opts := []string{"field", "doc"}
			if s.Head() == "entity" {
				opts = append(opts, "store")
			}
			v.Bad(it, "unexpected %s in %s%s", Short(it), s.Head(), DidYouMean(it.Head(), opts))
		}
	}
}

func (v *Validator) iface(i *sexp.Node, types map[string]bool) {
	b := v.Shape(i, "(interface ?name ?items...)")
	if b == nil {
		return
	}
	v.declName(b.One("name"), types)
	for _, it := range b.Rest("items") {
		switch it.Head() {
		case "doc":
			v.Shape(it, "(doc ?text)")
		case "method":
			v.method(it)
		case "embed":
			if eb := v.Shape(it, "(embed ?type)"); eb != nil {
				v.typ(eb.One("type"))
			}
		default:
			v.Bad(it, "unexpected %s in interface (want method, embed, doc)%s", Short(it), DidYouMean(it.Head(), []string{"method", "embed", "doc"}))
		}
	}
}

func (v *Validator) method(m *sexp.Node) {
	b := v.Shape(m, "(method ?name ?parts...)")
	if b == nil {
		return
	}
	v.ident(b.One("name"), "method name", false)
	for _, part := range b.Rest("parts") {
		switch part.Head() {
		case "params":
			ps := part.Args()
			for i, p := range ps {
				pb := v.Shape(p, "(?name ?type)")
				if pb == nil {
					continue
				}
				v.ident(pb.One("name"), "parameter name", false)
				t := pb.One("type")
				if !t.IsList && strings.HasPrefix(t.Atom, "...") { // variadic
					if i != len(ps)-1 {
						v.Bad(t, "only the last parameter can be variadic")
						continue
					}
					t = &sexp.Node{Atom: strings.TrimPrefix(t.Atom, "..."), Pos: t.Pos}
				}
				v.typ(t)
			}
		case "returns":
			for _, t := range part.Args() {
				v.typ(t)
			}
		case "doc":
			v.Shape(part, "(doc ?text)")
		default:
			v.Bad(part, "method parts are (params ...), (returns ...), (doc ...), got %s%s", Short(part), DidYouMean(part.Head(), []string{"params", "returns", "doc"}))
		}
	}
}

func (v *Validator) typ(n *sexp.Node) {
	if n == nil {
		return
	}
	if n.IsList {
		v.Bad(n, "type must be an atom or string, got %s (quote types that contain spaces or parens)", n.Flat())
		return
	}
	if strings.HasPrefix(n.Atom, "...") {
		v.Bad(n, "only a method's last parameter can be variadic, got %s", n.Atom)
		return
	}
	qs, err := typeQualifiers(n.Atom)
	if err != nil {
		v.Bad(n, "%v", err)
		return
	}
	for _, q := range qs {
		switch {
		case q == v.cur:
			v.Bad(n, "inside package %s, write the type without its package qualifier (%s)",
				q, strings.ReplaceAll(n.Atom, q+".", ""))
		case v.pkgNames[q]:
			if v.deps[v.cur] == nil {
				v.deps[v.cur] = map[string]*sexp.Node{}
			}
			if v.deps[v.cur][q] == nil {
				v.deps[v.cur][q] = n
			}
		}
	}
}

// typeQualifiers parses a Go type expression with go/parser - no hand-written
// type grammar - and returns the package qualifiers it uses (uuid in uuid.UUID).
func typeQualifiers(src string) ([]string, error) {
	e, err := parser.ParseExpr(strings.TrimPrefix(src, "...")) // variadic: validated separately
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

func Short(n *sexp.Node) string {
	s := n.Flat()
	if len(s) > 60 {
		s = s[:57] + "..."
	}
	return s
}

var appName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// localDir reports whether a package's (dir ...) stays inside the project:
// relative, slash-separated, and never climbing out with "..".
func localDir(dir string) bool {
	return !strings.Contains(dir, `\`) && !strings.Contains(dir, ":") && fs.ValidPath(path.Clean(dir))
}
