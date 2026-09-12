package main

import (
	"flag"
	"fmt"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
)

// tilegen import [-out spec] DIR
//
// lifts an existing Go project into a spec. Everything it emits comes from
// the type checker (x/tools/go/packages, go/types), never from names or
// source text: structs and their field types, interfaces and their method
// sets, defined string types with typed constants (enums), and which
// concrete type implements which interface (types.Implements). Anything it
// cannot prove is skipped and listed in NOTES.md, so the spec it writes is
// exactly right about everything it does say.
//
// It is a starting point, not a round trip: store ops, needs and events
// are yours to add, because guessing them from method names would be the
// kind of rule that works on one codebase and not the next.

func importCmd(args []string, stdout, log io.Writer) error {
	fs := flag.NewFlagSet("tilegen import", flag.ExitOnError)
	out := fs.String("out", "spec", "where to write the spec, relative to DIR")
	force := fs.Bool("force", false, "overwrite an existing spec folder, and import even if some packages do not type-check")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: tilegen import [-out spec] [DIR]\n\nLifts an existing Go module into a spec: structs, interfaces, enums and\nimplementations, all from the type checker. What it cannot prove is listed\nin NOTES.md.\n\n")
		fs.PrintDefaults()
	}
	pos := parseAnywhere(fs, args)
	dir := "."
	if len(pos) > 0 {
		dir = pos[0]
	}
	specDir := filepath.Join(dir, *out)
	if _, err := os.Stat(specDir); err == nil && !*force {
		return fmt.Errorf("%s already exists; use -force to overwrite, or -out to write elsewhere", specDir)
	}
	imp, err := importProject(dir, *force, log)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		return err
	}
	for _, f := range imp.Files {
		if err := os.WriteFile(filepath.Join(specDir, f.Name), []byte(f.Text), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "  wrote  %s\n", filepath.Join(*out, f.Name))
	}
	fmt.Fprintf(stdout, "imported %d of %d exported type(s) in %d package(s)", imp.Imported, imp.Total, len(imp.Packages))
	if imp.Skipped > 0 {
		fmt.Fprintf(stdout, "; %d skipped (see %s)", imp.Skipped, filepath.Join(*out, "NOTES.md"))
	}
	fmt.Fprintf(stdout, "\nnext: tilegen check %s   # what the spec would regenerate, next to what you have\n", specDir)
	return nil
}

// SpecFile is one file of the written spec.
type SpecFile struct{ Name, Text string }

// Import is the result of lifting a project.
type Import struct {
	generated                map[string]bool // types declared in files a generator owns
	Files                    []SpecFile
	Packages                 []string
	Imported, Skipped, Total int
	Notes                    []string
}

func importProject(dir string, force bool, log io.Writer) (*Import, error) {
	modPath, goVer, requires, err := readGoMod(dir)
	if err != nil {
		return nil, err
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedModule,
		Dir:  dir,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", dir, err)
	}
	imp := &Import{}
	// Import lifts types, so the project must type-check first: a spec
	// lifted from a project the compiler rejects would be quietly wrong.
	// packages.Load type-checks exactly as go build does.
	var broken []string
	seen := map[string]bool{}
	for _, p := range pkgs {
		for _, e := range p.Errors {
			// packages reports the same problem from the go command ("-: #
			// pkg\nfile:line: msg") and from the type checker; keep the
			// type checker's, which has a real position.
			msg := strings.TrimSpace(e.Error())
			if strings.HasPrefix(msg, "-: ") {
				continue
			}
			if !seen[msg] {
				seen[msg] = true
				broken = append(broken, msg)
			}
		}
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no Go packages under %s (is it a Go module?)", dir)
	}
	if len(broken) > 0 {
		sort.Strings(broken)
		if !force {
			if len(broken) > 10 {
				broken = append(broken[:10], fmt.Sprintf("... and %d more", len(broken)-10))
			}
			return nil, fmt.Errorf("%s does not type-check, so its types cannot be trusted; fix these first (or use -force to import the packages that do check):\n  %s",
				dir, strings.Join(broken, "\n  "))
		}
		fmt.Fprintf(log, "warning: %d type error(s); importing only the packages that check\n", len(broken))
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].PkgPath < pkgs[j].PkgPath })

	used := map[string]string{} // qualifier -> module path, for (require ...)
	var body []SpecFile
	n := 0
	for _, p := range pkgs {
		if p.Types == nil || len(p.Errors) > 0 || isGenerated(p) || strings.Contains(p.PkgPath, "/internal/db") {
			continue // untyped, broken (with -force), or code a generator owns
		}
		imp.generated = tileOwnedTypes(p)
		if len(imp.generated) > 0 {
			var evs []string
			for _, n := range sortedKeys(imp.generated) {
				if n != "Bus" && n != "LocalBus" {
					evs = append(evs, n)
				}
			}
			imp.note(fmt.Sprintf("%s: Bus, LocalBus and its event types (%s) look like tilegen's event bus; add the form yourself:\n  (events (event %s (field ...)))",
				p.Types.Name(), strings.Join(evs, ", "), evs[0]))
		}
		f := importPackage(p, imp, used, requires)
		if f == "" {
			continue
		}
		imp.Packages = append(imp.Packages, p.Types.Name())
		n++ // numbered by the order written, so a skipped package leaves no gap
		body = append(body, SpecFile{Name: fmt.Sprintf("%02d-%s.sexp", n*10, p.Types.Name()), Text: f})
	}
	imp.Files = append([]SpecFile{{Name: "00-project.sexp", Text: projectForm(modPath, goVer, used, requires)}}, body...)
	if len(imp.Notes) > 0 {
		imp.Files = append(imp.Files, SpecFile{Name: "NOTES.md", Text: notesText(imp.Notes)})
	}
	return imp, nil
}

func readGoMod(dir string) (modPath, goVer string, requires map[string]string, err error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", "", nil, fmt.Errorf("%s has no go.mod: point tilegen import at a Go module", dir)
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return "", "", nil, err
	}
	requires = map[string]string{}
	for _, r := range f.Require {
		if !r.Indirect {
			requires[r.Mod.Path] = r.Mod.Version
		}
	}
	goVer = "1.22"
	if f.Go != nil {
		goVer = f.Go.Version
	}
	return f.Module.Mod.Path, goVer, requires, nil
}

func projectForm(modPath, goVer string, used, requires map[string]string) string {
	var b strings.Builder
	b.WriteString("; Imported by tilegen from an existing module. Everything here comes\n")
	b.WriteString("; from the type checker. Add store ops, needs and events yourself:\n")
	b.WriteString(";   (entity X (field ...) (store get list save (durable)))\n\n")
	fmt.Fprintf(&b, "(project %s\n  (module %s)\n  (go %s)", filepath.Base(modPath), modPath, goVer)
	if len(used) > 0 {
		b.WriteString("\n  (require")
		for _, q := range sortedKeys(used) {
			fmt.Fprintf(&b, "\n    (%s %s %s)", q, used[q], requires[used[q]])
		}
		b.WriteString(")")
	}
	b.WriteString(")\n\n(workspace\n  (out ..))\n")
	return b.String()
}

// importPackage lifts one package. Everything comes from types.
func importPackage(p *packages.Package, imp *Import, used, requires map[string]string) string {
	scope := p.Types.Scope()
	names := scope.Names()
	var structs, ifaces, enums, impls []string
	// Enum values in declaration order: scope.Names() is sorted, so order by
	// source position, which is what StatusValues preserves.
	consts := map[string][]*types.Const{} // enum type -> its constants
	byPos := append([]string{}, names...)
	sort.Slice(byPos, func(i, j int) bool {
		return scope.Lookup(byPos[i]).Pos() < scope.Lookup(byPos[j]).Pos()
	})
	for _, n := range byPos {
		if c, ok := scope.Lookup(n).(*types.Const); ok && c.Exported() {
			if named, ok := c.Type().(*types.Named); ok && named.Obj().Pkg() == p.Types {
				if b, ok := named.Underlying().(*types.Basic); ok && b.Kind() == types.String {
					consts[named.Obj().Name()] = append(consts[named.Obj().Name()], c)
				}
			}
		}
	}
	qual := qualifier(p, used, requires, imp)
	structsByName := map[string]*types.Named{}
	var namedTypes []*types.Named
	implOf := map[string]*types.Named{} // struct name -> the interface it implements
	for _, n := range names {
		obj, ok := scope.Lookup(n).(*types.TypeName)
		if !ok || !obj.Exported() {
			continue
		}
		named, ok := obj.Type().(*types.Named)
		if !ok {
			imp.Total++
			imp.skip(p, n, "it is a type alias")
			continue
		}
		if imp.generated[n] {
			continue // the events tile rebuilds it from (events ...)
		}
		imp.Total++
		if named.TypeParams() != nil {
			imp.skip(p, n, "it is generic; tilegen has no form for generic types yet")
			continue
		}
		namedTypes = append(namedTypes, named)
		if _, ok := named.Underlying().(*types.Struct); ok {
			continue // structs are handled below, once implementations are known
		}
		switch u := named.Underlying().(type) {
		case *types.Struct:
			structsByName[named.Obj().Name()] = named
			imp.Imported++
		case *types.Interface:
			ifaces = append(ifaces, ifaceForm(named, u, qual))
			imp.Imported++
		case *types.Basic:
			if u.Kind() == types.String && len(consts[n]) > 0 {
				enums = append(enums, enumForm(n, consts[n]))
				imp.Imported++
				continue
			}
			imp.skip(p, n, fmt.Sprintf("it is a defined %s with no typed constants", u.Name()))
		default:
			imp.skip(p, n, fmt.Sprintf("its underlying type is %s, which no tile produces", typeString(named.Underlying(), qual)))
		}
	}
	// Which concrete type implements which interface: the type checker
	// decides. A struct that implements one is written as (implement ...),
	// which carries its dependencies; every other struct as (struct ...).
	for _, named := range namedTypes {
		if _, ok := named.Underlying().(*types.Struct); !ok {
			continue
		}
		for _, iface := range namedTypes {
			u, ok := iface.Underlying().(*types.Interface)
			if !ok || u.NumMethods() == 0 || implOf[named.Obj().Name()] != nil {
				continue
			}
			if types.Implements(types.NewPointer(named), u) {
				implOf[named.Obj().Name()] = iface
				impls = append(impls, implementForm(named, iface, qual))
				imp.Imported++
			}
		}
	}
	for _, named := range namedTypes {
		st, ok := named.Underlying().(*types.Struct)
		if !ok || implOf[named.Obj().Name()] != nil {
			continue
		}
		if s, ok := structForm(named, st, qual, p, imp); ok {
			structs = append(structs, s)
			imp.Imported++
		}
	}
	// Which concrete type implements which interface: the type checker
	// decides, not names. A struct that implements one is written as
	// (implement ...), which carries its fields, so it is not also written
	// as a (struct ...).
	implemented := map[string]bool{}
	for _, name := range sortedKeys(structsByName) {
		named := structsByName[name]
		for _, iface := range namedTypes {
			u, ok := iface.Underlying().(*types.Interface)
			if !ok || u.NumMethods() == 0 || iface.Obj().Name() == name {
				continue
			}
			if types.Implements(types.NewPointer(named), u) && !implemented[name] {
				impls = append(impls, implementForm(named, iface, qual))
				implemented[name] = true
			}
		}
	}
	for _, name := range sortedKeys(structsByName) {
		if implemented[name] {
			continue
		}
		named := structsByName[name]
		if f, ok := structForm(named, named.Underlying().(*types.Struct), qual, p, imp); ok {
			structs = append(structs, f)
		}
	}

	if len(structs)+len(ifaces)+len(enums) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "(package %s", p.Types.Name())
	for _, group := range [][]string{enums, structs, ifaces, impls} {
		for _, f := range group {
			b.WriteString("\n\n" + indent(f, "  "))
		}
	}
	b.WriteString(")\n")
	return b.String()
}

// qualifier prints types the way a spec writes them, and records which
// modules the spec will need to require.
func qualifier(p *packages.Package, used, requires map[string]string, imp *Import) types.Qualifier {
	mod := ""
	if p.Module != nil {
		mod = p.Module.Path
	}
	return func(other *types.Package) string {
		if other == p.Types {
			return "" // same package: unqualified, as tilegen writes it
		}
		if !strings.HasPrefix(other.Path(), mod+"/") && !isStd(other.Name()) {
			for m := range requires { // find the module this package belongs to
				if other.Path() == m || strings.HasPrefix(other.Path(), m+"/") {
					used[other.Name()] = m
				}
			}
		}
		return other.Name()
	}
}

func typeString(t types.Type, qual types.Qualifier) string { return types.TypeString(t, qual) }

// quoteType quotes a type when the spec needs it to (spaces or parens).
func quoteType(s string) string {
	if strings.ContainsAny(s, " ()\"") {
		return Str(s).Flat()
	}
	return s
}

func structForm(named *types.Named, st *types.Struct, qual types.Qualifier, p *packages.Package, imp *Import) (string, bool) {
	var b strings.Builder
	fmt.Fprintf(&b, "(struct %s", named.Obj().Name())
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if !f.Exported() {
			imp.note(fmt.Sprintf("%s.%s: unexported field %q was not imported", p.Types.Name(), named.Obj().Name(), f.Name()))
			continue
		}
		if f.Embedded() {
			imp.note(fmt.Sprintf("%s.%s: embedded field %q was not imported (tilegen has no form for embedded struct fields)", p.Types.Name(), named.Obj().Name(), f.Name()))
			continue
		}
		fmt.Fprintf(&b, "\n  (field %s %s", f.Name(), quoteType(typeString(f.Type(), qual)))
		if tag := reflectTag(st.Tag(i)); tag != "" {
			fmt.Fprintf(&b, " (tag %s)", Str(tag).Flat())
		}
		b.WriteString(")")
	}
	b.WriteString(")")
	return b.String(), true
}

// reflectTag keeps a struct tag as written, so regeneration does not
// silently restyle it.
func reflectTag(tag string) string { return strings.TrimSpace(tag) }

func ifaceForm(named *types.Named, it *types.Interface, qual types.Qualifier) string {
	var b strings.Builder
	fmt.Fprintf(&b, "(interface %s", named.Obj().Name())
	for i := 0; i < it.NumExplicitMethods(); i++ {
		b.WriteString("\n  " + methodForm(it.ExplicitMethod(i), qual))
	}
	for i := 0; i < it.NumEmbeddeds(); i++ {
		fmt.Fprintf(&b, "\n  (embed %s)", typeString(it.EmbeddedType(i), qual))
	}
	b.WriteString(")")
	return b.String()
}

func methodForm(fn *types.Func, qual types.Qualifier) string {
	sig := fn.Type().(*types.Signature)
	var b strings.Builder
	fmt.Fprintf(&b, "(method %s", fn.Name())
	params := sig.Params()
	var ps []string
	for i := 0; i < params.Len(); i++ {
		v := params.At(i)
		name := v.Name()
		if name == "" || name == "_" {
			name = fmt.Sprintf("p%d", i)
		}
		t := typeString(v.Type(), qual)
		if sig.Variadic() && i == params.Len()-1 {
			t = "..." + typeString(v.Type().(*types.Slice).Elem(), qual)
		}
		ps = append(ps, fmt.Sprintf("(%s %s)", name, quoteType(t)))
	}
	if len(ps) > 0 {
		fmt.Fprintf(&b, "\n    (params %s)", strings.Join(ps, " "))
	} else if sig.Results().Len() > 0 {
		b.WriteString("\n    (params)")
	}
	if res := sig.Results(); res.Len() > 0 {
		var rs []string
		for i := 0; i < res.Len(); i++ {
			rs = append(rs, quoteType(typeString(res.At(i).Type(), qual)))
		}
		fmt.Fprintf(&b, "\n    (returns %s)", strings.Join(rs, " "))
	}
	b.WriteString(")")
	return b.String()
}

func enumForm(name string, consts []*types.Const) string {
	var vals []string
	for _, c := range consts {
		vals = append(vals, strings.Trim(c.Val().String(), `"`))
	}
	return fmt.Sprintf("(enum %s %s)", name, strings.Join(vals, " "))
}

func implementForm(impl, iface *types.Named, qual types.Qualifier) string {
	var b strings.Builder
	fmt.Fprintf(&b, "(implement %s (as %s)", iface.Obj().Name(), impl.Obj().Name())
	if st, ok := impl.Underlying().(*types.Struct); ok {
		for i := 0; i < st.NumFields(); i++ {
			f := st.Field(i)
			if f.Exported() || f.Embedded() {
				continue // dependencies are the unexported fields tilegen injects
			}
			fmt.Fprintf(&b, "\n  (field %s %s)", f.Name(), quoteType(typeString(f.Type(), qual)))
		}
	}
	b.WriteString(")")
	return b.String()
}

func (imp *Import) skip(p *packages.Package, name, why string) {
	imp.Skipped++
	imp.note(fmt.Sprintf("%s.%s: %s", p.Types.Name(), name, why))
}

func (imp *Import) note(s string) { imp.Notes = append(imp.Notes, s) }

func notesText(notes []string) string {
	var b strings.Builder
	b.WriteString("# What tilegen import did not put in the spec\n\n")
	b.WriteString("tilegen imports only what the type checker can prove, so everything in\n")
	b.WriteString("the spec is exactly right. The rest is listed here. Nothing is lost:\n")
	b.WriteString("this code stays as it is, and tilegen leaves it alone.\n\n")
	for _, n := range notes {
		b.WriteString("- " + n + "\n")
	}
	b.WriteString("\n## Worth adding by hand\n\n")
	b.WriteString("Store operations, needs and events are not guessed from method names,\n")
	b.WriteString("because a rule like \"a method called GetByX means (get-by X)\" works on\n")
	b.WriteString("one codebase and not the next. Where you have a store, say so:\n\n")
	b.WriteString("```lisp\n(entity Order\n  (field ID uuid.UUID)\n  (store get list save (durable)))\n```\n\n")
	b.WriteString("Then `tilegen explain` will pick a backend, and `tilegen check` will\n")
	b.WriteString("show what regenerating would change.\n")
	return b.String()
}

// tileOwnedTypes lists types a tile will produce again from a form that is
// itself imported, so importing them as plain structs would be wrong: the
// events tile rebuilds Bus, LocalBus and the event structs from
// (events ...), together with the unexported machinery they rely on.
// Everything else in a generated file is imported normally, since most
// imported projects are not tilegen's own output.
func tileOwnedTypes(p *packages.Package) map[string]bool {
	out := map[string]bool{}
	scope := p.Types.Scope()
	bus, _ := scope.Lookup("Bus").(*types.TypeName)
	local, _ := scope.Lookup("LocalBus").(*types.TypeName)
	if bus == nil || local == nil {
		return out
	}
	it, ok := bus.Type().Underlying().(*types.Interface)
	if !ok || !types.Implements(types.NewPointer(local.Type()), it) {
		return out
	}
	out["Bus"], out["LocalBus"] = true, true
	// Not imported and not silently lost: say so, since the (events ...)
	// form itself cannot be proved from types.
	for i := 0; i < it.NumMethods(); i++ { // PublishX(ctx, e X) error -> X is an event
		m := it.Method(i)
		name, ok := strings.CutPrefix(m.Name(), "Publish")
		if !ok {
			continue
		}
		if _, isType := scope.Lookup(name).(*types.TypeName); isType {
			out[name] = true
		}
	}
	return out
}

// isGenerated reports whether every file of a package carries a
// generated-code header, so tilegen does not import its own output.
func isGenerated(p *packages.Package) bool {
	if len(p.Syntax) == 0 {
		return false
	}
	for _, f := range p.Syntax {
		gen := false
		for _, cg := range f.Comments {
			if cg.Pos() < f.Package && strings.Contains(cg.Text(), "Code generated") {
				gen = true
			}
		}
		if !gen {
			return false
		}
	}
	return true
}

// indent prefixes every line of s.
func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}
