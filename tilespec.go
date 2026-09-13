package main

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
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
// generates tile_bolt.go: the whole registration, which is bookkeeping, and
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

// tileKinds are the shapes a tile-spec can describe. Each is a different
// registration and a different pair of holes.
var tileKinds = []string{"store-backend"}

func init() {
	RegisterPackageTile(&PackageTile{
		Form: "tile-spec",
		Pass: Select,
		Rule: Rule{
			Name:     "tile-spec",
			Pattern:  Pat("(tile-spec ?name ?items...)"),
			Then:     selectTileSpec,
			Produces: "go/file",
			Doc:      "a tile for tilegen itself: the registration compiled, the judgment left open",
			Cost:     Cost{{Dim: "llm-work", Value: 2}, {Dim: "maintenance", Value: 1}},
		},
		Validate: validateTileSpec,
	})
}

// costDims are the dimensions a cost term may name, in the order the
// registry prints them.
var costDims = []string{"llm-work", "maintenance", "dependency", "runtime", "uncertainty"}

var costSources = []string{"default", "measured", "derived", "user"}

func validateTileSpec(v *validator, n *Node, types map[string]bool) {
	b := v.shape(n, "(tile-spec ?name ?items...)")
	if b == nil {
		return
	}
	name := b.One("name")
	if name == nil || name.IsList || !validTileName(name.Atom) {
		v.bad(n, "a tile name is lower-case letters, digits and hyphens, like postgres-sqlc, got %s", short(n))
		return
	}

	// The generated file declares one type; claim it so a clash with
	// anything else in the package is reported here rather than by the
	// Go compiler later.
	if decl := tileStructName(name.Atom); types[decl] {
		v.bad(n, "(tile-spec %s ...) generates %s, but the package already declares it", name.Atom, decl)
	} else {
		types[decl] = true
	}

	kind := ""
	seen := map[string]bool{}
	for _, it := range b.Rest("items") {
		switch it.Head() {
		case "kind":
			if kb := v.shape(it, "(kind ?name)"); kb != nil {
				kind = kb.Atom("name")
				if !contains(tileKinds, kind) {
					v.bad(it, "unknown tile kind %q%s; tilegen knows %s", kind,
						didYouMean(kind, tileKinds), strings.Join(tileKinds, ", "))
				}
			}
		case "doc":
			v.shape(it, "(doc ?text)")
		case "alias":
			v.shape(it, "(alias ?name)")
		case "error-package":
			v.shape(it, "(error-package ?qualifier)")
		case "import":
			v.shape(it, "(import ?qualifier ?path)")
		case "topics":
			if len(it.Args()) == 0 {
				v.bad(it, "(topics ...) needs at least one topic")
			}
		case "form":
			v.shape(it, "(form ?name)")
		case "requires":
			for _, r := range it.Args() {
				v.shape(r, "(tool ?name)")
			}
		case "illegal-when":
			validateIllegalWhen(v, it)
		case "cost":
			validateTileCost(v, it)
		default:
			known := []string{"kind", "doc", "alias", "import", "error-package", "topics", "form", "requires", "illegal-when", "cost"}
			v.bad(it, "a tile-spec contains %s, got %s%s",
				strings.Join(known, ", "), short(it), didYouMean(it.Head(), known))
			continue
		}
		if it.Head() != "illegal-when" && it.Head() != "import" && seen[it.Head()] {
			v.bad(it, "duplicate (%s ...) in tile-spec %s", it.Head(), name.Atom)
		}
		seen[it.Head()] = true
	}
	if kind == "" {
		v.bad(n, "(tile-spec %s ...) needs a (kind ...): one of %s", name.Atom, strings.Join(tileKinds, ", "))
	}
	if n.Text("doc") == "" {
		v.bad(n, "(tile-spec %s ...) needs a (doc \"...\"): it is the one line `tilegen tiles` prints", name.Atom)
	}
	if n.Find("cost") == nil {
		v.bad(n, "(tile-spec %s ...) needs a (cost ...): a tile that declares no cost can never be compared with one that does", name.Atom)
	}
}

// validateIllegalWhen checks (illegal-when (durable) "reason"). The reason
// is not optional: it is the whole explanation a user reads in explain.
func validateIllegalWhen(v *validator, it *Node) {
	b := v.shape(it, "(illegal-when ?req ?reason)")
	if b == nil {
		return
	}
	req := b.One("req")
	if req == nil || !req.IsList || len(req.List) != 1 || req.List[0].IsList {
		v.bad(it, "(illegal-when ...) takes a bare requirement form like (durable), got %s", short(it))
		return
	}
	name := req.List[0].Atom
	if !contains(knownRequirements(), name) {
		v.bad(req, "unknown requirement (%s)%s; the shared vocabulary is %s", name,
			didYouMean(name, knownRequirements()), strings.Join(knownRequirements(), ", "))
	}
	if reason := b.One("reason"); reason == nil || strings.TrimSpace(reason.Atom) == "" {
		v.bad(it, "(illegal-when (%s) ...) needs a reason: it is what explain prints when this tile is ruled out", name)
	}
}

// validateTileCost checks (cost (llm-work 4 (source derived "...")) ...).
func validateTileCost(v *validator, it *Node) {
	if len(it.Args()) == 0 {
		v.bad(it, "(cost ...) needs at least one dimension: %s", strings.Join(costDims, ", "))
		return
	}
	seen := map[string]bool{}
	for _, term := range it.Args() {
		if !term.IsList || len(term.List) < 2 {
			v.bad(term, "a cost term is (DIMENSION N [(source ...)]), got %s", short(term))
			continue
		}
		dim := term.List[0].Atom
		if !contains(costDims, dim) {
			v.bad(term, "unknown cost dimension %q%s; tilegen weighs %s", dim,
				didYouMean(dim, costDims), strings.Join(costDims, ", "))
		}
		if seen[dim] {
			v.bad(term, "duplicate cost dimension %q", dim)
		}
		seen[dim] = true
		if _, err := strconv.Atoi(term.List[1].Atom); err != nil {
			v.bad(term, "cost %s must be a whole number, got %q", dim, term.List[1].Atom)
		}
		for _, extra := range term.List[2:] {
			sb := v.shape(extra, "(source ?kind ?note...)")
			if sb == nil {
				continue
			}
			if kind := sb.Atom("kind"); !contains(costSources, kind) {
				v.bad(extra, "unknown cost source %q%s; tilegen records %s", kind,
					didYouMean(kind, costSources), strings.Join(costSources, ", "))
			}
		}
	}
}

// tileStructName is the type the generated file declares: the address the
// two holes live at.
func tileStructName(tile string) string { return lowerFirstWord(goName(tile)) + "Tile" }

// validTileName accepts the shape every tile in the registry already has:
// lower-case letters, digits and hyphens, starting with a letter.
func validTileName(s string) bool {
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
func selectTileSpec(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	c, pkg := m.C, m.C.Pkg
	name := b.Atom("name")
	kind := n.Text("kind")
	if kind != "store-backend" {
		return nil, fmt.Errorf("tile-spec %s: kind %q is not implemented yet; tilegen knows %s",
			name, kind, strings.Join(tileKinds, ", "))
	}

	impl := tileStructName(name)
	file := path.Join(pkg.Dir, "tile_"+strings.ReplaceAll(name, "-", "_")+".go")

	methods := storeBackendHoles()
	stubs, tasks := stubsAndTasks(pkg, impl, "the "+name+" tile", file, methods,
		func(meth *Node) string { return storeBackendIntent(meth.List[1].Atom, name, n) },
		[]string{"backend_memory.go", "registry.go"},
		[]string{
			"Return only the method asked for, with the signature given.",
			"Emit target forms with L, Sym and Str; never write files.",
		})

	decls := []*Node{
		L(Sym("go/raw"), Str(storeBackendRegistration(name, impl, n))),
		L(Sym("go/struct"), Sym(impl), L(Sym("doc"), Str(fmt.Sprintf(
			"%s carries the parts of the %s tile that are judgment rather than\ndeclaration. It holds nothing: it gives the two holes an address.", impl, name)))),
	}
	out, err := goFile(c, file, "keep", "", append(decls, stubs...))
	if err != nil {
		return nil, err
	}
	return append([]*Node{out}, tasks...), nil
}

// storeBackendHoles are the two methods a store backend cannot declare:
// what its implementation struct holds, and what each op means for it.
func storeBackendHoles() []*Node {
	return []*Node{
		L(Sym("method"), Sym("Implement"),
			L(Sym("params"), L(Sym("in"), Sym("StoreInput"))),
			L(Sym("returns"), Sym("StoreParts"), Sym("error"))),
		L(Sym("method"), Sym("Hint"),
			L(Sym("params"), L(Sym("meth"), Sym("*Node"))),
			L(Sym("returns"), Sym("string"))),
	}
}

func storeBackendIntent(method, tile string, n *Node) string {
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
		return fmt.Sprintf("Scaffold one store for the %s tile. Return StoreParts: Fields are the "+
			"implementation struct's fields as (field name Type) nodes, Params is the constructor's "+
			"(params ...), Body is the constructor's body as a single return statement building "+
			"&in.Impl{...}, and Hint is this tile's Hint method. Build every node with L, Sym and Str. "+
			"backend_memory.go is the same method for the memory tile and is the shape to follow.%s", tile, using)
	case "Hint":
		return fmt.Sprintf("Describe what each store method must do in the %s tile's own terms, for the LLM "+
			"that will write the body. Switch on methodOp(meth), which returns the op kind and field: "+
			"get, list, save, delete, count, list-by, get-by, count-by, exists-by, delete-by. "+
			"firstParam(meth) is the first non-context parameter's name. Return customHint(meth) for "+
			"anything else. memoryHint in pass_select.go is the same method for the memory tile.%s", tile, using)
	}
	return ""
}

// storeBackendRegistration writes the half of the tile that follows from
// the declaration. Nothing here is a decision; it is the reason writing a
// tile by hand felt like paperwork.
func storeBackendRegistration(name, impl string, n *Node) string {
	var b strings.Builder
	fmt.Fprintf(&b, "// The %s tile, registered from a (tile-spec %s ...) form.\n", name, name)
	fmt.Fprintf(&b, "// %s\nfunc init() {\n\tregisterStoreBackend(&Backend{\n", n.Text("doc"))

	alias := n.Text("alias")
	if alias == "" {
		alias = name
	}
	fmt.Fprintf(&b, "\t\tName: %s,\n", strconv.Quote(alias))
	fmt.Fprintf(&b, "\t\tTile: %s,\n", strconv.Quote(name))
	fmt.Fprintf(&b, "\t\tDoc:  %s,\n", strconv.Quote(n.Text("doc")))

	b.WriteString("\t\tCost: Cost{\n")
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

	if illegal := n.FindAll("illegal-when"); len(illegal) > 0 {
		reasons := map[string]string{}
		for _, it := range illegal {
			if len(it.List) >= 3 && it.List[1].IsList && len(it.List[1].List) == 1 {
				reasons[it.List[1].List[0].Atom] = it.List[2].Atom
			}
		}
		b.WriteString("\t\tIllegalWhen: map[string]string{\n")
		for _, req := range sortedKeys(reasons) {
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
func importPairs(n *Node) [][2]string {
	var out [][2]string
	for _, it := range n.FindAll("import") {
		if len(it.List) == 3 {
			out = append(out, [2]string{it.List[1].Atom, it.List[2].Atom})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}
