package tiles

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

// tilegen, applied to itself.
//
//	(package main
//	  (tile-spec bolt
//	    (kind store-backend)
//	    (doc "embedded key-value: one file, a bucket per entity, values as JSON")
//	    (import bbolt go.etcd.io/bbolt)
//	    (error-package bbolt)
//	    (topics bbolt embedded-database)
//	    (illegal-when (lookup-by-field) "a bucket has one key; a secondary index would be written and repaired by hand")
//	    (cost (llm-work 4 (source derived "id-keyed get, save and delete over a bucket"))
//	          (maintenance 3) (dependency 1) (runtime 1))))
//
// generates store_bolt.go: the whole registration, which is bookkeeping, and
// two holes, which are not.
//
//	func (s *boltTile) Implement(in StoreInput) (StoreParts, error)  // what the impl struct holds
//	func (s *boltTile) Hint(meth *Node) string                       // what each op means here
//
// The split is the project's own thesis turned on its own extension point:
// everything derivable from the declaration is compiled, and only judgment
// is left open. Because the holes are ordinary stubs made by stubsAndTasks,
// `tilegen prompt`, `tilegen fill` and reconciliation work on them with no
// new machinery.
//
// The holes are methods on a small per-tile struct rather than plain
// functions, because fill splices by receiver and method name. The struct
// holds nothing; it exists to give the two holes an address.

// TileKinds are the shapes a tile-spec can describe. Each is a different
// registration and a different pair of holes.
var TileKinds = []string{"store-backend"}

// tilePackages are the packages of tilegen a generated tile's signatures
// name, by qualifier.
var tilePackages = map[string]string{
	"compile": "github.com/vinodhalaharvi/tilegen/internal/compile",
	"sexp":    "github.com/vinodhalaharvi/tilegen/internal/sexp",
}

// TileKindCapability is the capability each kind of tile offers.
var TileKindCapability = map[string]string{"store-backend": "store"}

// Unanswered reports whether a form still carries a TODO: the atom, or a
// string that starts with it. A skeleton is full of them, and none may
// reach a decision.
func Unanswered(n *sexp.Node) bool {
	if !n.IsList {
		return n.Atom == "TODO" || strings.HasPrefix(n.Atom, "TODO:") || strings.HasPrefix(n.Atom, "TODO ")
	}
	for _, k := range n.List {
		if Unanswered(k) {
			return true
		}
	}
	return false
}

func init() {
	compile.RegisterPackageTile(&compile.PackageTile{
		Form: "tile-spec",
		Pass: compile.Select,
		Rule: compile.Rule{
			Name:     "tile-spec",
			Pattern:  sexp.Pat("(tile-spec ?name ?items...)"),
			Then:     selectTileSpec,
			Produces: "go/file",
			Doc:      "a tile for tilegen itself: the registration compiled, the judgment left open",
			Cost:     compile.Cost{{Dim: "llm-work", Value: 2}, {Dim: "maintenance", Value: 1}},
		},
		Validate: validateTileSpec,
	})
}

// costDims are the dimensions a cost term may name, in the order the
// registry prints them.
var costDims = []string{"llm-work", "maintenance", "dependency", "runtime", "operations", "uncertainty"}

var costSources = []string{"default", "measured", "derived", "user"}

func validateTileSpec(v *compile.Validator, n *sexp.Node, types map[string]bool) {
	b := v.Shape(n, "(tile-spec ?name ?items...)")
	if b == nil {
		return
	}
	name := b.One("name")
	if name == nil || name.IsList || !ValidTileName(name.Atom) {
		v.Bad(n, "a tile name is lower-case letters, digits and hyphens, like postgres-sqlc, got %s", compile.Short(n))
		return
	}

	// The generated file declares one type; claim it so a clash with
	// anything else in the package is reported here rather than by the
	// Go compiler later.
	if decl := tileStructName(name.Atom); types[decl] {
		v.Bad(n, "(tile-spec %s ...) generates %s, but the package already declares it", name.Atom, decl)
	} else {
		types[decl] = true
	}

	// A skeleton's TODOs are questions. Until each is answered there is
	// nothing here to check, and nothing that may be generated from.
	open := false
	for _, it := range b.Rest("items") {
		if Unanswered(it) {
			v.Bad(it, "(tile-spec %s ...): %s is still a TODO; answer it before this tile can be used", name.Atom, compile.Short(it))
			open = true
		}
	}
	if open {
		return
	}

	kind := ""
	seen := map[string]bool{}
	claimed := map[string]string{} // requirement -> the heading it is under
	for _, it := range b.Rest("items") {
		switch it.Head() {
		case "kind":
			if kb := v.Shape(it, "(kind ?name)"); kb != nil {
				kind = kb.Atom("name")
				if !compile.Contains(TileKinds, kind) {
					v.Bad(it, "unknown tile kind %q%s; tilegen knows %s", kind,
						compile.DidYouMean(kind, TileKinds), strings.Join(TileKinds, ", "))
				}
			}
		case "doc":
			v.Shape(it, "(doc ?text)")
		case "alias":
			v.Shape(it, "(alias ?name)")
		case "error-package":
			v.Shape(it, "(error-package ?qualifier)")
		case "import":
			v.Shape(it, "(import ?qualifier ?path)")
		case "topics":
			if len(it.Args()) == 0 {
				v.Bad(it, "(topics ...) needs at least one topic")
			}
		case "form":
			v.Shape(it, "(form ?name)")
		case "engine":
			v.Shape(it, "(engine ?name)")
		case "requires":
			for _, r := range it.Args() {
				v.Shape(r, "(tool ?name)")
			}
		case "illegal-when", "satisfies", "unverified":
			if req := validateClaim(v, it); req != "" {
				if was := claimed[req]; was != "" {
					v.Bad(it, "(%s) is already under (%s ...): a requirement is met, ruled out or unchecked, and only one of them", req, was)
				}
				claimed[req] = it.Head()
			}
		case "cost":
			validateTileCost(v, it)
		default:
			known := []string{"kind", "doc", "alias", "import", "error-package", "topics", "form", "engine", "requires", "satisfies", "illegal-when", "unverified", "cost"}
			v.Bad(it, "a tile-spec contains %s, got %s%s",
				strings.Join(known, ", "), compile.Short(it), compile.DidYouMean(it.Head(), known))
			continue
		}
		if it.Head() != "illegal-when" && it.Head() != "satisfies" && it.Head() != "unverified" && it.Head() != "import" && seen[it.Head()] {
			v.Bad(it, "duplicate (%s ...) in tile-spec %s", it.Head(), name.Atom)
		}
		seen[it.Head()] = true
	}
	// A store's short name is what a config says after (storage ...), and
	// the front of every type the tile generates: PostgresOrderStore.
	if kind == "store-backend" {
		short := n.Text("alias")
		if short == "" {
			short = name.Atom
		}
		if !shortName.MatchString(short) {
			v.Bad(n, "(tile-spec %s ...): a store's short name becomes part of a Go type, so it is lower-case letters and digits only; %q is not. Add (alias NAME), as postgres-pgx has (alias pgx)", name.Atom, short)
		}
	}
	if kind == "" {
		v.Bad(n, "(tile-spec %s ...) needs a (kind ...): one of %s", name.Atom, strings.Join(TileKinds, ", "))
	}
	if n.Text("doc") == "" {
		v.Bad(n, "(tile-spec %s ...) needs a (doc \"...\"): it is the one line `tilegen tiles` prints", name.Atom)
	}
	if n.Find("cost") == nil {
		v.Bad(n, "(tile-spec %s ...) needs a (cost ...): a tile that declares no cost can never be compared with one that does", name.Atom)
	}
	// Silence is not a yes. Each thing a spec can ask of this kind of tile
	// is answered: met, ruled out, or said to be unchecked.
	if c := compile.Capabilities[TileKindCapability[kind]]; c != nil {
		var open []string
		for _, req := range c.Requirements {
			if claimed[req] == "" {
				open = append(open, "("+req+")")
			}
		}
		if len(open) > 0 {
			v.Bad(n, "(tile-spec %s ...) says nothing about %s: put each under (satisfies ...), (illegal-when ...) or (unverified ...), with the reason",
				name.Atom, strings.Join(open, ", "))
		}
	}
}

// validateClaim checks (satisfies (durable) "reason"), and the same shape
// under illegal-when and unverified, and returns the requirement named.
// The reason is not optional: it is what a user reads in explain.
func validateClaim(v *compile.Validator, it *sexp.Node) string {
	head := it.Head()
	b := v.Shape(it, "("+head+" ?req ?reason)")
	if b == nil {
		return ""
	}
	req := b.One("req")
	if req == nil || !req.IsList || len(req.List) != 1 || req.List[0].IsList {
		v.Bad(it, "(%s ...) takes a bare requirement form like (durable), got %s", head, compile.Short(it))
		return ""
	}
	name := req.List[0].Atom
	if !compile.Contains(compile.KnownRequirements(), name) {
		v.Bad(req, "unknown requirement (%s)%s; the shared vocabulary is %s", name,
			compile.DidYouMean(name, compile.KnownRequirements()), strings.Join(compile.KnownRequirements(), ", "))
		return ""
	}
	if reason := b.One("reason"); reason == nil || strings.TrimSpace(reason.Atom) == "" {
		v.Bad(it, "(%s (%s) ...) needs a reason: it is what explain prints for this tile", head, name)
	}
	return name
}

// validateTileCost checks (cost (llm-work 4 (source derived "...")) ...).
func validateTileCost(v *compile.Validator, it *sexp.Node) {
	if len(it.Args()) == 0 {
		v.Bad(it, "(cost ...) needs at least one dimension: %s", strings.Join(costDims, ", "))
		return
	}
	seen := map[string]bool{}
	for _, term := range it.Args() {
		if !term.IsList || len(term.List) < 2 {
			v.Bad(term, "a cost term is (DIMENSION N [(source ...)]), got %s", compile.Short(term))
			continue
		}
		dim := term.List[0].Atom
		if !compile.Contains(costDims, dim) {
			v.Bad(term, "unknown cost dimension %q%s; tilegen weighs %s", dim,
				compile.DidYouMean(dim, costDims), strings.Join(costDims, ", "))
		}
		if seen[dim] {
			v.Bad(term, "duplicate cost dimension %q", dim)
		}
		seen[dim] = true
		if _, err := strconv.Atoi(term.List[1].Atom); err != nil {
			v.Bad(term, "cost %s must be a whole number, got %q", dim, term.List[1].Atom)
		}
		for _, extra := range term.List[2:] {
			sb := v.Shape(extra, "(source ?kind ?note...)")
			if sb == nil {
				continue
			}
			if kind := sb.Atom("kind"); !compile.Contains(costSources, kind) {
				v.Bad(extra, "unknown cost source %q%s; tilegen records %s", kind,
					compile.DidYouMean(kind, costSources), strings.Join(costSources, ", "))
			}
		}
	}
}

// tileStructName is the type the generated file declares: the address the
// two holes live at.
func tileStructName(tile string) string { return compile.LowerFirstWord(compile.GoName(tile)) + "Tile" }

// shortName is a store's short name: what a config says, and the front of
// a Go type name.
var shortName = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// ValidTileName accepts the shape every tile in the registry already has:
// lower-case letters, digits and hyphens, starting with a letter.
func ValidTileName(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return !strings.HasSuffix(s, "-")
}

// goName, from enum.go, turns bolt into Bolt and postgres-sqlc into
// PostgresSqlc: exactly the mapping a tile name needs.

// selectTileSpec emits the tile: one scaffolded file holding the whole
// registration and two stubs, plus a task per stub.
func selectTileSpec(m *compile.Munch, b sexp.Bindings, n *sexp.Node) ([]*sexp.Node, error) {
	c, pkg := m.C, m.C.Pkg
	name := b.Atom("name")
	kind := n.Text("kind")
	if kind != "store-backend" {
		return nil, fmt.Errorf("tile-spec %s: kind %q is not implemented yet; tilegen knows %s",
			name, kind, strings.Join(TileKinds, ", "))
	}

	// A tile is a file of tilegen's own, so it may name tilegen's packages.
	for q, imp := range tilePackages {
		if c.Local[q] == "" {
			c.Local[q] = imp
		}
	}

	impl := tileStructName(name)
	file := path.Join(pkg.Dir, TileKindCapability[kind]+"_"+strings.ReplaceAll(name, "-", "_")+".go")

	methods := storeBackendHoles()
	stubs, tasks := compile.StubsAndTasks(pkg, impl, "the "+name+" tile", file, methods,
		func(meth *sexp.Node) string { return storeBackendIntent(meth.List[1].Atom, name, n) },
		[]string{"internal/tiles/store_memory.go", "internal/compile/registry.go", "internal/sexp/sexp.go"},
		[]string{
			"Return only the method asked for, with the signature given.",
			"Emit target forms with sexp.L, sexp.Sym and sexp.Str; never write files.",
		})

	decls := []*sexp.Node{
		sexp.L(sexp.Sym("go/raw"), sexp.Str(storeBackendRegistration(name, impl, n))),
		sexp.L(sexp.Sym("go/struct"), sexp.Sym(impl), sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf(
			"%s carries the parts of the %s tile that are judgment rather than\ndeclaration. It holds nothing: it gives the two holes an address.", impl, name)))),
	}
	out, err := compile.GoFile(c, file, "keep", "", append(decls, stubs...))
	if err != nil {
		return nil, err
	}
	return append([]*sexp.Node{out}, tasks...), nil
}

// storeBackendHoles are the two methods a store backend cannot declare:
// what its implementation struct holds, and what each op means for it.
func storeBackendHoles() []*sexp.Node {
	return []*sexp.Node{
		sexp.L(sexp.Sym("method"), sexp.Sym("Implement"),
			sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("in"), sexp.Sym("compile.StoreInput"))),
			sexp.L(sexp.Sym("returns"), sexp.Sym("compile.StoreParts"), sexp.Sym("error"))),
		sexp.L(sexp.Sym("method"), sexp.Sym("Hint"),
			sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("meth"), sexp.Sym("*sexp.Node"))),
			sexp.L(sexp.Sym("returns"), sexp.Sym("string"))),
	}
}

func storeBackendIntent(method, tile string, n *sexp.Node) string {
	imports := importPairs(n)
	var qualifiers []string
	for _, im := range imports {
		qualifiers = append(qualifiers, im[0]+" ("+im[1]+")")
	}
	using := ""
	if len(qualifiers) > 0 {
		using = " The tile's code may use " + strings.Join(qualifiers, ", ") + " and nothing else beyond the standard library."
	}
	switch method {
	case "Implement":
		return fmt.Sprintf("Scaffold one store for the %s tile. Return compile.StoreParts: Fields are the "+
			"implementation struct's fields as (field name Type) nodes, Params is the constructor's "+
			"(params ...), Body is the constructor's body as a single return statement building "+
			"&in.Impl{...}, and Hint is this tile's Hint method. Build every node with sexp.L, sexp.Sym and sexp.Str. "+
			"internal/tiles/store_memory.go is the same method for the memory tile and is the shape to follow.%s", tile, using)
	case "Hint":
		return fmt.Sprintf("Describe what each store method must do in the %s tile's own terms, for the LLM "+
			"that will write the body. Switch on compile.MethodOp(meth), which returns the op kind and field: "+
			"get, list, save, delete, count, list-by, get-by, count-by, exists-by, delete-by. "+
			"compile.FirstParam(meth) is the first non-context parameter's name. Return compile.CustomHint(meth) for "+
			"anything else. compile.MemoryHint in internal/compile/pass_select.go is the same method for the memory tile.%s%s", tile, using, unreachableOps(n))
	}
	return ""
}

// unreachableOps names the store ops this tile has declared it cannot
// serve. Without it the LLM writes a case for every op kind, including the
// ones illegal-when rules out, and the tile ends up arguing with itself
// three lines below its own clause.
func unreachableOps(n *sexp.Node) string {
	var reasons []string
	for _, it := range n.FindAll("illegal-when") {
		if len(it.List) >= 3 && it.List[1].IsList && len(it.List[1].List) == 1 &&
			it.List[1].List[0].Atom == "lookup-by-field" {
			reasons = append(reasons, it.List[2].Atom)
		}
	}
	if len(reasons) == 0 {
		return ""
	}
	return " This tile declares lookup-by-field illegal (" + strings.Join(reasons, "; ") +
		"), so list-by, get-by, count-by, exists-by and delete-by never reach this method. " +
		"Write no cases for them: they would be code that contradicts the tile's own clause."
}

// storeBackendRegistration writes the half of the tile that follows from
// the declaration. Nothing here is a decision; it is the reason writing a
// tile by hand felt like paperwork.
func storeBackendRegistration(name, impl string, n *sexp.Node) string {
	var b strings.Builder
	fmt.Fprintf(&b, "// The %s tile, registered from a (tile-spec %s ...) form.\n", name, name)
	fmt.Fprintf(&b, "// %s\nfunc init() {\n\tcompile.RegisterStoreBackend(&compile.Backend{\n", n.Text("doc"))

	alias := n.Text("alias")
	if alias == "" {
		alias = name
	}
	fmt.Fprintf(&b, "\t\tName: %s,\n", strconv.Quote(alias))
	fmt.Fprintf(&b, "\t\tTile: %s,\n", strconv.Quote(name))
	fmt.Fprintf(&b, "\t\tDoc:  %s,\n", strconv.Quote(n.Text("doc")))

	b.WriteString("\t\tCost: compile.Cost{\n")
	for _, term := range n.Find("cost").Args() {
		dim, value := term.List[0].Atom, term.List[1].Atom
		fmt.Fprintf(&b, "\t\t\t{Dim: %s, Value: %s", strconv.Quote(dim), value)
		if src := term.Find("source"); src != nil && len(src.Args()) > 0 {
			fmt.Fprintf(&b, ", Source: %s", strconv.Quote(src.Args()[0].Atom))
			if len(src.Args()) > 1 {
				fmt.Fprintf(&b, ", Note: %s", strconv.Quote(src.Args()[1].Atom))
			}
		}
		b.WriteString("},\n")
	}
	b.WriteString("\t\t},\n")

	for _, claim := range [][2]string{{"illegal-when", "IllegalWhen"}, {"unverified", "Unverified"}, {"satisfies", "Satisfies"}} {
		items := n.FindAll(claim[0])
		if len(items) == 0 {
			continue
		}
		reasons := map[string]string{}
		for _, it := range items {
			if len(it.List) >= 3 && it.List[1].IsList && len(it.List[1].List) == 1 {
				reasons[it.List[1].List[0].Atom] = it.List[2].Atom
			}
		}
		fmt.Fprintf(&b, "\t\t%s: map[string]string{\n", claim[1])
		for _, req := range compile.SortedKeys(reasons) {
			fmt.Fprintf(&b, "\t\t\t%s: %s,\n", strconv.Quote(req), strconv.Quote(reasons[req]))
		}
		b.WriteString("\t\t},\n")
	}

	if imports := importPairs(n); len(imports) > 0 {
		b.WriteString("\t\tImports: map[string]string{\n")
		for _, im := range imports {
			fmt.Fprintf(&b, "\t\t\t%s: %s,\n", strconv.Quote(im[0]), strconv.Quote(im[1]))
		}
		b.WriteString("\t\t},\n")
	}
	if topics := n.Find("topics"); topics != nil && len(topics.Args()) > 0 {
		var quoted []string
		for _, t := range topics.Args() {
			quoted = append(quoted, strconv.Quote(t.Atom))
		}
		fmt.Fprintf(&b, "\t\tTopics: []string{%s},\n", strings.Join(quoted, ", "))
	}
	if ep := n.Text("error-package"); ep != "" {
		fmt.Fprintf(&b, "\t\tErrorPackage: %s,\n", strconv.Quote(ep))
	}
	if form := n.Text("form"); form != "" {
		fmt.Fprintf(&b, "\t\tForm: %s,\n", strconv.Quote(form))
	}
	if engine := n.Text("engine"); engine != "" {
		fmt.Fprintf(&b, "\t\tEngine: %s,\n", strconv.Quote(engine))
	}
	if req := n.Find("requires"); req != nil {
		var tools []string
		for _, t := range req.Args() {
			if t.IsList && len(t.List) == 2 {
				tools = append(tools, strconv.Quote(t.List[1].Atom))
			}
		}
		if len(tools) > 0 {
			fmt.Fprintf(&b, "\t\tRequires: []string{%s},\n", strings.Join(tools, ", "))
		}
	}

	fmt.Fprintf(&b, "\t\tImplement: (&%s{}).Implement,\n", impl)
	b.WriteString("\t})\n}")
	return b.String()
}

// importPairs reads every (import qualifier path), sorted so the generated
// file is stable.
func importPairs(n *sexp.Node) [][2]string {
	var out [][2]string
	for _, it := range n.FindAll("import") {
		if len(it.List) == 3 {
			out = append(out, [2]string{it.List[1].Atom, it.List[2].Atom})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}
