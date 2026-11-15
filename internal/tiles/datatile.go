package tiles

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

// A storage tile that only has to say how a library is used is a file of
// data: a (tile ...) with what it claims, what it costs, what the
// generated store holds, and a hint for each method. Nothing in it runs.
// The .sexp files beside this one are built into tilegen.
//
// A tile that has to work something out from the entity, such as a schema,
// is Go: see store_postgres_sqlc.go.

//go:embed *.sexp
var specFiles embed.FS

func init() {
	names, err := fs.Glob(specFiles, "*.sexp")
	if err != nil {
		panic("tilegen: " + err.Error())
	}
	sort.Strings(names)
	for _, name := range names {
		src, err := specFiles.ReadFile(name)
		if err != nil {
			panic("tilegen: " + err.Error())
		}
		backends, err := LoadTiles(name, string(src))
		if err != nil {
			panic("tilegen: a built-in tile does not load: " + err.Error())
		}
		for _, t := range backends {
			t.Register()
		}
	}
}

// A Tile is one tile read from a file: what it is called, what it offers,
// and its registration. A store's is a Backend; anything else's is an
// Offer.
type Tile struct {
	Tile   string // its name: nats-jetstream
	Name   string // the short name a config uses: jetstream
	Offers string // the capability
	Store  *compile.Backend
	Offer  *compile.Offer
}

// Register adds the tile to the registry, beside the ones written in Go.
func (t *Tile) Register() {
	if t.Store != nil {
		compile.RegisterStoreBackend(t.Store)
		return
	}
	compile.RegisterOffer(t.Offer)
	compile.RegisterAlias(t.Tile, t.Name)
}

// LoadTiles reads a file of (tile ...) forms and returns the tiles
// they describe, checked but not registered.
func LoadTiles(file, src string) ([]*Tile, error) {
	forms, err := sexp.Parse(file, src)
	if err != nil {
		return nil, err
	}
	if len(forms) == 0 {
		return nil, fmt.Errorf("%s: no (tile ...) in it", file)
	}
	var out []*Tile
	for _, n := range forms {
		t, err := loadTile(n)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func loadTile(n *sexp.Node) (*Tile, error) {
	if n.Head() != "tile" {
		return nil, fmt.Errorf("%s: a tile file holds (tile NAME (offers CAPABILITY) ...) forms, got %s", n.Pos, compile.Short(n))
	}
	if err := compile.Check(func(v *compile.Validator) {
		validateTileSpec(v, n)
	}); err != nil {
		return nil, err
	}
	name, offers := n.List[1].Atom, n.Text("offers")
	t := &Tile{Tile: name, Name: name, Offers: offers}
	if alias := n.Text("alias"); alias != "" {
		t.Name = alias
	}
	shapes[offers].build(t, n)
	return t, nil
}

// storeBackend builds the registration from a checked tile.
func storeBackend(t *Tile, n *sexp.Node) {
	b := &compile.Backend{
		Name:         t.Name,
		Tile:         t.Tile,
		Doc:          n.Text("doc"),
		Engine:       n.Text("engine"),
		ErrorPackage: n.Text("error-package"),
		Form:         n.Text("form"),
	}
	t.Store = b
	b.Cost = costOf(n)
	b.IllegalWhen, b.Unverified, b.Satisfies = claims(n, "illegal-when"), claims(n, "unverified"), claims(n, "satisfies")
	for _, im := range importPairs(n) {
		if b.Imports == nil {
			b.Imports = map[string]string{}
		}
		b.Imports[im[0]] = im[1]
	}
	b.Topics, b.Requires = topicsOf(n), toolsOf(n)

	holds, ctor := n.Find("holds").Args(), n.Find("new")
	params, body := ctor.List[1], ctor.List[2].Atom
	hints := map[string]string{}
	for _, h := range n.FindAll("hint") {
		hints[h.List[1].Atom] = h.List[2].Atom
	}
	b.Implement = func(in compile.StoreInput) (compile.StoreParts, error) {
		names := entityNames(in)
		parts := compile.StoreParts{
			Params: sexp.L(sexp.Sym("params")),
			Body:   names.Replace(body),
			Hint: func(meth *sexp.Node) string {
				kind, field := compile.MethodOp(meth)
				text, ok := hints[kind]
				if !ok {
					return compile.CustomHint(meth)
				}
				if strings.Contains(text, "{param}") {
					text = strings.ReplaceAll(text, "{param}", compile.FirstParam(meth))
				}
				return names.Replace(strings.ReplaceAll(text, "{field}", field))
			},
		}
		for _, h := range holds {
			parts.Fields = append(parts.Fields, sexp.L(sexp.Sym("field"), sexp.Sym(h.List[0].Atom), named(h.List[1], names)))
		}
		for _, p := range params.Args() {
			parts.Params.List = append(parts.Params.List, sexp.L(sexp.Sym(p.List[0].Atom), named(p.List[1], names)))
		}
		return parts, nil
	}
}

// The names a tile's text may stand in for, each written {so}.
var (
	storeNames = []string{"impl", "entity", "name", "names", "id"}
	nameUse    = regexp.MustCompile(`\{[a-z][a-z-]*\}`)
)

// entityNames fills them in for one store: for an entity Note kept by the
// pebble tile, {impl} is PebbleNoteStore, {entity} is Note, {name} is
// note, {names} is notes and {id} is the type of its ID.
func entityNames(in compile.StoreInput) *strings.Replacer {
	lower := compile.LowerFirstWord(in.Entity)
	return strings.NewReplacer("{impl}", in.Impl, "{entity}", in.Entity,
		"{names}", lower+"s", "{name}", lower, "{id}", in.IDType)
}

// named is a type with the names filled in, quoted if it was quoted.
func named(typ *sexp.Node, names *strings.Replacer) *sexp.Node {
	return &sexp.Node{Atom: names.Replace(typ.Atom), Quoted: typ.Quoted}
}

// unknownNames are the {names} in text that tilegen would leave as they
// are, which is what a misspelt one looks like.
func unknownNames(text string, known []string) []string {
	var out []string
	for _, use := range nameUse.FindAllString(text, -1) {
		if !compile.Contains(known, strings.Trim(use, "{}")) && !compile.Contains(out, use) {
			out = append(out, use)
		}
	}
	return out
}

func checkNames(v *compile.Validator, at *sexp.Node, text string, known []string) {
	if bad := unknownNames(text, known); len(bad) > 0 {
		var braced []string
		for _, k := range known {
			braced = append(braced, "{"+k+"}")
		}
		v.Bad(at, "%s is not a name tilegen fills in; it knows %s", strings.Join(bad, ", "), strings.Join(braced, ", "))
	}
}

// validateHolds checks (holds (db *pebble.DB) (prefix string)): the
// fields of the generated store.
func validateHolds(v *compile.Validator, it *sexp.Node, sh *shape) {
	seen := map[string]bool{}
	for _, f := range it.Args() {
		fb := v.Shape(f, "(?name ?type)")
		if fb == nil {
			continue
		}
		name, typ := fb.One("name"), fb.One("type")
		if name.IsList || typ.IsList || name.Atom == "" || typ.Atom == "" {
			v.Bad(f, "a field the store holds is (name Type), got %s", compile.Short(f))
			continue
		}
		if seen[name.Atom] {
			v.Bad(f, "(holds ...) names %s twice", name.Atom)
		}
		seen[name.Atom] = true
		checkNames(v, f, typ.Atom, sh.fills)
	}
}

// validateNew checks (new (params (db *pebble.DB)) "return &{impl}{...}"):
// what the constructor takes and its body.
func validateNew(v *compile.Validator, it *sexp.Node, sh *shape) {
	nb := v.Shape(it, "(new (params ?params...) ?body)")
	if nb == nil {
		return
	}
	for _, p := range nb.Rest("params") {
		pb := v.Shape(p, "(?name ?type)")
		if pb == nil {
			continue
		}
		if pb.One("name").IsList || pb.One("type").IsList {
			v.Bad(p, "a constructor parameter is (name Type), got %s", compile.Short(p))
			continue
		}
		checkNames(v, p, pb.One("type").Atom, sh.fills)
	}
	body := nb.One("body")
	if body.IsList || !body.Quoted || strings.TrimSpace(body.Atom) == "" {
		v.Bad(it, "(new ...) ends with the constructor's body as a string, like \"return &{impl}{db: db}\"")
		return
	}
	checkNames(v, it, body.Atom, sh.fills)
}

// hintOps are the method kinds a hint may be for.
var (
	plainOps = []string{"get", "list", "save", "delete", "count"}
	fieldOps = []string{"list-by", "get-by", "count-by", "exists-by", "delete-by"}
	hintOps  = append(append([]string{}, plainOps...), append(fieldOps, "custom")...)
)

// validateHint checks (hint get "..."), and returns the op it is for.
func validateHint(v *compile.Validator, it *sexp.Node, sh *shape) string {
	hb := v.Shape(it, "(hint ?op ?text)")
	if hb == nil {
		return ""
	}
	op, text := hb.One("op"), hb.One("text")
	if op.IsList || !compile.Contains(sh.ops, op.Atom) {
		v.Bad(it, "a hint is for one kind of method: %s; got %s%s", strings.Join(sh.ops, ", "),
			compile.Short(op), compile.DidYouMean(op.Atom, sh.ops))
		return ""
	}
	if text.IsList || !text.Quoted || strings.TrimSpace(text.Atom) == "" {
		v.Bad(it, "(hint %s ...) needs its text: what the method has to do, in the library's own terms", op.Atom)
		return op.Atom
	}
	known := append(append([]string{}, sh.opFills[op.Atom]...), sh.fills...)
	if sh.what == "store" && !compile.Contains(fieldOps, op.Atom) && strings.Contains(text.Atom, "{field}") {
		v.Bad(it, "(hint %s ...) uses {field}, and only a by-field method has one", op.Atom)
	}
	checkNames(v, it, text.Atom, known)
	return op.Atom
}

// LoadDir registers the tiles in a folder of a team's own: every .sexp
// file in it, in name order. It returns the tiles it added. A tile whose
// name tilegen already has is refused rather than replaced, so a built-in
// tile is never silently something else.
func LoadDir(dir string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.sexp"))
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("-tiles %s: not a folder of tile files", dir)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("-tiles %s: no .sexp files in it", dir)
	}
	sort.Strings(files)
	var all []*Tile
	taken := map[string]string{}
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		tiles, err := LoadTiles(file, string(src))
		if err != nil {
			return nil, err
		}
		for _, b := range tiles {
			if nameTaken(b.Tile, "") {
				return nil, fmt.Errorf("%s: tilegen already has a tile called %s; give this one a name of its own", file, b.Tile)
			}
			if nameTaken(b.Name, b.Offers) {
				return nil, fmt.Errorf("%s: tilegen already has a tile called %s; give this one a name of its own", file, b.Name)
			}
			// A tile's name is its own among all tiles; its short name, among
			// the tiles that offer what it offers.
			for _, key := range []string{b.Tile, b.Offers + " " + b.Name} {
				if prev := taken[key]; prev != "" && prev != file+"#"+b.Tile {
					return nil, fmt.Errorf("%s: %s is also the name of a tile in %s", file, key[strings.LastIndex(key, " ")+1:], strings.SplitN(prev, "#", 2)[0])
				}
				taken[key] = file + "#" + b.Tile
			}
			all = append(all, b)
		}
	}
	var added []string
	for _, b := range all {
		b.Register()
		added = append(added, b.Tile)
	}
	return added, nil
}

// nameTaken reports whether a tile is already called this. With a
// capability, it asks about short names among the tiles that offer it.
func nameTaken(name, capability string) bool {
	if capability == "" {
		for _, offers := range compile.Offers {
			for _, o := range offers {
				if o.Tile == name {
					return true
				}
			}
		}
		return false
	}
	if _, has := compile.Backends[name]; has && capability == "store" {
		return true
	}
	for _, o := range compile.Offers[capability] {
		if o.Tile == name || compile.TileAliases[o.Tile] == name {
			return true
		}
	}
	return false
}

// A shape is what tilegen needs to know about a capability to take a tile
// for it from a file: what the thing it generates is called, the kinds of
// method a hint may be for, the names tilegen fills in, and how to turn a
// checked tile into a registration.
type shape struct {
	what     string              // "store", "bus", "authenticator": the noun in a message
	ops      []string            // the kinds of method a hint may be for
	fills    []string            // the names filled in everywhere
	opFills  map[string][]string // and the ones only some hints have
	typeName bool                // the tile names the generated type's suffix, with (type-name ...)
	build    func(t *Tile, n *sexp.Node)
}

// shapes are the capabilities a tile written as a file can offer. Adding
// one here is all it takes for that capability's tiles to be files.
var shapes = map[string]*shape{
	"store": {
		what: "store", ops: hintOps, fills: storeNames,
		opFills: map[string][]string{"get": {"param"}, "list": {"param"}, "save": {"param"}, "delete": {"param"}, "count": {"param"},
			"list-by": {"field", "param"}, "get-by": {"field", "param"}, "count-by": {"field", "param"}, "exists-by": {"field", "param"}, "delete-by": {"field", "param"}},
		build: storeBackend,
	},
	"event-bus": {
		what: "bus", ops: []string{"publish", "on"}, fills: declaredNames,
		opFills:  map[string][]string{"publish": {"event"}, "on": {"event"}},
		typeName: true, build: busOffer,
	},
	"auth": {
		what: "authenticator", ops: []string{"authenticate"}, fills: declaredNames,
		typeName: true, build: authOffer,
	},
}

// declaredNames are the names filled in for a capability whose tiles
// declare the whole type.
var declaredNames = []string{"impl", "ctx-type"}

// offerOf is the part of a registration every capability shares.
func offerOf(t *Tile, n *sexp.Node) *compile.Offer {
	return &compile.Offer{
		Tile: t.Tile, Capability: t.Offers, Doc: n.Text("doc"), Engine: n.Text("engine"), Form: n.Text("form"),
		Cost: costOf(n), Requires: toolsOf(n),
		IllegalFor: claims(n, "illegal-when"), Unverified: claims(n, "unverified"), Satisfies: claims(n, "satisfies"),
	}
}

// declared is the struct and constructor a tile's holds and new describe,
// for the capabilities whose tiles declare the whole type.
//
// {impl} is the type's name. {ctx-type} is "context.Context, " in a
// project whose methods take a context first, and nothing in one whose
// methods do not, so a function type can follow the project.
func declared(n *sexp.Node, impl, ctxType string) []*sexp.Node {
	names := strings.NewReplacer("{impl}", impl, "{ctx-type}", ctxType)
	decl := sexp.L(sexp.Sym("go/struct"), sexp.Sym(impl), sexp.L(sexp.Sym("doc"), sexp.Str(names.Replace(n.Text("about")))))
	for _, h := range n.Find("holds").Args() {
		decl.List = append(decl.List, sexp.L(sexp.Sym("field"), sexp.Sym(h.List[0].Atom), named(h.List[1], names)))
	}
	ctor := n.Find("new")
	params := sexp.L(sexp.Sym("params"))
	for _, p := range ctor.List[1].Args() {
		params.List = append(params.List, sexp.L(sexp.Sym(p.List[0].Atom), named(p.List[1], names)))
	}
	return []*sexp.Node{decl, sexp.L(sexp.Sym("go/func"), sexp.Sym("New"+impl),
		sexp.L(sexp.Sym("doc"), sexp.Str(names.Replace(n.Text("about-new")))),
		params,
		sexp.L(sexp.Sym("returns"), sexp.Sym("*"+impl)),
		sexp.L(sexp.Sym("body"), sexp.Str(names.Replace(ctor.List[2].Atom))))}
}

func hintsOf(n *sexp.Node) map[string]string {
	hints := map[string]string{}
	for _, h := range n.FindAll("hint") {
		hints[h.List[1].Atom] = h.List[2].Atom
	}
	return hints
}

// oneImport is the import of a tile whose capability takes one.
func oneImport(n *sexp.Node) [2]string {
	if im := importPairs(n); len(im) > 0 {
		return im[0]
	}
	return [2]string{}
}

// busOffer builds an event bus's registration: the type and constructor
// are declared, and Publish and On are left as holes with the tile's hints.
func busOffer(t *Tile, n *sexp.Node) {
	hints := hintsOf(n)
	t.Offer = offerOf(t, n)
	t.Offer.Impl = &compile.BusTransport{
		Suffix: n.Text("type-name"), Topics: topicsOf(n), Import: oneImport(n),
		Emit: func(in compile.BusInput) ([]*sexp.Node, error) { return declared(n, in.Impl, in.CtxType), nil },
		Hint: func(method, event string) string {
			return strings.NewReplacer("{event}", event).Replace(hints[strings.ToLower(method)])
		},
	}
}

// authOffer builds an auth scheme's registration: the type and
// constructor are declared, and Authenticate is left as a hole.
func authOffer(t *Tile, n *sexp.Node) {
	hints := hintsOf(n)
	t.Offer = offerOf(t, n)
	t.Offer.Impl = &compile.AuthScheme{
		Suffix: n.Text("type-name"), Topics: topicsOf(n), Import: oneImport(n),
		Emit: func(in compile.AuthInput) ([]*sexp.Node, error) { return declared(n, in.Impl, in.CtxType), nil },
		Hint: func(in compile.AuthInput) string {
			return strings.NewReplacer("{impl}", in.Impl, "{ctx-type}", in.CtxType).Replace(hints["authenticate"])
		},
	}
}

func costOf(n *sexp.Node) compile.Cost {
	var cost compile.Cost
	for _, term := range n.Find("cost").Args() {
		value, _ := strconv.Atoi(term.List[1].Atom)
		ct := compile.CostTerm{Dim: term.List[0].Atom, Value: value}
		if len(term.List) > 2 && !term.List[2].IsList {
			ct.Source, ct.Note = "derived", term.List[2].Atom
		} else if src := term.Find("source"); src != nil && len(src.Args()) > 0 {
			ct.Source = src.Args()[0].Atom
			if len(src.Args()) > 1 {
				ct.Note = src.Args()[1].Atom
			}
		}
		cost = append(cost, ct)
	}
	return cost
}

func claims(n *sexp.Node, head string) map[string]string {
	var out map[string]string
	for _, it := range n.FindAll(head) {
		if out == nil {
			out = map[string]string{}
		}
		out[it.List[1].List[0].Atom] = it.List[2].Atom
	}
	return out
}

func topicsOf(n *sexp.Node) []string {
	var out []string
	if topics := n.Find("topics"); topics != nil {
		for _, t := range topics.Args() {
			out = append(out, t.Atom)
		}
	}
	return out
}

func toolsOf(n *sexp.Node) []string {
	var out []string
	if req := n.Find("requires"); req != nil {
		for _, t := range req.Args() {
			out = append(out, t.List[1].Atom)
		}
	}
	return out
}
