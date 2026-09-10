package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestParseRoundTrip(t *testing.T) {
	src := `; comment
(project x (doc "a \"quoted\" (paren)") (field m "map[string]int") ())`
	a, err := Parse("t", src)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Parse("t", Dump(a))
	if err != nil {
		t.Fatal(err)
	}
	if a[0].Flat() != b[0].Flat() {
		t.Fatalf("round trip changed:\n%s\n%s", a[0].Flat(), b[0].Flat())
	}
	if got := a[0].List[1].Pos; got.Line != 2 || got.Col != 10 {
		t.Fatalf("position = %v, want line 2 col 10", got)
	}
}

func TestParseErrors(t *testing.T) {
	for _, src := range []string{"(a", ")", `"open`, `"bad \q"`} {
		if _, err := Parse("t", src); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", src)
		}
	}
}

func TestMatch(t *testing.T) {
	n, _ := Parse("t", "(entity Order (field ID int) (field Name string))")
	b := Bindings{}
	if !Match(Pat("(entity ?name ?items...)"), n[0], b) {
		t.Fatal("no match")
	}
	if b.Atom("name") != "Order" || len(b.Rest("items")) != 2 {
		t.Fatalf("bindings = %v", b)
	}
	for pat, want := range map[string]bool{
		"(entity _ _ _)":              true,
		"(entity _ _)":                false, // lengths must agree
		"(struct ?n ?rest...)":        false,
		"(entity ?n (field ID ?t) _)": true,
	} {
		if got := Match(Pat(pat), n[0], Bindings{}); got != want {
			t.Errorf("Match(%s) = %v, want %v", pat, got, want)
		}
	}
}

func TestNames(t *testing.T) {
	for in, want := range map[string]string{
		"ID": "id", "PlacedAt": "placed_at", "UserID": "user_id",
		"HTTPServer": "http_server", "TotalCents": "total_cents", "OAuth2Token": "o_auth2_token",
	} {
		if got := snake(in); got != want {
			t.Errorf("snake(%s) = %s, want %s", in, got, want)
		}
	}
	for in, want := range map[string]string{"ID": "id", "PlacedAt": "placedAt", "HTTPServer": "httpServer"} {
		if got := camel(in); got != want {
			t.Errorf("camel(%s) = %s, want %s", in, got, want)
		}
	}
	for in, want := range map[string]string{"order": "orders", "category": "categories", "box": "boxes", "day": "days"} {
		if got := plural(in); got != want {
			t.Errorf("plural(%s) = %s, want %s", in, got, want)
		}
	}
	if paramName("Type") != "v" || paramName("Order") != "order" {
		t.Error("paramName should dodge keywords")
	}
}

func validateSrc(t *testing.T, src string, strict bool) error {
	t.Helper()
	nodes, err := Parse("spec.sexp", src)
	if err != nil {
		t.Fatal(err)
	}
	return Validate(nodes, &Ctx{Cfg: DefaultConfig(), Strict: strict})
}

const okPrefix = `(project p (module example.com/p) (go 1.22) (package a `

func TestValidateErrors(t *testing.T) {
	cases := map[string]string{
		"store needs ID":  okPrefix + `(entity E (field Name string) (store get))))`,
		"bad type":        okPrefix + `(struct S (field X "map[string"))))`,
		"bad op":          okPrefix + `(entity E (field ID int) (store fly))))`,
		"unexported type": okPrefix + `(struct s (field X int))))`,
		"dup field":       okPrefix + `(struct S (field X int) (field X int))))`,
		"bad module":      `(project p (module "not a path") (go 1.22) (package a))`,
		"bad version":     `(project p (module example.com/p) (go 1.22) (require (u github.com/google/uuid 1.6)) (package a))`,
		"no package":      `(project p (module example.com/p) (go 1.22))`,
	}
	for name, src := range cases {
		err := validateSrc(t, src, false)
		if err == nil {
			t.Errorf("%s: want error", name)
			continue
		}
		if !strings.Contains(err.Error(), "spec.sexp:1:") && name != "no package" {
			t.Errorf("%s: error lacks a position: %v", name, err)
		}
	}
}

func TestStrictMode(t *testing.T) {
	src := okPrefix + `(enum Color red green)))`
	if err := validateSrc(t, src, false); err != nil {
		t.Fatalf("non-strict should allow uncovered forms: %v", err)
	}
	if err := validateSrc(t, src, true); err == nil {
		t.Fatal("strict mode should reject uncovered forms")
	}
}

func writeSpec(t *testing.T, dir, spec, cfg string) (string, string) {
	t.Helper()
	sp, cp := filepath.Join(dir, "spec.sexp"), ""
	if err := os.WriteFile(sp, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg != "" {
		cp = filepath.Join(dir, "config.sexp")
		if err := os.WriteFile(cp, []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return sp, cp
}

func TestUncoveredBecomesTask(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, okPrefix+`(enum Color red green)))`, "")
	out := filepath.Join(dir, "out")
	if err := run(sp, "", out, false, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	tasks, _ := os.ReadFile(filepath.Join(out, "tilegen.tasks.json"))
	if !strings.Contains(string(tasks), "(enum Color red green)") {
		t.Fatalf("uncovered form should become an LLM task:\n%s", tasks)
	}
}

func TestUnknownQualifier(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, okPrefix+`(struct S (field X decimal.Decimal))))`, "")
	err := run(sp, "", filepath.Join(dir, "out"), false, false, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "require") || !strings.Contains(err.Error(), "spec.sexp:1:") {
		t.Fatalf("want positioned 'add (require ...)' error, got %v", err)
	}
}

func TestShopEndToEnd(t *testing.T) {
	out := t.TempDir()
	err := run("examples/shop/spec.sexp", "examples/shop/config.sexp", out, true, false, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	gen := read(t, out, "orders/orders_gen.go")
	for _, want := range []string{
		"// Code generated by tilegen. DO NOT EDIT.",
		`"github.com/google/uuid"`, `"context"`, `"time"`, // declared + goimports-added
		"Get(ctx context.Context, id uuid.UUID) (*Order, error)",
		"`json:\"placed_at\"`",
		"var _ OrderStore = (*MemoryOrderStore)(nil)",
	} {
		if !strings.Contains(gen, want) {
			t.Errorf("orders_gen.go missing %q", want)
		}
	}
	if mod := read(t, out, "go.mod"); !strings.Contains(mod, "github.com/google/uuid v1.6.0") {
		t.Errorf("go.mod missing require:\n%s", mod)
	}

	// Golden: the tiling result is the contract of the passes.
	got := read(t, out, ".tilegen/03-select.sexp")
	golden := "testdata/shop.select.golden"
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if want, _ := os.ReadFile(golden); string(want) != got {
		t.Errorf("select output differs from %s (run: go test -run Shop -update)", golden)
	}
}

func TestRegenerationKeepsYourCode(t *testing.T) {
	out := t.TempDir()
	args := func() error {
		return run("examples/shop/spec.sexp", "examples/shop/config.sexp", out, false, false, io.Discard)
	}
	if err := args(); err != nil {
		t.Fatal(err)
	}
	impl := filepath.Join(out, "orders/memory_order_store.go")
	src := read(t, out, "orders/memory_order_store.go")
	filled := strings.Replace(src, `panic("tilegen:hole orders.MemoryOrderStore.Delete")`, "return nil", 1)
	if err := os.WriteFile(impl, []byte(filled), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := args(); err != nil {
		t.Fatal(err)
	}
	if read(t, out, "orders/memory_order_store.go") != filled {
		t.Fatal("tilegen overwrote a keep file")
	}
	if strings.Contains(read(t, out, "tilegen.tasks.json"), "MemoryOrderStore.Delete") {
		t.Fatal("filled hole is still listed as a task")
	}
}

func TestPostgresConfig(t *testing.T) {
	out := t.TempDir()
	err := run("examples/shop/spec.sexp", "examples/shop/config.postgres.sexp", out, false, false, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, out, "db/query.sql"), "-- name: ListOrders :many") {
		t.Error("query.sql missing ListOrders")
	}
	if !strings.Contains(read(t, out, "sqlc.yaml"), `go_type: "github.com/google/uuid.UUID"`) {
		t.Error("sqlc.yaml missing uuid override")
	}
	if !strings.Contains(read(t, out, "internal/orders/postgres_order_store.go"), `"github.com/acme/shop/internal/db"`) {
		t.Error("impl should import the sqlc package")
	}
	if !strings.Contains(read(t, out, "internal/orders/orders_gen.go"), "`json:\"placedAt\"`") {
		t.Error("camel json tags not applied")
	}
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
