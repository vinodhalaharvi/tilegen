package tiles

import (
	"strings"
	"testing"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

// rocks is a whole tile; each test changes one thing about it.
const rocks = `(tile-spec rocks
  (kind store-backend)
  (doc "embedded key-value")
  (alias rk)
  (engine none)
  (import rocks example.com/rocks)
  (error-package rocks)
  (topics rocks embedded)
  (requires (tool rockctl))
  (satisfies (durable) "a synced log")
  (satisfies (no-broker) "a library")
  (satisfies (lookup-by-field) "an index per field")
  (illegal-when (cross-process) "one process holds the directory")
  (unverified (outbox) "not generated")
  (unverified (inbox) "not generated")
  (cost (llm-work 4 (source derived "by hand")) (maintenance 3 (source measured)) (runtime 1))
  (holds (db *rocks.DB) (cache "map[{id}]*{entity}") (prefix string))
  (new (params (db *rocks.DB) (seen "map[{id}]bool"))
    "return &{impl}{db: db, prefix: \"{names}/\", cache: map[{id}]*{entity}{}} // {name}")
  (hint get "Get the {entity} from s.db under s.prefix; opts are rocks.Options{Sync: true}.")
  (hint get-by "Scan the {field} index for {param}.")
  (hint custom "Do it for {names}."))`

func meth(op, field string, doc string) *sexp.Node {
	m := sexp.L(sexp.Sym("method"), sexp.Sym("M"), sexp.L(sexp.Sym("op"), sexp.Sym(op)),
		sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("ctx"), sexp.Sym("context.Context")), sexp.L(sexp.Sym("addr"), sexp.Sym("string"))))
	if field != "" {
		m.List[2].List = append(m.List[2].List, sexp.Sym(field))
	}
	if doc != "" {
		m.List = append(m.List, sexp.L(sexp.Sym("doc"), sexp.Str(doc)))
	}
	return m
}

func TestAWholeTileSpecIsATile(t *testing.T) {
	got, err := LoadTiles("rocks.sexp", rocks)
	if err != nil || len(got) != 1 {
		t.Fatalf("%d tiles, %v", len(got), err)
	}
	b := got[0]
	if b.Name != "rk" || b.Tile != "rocks" || b.Doc != "embedded key-value" || b.Engine != "none" ||
		b.ErrorPackage != "rocks" || b.Imports["rocks"] != "example.com/rocks" ||
		strings.Join(b.Topics, " ") != "rocks embedded" || strings.Join(b.Requires, " ") != "rockctl" {
		t.Errorf("what it is: %+v", b)
	}
	if len(b.Satisfies) != 3 || b.Satisfies["durable"] != "a synced log" || len(b.IllegalWhen) != 1 ||
		b.IllegalWhen["cross-process"] != "one process holds the directory" || len(b.Unverified) != 2 || b.Unverified["inbox"] != "not generated" {
		t.Errorf("what it claims: %+v %+v %+v", b.Satisfies, b.IllegalWhen, b.Unverified)
	}
	want := compile.Cost{{Dim: "llm-work", Value: 4, Source: "derived", Note: "by hand"}, {Dim: "maintenance", Value: 3, Source: "measured"}, {Dim: "runtime", Value: 1}}
	if len(b.Cost) != 3 || b.Cost[0] != want[0] || b.Cost[1] != want[1] || b.Cost[2] != want[2] {
		t.Errorf("what it costs: %+v", b.Cost)
	}

	p, err := b.Implement(compile.StoreInput{Entity: "LineItem", Impl: "RkLineItemStore", IDType: "int64"})
	if err != nil {
		t.Fatal(err)
	}
	var fields []string
	for _, f := range p.Fields {
		fields = append(fields, f.Flat())
	}
	if strings.Join(fields, " ") != `(field db *rocks.DB) (field cache "map[int64]*LineItem") (field prefix string)` {
		t.Errorf("what the store holds: %s", fields)
	}
	if p.Params.Flat() != `(params (db *rocks.DB) (seen "map[int64]bool"))` {
		t.Errorf("what the constructor takes: %s", p.Params.Flat())
	}
	if p.Body != `return &RkLineItemStore{db: db, prefix: "lineItems/", cache: map[int64]*LineItem{}} // lineItem` {
		t.Errorf("the constructor: %s", p.Body)
	}
	for _, c := range []struct{ op, field, doc, want string }{
		// Braces that are Go's own are left alone.
		{"get", "", "", "Get the LineItem from s.db under s.prefix; opts are rocks.Options{Sync: true}."},
		{"get-by", "Email", "", "Scan the Email index for addr."},
		{"custom", "", "anything", "Do it for lineItems."},
		// An op with no hint falls back to what the method documents.
		{"save", "", "", ""},
		{"list-by", "Email", "newest first", "Implement it as documented: newest first"},
	} {
		if got := p.Hint(meth(c.op, c.field, c.doc)); got != c.want {
			t.Errorf("hint for %s: %q, want %q", c.op, got, c.want)
		}
	}
	// One load does not leak into the next: a second store gets its own names.
	p2, _ := b.Implement(compile.StoreInput{Entity: "Note", Impl: "RkNoteStore", IDType: "string"})
	if p2.Body != `return &RkNoteStore{db: db, prefix: "notes/", cache: map[string]*Note{}} // note` || len(p2.Params.List) != 3 || len(p.Params.List) != 3 {
		t.Errorf("a second store: %s %s", p2.Body, p2.Params.Flat())
	}
}

func TestATileFileIsRefusedForWhatIsWrongWithIt(t *testing.T) {
	change := func(old, new string) string {
		if !strings.Contains(rocks, old) {
			t.Fatalf("the tile has no %q to change", old)
		}
		return strings.Replace(rocks, old, new, 1)
	}
	for name, c := range map[string]struct{ src, want string }{
		"empty":             {"; nothing\n", "no (tile-spec ...) in it"},
		"not a tile":        {`(project x)`, "a tile file holds (tile-spec ...) forms, got (project x)"},
		"unparsable":        {`(tile-spec rocks`, "unclosed"},
		"no constructor":    {change(`(new (params (db *rocks.DB) (seen "map[{id}]bool"))`, `(holdz (params) `), "a tile-spec contains"},
		"holds without new": {strings.Replace(change(`(hint custom "Do it for {names}.")`, ""), `(new (params (db *rocks.DB) (seen "map[{id}]bool"))`+"\n    "+`"return &{impl}{db: db, prefix: \"{names}/\", cache: map[{id}]*{entity}{}} // {name}")`, "", 1), "(tile-spec rocks ...) needs (holds ...) and (new ...)"},
		"a misspelt name":   {change("{names}/", "{entitys}/"), "{entitys} is not a name tilegen fills in; it knows {impl}, {entity}, {name}, {names}, {id}"},
		"a name in a type":  {change(`(prefix string)`, `(prefix "{nam}")`), "{nam} is not a name tilegen fills in"},
		"a field elsewhere": {change("Get the {entity}", "Get the {field}"), "(hint get ...) uses {field}, and only a by-field method has one"},
		"a param in custom": {change("Do it for {names}.", "Do it for {param}."), "{param} is not a name tilegen fills in"},
		"an unknown op":     {change("(hint get-by", "(hint find-by"), "a hint is for one kind of method: get, list, save, delete, count, list-by, get-by, count-by, exists-by, delete-by, custom; got find-by"},
		"a hint twice":      {change(`(hint custom "Do it for {names}.")`, `(hint get "again")`), "duplicate (hint get ...) in tile-spec rocks"},
		"an empty hint":     {change(`"Do it for {names}."`, `" "`), "(hint custom ...) needs its text"},
		"a field twice":     {change("(prefix string)", "(db string)"), "(holds ...) names db twice"},
		"a field's shape":   {change("(prefix string)", "(prefix)"), "expected (?name ?type)"},
		"a body that is not text": {change(`"return &{impl}{db: db, prefix: \"{names}/\", cache: map[{id}]*{entity}{}} // {name}"`, `body`),
			"(new ...) ends with the constructor's body as a string"},
		"a lookup it ruled out": {change(`(satisfies (lookup-by-field) "an index per field")`, `(illegal-when (lookup-by-field) "keys only")`),
			"rules out (lookup-by-field), so no store it serves has a get-by method: remove (hint get-by ...)"},
		"a requirement unanswered":  {change(`(unverified (inbox) "not generated")`, ""), "says nothing about (inbox)"},
		"a name that is not a type": {change("(alias rk)", "(alias r-k)"), "a store's short name becomes part of a Go type"},
	} {
		got, err := LoadTiles("rocks.sexp", c.src)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want %q, got %v (%d tiles)", name, c.want, err, len(got))
		}
	}

	// Without what the store holds, it is a tile still to be written in Go.
	half := rocks[:strings.Index(rocks, "  (holds")] + ")"
	if _, err := LoadTiles("rocks.sexp", half); err == nil || !strings.Contains(err.Error(), "(tile-spec rocks ...) needs (holds ...) and (new ...)") {
		t.Errorf("a tile with nothing to hold: %v", err)
	}
}
