package compile

import (
	"reflect"
	"strings"
	"testing"
)

func TestDidYouMean(t *testing.T) {
	opts := []string{"durable", "cross-process", "lookup-by-field", "ID"}
	for word, want := range map[string]string{
		"durabel":        "durable",       // two neighbours swapped
		"durabl":         "durable",       // one letter missing
		"Durable":        "durable",       // case is not a typo's worth of distance
		"cross-proces":   "cross-process", // one edit in a long word
		"lookup-by-feld": "lookup-by-field",
		"durable":        "", // exact: not a typo
		"replay":         "", // nothing close
		"IX":             "", // too short to call a typo
		"":               "",
	} {
		if got := Closest(word, opts); got != want {
			t.Errorf("Closest(%q) = %q, want %q", word, got, want)
		}
	}
	if got := DidYouMean("durabel", opts); got != " (did you mean durable?)" {
		t.Errorf("DidYouMean: %q", got)
	}
	if got := DidYouMean("replay", opts); got != "" {
		t.Errorf("DidYouMean with nothing close: %q", got)
	}
	if Closest("durabel", nil) != "" {
		t.Error("no options, no suggestion")
	}
}

func TestOSADistance(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"", "", 0}, {"abc", "abc", 0}, {"", "abc", 3}, {"abc", "", 3},
		{"abc", "abd", 1}, {"abc", "ab", 1}, {"ab", "abc", 1},
		{"ab", "ba", 1},     // a swap is one edit
		{"abcd", "badc", 2}, // two swaps
		{"kitten", "sitting", 3},
		{"héllo", "hello", 1}, // by rune, not by byte
	} {
		if got := OSADistance(c.a, c.b); got != c.want {
			t.Errorf("OSADistance(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if OSADistance(c.a, c.b) != OSADistance(c.b, c.a) {
			t.Errorf("OSADistance(%q, %q) is not symmetric", c.a, c.b)
		}
	}
}

func TestNames(t *testing.T) {
	for in, want := range map[string]string{
		"ID": "id", "PlacedAt": "placed_at", "UserID": "user_id", "HTTPServer": "http_server",
		"Order": "order", "order": "order", "A": "a", "": "", "Line2Item": "line2_item", "APIKey": "api_key",
	} {
		if got := Snake(in); got != want {
			t.Errorf("Snake(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"ID": "id", "PlacedAt": "placedAt", "HTTPServer": "httpServer", "UserID": "userId", "Order": "order", "": ""} {
		if got := Camel(in); got != want {
			t.Errorf("Camel(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"Order": "order", "Type": "v", "Map": "v", "Func": "v", "": "v", "PlacedAt": "placedAt"} {
		if got := ParamName(in); got != want {
			t.Errorf("ParamName(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"order": "orders", "category": "categories", "day": "days", "bus": "buses", "box": "boxes",
		"batch": "batches", "dish": "dishes", "y": "ys", "item": "items",
	} {
		if got := Plural(in); got != want {
			t.Errorf("Plural(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"paid": "Paid", "in-transit": "InTransit", "on_hold": "OnHold", "a.b c": "ABC", "postgres-sqlc": "PostgresSqlc", "": "", "--": ""} {
		if got := GoName(in); got != want {
			t.Errorf("GoName(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"NoteID": "noteID", "ID": "id", "HTTPServer": "httpServer", "Email": "email", "already": "already", "": "", "X": "x"} {
		if got := LowerFirstWord(in); got != want {
			t.Errorf("LowerFirstWord(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSmallHelpers(t *testing.T) {
	if got := Indent("a\n\nb\n", "  "); got != "  a\n\n  b\n" {
		t.Errorf("Indent leaves empty lines empty: %q", got)
	}
	if Indent("", "> ") != "" {
		t.Error("Indent of nothing")
	}
	if !Contains([]string{"a", "b"}, "b") || Contains([]string{"a", "b"}, "c") || Contains(nil, "") {
		t.Error("Contains")
	}
	if got := SortedKeys(map[string]int{"b": 1, "a": 2, "c": 3}); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("SortedKeys: %v", got)
	}
	if got := SortedKeys(map[string]bool{}); len(got) != 0 {
		t.Errorf("SortedKeys of nothing: %v", got)
	}
	if got := dedupe([]string{"b", "a", "b", "a"}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("dedupe sorts and drops repeats: %v", got)
	}
}

// A plan's levels put everything after what it needs, and say so when
// that cannot be done.
func TestPlanGraphLevels(t *testing.T) {
	g := NewPlanGraph()
	g.Add(&PlanNode{ID: "file:orders_gen.go", Kind: "file"})
	g.Add(&PlanNode{ID: "tool:sqlc generate", Kind: "tool", Deps: []string{"file:query.sql", "file:schema.sql"}})
	g.Add(&PlanNode{ID: "file:query.sql", Kind: "file"})
	g.Add(&PlanNode{ID: "file:schema.sql", Kind: "file"})
	g.Add(&PlanNode{ID: "task:orders.Get", Kind: "task", Deps: []string{"tool:sqlc generate", "file:orders_gen.go", "file:on-disk-not-in-the-plan"}})
	g.Dep("file:orders_gen.go", "file:schema.sql")
	levels, err := g.Levels()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"file:query.sql", "file:schema.sql"}, {"file:orders_gen.go", "tool:sqlc generate"}, {"task:orders.Get"}}
	if !reflect.DeepEqual(levels, want) {
		t.Errorf("levels:\n got %v\nwant %v", levels, want)
	}
	// Every node appears once, after everything it depends on.
	at := map[string]int{}
	for i, level := range levels {
		for _, id := range level {
			if _, twice := at[id]; twice {
				t.Errorf("%s is in two levels", id)
			}
			at[id] = i
		}
	}
	for _, id := range g.Order {
		for _, d := range g.Nodes[id].Deps {
			if lvl, inPlan := at[d]; inPlan && lvl >= at[id] {
				t.Errorf("%s is not after %s", id, d)
			}
		}
	}

	// Adding a node twice keeps one; the dot output names every node.
	before := len(g.Order)
	g.Add(&PlanNode{ID: "file:query.sql", Kind: "file"})
	if len(g.Order) != before {
		t.Error("a node added twice is in the order twice")
	}
	dot := g.Dot()
	for _, id := range g.Order {
		if !strings.Contains(dot, id) {
			t.Errorf("the dot output has no %s", id)
		}
	}

	// A cycle is reported with the nodes in it, not dropped.
	c := NewPlanGraph()
	c.Add(&PlanNode{ID: "a"})
	c.Add(&PlanNode{ID: "b", Deps: []string{"c"}})
	c.Add(&PlanNode{ID: "c", Deps: []string{"b"}})
	levels, err = c.Levels()
	if err == nil || !strings.Contains(err.Error(), "dependency cycle among 2 node(s): b, c") {
		t.Errorf("a cycle: %v", err)
	}
	if !reflect.DeepEqual(levels, [][]string{{"a"}}) {
		t.Errorf("what could be ordered before the cycle: %v", levels)
	}
	if lv, err := NewPlanGraph().Levels(); err != nil || lv != nil {
		t.Errorf("an empty plan: %v %v", lv, err)
	}
}
