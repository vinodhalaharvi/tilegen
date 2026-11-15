package sexp

import (
	"reflect"
	"strings"
	"testing"
)

func mustParse(t *testing.T, src string) []*Node {
	t.Helper()
	ns, err := Parse("f.sexp", src)
	if err != nil {
		t.Fatal(err)
	}
	return ns
}

func TestParseReadsFormsWithPositions(t *testing.T) {
	ns := mustParse(t, "; a comment\n(project shop ; trailing\n  (module \"example.com/shop\")\n  ())\nbare \"two words\"\n")
	if len(ns) != 3 {
		t.Fatalf("%d top-level forms, want 3", len(ns))
	}
	p := ns[0]
	if !p.IsList || p.Head() != "project" || len(p.List) != 4 {
		t.Fatalf("the first form: %s", p.Flat())
	}
	if got := p.Pos.String(); got != "f.sexp:2:1" {
		t.Errorf("the form's position: %s", got)
	}
	mod := p.List[2]
	if got := mod.Pos.String(); got != "f.sexp:3:3" {
		t.Errorf("a nested form's position: %s", got)
	}
	if s := mod.List[1]; !s.Quoted || s.Atom != "example.com/shop" || s.Pos.String() != "f.sexp:3:11" {
		t.Errorf("a string: %+v", s)
	}
	if e := p.List[3]; !e.IsList || e.List == nil || len(e.List) != 0 || e.Head() != "" || e.Args() != nil {
		t.Errorf("the empty list: %+v", e)
	}
	if ns[1].IsList || ns[1].Quoted || ns[1].Atom != "bare" {
		t.Errorf("a bare atom: %+v", ns[1])
	}
	if !ns[2].Quoted || ns[2].Atom != "two words" {
		t.Errorf("a quoted atom: %+v", ns[2])
	}
	if out := mustParse(t, "  ; only a comment\n\t\r\n"); out != nil {
		t.Errorf("a file of comments has %d forms", len(out))
	}
}

func TestParseStrings(t *testing.T) {
	for src, want := range map[string]string{
		`"a\"b"`:                         `a"b`,
		`"a\\b"`:                         `a\b`,
		`"line\nnext"`:                   "line\nnext",
		`"tab\there"`:                    "tab\there",
		`""`:                             "",
		`"(not a list) ; not a comment"`: "(not a list) ; not a comment",
	} {
		ns := mustParse(t, src)
		if len(ns) != 1 || !ns[0].Quoted || ns[0].Atom != want {
			t.Errorf("%s read as %q", src, ns[0].Atom)
		}
	}
	// An atom ends at a delimiter, with or without a space before it.
	ns := mustParse(t, `(a"b"c;x`+"\n)")
	if got := ns[0].Flat(); got != `(a "b" c)` {
		t.Errorf("atoms and delimiters: %s", got)
	}
}

func TestParseSaysWhereItStopped(t *testing.T) {
	for src, want := range map[string]string{
		"(a (b c)":        "f.sexp:1:1: unclosed '('",
		"(a\n  (b c":      "f.sexp:2:3: unclosed '('",
		")":               "f.sexp:1:1: unexpected ')'",
		"(a))":            "f.sexp:1:4: unexpected ')'",
		`(a "open`:        "f.sexp:1:4: unterminated string",
		`"bad \q escape"`: "f.sexp:1:1: bad string literal",
	} {
		ns, err := Parse("f.sexp", src)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: want %q, got %v", src, want, err)
		}
		if ns != nil {
			t.Errorf("%q: forms were returned beside the error", src)
		}
	}
	if got := (Pos{}).String(); got != "<generated>" {
		t.Errorf("a node nobody wrote is at %q", got)
	}
}

func TestBuildersAndAccessors(t *testing.T) {
	n := L(Sym("entity"), Sym("Order"),
		L(Sym("field"), Sym("ID"), Sym("string")),
		L(Sym("doc"), Str("an order")),
		L(Sym("field"), Sym("Total"), Sym("int64")),
		L(L(Sym("nested")), Sym("x")),
		Sym("field"))
	if n.Head() != "entity" || len(n.Args()) != 6 {
		t.Errorf("head %q, %d args", n.Head(), len(n.Args()))
	}
	if f := n.Find("field"); f == nil || f.List[1].Atom != "ID" {
		t.Errorf("Find returns the first: %v", f)
	}
	if fs := n.FindAll("field"); len(fs) != 2 || fs[1].List[1].Atom != "Total" {
		t.Errorf("FindAll returns the lists headed field, in order: %d", len(fs))
	}
	if n.Find("nested") != nil || n.Find("nope") != nil || n.FindAll("nope") != nil {
		t.Error("Find looks one level down, and only at heads")
	}
	if n.Text("doc") != "an order" || n.Text("field") != "ID" || n.Text("nope") != "" {
		t.Errorf("Text: %q %q %q", n.Text("doc"), n.Text("field"), n.Text("nope"))
	}
	if L(Sym("a"), L(Sym("b"), L(Sym("c")))).Text("b") != "" {
		t.Error("Text of a form whose first argument is a list should be empty")
	}
	// None of them minds a nil or an atom.
	var none *Node
	if none.Head() != "" || none.Args() != nil || Sym("x").Head() != "" || Sym("x").Args() != nil || L().Head() != "" || L(L()).Head() != "" {
		t.Error("Head and Args on nil, an atom and an empty list")
	}
	if got := Syms("a", "b"); len(got) != 2 || got[0].Atom != "a" || got[1].IsList || Syms() == nil {
		t.Errorf("Syms: %v", got)
	}
}

// What is printed reads back as the same tree.
func TestPrintingRoundTrips(t *testing.T) {
	same := func(a, b *Node) bool {
		var eq func(a, b *Node) bool
		eq = func(a, b *Node) bool {
			if a.IsList != b.IsList || a.Atom != b.Atom || len(a.List) != len(b.List) {
				return false
			}
			for i := range a.List {
				if !eq(a.List[i], b.List[i]) {
					return false
				}
			}
			return true
		}
		return eq(a, b)
	}
	long := L(Sym("project"), Sym("shop"))
	for i := 0; i < 12; i++ {
		long.List = append(long.List, L(Sym("field"), Sym("Name"), Str("a string with spaces"), L(Sym("deep"), L(Sym("deeper"), Sym("x")))))
	}
	for _, n := range []*Node{
		Sym("atom"), Str("quoted"), Str(""), Sym(""), Sym("has space"), Sym("semi;colon"), Sym(`quo"te`), Str("new\nline"),
		L(), L(L(), L(L())), L(Sym("a"), Str("b c"), L(Sym("d"))), long,
	} {
		for name, text := range map[string]string{"Flat": n.Flat(), "String": n.String()} {
			back, err := Parse("t", text)
			if err != nil || len(back) != 1 || !same(n, back[0]) {
				t.Errorf("%s of %s does not read back: %v\n%s", name, n.Flat(), err, text)
			}
		}
	}
	// Short forms stay on one line; long ones break, a sub-list per line.
	if s := L(Sym("a"), L(Sym("b"))).String(); s != "(a (b))" {
		t.Errorf("a short form: %q", s)
	}
	lines := strings.Split(long.String(), "\n")
	if lines[0] != "(project shop" || len(lines) != 13 || !strings.HasPrefix(lines[1], "  (field Name ") || !strings.HasSuffix(lines[12], "))") {
		t.Errorf("a long form:\n%s", long.String())
	}
	for _, l := range lines {
		if len(l) > 82 {
			t.Errorf("a line of %d characters: %s", len(l), l)
		}
	}
	if got := Dump([]*Node{Sym("a"), L(Sym("b"))}); got != "a\n\n(b)\n" {
		t.Errorf("Dump: %q", got)
	}
	if got := Dump(nil); got != "\n" {
		t.Errorf("Dump of nothing: %q", got)
	}
}

func TestInheritPos(t *testing.T) {
	written := Pos{"f.sexp", 7, 3}
	kept := &Node{Atom: "kept", Pos: Pos{"g.sexp", 2, 1}}
	n := L(Sym("made"), L(Sym("deep"), kept))
	InheritPos(n, written)
	if n.Pos != written || n.List[0].Pos != written || n.List[1].Pos != written || n.List[1].List[0].Pos != written {
		t.Error("generated nodes should take the position given")
	}
	if kept.Pos.File != "g.sexp" {
		t.Error("a node that has a position keeps it")
	}
}

func TestMatch(t *testing.T) {
	form := mustParse(t, `(entity Order (field ID string) (field Total int64) "doc")`)[0]
	for _, c := range []struct {
		pat  string
		want bool
		vars map[string]string // variable -> the nodes it bound, printed
	}{
		{"(entity ?name ?items...)", true, map[string]string{"name": "Order", "items": `(field ID string) (field Total int64) "doc"`}},
		{"(entity ?name ?a ?b ?c)", true, map[string]string{"name": "Order", "a": "(field ID string)", "c": `"doc"`}},
		{"(entity _ _ _ _)", true, map[string]string{}},
		{"(entity Order ?rest...)", true, map[string]string{"rest": `(field ID string) (field Total int64) "doc"`}},
		{"(entity ?name (field ?f ?t) ?rest...)", true, map[string]string{"f": "ID", "t": "string", "rest": `(field Total int64) "doc"`}},
		{"(entity ?name ?a ?b ?c ?none...)", true, map[string]string{"none": ""}},
		{"?whole", true, map[string]string{"whole": form.Flat()}},
		{"_", true, map[string]string{}},
		{"(entity ?name)", false, nil},
		{"(entity ?name ?a ?b ?c ?d)", false, nil},
		{"(struct ?name ?items...)", false, nil},
		{"(entity Customer ?items...)", false, nil},
		{"(entity ?name (method ?m) ?rest...)", false, nil},
		{"entity", false, nil},
		{"(entity (?name) ?rest...)", false, nil},
	} {
		b := Bindings{}
		if got := Match(Pat(c.pat), form, b); got != c.want {
			t.Errorf("%s matches: %v, want %v", c.pat, got, c.want)
			continue
		}
		for k, want := range c.vars {
			parts := make([]string, len(b.Rest(k)))
			for i, n := range b.Rest(k) {
				parts[i] = n.Flat()
			}
			if got := strings.Join(parts, " "); got != want {
				t.Errorf("%s binds %s to %q, want %q", c.pat, k, got, want)
			}
		}
	}
	// An atom pattern matches the identical atom, quoted or not.
	if !Match(Pat("durable"), Sym("durable"), Bindings{}) || Match(Pat("durable"), Sym("durabl"), Bindings{}) || Match(Pat("durable"), L(Sym("durable")), Bindings{}) {
		t.Error("an atom pattern")
	}
}

func TestBindings(t *testing.T) {
	b := Bindings{"name": {Sym("Order")}, "list": {L(Sym("a"))}, "many": {Sym("x"), Sym("y")}, "empty": {}}
	if b.One("name").Atom != "Order" || b.One("many").Atom != "x" || b.One("empty") != nil || b.One("absent") != nil {
		t.Error("One is the first node bound, or nil")
	}
	if b.Atom("name") != "Order" || b.Atom("list") != "" || b.Atom("absent") != "" {
		t.Error("Atom is the first node's text when it is an atom")
	}
	if !reflect.DeepEqual(b.Rest("many"), []*Node{b["many"][0], b["many"][1]}) || b.Rest("absent") != nil {
		t.Error("Rest is every node bound")
	}
}

// Patterns are written by tilegen's authors: a bad one is a bug, said loudly.
func TestPatPanicsOnABadPattern(t *testing.T) {
	for _, src := range []string{"(unclosed ?x", "", "(one) (two)", ")"} {
		func() {
			defer func() {
				if r := recover(); r == nil || !strings.Contains(r.(string), "bad pattern") {
					t.Errorf("Pat(%q) should panic with the pattern named, got %v", src, r)
				}
			}()
			Pat(src)
		}()
	}
}
