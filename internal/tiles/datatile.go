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
// data: a (tile-spec ...) with what it claims, what it costs, what the
// generated store holds, and a hint for each method. Nothing in it runs.
// The tiles in specs/ are built into tilegen from those files.
//
// A tile that has to work something out from the entity, such as a schema,
// is Go: see store_postgres_sqlc.go.

//go:embed specs/*.sexp
var specFiles embed.FS

func init() {
	names, err := fs.Glob(specFiles, "specs/*.sexp")
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
		for _, b := range backends {
			compile.RegisterStoreBackend(b)
		}
	}
}

// LoadTiles reads a file of (tile-spec ...) forms and returns the tiles
// they describe, checked but not registered.
func LoadTiles(file, src string) ([]*compile.Backend, error) {
	forms, err := sexp.Parse(file, src)
	if err != nil {
		return nil, err
	}
	if len(forms) == 0 {
		return nil, fmt.Errorf("%s: no (tile-spec ...) in it", file)
	}
	var out []*compile.Backend
	for _, n := range forms {
		b, err := loadTile(n)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

func loadTile(n *sexp.Node) (*compile.Backend, error) {
	if n.Head() != "tile-spec" {
		return nil, fmt.Errorf("%s: a tile file holds (tile-spec ...) forms, got %s", n.Pos, compile.Short(n))
	}
	if err := compile.Check(func(v *compile.Validator) {
		validateTileSpec(v, n)
	}); err != nil {
		return nil, err
	}
	name := n.List[1].Atom
	if kind := n.Text("kind"); kind != "store-backend" {
		return nil, fmt.Errorf("%s: (tile-spec %s ...): a tile read from a file is a store-backend, got %q", n.Pos, name, kind)
	}
	return storeBackend(name, n), nil
}

// storeBackend builds the registration from a checked tile-spec.
func storeBackend(name string, n *sexp.Node) *compile.Backend {
	b := &compile.Backend{
		Name:         name,
		Tile:         name,
		Doc:          n.Text("doc"),
		Engine:       n.Text("engine"),
		ErrorPackage: n.Text("error-package"),
		Form:         n.Text("form"),
	}
	if alias := n.Text("alias"); alias != "" {
		b.Name = alias
	}
	for _, term := range n.Find("cost").Args() {
		value, _ := strconv.Atoi(term.List[1].Atom)
		ct := compile.CostTerm{Dim: term.List[0].Atom, Value: value}
		if src := term.Find("source"); src != nil && len(src.Args()) > 0 {
			ct.Source = src.Args()[0].Atom
			if len(src.Args()) > 1 {
				ct.Note = src.Args()[1].Atom
			}
		}
		b.Cost = append(b.Cost, ct)
	}
	for _, claim := range []struct {
		head string
		into *map[string]string
	}{{"illegal-when", &b.IllegalWhen}, {"unverified", &b.Unverified}, {"satisfies", &b.Satisfies}} {
		for _, it := range n.FindAll(claim.head) {
			if *claim.into == nil {
				*claim.into = map[string]string{}
			}
			(*claim.into)[it.List[1].List[0].Atom] = it.List[2].Atom
		}
	}
	for _, im := range importPairs(n) {
		if b.Imports == nil {
			b.Imports = map[string]string{}
		}
		b.Imports[im[0]] = im[1]
	}
	if topics := n.Find("topics"); topics != nil {
		for _, t := range topics.Args() {
			b.Topics = append(b.Topics, t.Atom)
		}
	}
	if req := n.Find("requires"); req != nil {
		for _, t := range req.Args() {
			b.Requires = append(b.Requires, t.List[1].Atom)
		}
	}

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
	return b
}

// The names a tile's text may stand in for, each written {so}.
var (
	storeNames = []string{"impl", "entity", "name", "names", "id"}
	hintNames  = append([]string{"field", "param"}, storeNames...)
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
func validateHolds(v *compile.Validator, it *sexp.Node) {
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
		checkNames(v, f, typ.Atom, storeNames)
	}
}

// validateNew checks (new (params (db *pebble.DB)) "return &{impl}{...}"):
// what the constructor takes and its body.
func validateNew(v *compile.Validator, it *sexp.Node) {
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
		checkNames(v, p, pb.One("type").Atom, storeNames)
	}
	body := nb.One("body")
	if body.IsList || !body.Quoted || strings.TrimSpace(body.Atom) == "" {
		v.Bad(it, "(new ...) ends with the constructor's body as a string, like \"return &{impl}{db: db}\"")
		return
	}
	checkNames(v, it, body.Atom, storeNames)
}

// hintOps are the method kinds a hint may be for.
var (
	plainOps = []string{"get", "list", "save", "delete", "count"}
	fieldOps = []string{"list-by", "get-by", "count-by", "exists-by", "delete-by"}
	hintOps  = append(append([]string{}, plainOps...), append(fieldOps, "custom")...)
)

// validateHint checks (hint get "..."), and returns the op it is for.
func validateHint(v *compile.Validator, it *sexp.Node) string {
	hb := v.Shape(it, "(hint ?op ?text)")
	if hb == nil {
		return ""
	}
	op, text := hb.One("op"), hb.One("text")
	if op.IsList || !compile.Contains(hintOps, op.Atom) {
		v.Bad(it, "a hint is for one kind of method: %s; got %s%s", strings.Join(hintOps, ", "),
			compile.Short(op), compile.DidYouMean(op.Atom, hintOps))
		return ""
	}
	if text.IsList || !text.Quoted || strings.TrimSpace(text.Atom) == "" {
		v.Bad(it, "(hint %s ...) needs its text: what the method has to do, in the library's own terms", op.Atom)
		return op.Atom
	}
	known := storeNames
	if op.Atom != "custom" {
		known = hintNames
	}
	if !compile.Contains(fieldOps, op.Atom) && strings.Contains(text.Atom, "{field}") {
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
	var all []*compile.Backend
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
			for _, name := range []string{b.Tile, b.Name} {
				if nameTaken(name) {
					return nil, fmt.Errorf("%s: tilegen already has a tile called %s; give this one a name of its own", file, name)
				}
				if prev := taken[name]; prev != "" && prev != file+"#"+b.Tile {
					return nil, fmt.Errorf("%s: %s is also the name of a tile in %s", file, name, strings.SplitN(prev, "#", 2)[0])
				}
				taken[name] = file + "#" + b.Tile
			}
			all = append(all, b)
		}
	}
	var added []string
	for _, b := range all {
		compile.RegisterStoreBackend(b)
		added = append(added, b.Tile)
	}
	return added, nil
}

// nameTaken reports whether a tile, or the short name of one, is already
// called this.
func nameTaken(name string) bool {
	if _, has := compile.Backends[name]; has {
		return true
	}
	for _, offers := range compile.Offers {
		for _, o := range offers {
			if o.Tile == name || compile.TileAliases[o.Tile] == name {
				return true
			}
		}
	}
	return false
}
