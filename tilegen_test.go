package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	if err := run(Options{Spec: sp, Config: "", Out: out, Dump: false, Strict: false}, io.Discard); err != nil {
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
	err := run(Options{Spec: sp, Config: "", Out: filepath.Join(dir, "out"), Dump: false, Strict: false}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "require") || !strings.Contains(err.Error(), "spec.sexp:1:") {
		t.Fatalf("want positioned 'add (require ...)' error, got %v", err)
	}
}

func TestShopEndToEnd(t *testing.T) {
	out := t.TempDir()
	err := run(Options{Spec: "examples/shop/spec.sexp", Config: "examples/shop/config.sexp", Out: out, Dump: true, Strict: false}, io.Discard)
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
	got := read(t, out, ".tilegen/04-select.sexp")
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
		return run(Options{Spec: "examples/shop/spec.sexp", Config: "examples/shop/config.sexp", Out: out, Dump: false, Strict: false}, io.Discard)
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
	err := run(Options{Spec: "examples/shop/spec.sexp", Config: "examples/shop/config.postgres.sexp", Out: out, Dump: false, Strict: false}, io.Discard)
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

const twoPkgs = `(project p (module example.com/p) (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0))
  (package orders (struct Order (field ID uuid.UUID) %s))
  (package billing (entity Invoice (field ID uuid.UUID) %s (store get))))`

func TestCrossPackageImport(t *testing.T) {
	out := t.TempDir()
	if err := run(Options{Spec: "examples/shop/spec.sexp", Config: "examples/shop/config.sexp", Out: out, Dump: false, Strict: false}, io.Discard); err != nil {
		t.Fatal(err)
	}
	gen := read(t, out, "billing/billing_gen.go")
	for _, want := range []string{`"github.com/acme/shop/orders"`, "o *orders.Order, p orders.Pricer"} {
		if !strings.Contains(gen, want) {
			t.Errorf("billing_gen.go missing %q", want)
		}
	}
}

func TestImportCycleRejected(t *testing.T) {
	src := fmt.Sprintf(twoPkgs, "(field Inv *billing.Invoice)", "(field Order *orders.Order)")
	err := validateSrc(t, src, false)
	if err == nil || !strings.Contains(err.Error(), "import cycle between project packages: orders -> billing -> orders") {
		t.Fatalf("want import cycle error, got %v", err)
	}
	if !strings.Contains(err.Error(), "spec.sexp:4:70:") { // billing's *orders.Order closes the loop
		t.Fatalf("cycle error should point at the type that closes the loop: %v", err)
	}
}

func TestSelfQualifierRejected(t *testing.T) {
	src := fmt.Sprintf(twoPkgs, "(field Parent *orders.Order)", "")
	err := validateSrc(t, src, false)
	if err == nil || !strings.Contains(err.Error(), "without its package qualifier (*Order)") {
		t.Fatalf("want self-qualifier error, got %v", err)
	}
}

func TestTasksIncludeForeignContext(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, fmt.Sprintf(twoPkgs, "", "(field Order *orders.Order)"), "")
	out := filepath.Join(dir, "out")
	if err := run(Options{Spec: sp, Config: "", Out: out, Dump: false, Strict: false}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if tasks := read(t, out, "tilegen.tasks.json"); !strings.Contains(tasks, `"orders/orders_gen.go"`) {
		t.Fatalf("billing store tasks should list orders' generated file as context:\n%s", tasks)
	}
}

// ---- spec directories and the merge pass ----

func TestSpecDirMatchesSingleFile(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if err := run(Options{Spec: "examples/shop/spec.sexp", Config: "examples/shop/config.sexp", Out: a}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run(Options{Spec: "examples/shopdir", Out: b}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"go.mod", "orders/orders_gen.go", "orders/memory_order_store.go",
		"billing/billing_gen.go", "billing/memory_invoice_store.go", "tilegen.tasks.json"} {
		if read(t, a, f) != read(t, b, f) {
			t.Errorf("%s differs between the single-file spec and the spec directory", f)
		}
	}
}

func mergeSrc(t *testing.T, files ...string) error {
	t.Helper()
	var forms []*Node
	for i, src := range files {
		fs, err := Parse(fmt.Sprintf("f%d.sexp", i), src)
		if err != nil {
			t.Fatal(err)
		}
		forms = append(forms, fs...)
	}
	_, _, err := Merge(forms)
	return err
}

func TestMergeErrors(t *testing.T) {
	proj := `(project p (module example.com/p) (go 1.22) (require (u github.com/google/uuid v1.6.0)))`
	for name, tc := range map[string]struct {
		files []string
		want  string
	}{
		"two projects":     {[]string{proj, proj}, "second (project ...) form; the first is at f0.sexp:1:1"},
		"require conflict": {[]string{proj, `(require (u github.com/google/uuid v1.5.0))`}, "conflicts with (u github.com/google/uuid v1.6.0) at f0.sexp"},
		"two package docs": {[]string{proj, `(package a (doc "x"))`, `(package a (doc "y"))`}, "package a already has a (doc ...) at f1.sexp"},
		"unknown form":     {[]string{proj, `(servce x)`}, "f1.sexp:1:1: unknown top-level form (servce x)"},
		"no project":       {[]string{`(package a)`}, "exactly one (project NAME ...)"},
	} {
		err := mergeSrc(t, tc.files...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: want error containing %q, got %v", name, tc.want, err)
		}
	}
	if err := mergeSrc(t, proj, `(require (u github.com/google/uuid v1.6.0))`); err != nil {
		t.Errorf("identical requires should dedupe, got %v", err)
	}
}

func TestRepoValidation(t *testing.T) {
	base := `(project p (module example.com/p) (go 1.22) (package a) %s)`
	for form, want := range map[string]string{
		`(repo (visibility secret))`:   "visibility must be public, private or internal",
		`(repo (topics Go))`:           "topic Go must be lowercase",
		`(repo (license gpl))`:         "license must be mit or none",
		`(repo (github a/b/c))`:        "github must be owner/name or name",
		`(repo (colour blue))`:         "unknown repo item",
		`(repo (github x) (github y))`: "duplicate (github ...)",
	} {
		err := validateSrc(t, fmt.Sprintf(base, form), false)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", form, want, err)
		}
	}
}

// ---- git and GitHub, with an in-process fake ----

type fakeGH struct {
	exists bool
	did    []string
}

func (f *fakeGH) Query(dir, name string, args ...string) (string, error) {
	switch cmd := name + " " + strings.Join(args, " "); {
	case strings.HasPrefix(cmd, "gh api user"):
		return "octocat", nil
	case strings.HasPrefix(cmd, "gh repo view"):
		if f.exists {
			return `{"name":"x"}`, nil
		}
		return "GraphQL: Could not resolve to a Repository with the name 'x'.", fmt.Errorf("exit status 1")
	default:
		return "", fmt.Errorf("unexpected query %s", cmd)
	}
}

func (f *fakeGH) Do(dir, name string, args ...string) error {
	f.did = append(f.did, name+" "+strings.Join(args, " "))
	return nil
}

func (f *fakeGH) ran(prefix string) bool {
	for _, c := range f.did {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

const ghSpec = `(project shop (go 1.22)
  (repo (github shop) (visibility public) (description "d") (topics orders))
  (package orders (struct Order (field ID int64))))`

func TestGitCreatesMissingRepo(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, ghSpec, "")
	gh := &fakeGH{}
	out := filepath.Join(dir, "out")
	if err := run(Options{Spec: sp, Out: out, Name: "myshop", Git: true, Runner: gh}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"git init -q -b main", "go mod tidy", "git add -A", "git commit",
		"gh repo create octocat/myshop --public --source=. --remote=origin --push --description d",
		"gh repo edit octocat/myshop --add-topic go,golang,tilegen,orders",
	} {
		if !gh.ran(want) {
			t.Errorf("missing command %q in %q", want, gh.did)
		}
	}
	if mod := read(t, out, "go.mod"); !strings.Contains(mod, "module github.com/octocat/myshop") {
		t.Errorf("module should derive from -name and the gh login:\n%s", mod)
	}
	for _, f := range []string{"README.md", "LICENSE", "Makefile", ".gitignore"} {
		read(t, out, f)
	}
}

func TestGitClonesExistingRepo(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, ghSpec, "")
	gh := &fakeGH{exists: true}
	if err := run(Options{Spec: sp, Out: filepath.Join(dir, "out"), Git: true, Runner: gh}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !gh.ran("gh repo clone octocat/shop") || gh.ran("gh repo create") || gh.ran("git commit") {
		t.Fatalf("want clone without create or commit, got %q", gh.did)
	}
}

func TestGitUsesExistingLocalRepo(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, ghSpec, "")
	out := filepath.Join(dir, "out")
	if err := os.MkdirAll(filepath.Join(out, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	gh := &fakeGH{exists: true}
	if err := run(Options{Spec: sp, Out: out, Git: true, Runner: gh}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if gh.ran("gh repo clone") || gh.ran("git init") || gh.ran("git commit") {
		t.Fatalf("an existing local repository must be used as is, got %q", gh.did)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, ghSpec, "")
	out := filepath.Join(dir, "out")
	if err := run(Options{Spec: sp, Out: out, Git: true, DryRun: true, Runner: &fakeGH{}}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("dry run created %s", out)
	}
}

// TestGitLocalOnlyRealGit runs real git: -git without (repo ...) makes a
// local repository with one commit and never calls gh.
func TestGitLocalOnlyRealGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.com",
		"GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.com"} {
		t.Setenv(k, v)
	}
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, `(project p (module example.com/p) (go 1.22) (package a (struct S (field X int))))`, "")
	out := filepath.Join(dir, "out")
	if err := run(Options{Spec: sp, Out: out, Git: true}, io.Discard); err != nil {
		t.Fatal(err)
	}
	log, err := exec.Command("git", "-C", out, "log", "--oneline").Output()
	if err != nil || !strings.Contains(string(log), "Initial scaffold from tilegen") {
		t.Fatalf("want one commit, got %q, %v", log, err)
	}
}
