package tiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

// rocks is a whole tile; each test changes one thing about it.
const rocks = `(tile rocks
  (offers store)
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
  (module example.com/rocks)
  (symbols rocks.DB.Get rocks.Open)
  (cost (llm-work 4 (source derived "by hand")) (maintenance 3 (source measured)) (runtime 1) (dependency 2 "one module"))
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
	if got[0].Tile != "rocks" || got[0].Name != "rk" || got[0].Offers != "store" || got[0].Offer != nil {
		t.Errorf("the tile: %+v", got[0])
	}
	b := got[0].Store
	if b.Name != "rk" || b.Tile != "rocks" || b.Doc != "embedded key-value" || b.Engine != "none" ||
		b.ErrorPackage != "rocks" || b.Imports["rocks"] != "example.com/rocks" ||
		strings.Join(b.Topics, " ") != "rocks embedded" || strings.Join(b.Requires, " ") != "rockctl" {
		t.Errorf("what it is: %+v", b)
	}
	if len(b.Satisfies) != 3 || b.Satisfies["durable"] != "a synced log" || len(b.IllegalWhen) != 1 ||
		b.IllegalWhen["cross-process"] != "one process holds the directory" || len(b.Unverified) != 2 || b.Unverified["inbox"] != "not generated" {
		t.Errorf("what it claims: %+v %+v %+v", b.Satisfies, b.IllegalWhen, b.Unverified)
	}
	want := compile.Cost{{Dim: "llm-work", Value: 4, Source: "derived", Note: "by hand"}, {Dim: "maintenance", Value: 3, Source: "measured"}, {Dim: "runtime", Value: 1}, {Dim: "dependency", Value: 2, Source: "derived", Note: "one module"}}
	if len(b.Cost) != 4 || b.Cost[0] != want[0] || b.Cost[1] != want[1] || b.Cost[2] != want[2] || b.Cost[3] != want[3] {
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
		"empty":             {"; nothing\n", "no (tile ...) in it"},
		"not a tile":        {`(project x)`, "a tile file holds (tile NAME (offers CAPABILITY) ...) forms, got (project x)"},
		"unparsable":        {`(tile rocks`, "unclosed"},
		"no constructor":    {change(`(new (params (db *rocks.DB) (seen "map[{id}]bool"))`, `(holdz (params) `), "a tile contains"},
		"holds without new": {strings.Replace(change(`(hint custom "Do it for {names}.")`, ""), `(new (params (db *rocks.DB) (seen "map[{id}]bool"))`+"\n    "+`"return &{impl}{db: db, prefix: \"{names}/\", cache: map[{id}]*{entity}{}} // {name}")`, "", 1), "(tile rocks ...) needs (holds ...) and (new ...)"},
		"a misspelt name":   {change("{names}/", "{entitys}/"), "{entitys} is not a name tilegen fills in; it knows {impl}, {entity}, {name}, {names}, {id}"},
		"a name in a type":  {change(`(prefix string)`, `(prefix "{nam}")`), "{nam} is not a name tilegen fills in"},
		"a field elsewhere": {change("Get the {entity}", "Get the {field}"), "(hint get ...) uses {field}, and only a by-field method has one"},
		"a param in custom": {change("Do it for {names}.", "Do it for {param}."), "{param} is not a name tilegen fills in"},
		"an unknown op":     {change("(hint get-by", "(hint find-by"), "a hint is for one kind of method: get, list, save, delete, count, list-by, get-by, count-by, exists-by, delete-by, custom; got find-by"},
		"a hint twice":      {change(`(hint custom "Do it for {names}.")`, `(hint get "again")`), "duplicate (hint get ...) in tile rocks"},
		"an empty hint":     {change(`"Do it for {names}."`, `" "`), "(hint custom ...) needs its text"},
		"a field twice":     {change("(prefix string)", "(db string)"), "(holds ...) names db twice"},
		"a field's shape":   {change("(prefix string)", "(prefix)"), "expected (?name ?type)"},
		"a body that is not text": {change(`"return &{impl}{db: db, prefix: \"{names}/\", cache: map[{id}]*{entity}{}} // {name}"`, `body`),
			"(new ...) ends with the constructor's body as a string"},
		"a lookup it ruled out": {change(`(satisfies (lookup-by-field) "an index per field")`, `(illegal-when (lookup-by-field) "keys only")`),
			"rules out (lookup-by-field), so no store it serves has a get-by method: remove (hint get-by ...)"},
		"a requirement unanswered":    {change(`(unverified (inbox) "not generated")`, ""), "says nothing about (inbox)"},
		"offering what a file cannot": {change("(offers store)", "(offers api)"), `a tile written as a file cannot offer "api"; it can offer auth, event-bus, store`},
		"offering nothing":            {change("(offers store)", ""), "(tile rocks ...) needs an (offers ...): one of auth, event-bus, store"},
		"a symbol that is a list":     {change("(alias rk)", "(alias rk) (symbols (x))"), "(symbols ...) lists names like pebble.DB.Get"},
		"a name that is not a type":   {change("(alias rk)", "(alias r-k)"), "a store's short name becomes part of a Go type"},
	} {
		got, err := LoadTiles("rocks.sexp", c.src)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want %q, got %v (%d tiles)", name, c.want, err, len(got))
		}
	}

	// Without what the store holds, it is a tile still to be written in Go.
	half := rocks[:strings.Index(rocks, "  (holds")] + ")"
	if _, err := LoadTiles("rocks.sexp", half); err == nil || !strings.Contains(err.Error(), "(tile rocks ...) needs (holds ...) and (new ...)") {
		t.Errorf("a tile with nothing to hold: %v", err)
	}
}

// The text of a tile kept in a file is also a draft: pasted into the
// studio, or given to a compile, it takes part in decisions as it is.
func TestATileFileIsAlsoADraft(t *testing.T) {
	drafts, err := compile.ParseDrafts(rocks)
	if err != nil || len(drafts) != 1 {
		t.Fatalf("%d drafts, %v", len(drafts), err)
	}
	d := drafts[0]
	if d.Tile != "rocks" || d.Capability != "store" || d.Satisfies["durable"] != "a synced log" || d.IllegalFor["cross-process"] == "" || d.Unverified["inbox"] == "" {
		t.Errorf("the draft: %+v", d)
	}
	var llm, dep compile.CostTerm
	for _, c := range d.Cost {
		switch c.Dim {
		case "llm-work":
			llm = c
		case "dependency":
			dep = c
		}
	}
	if llm.Value != 4 || llm.Note != "by hand" || dep.Value != 2 || dep.Note != "one module" {
		t.Errorf("its costs: %+v", d.Cost)
	}
	// And every tile tilegen ships as a file reads as one, under a name
	// of its own.
	files, _ := filepath.Glob("*.sexp")
	for _, f := range files {
		src, _ := os.ReadFile(f)
		if got, err := compile.ParseDrafts(strings.Replace(string(src), "(tile ", "(tile my-", 1)); err != nil || len(got) != 1 {
			t.Errorf("%s as a draft: %d, %v", f, len(got), err)
		}
	}
}

// A tile for a capability whose tiles declare the whole type: the same
// form, with the type's name and its two doc comments.
func TestATileOfAnotherCapability(t *testing.T) {
	bus, err := os.ReadFile("bus_rabbitmq.sexp")
	if err != nil {
		t.Fatal(err)
	}
	queue := strings.Replace(string(bus), "(tile rabbitmq", "(tile hare", 1)
	got, err := LoadTiles("hare.sexp", queue)
	if err != nil || len(got) != 1 {
		t.Fatalf("%d tiles, %v", len(got), err)
	}
	h := got[0]
	tr, ok := h.Offer.Impl.(*compile.BusTransport)
	if h.Tile != "hare" || h.Offers != "event-bus" || h.Store != nil || !ok || tr.Suffix != "RabbitBus" || tr.Import[0] != "amqp" || h.Offer.Capability != "event-bus" {
		t.Fatalf("the tile: %+v", h)
	}
	nodes, err := tr.Emit(compile.BusInput{Impl: "HareBus"})
	if err != nil || len(nodes) != 2 || nodes[0].List[1].Atom != "HareBus" || nodes[1].List[1].Atom != "NewHareBus" ||
		!strings.HasPrefix(nodes[0].Text("doc"), "HareBus is a Bus backed by RabbitMQ") || !strings.Contains(nodes[1].Text("body"), "&HareBus{") {
		t.Errorf("what it declares: %v %v", nodes, err)
	}
	if pub, on := tr.Hint("Publish", "OrderPlaced"), tr.Hint("On", "OrderPlaced"); !strings.Contains(pub, "OrderPlaced") || !strings.Contains(on, "OrderPlaced") || pub == on || tr.Hint("Close", "X") != "" {
		t.Errorf("its hints: %q %q", pub, on)
	}

	change := func(old, new string) string {
		if !strings.Contains(queue, old) {
			t.Fatalf("the tile has no %q to change", old)
		}
		return strings.Replace(queue, old, new, 1)
	}
	for name, c := range map[string]struct{ src, want string }{
		"no type name":                        {change("(type-name RabbitBus)", ""), "(tile hare ...) needs a (type-name ...): the bus it generates is a type of its own"},
		"a type name that is not one":         {change("(type-name RabbitBus)", "(type-name rabbit-bus)"), "(type-name ...) is a Go type's name, like KafkaBus"},
		"no doc for the type":                 {change("(about\n", "(abouts\n"), "a tile contains"},
		"a hint missing":                      {change("(hint on\n", "(hint publish\n"), "duplicate (hint publish ...)"},
		"a store's hint":                      {change("(hint on\n", "(hint get\n"), "a hint is for one kind of method: publish, on; got get"},
		"a store's name":                      {change("{event}", "{entity}"), "{entity} is not a name tilegen fills in; it knows {event}, {impl}, {ctx-type}"},
		"a requirement of another capability": {change("(satisfies (cross-process)", "(satisfies (lookup-by-field)"), "(lookup-by-field) is not something a spec asks of event-bus; that is "},
	} {
		if _, err := LoadTiles("hare.sexp", c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want %q, got %v", name, c.want, err)
		}
	}
	// More than one import is carried: one as the tile's own, the rest beside it.
	two, err := LoadTiles("hare.sexp", change("(import amqp ", "(import other example.com/other)\n  (import amqp "))
	if err != nil {
		t.Fatalf("a bus with two imports: %v", err)
	}
	if tr := two[0].Offer.Impl.(*compile.BusTransport); tr.Import[0] != "amqp" || len(tr.More) != 1 || tr.More[0][1] != "example.com/other" {
		t.Errorf("a bus with two imports: %v and %v", tr.Import, tr.More)
	}
	// One hint of the two is not enough.
	onAt := strings.Index(queue, "(hint on")
	if _, err := LoadTiles("hare.sexp", queue[:onAt]+")"); err == nil || !strings.Contains(err.Error(), "(tile hare ...) needs a (hint on ...)") {
		t.Errorf("a bus with no hint for On: %v", err)
	}
	// And a store is named and described from its entity, not by the tile.
	if _, err := LoadTiles("rocks.sexp", strings.Replace(rocks, "(alias rk)", "(alias rk) (type-name RocksStore)", 1)); err == nil ||
		!strings.Contains(err.Error(), "has a (type-name ...), and a store's type is named and described from the entity") {
		t.Errorf("a store with a type name: %v", err)
	}
}
