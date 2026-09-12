package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	src := okPrefix + `(workflow Checkout (step pay))))`
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
	sp, _ := writeSpec(t, dir, okPrefix+`(workflow Checkout (step pay))))`, "")
	out := filepath.Join(dir, "out")
	if err := run(Options{Spec: sp, Config: "", Out: out, Dump: false, Strict: false}, io.Discard); err != nil {
		t.Fatal(err)
	}
	tasks, _ := os.ReadFile(filepath.Join(out, "tilegen.tasks.json"))
	if !strings.Contains(string(tasks), "(workflow Checkout (step pay))") {
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
	_, err := Merge(forms)
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

// ---- workspace: worktrees and tmux ----

func parseWS(t *testing.T, src string) (*Workspace, error) {
	t.Helper()
	n, err := Parse("ws.sexp", src)
	if err != nil {
		t.Fatal(err)
	}
	return ParseWorkspace(n[0], "/base/spec")
}

func TestWorkspaceParse(t *testing.T) {
	ws, err := parseWS(t, `(workspace (name shop) (out ..)
	  (worktrees (worktree billing (branch feat/billing)) (worktree docs))
	  (tmux (session shop (window code (dir .)) (window b (worktree billing) (run "claude")))))`)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Out != "/base" || ws.Worktrees[0].Path != "/base.wt/billing" || ws.Worktrees[1].Branch != "docs" {
		t.Fatalf("paths or default branch wrong: %+v", ws)
	}
	if ws.Windows[1].Dir != "/base.wt/billing" || ws.Windows[1].Run != "claude" {
		t.Fatalf("window not resolved: %+v", ws.Windows[1])
	}
}

func TestWorkspaceValidation(t *testing.T) {
	for src, want := range map[string]string{
		`(workspace (worktrees (worktree a)))`:                                                             "worktrees need (out ...)",
		`(workspace (out ..) (worktrees (worktree a (branch ../x))))`:                                      `invalid branch name "../x"`,
		`(workspace (out ..) (tmux (session my.shop (window a))))`:                                         `session name "my.shop"`,
		`(workspace (out ..) (tmux (session s (window a) (window a))))`:                                    `duplicate window "a"`,
		`(workspace (out ..) (tmux (session s (window a (worktree nope)))))`:                               `no worktree named "nope"`,
		`(workspace (out ..) (worktrees (worktree w)) (tmux (session s (window a (dir .) (worktree w)))))`: "not both",
		`(workspace (out ..) (tmux (session s)))`:                                                          "at least one (window ...)",
		`(workspace (colour blue))`:                                                                        "unknown workspace item",
	} {
		if _, err := parseWS(t, src); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", src, want, err)
		}
	}
}

// scriptRunner answers queries from a table of command prefixes and
// records every command that would change something.
type scriptRunner struct {
	answers map[string]string // prefix -> output; missing prefix -> error
	did     []string
}

func (s *scriptRunner) Query(dir, name string, args ...string) (string, error) {
	cmd := name + " " + strings.Join(args, " ")
	for prefix, out := range s.answers {
		if strings.HasPrefix(cmd, prefix) {
			return out, nil
		}
	}
	return "", fmt.Errorf("exit status 1")
}

func (s *scriptRunner) Do(dir, name string, args ...string) error {
	s.did = append(s.did, name+" "+strings.Join(args, " "))
	return nil
}

func testWorkspace(t *testing.T) *Workspace {
	ws, err := parseWS(t, `(workspace (out ..)
	  (worktrees (worktree billing (branch feat/billing)) (worktree pricing (branch feat/pricer)))
	  (tmux (session shop (window code (dir .) (run "make check")) (window billing (worktree billing)))))`)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestUpCreatesWorktreesAndSession(t *testing.T) {
	r := &scriptRunner{answers: map[string]string{
		"git worktree list": "worktree /base\nHEAD abc\nbranch refs/heads/main",
		"git rev-parse --verify --quiet refs/heads/feat/pricer": "abc", // exists; feat/billing does not
	}}
	if err := Up(testWorkspace(t), r, io.Discard, true); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"git worktree add -q -b feat/billing /base.wt/billing",
		"git worktree add -q /base.wt/pricing feat/pricer",
		"tmux new-session -d -s shop -n code -c /base",
		"tmux send-keys -t =shop:code make check Enter",
		"tmux new-window -d -t =shop: -n billing -c /base.wt/billing",
	}
	if strings.Join(r.did, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands:\n%s\nwant:\n%s", strings.Join(r.did, "\n"), strings.Join(want, "\n"))
	}
}

func TestUpReusesAndCompletes(t *testing.T) {
	r := &scriptRunner{answers: map[string]string{
		"git worktree list": "worktree /base\n\nworktree /base.wt/billing\n\nworktree /base.wt/pricing",
		"tmux has-session":  "",
		"tmux list-windows": "code", // billing window was added to the spec later
	}}
	if err := Up(testWorkspace(t), r, io.Discard, true); err != nil {
		t.Fatal(err)
	}
	if len(r.did) != 1 || r.did[0] != "tmux new-window -d -t =shop: -n billing -c /base.wt/billing" {
		t.Fatalf("want only the missing window, got %q", r.did)
	}
}

func TestDownPrune(t *testing.T) {
	r := &scriptRunner{answers: map[string]string{
		"tmux has-session":  "",
		"git worktree list": "worktree /base\n\nworktree /base.wt/billing",
	}}
	if err := Down(testWorkspace(t), r, io.Discard, true); err != nil {
		t.Fatal(err)
	}
	want := "tmux kill-session -t =shop\ngit worktree remove /base.wt/billing" // pricing was never created
	if strings.Join(r.did, "\n") != want {
		t.Fatalf("got %q", r.did)
	}
}

func TestGenerationUsesWorkspaceDefaults(t *testing.T) {
	root := t.TempDir()
	spec := filepath.Join(root, "spec")
	if err := os.MkdirAll(spec, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(spec, "p.sexp"), []byte(`(project p (module example.com/p) (go 1.22) (package a (struct S (field X int))))
(workspace (name renamed) (out ..))`), 0o644)
	if err := run(Options{Spec: spec}, io.Discard); err != nil {
		t.Fatal(err)
	}
	read(t, root, "a/a_gen.go") // generated next to spec/, per (out ..)
}

// TestWorkspaceRealGitAndTmux runs `up` and `down -prune` with real git and
// tmux, on a private tmux server so it never touches your sessions.
func TestWorkspaceRealGitAndTmux(t *testing.T) {
	for _, tool := range []string{"git", "tmux"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " not installed")
		}
	}
	// tmux's socket path must fit in ~104 bytes on macOS, and t.TempDir()
	// there is long (/private/var/folders/...), so use a short folder in /tmp.
	sock, err := os.MkdirTemp("/tmp", "tg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sock) })
	t.Setenv("TMUX_TMPDIR", sock)
	t.Setenv("TMUX", "")
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.com",
		"GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.com"} {
		t.Setenv(k, v)
	}
	t.Cleanup(func() { exec.Command("tmux", "kill-server").Run() })

	root := canonical(t.TempDir())
	main := filepath.Join(root, "proj")
	for _, args := range [][]string{{"init", "-q", "-b", "main", main}, {"-C", main, "commit", "-q", "--allow-empty", "-m", "init"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	n, _ := Parse("ws.sexp", `(workspace (out proj) (worktrees (worktree feat))
	  (tmux (session tgtest (window code (dir .)) (window feat (worktree feat)))))`)
	ws, err := ParseWorkspace(n[0], root)
	if err != nil {
		t.Fatal(err)
	}
	r := execRunner{log: io.Discard}
	if err := Up(ws, r, io.Discard, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "proj.wt", "feat", ".git")); err != nil {
		t.Fatalf("worktree not created: %v", err)
	}
	out, err := exec.Command("tmux", "list-windows", "-t", "=tgtest", "-F", "#{window_name}:#{pane_current_path}").Output()
	if err != nil || !strings.Contains(string(out), "feat:"+filepath.Join(root, "proj.wt", "feat")) {
		t.Fatalf("tmux windows = %q, %v", out, err)
	}
	if err := Down(ws, r, io.Discard, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "proj.wt", "feat")); !os.IsNotExist(err) {
		t.Fatal("clean worktree should be removed by down -prune")
	}
	if exec.Command("tmux", "has-session", "-t", "=tgtest").Run() == nil {
		t.Fatal("session should be closed")
	}
}

// TestGoModNeverLowersVersions: `go mod tidy` may raise the go line or a
// require (a dependency needs newer Go); regenerating must keep them.
func TestGoModNeverLowersVersions(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, `(project p (module example.com/p) (go 1.22)
  (require (uuid github.com/google/uuid v1.5.0))
  (package a (struct S (field ID uuid.UUID))))`, "")
	out := filepath.Join(dir, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	tidied := "module example.com/p\n\ngo 1.25.0\n\nrequire github.com/google/uuid v1.6.0\n"
	if err := os.WriteFile(filepath.Join(out, "go.mod"), []byte(tidied), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	mod := read(t, out, "go.mod")
	if !strings.Contains(mod, "go 1.25.0") || !strings.Contains(mod, "github.com/google/uuid v1.6.0") {
		t.Fatalf("tilegen lowered versions tidy had raised:\n%s", mod)
	}

	// And it still raises: a spec asking for more than go.mod has wins.
	raised := "module example.com/p\n\ngo 1.21\n\nrequire github.com/google/uuid v1.4.0\n"
	os.WriteFile(filepath.Join(out, "go.mod"), []byte(raised), 0o644)
	if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	mod = read(t, out, "go.mod")
	if !strings.Contains(mod, "go 1.22") || !strings.Contains(mod, "github.com/google/uuid v1.5.0") {
		t.Fatalf("tilegen should raise versions below the spec:\n%s", mod)
	}
}

// ---- reconciliation: spec forms come and go, code follows ----

const rcSpec = `(project p (module example.com/p) (go 1.22)
  (package notes
    (entity Note (field ID int64) (field Title string) (store %s))))
(config (context-first %s) (storage %s))`

type rcProject struct {
	t        *testing.T
	dir, out string
}

func newRC(t *testing.T) *rcProject {
	dir := t.TempDir()
	return &rcProject{t: t, dir: dir, out: filepath.Join(dir, "out")}
}

// gen regenerates from a spec with the given store ops, context-first
// setting and storage, and returns the console report.
func (p *rcProject) gen(ops, ctx, storage string) string {
	p.t.Helper()
	writeSpec(p.t, p.dir, fmt.Sprintf(rcSpec, ops, ctx, storage), "")
	var log strings.Builder
	if err := run(Options{Spec: filepath.Join(p.dir, "spec.sexp"), Out: p.out}, &log); err != nil {
		p.t.Fatal(err)
	}
	return log.String()
}

func (p *rcProject) fill(file, hole, body string) {
	p.t.Helper()
	full := filepath.Join(p.out, file)
	src := read(p.t, p.out, file)
	marker := fmt.Sprintf("panic(%q)", holePrefix+hole)
	if !strings.Contains(src, marker) {
		p.t.Fatalf("no hole %s in %s", hole, file)
	}
	os.WriteFile(full, []byte(strings.Replace(src, marker, body, 1)), 0o644)
}

func TestReconcileAppendsMissingMethods(t *testing.T) {
	p := newRC(t)
	p.gen("get save", "yes", "memory")
	p.fill("notes/memory_note_store.go", "notes.MemoryNoteStore.Get", "return s.m[id], nil")
	log := p.gen("get list save", "yes", "memory")
	if !strings.Contains(log, "added  List to notes/memory_note_store.go") {
		t.Fatalf("want List appended, got:\n%s", log)
	}
	src := read(t, p.out, "notes/memory_note_store.go")
	if !strings.Contains(src, "return s.m[id], nil") || !strings.Contains(src, `"tilegen:hole notes.MemoryNoteStore.List"`) {
		t.Fatalf("want your Get kept and a List stub:\n%s", src)
	}
	if tasks := read(t, p.out, "tilegen.tasks.json"); !strings.Contains(tasks, `"notes.MemoryNoteStore.List"`) {
		t.Fatal("the appended stub should be an open task")
	}
}

func TestReconcileUntouchedStubsFollowSpecYourCodeDrifts(t *testing.T) {
	p := newRC(t)
	p.gen("get save", "yes", "memory")
	p.fill("notes/memory_note_store.go", "notes.MemoryNoteStore.Get", "_ = ctx\n\treturn s.m[id], nil")
	log := p.gen("get save", "no", "memory")
	if !strings.Contains(log, "updated Save in notes/memory_note_store.go") {
		t.Errorf("untouched Save stub should take the new signature:\n%s", log)
	}
	if !strings.Contains(log, "drift   (*MemoryNoteStore).Get") || strings.Contains(log, "drift   (*MemoryNoteStore).Save") {
		t.Errorf("only your implemented Get should drift:\n%s", log)
	}
	src := read(t, p.out, "notes/memory_note_store.go")
	if !strings.Contains(src, "Save(note *Note) error") || !strings.Contains(src, "Get(ctx context.Context, id int64)") {
		t.Errorf("stub should be updated and your method left alone:\n%s", src)
	}
	if tasks := read(t, p.out, "tilegen.tasks.json"); !strings.Contains(tasks, `"status": "drift"`) {
		t.Error("drift should be an LLM task")
	}
}

func TestReconcileRemovedMethods(t *testing.T) {
	p := newRC(t)
	p.gen("get list save delete", "yes", "memory")
	p.fill("notes/memory_note_store.go", "notes.MemoryNoteStore.Delete", "delete(s.m, id)\n\treturn nil")
	log := p.gen("get save", "yes", "memory")
	if !strings.Contains(log, "dropped List from notes/memory_note_store.go") {
		t.Errorf("untouched List stub should be dropped:\n%s", log)
	}
	if !strings.Contains(log, "orphan  (*MemoryNoteStore).Delete") {
		t.Errorf("your Delete should be reported, not deleted:\n%s", log)
	}
	src := read(t, p.out, "notes/memory_note_store.go")
	if strings.Contains(src, "List(") || !strings.Contains(src, "delete(s.m, id)") {
		t.Errorf("want List gone and your Delete kept:\n%s", src)
	}
}

func TestReconcileStorageSwitchAndBack(t *testing.T) {
	p := newRC(t)
	p.gen("get save", "yes", "memory")
	p.fill("notes/memory_note_store.go", "notes.MemoryNoteStore.Get", "return s.m[id], nil")
	log := p.gen("get save", "yes", "postgres")
	if !strings.Contains(log, "orphan  notes/memory_note_store.go: you implemented code here") {
		t.Errorf("memory store with your code should be an orphan:\n%s", log)
	}
	log = p.gen("get save", "yes", "memory")
	for _, want := range []string{
		"removed db/schema.sql", "removed db/query.sql", "removed sqlc.yaml",
		"removed notes/postgres_note_store.go (untouched scaffolding",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("missing %q in:\n%s", want, log)
		}
	}
	if _, err := os.Stat(filepath.Join(p.out, "db")); !os.IsNotExist(err) {
		t.Error("empty db/ folder should be removed")
	}
	if !strings.Contains(read(t, p.out, "notes/memory_note_store.go"), "return s.m[id], nil") {
		t.Error("your memory store must survive the round trip")
	}
}

func TestReconcileRemovedPackage(t *testing.T) {
	dir := t.TempDir()
	two := `(project p (module example.com/p) (go 1.22)
  (package a (entity A (field ID int64) (store get)))
  (package b (entity B (field ID int64) (store get))))`
	one := `(project p (module example.com/p) (go 1.22)
  (package a (entity A (field ID int64) (store get))))`
	sp, _ := writeSpec(t, dir, two, "")
	out := filepath.Join(dir, "out")
	if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, dir, one, "")
	var log strings.Builder
	if err := run(Options{Spec: sp, Out: out}, &log); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "removed b/b_gen.go") || !strings.Contains(log.String(), "removed b/memory_b_store.go (untouched scaffolding") {
		t.Fatalf("package b should be swept completely:\n%s", log.String())
	}
	if _, err := os.Stat(filepath.Join(out, "b")); !os.IsNotExist(err) {
		t.Fatal("empty package folder should be removed")
	}
}

// TestSweepSkipsNestedModules: a tilegen project nested inside another
// project's output must never be swept by the outer project.
func TestSweepSkipsNestedModules(t *testing.T) {
	p := newRC(t)
	p.gen("get", "yes", "memory")
	nested := filepath.Join(p.out, "tools", "other")
	os.MkdirAll(nested, 0o755)
	os.WriteFile(filepath.Join(nested, "go.mod"), []byte("module example.com/other\n"), 0o644)
	gen := filepath.Join(nested, "x_gen.go")
	os.WriteFile(gen, []byte("// "+generatedMarker+"\n\npackage x\n"), 0o644)
	p.gen("get", "yes", "memory")
	if _, err := os.Stat(gen); err != nil {
		t.Fatal("sweep deleted a generated file inside a nested module")
	}
}

// ---- store query ops ----

func TestStoreOpsValidation(t *testing.T) {
	base := okPrefix + `(entity E (field ID int64) (field Email string) (store %s))))`
	for ops, want := range map[string]string{
		`get fly`:                       `unknown store op "fly"`,
		`(list-by Phone)`:               "Phone is not a field declared before the store",
		`(get-by Email) (get-by Email)`: "duplicate store method GetByEmail",
		`count (method Count (returns int64 error))`: "duplicate store method Count",
		`(frobnicate Email)`:                         "unknown store op (frobnicate Email)",
		`(list-by)`:                                  "expected (_ ?field)",
	} {
		if err := validateSrc(t, fmt.Sprintf(base, ops), false); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", ops, want, err)
		}
	}
}

func TestStoreQueryOps(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, `(project p (module example.com/p) (go 1.22)
  (package s (entity Share (field ID int64) (field NoteID int64) (field Email string)
    (store get count (list-by NoteID) (get-by Email) (count-by NoteID) (exists-by Email) (delete-by NoteID)
      (method RevokeAll (doc "RevokeAll removes all.") (params (noteID int64)) (returns int64 error))))))
(config (storage postgres))`, "")
	out := filepath.Join(dir, "out")
	if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	gen := read(t, out, "s/s_gen.go")
	for _, want := range []string{
		"Count(ctx context.Context) (int64, error)",
		"ListByNoteID(ctx context.Context, noteID int64) ([]*Share, error)",
		"GetByEmail(ctx context.Context, email string) (*Share, error)",
		"CountByNoteID(ctx context.Context, noteID int64) (int64, error)",
		"ExistsByEmail(ctx context.Context, email string) (bool, error)",
		"DeleteByNoteID(ctx context.Context, noteID int64) error",
		"RevokeAll(ctx context.Context, noteID int64) (int64, error)",
	} {
		if !strings.Contains(gen, want) {
			t.Errorf("interface missing %q", want)
		}
	}
	sql := read(t, out, "db/query.sql")
	for _, want := range []string{
		"-- name: CountShares :one\nSELECT count(*) FROM shares;",
		"-- name: ListSharesByNoteID :many\nSELECT * FROM shares WHERE note_id = $1 ORDER BY id;",
		"-- name: GetShareByEmail :one\nSELECT * FROM shares WHERE email = $1 ORDER BY id LIMIT 1;",
		"-- name: ExistsShareByEmail :one\nSELECT EXISTS (SELECT 1 FROM shares WHERE email = $1);",
		"-- name: DeleteSharesByNoteID :exec\nDELETE FROM shares WHERE note_id = $1;",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("query.sql missing %q", want)
		}
	}
	if strings.Contains(sql, "RevokeAll") {
		t.Error("custom methods get no SQL")
	}
	tasks := read(t, out, "tilegen.tasks.json")
	if !strings.Contains(tasks, "s.q.ListSharesByNoteID") || !strings.Contains(tasks, "Implement it as documented: RevokeAll removes all.") {
		t.Errorf("tasks should point at the sqlc query and the custom doc:\n%s", tasks)
	}
}

func TestLowerFirstWord(t *testing.T) {
	for in, want := range map[string]string{"NoteID": "noteID", "Email": "email", "ID": "id", "HTTPServer": "httpServer", "URLPath": "urlPath"} {
		if got := lowerFirstWord(in); got != want {
			t.Errorf("lowerFirstWord(%s) = %s, want %s", in, got, want)
		}
	}
}

// ---- implement, variadic parameters, embedded interfaces ----

const implSpec = `(project p (module example.com/p) (go 1.22)
  (package a
    (entity Note (field ID int64) (store get))
    (interface Mailer (method Send (params (to string) (args "...any")) (returns error)))
    (interface Base (method Ping (returns error)))
    (interface Service (embed Base) (embed io.Writer) (method Run (params (n *Note)) (returns error)))
    (implement Service (as Worker) (field mailer Mailer) (field store NoteStore) (constraint "be idempotent"))
    (implement NoteStore (as CachedNoteStore) (field next NoteStore))))`

func TestImplementAnyInterface(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, implSpec, "")
	out := filepath.Join(dir, "out")
	if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	w := read(t, out, "a/worker.go")
	for _, want := range []string{
		"type Worker struct {\n\tmailer Mailer\n\tstore  NoteStore\n}",
		"func NewWorker(mailer Mailer, store NoteStore) *Worker",
		"func (s *Worker) Run(ctx context.Context, n *Note) error",
		"func (s *Worker) Ping(ctx context.Context) error", // from the embedded project interface
		"func (s *Worker) Write(p []byte) (int, error)",    // from io.Writer, via the type checker
	} {
		if !strings.Contains(w, want) {
			t.Errorf("worker.go missing %q:\n%s", want, w)
		}
	}
	if gen := read(t, out, "a/a_gen.go"); !strings.Contains(gen, "var _ Service = (*Worker)(nil)") ||
		!strings.Contains(gen, "Send(ctx context.Context, to string, args ...any) error") ||
		!strings.Contains(gen, "\tBase\n\tio.Writer\n") {
		t.Errorf("a_gen.go missing assertion, variadic method or embeds:\n%s", gen)
	}
	if c := read(t, out, "a/cached_note_store.go"); !strings.Contains(c, "func (s *CachedNoteStore) Get(ctx context.Context, id int64) (*Note, error)") {
		t.Errorf("implementing a generated store interface should work:\n%s", c)
	}
	tasks := read(t, out, "tilegen.tasks.json")
	if !strings.Contains(tasks, "Dependencies: s.mailer (Mailer), s.store (NoteStore).") || !strings.Contains(tasks, `"be idempotent"`) {
		t.Errorf("implement tasks should carry dependencies and constraints:\n%s", tasks)
	}
}

func TestImplementValidation(t *testing.T) {
	base := okPrefix + `(interface I (method M (returns error))) %s))`
	for form, want := range map[string]string{
		`(implement J (as X))`: "J is not an interface declared in this package (have: I)",
		`(implement I)`:        "implement needs exactly one (as TypeName)",
		`(implement I (as X) (field a int) (field a int))`:       `duplicate field "a"`,
		`(implement I (as I))`:                                   `duplicate type "I"`,
		`(implement I (as X) (wire y))`:                          "implement takes (as Name)",
		`(interface V (method M (params (a "...int") (b int))))`: "only the last parameter can be variadic",
		`(struct S (field X "...int"))`:                          "only a method's last parameter can be variadic",
	} {
		if err := validateSrc(t, fmt.Sprintf(base, form), false); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", form, want, err)
		}
	}
}

func TestImplementUnresolvedEmbedWarns(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, `(project p (module example.com/p) (go 1.22)
  (require (x github.com/example/x v1.0.0))
  (package a (interface I (embed x.Thing) (method M (returns error))) (implement I (as Impl))))`, "")
	var log strings.Builder
	if err := run(Options{Spec: sp, Out: filepath.Join(dir, "out")}, &log); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "cannot list the methods of x.Thing") {
		t.Fatalf("want a warning for an embed tilegen cannot resolve:\n%s", log.String())
	}
}

func TestImplementReconcilesAndSweeps(t *testing.T) {
	dir := t.TempDir()
	spec := `(project p (module example.com/p) (go 1.22)
  (package a (interface I (method M (returns error)) %s) %s))`
	sp, _ := writeSpec(t, dir, fmt.Sprintf(spec, "", "(implement I (as Impl))"), "")
	out := filepath.Join(dir, "out")
	run(Options{Spec: sp, Out: out}, io.Discard)
	writeSpec(t, dir, fmt.Sprintf(spec, "(method N (returns error))", "(implement I (as Impl))"), "")
	var log strings.Builder
	run(Options{Spec: sp, Out: out}, &log)
	if !strings.Contains(log.String(), "added  N to a/impl.go") {
		t.Errorf("new interface method should be appended:\n%s", log.String())
	}
	writeSpec(t, dir, fmt.Sprintf(spec, "", ""), "")
	log.Reset()
	run(Options{Spec: sp, Out: out}, &log)
	if !strings.Contains(log.String(), "removed a/impl.go (untouched scaffolding") {
		t.Errorf("dropping the implement should remove its untouched file:\n%s", log.String())
	}
}

// noNetRunner fails every query: generation must not need gh.
type noNetRunner struct{ did []string }

func (n *noNetRunner) Query(dir, name string, args ...string) (string, error) {
	return "", fmt.Errorf("unexpected network call: %s %s", name, strings.Join(args, " "))
}
func (n *noNetRunner) Do(dir, name string, args ...string) error {
	n.did = append(n.did, name)
	return nil
}

const ownerlessSpec = `(project shop (go 1.22) (repo (github shop)) (package a (struct S (field X int))))`

func TestModulePathFromGoModWithoutNetwork(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, ownerlessSpec, "")
	out := filepath.Join(dir, "out")
	os.MkdirAll(out, 0o755)
	os.WriteFile(filepath.Join(out, "go.mod"), []byte("module github.com/acme/shop\n\ngo 1.22\n"), 0o644)
	var log strings.Builder
	if err := run(Options{Spec: sp, Out: out, Runner: &noNetRunner{}}, &log); err != nil {
		t.Fatal(err)
	}
	if mod := read(t, out, "go.mod"); !strings.Contains(mod, "module github.com/acme/shop") {
		t.Fatalf("module path should come from go.mod:\n%s", mod)
	}
	if !strings.Contains(log.String(), "github.com/acme/shop") {
		t.Errorf("the repository owner should come from go.mod too:\n%s", log.String())
	}
}

func TestOwnerlessWithoutGoModIsAnErrorNotANetworkCall(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, ownerlessSpec, "")
	err := run(Options{Spec: sp, Out: filepath.Join(dir, "out"), Runner: &noNetRunner{}}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "write (github OWNER/shop)") || strings.Contains(err.Error(), "network") {
		t.Fatalf("want a clear error without touching the network, got %v", err)
	}
}

func TestOwnerFromExplicitModule(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, `(project shop (module github.com/acme/shop) (go 1.22) (repo (github shop))
  (package a (struct S (field X int))))`, "")
	var log strings.Builder
	if err := run(Options{Spec: sp, Out: filepath.Join(dir, "out"), Runner: &noNetRunner{}}, &log); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "github.com/acme/shop") || strings.Contains(log.String(), "differs") {
		t.Errorf("owner should come from the module path:\n%s", log.String())
	}
}

// ---- did you mean ----

func TestOSADistance(t *testing.T) {
	for _, c := range []struct {
		a, b string
		d    int
	}{{"entity", "entity", 0}, {"entiy", "entity", 1}, {"strcut", "struct", 1}, {"lst", "list", 1}, {"enum", "entity", 4}} {
		if got := osaDistance(c.a, c.b); got != c.d {
			t.Errorf("osaDistance(%s, %s) = %d, want %d", c.a, c.b, got, c.d)
		}
	}
	if closest("J", []string{"I"}) != "" {
		t.Error("one-letter names are different names, not typos")
	}
}

func TestTypoIsAnErrorUnknownFormIsATask(t *testing.T) {
	if err := validateSrc(t, okPrefix+`(entiy E (field ID int64))))`, false); err == nil ||
		!strings.Contains(err.Error(), "unknown form (entiy E (field ID int64)) (did you mean entity?)") {
		t.Errorf("a typo of a known form must be an error with a suggestion, got %v", err)
	}
	if err := validateSrc(t, okPrefix+`(workflow Checkout (step pay))))`, false); err != nil {
		t.Errorf("a genuinely unknown form should still go to the LLM, got %v", err)
	}
}

func TestDidYouMeanEverywhere(t *testing.T) {
	spec := func(pkg, extra string) string {
		return `(project p (module example.com/p) (go 1.22) ` + extra + ` (package a ` + pkg + `))`
	}
	for src, want := range map[string]string{
		spec(`(struct S (feild X int))`, ""):                                                  "unexpected (feild X int) in struct (did you mean field?)",
		spec(`(entity E (field ID int64) (field NoteID int64) (store lst))`, ""):              `unknown store op "lst" (did you mean list?)`,
		spec(`(entity E (field ID int64) (field NoteID int64) (store (list-by NoteId)))`, ""): "(did you mean NoteID?)",
		spec(`(entity E (field ID int64) (store (cont-by ID)))`, ""):                          "(did you mean count-by?)",
		spec(`(interface I (methd M))`, ""):                                                   "(did you mean method?)",
		spec(`(interface I (method M (retrns error)))`, ""):                                   "(did you mean returns?)",
		spec(`(struct S (field X int (tga "x")))`, ""):                                        "(did you mean tag?)",
		spec(`(interface Mailer (method M)) (implement Mailr (as X))`, ""):                    "(did you mean Mailer?)",
		spec(``, `(reqire (u github.com/google/uuid v1.6.0))`):                                "unknown project item (reqire (u github.com/google/uuid v1.6.0)) (did you mean require?)",
		spec(``, `(repo (visibilty private))`):                                                "(did you mean visibility?)",
	} {
		if err := validateSrc(t, src, false); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s\n  want %q\n  got  %v", src, want, err)
		}
	}
	cfg, _ := Parse("c.sexp", `(config (json-tag snake) (storage postgress))`)
	_, err := ParseConfig(cfg[0])
	if err == nil || !strings.Contains(err.Error(), "(did you mean json-tags?)") || !strings.Contains(err.Error(), "(did you mean postgres?)") {
		t.Errorf("config should report both typos at once, got %v", err)
	}
	if err := mergeSrc(t, `(project p (module example.com/p) (go 1.22))`, `(pakage a)`); err == nil || !strings.Contains(err.Error(), "(did you mean package?)") {
		t.Errorf("top-level typo, got %v", err)
	}
	if _, err := parseWS(t, `(workspace (out ..) (worktress))`); err == nil || !strings.Contains(err.Error(), "(did you mean worktrees?)") {
		t.Errorf("workspace typo, got %v", err)
	}
}

// ---- enum ----

func TestEnumValidation(t *testing.T) {
	for form, want := range map[string]string{
		`(enum Status)`:                          "enum needs at least one value",
		`(enum Status paid paid)`:                `duplicate enum value "paid"`,
		`(enum Status "it's")`:                   "free of quotes",
		`(enum status paid)`:                     `type name "status" must be exported`,
		`(enum Status paid) (struct StatusPaid)`: "", // checked below: collision
		`(enum Status (docs "x") paid)`:          "(did you mean doc?)",
	} {
		err := validateSrc(t, okPrefix+form+`))`, false)
		if want == "" {
			if err == nil || !strings.Contains(err.Error(), "StatusPaid") {
				t.Errorf("%s: want a name collision error, got %v", form, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", form, want, err)
		}
	}
}

func TestEnumTile(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, `(project p (module example.com/p) (go 1.22)
  (package orders
    (enum Status pending in-transit on_hold)
    (entity Order (field ID int64) (field Status Status) (field Prev *Status) (store get)))
  (package billing
    (entity Invoice (field ID int64) (field OrderStatus orders.Status) (store get))))
(config (storage postgres))`, "")
	out := filepath.Join(dir, "out")
	if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	gen := read(t, out, "orders/orders_gen.go")
	for _, want := range []string{
		"type Status string",
		`StatusInTransit Status = "in-transit"`,
		`StatusOnHold    Status = "on_hold"`,
		"var StatusValues = []Status{StatusPending, StatusInTransit, StatusOnHold}",
		"func (s Status) Valid() bool",
		"func ParseStatus(s string) (Status, error)",
	} {
		if !strings.Contains(gen, want) {
			t.Errorf("orders_gen.go missing %q", want)
		}
	}
	schema := read(t, out, "db/schema.sql")
	for _, want := range []string{
		"status TEXT CHECK (status IN ('pending', 'in-transit', 'on_hold')) NOT NULL,",
		"prev TEXT CHECK (prev IN ('pending', 'in-transit', 'on_hold'))\n",                        // nullable: no NOT NULL
		"order_status TEXT CHECK (order_status IN ('pending', 'in-transit', 'on_hold')) NOT NULL", // other package
	} {
		if !strings.Contains(schema, want) {
			t.Errorf("schema.sql missing %q:\n%s", want, schema)
		}
	}
	tasks := read(t, out, "tilegen.tasks.json")
	if !strings.Contains(tasks, "Convert Status with ParseStatus, Prev with ParseStatus") ||
		!strings.Contains(tasks, "Convert OrderStatus with orders.ParseStatus") {
		t.Errorf("postgres tasks should say how to convert enum columns:\n%s", tasks)
	}
}

// ---- tilegen check ----

// snapshot records every file under dir, so tests can prove check wrote nothing.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := os.ReadFile(p)
			files[p] = string(b)
		}
		return nil
	})
	return files
}

func checkRun(t *testing.T, spec, out string, allowHoles bool) (string, error) {
	t.Helper()
	var log strings.Builder
	err := run(Options{Spec: spec, Out: out, Check: true, AllowHoles: allowHoles}, &log)
	return log.String(), err
}

func TestCheckPassesRightAfterGenerate(t *testing.T) {
	p := newRC(t)
	p.gen("get save", "yes", "memory")
	sp := filepath.Join(p.dir, "spec.sexp")
	if log, err := checkRun(t, sp, p.out, true); err != nil || !strings.Contains(log, "ok:") {
		t.Fatalf("check right after generate must pass: %v\n%s", err, log)
	}
	if _, err := checkRun(t, sp, p.out, false); err == nil || !strings.Contains(err.Error(), "2 open hole(s)") {
		t.Fatalf("without -allow-holes, open holes fail: %v", err)
	}
}

func TestCheckReportsAndWritesNothing(t *testing.T) {
	p := newRC(t)
	p.gen("get save", "yes", "memory")
	sp := filepath.Join(p.dir, "spec.sexp")
	writeSpec(t, p.dir, fmt.Sprintf(rcSpec, "get save count", "yes", "memory"), "")
	before := snapshot(t, p.out)
	log, err := checkRun(t, sp, p.out, true)
	if err == nil || !strings.Contains(err.Error(), "out of date") {
		t.Fatalf("a spec change must fail check: %v", err)
	}
	for _, want := range []string{"stale    notes/notes_gen.go", "stub     Count to notes/memory_note_store.go"} {
		if !strings.Contains(log, want) {
			t.Errorf("missing %q in:\n%s", want, log)
		}
	}
	after := snapshot(t, p.out)
	if len(before) != len(after) {
		t.Fatalf("check created or deleted files: %d -> %d", len(before), len(after))
	}
	for f, content := range before {
		if after[f] != content {
			t.Fatalf("check modified %s", f)
		}
	}
}

func TestCheckDriftFailsEvenWithAllowHoles(t *testing.T) {
	p := newRC(t)
	p.gen("get save", "yes", "memory")
	p.fill("notes/memory_note_store.go", "notes.MemoryNoteStore.Get", "_ = ctx\n\treturn s.m[id], nil")
	p.gen("get save", "yes", "memory")
	writeSpec(t, p.dir, fmt.Sprintf(rcSpec, "get save", "no", "memory"), "")
	log, err := checkRun(t, filepath.Join(p.dir, "spec.sexp"), p.out, true)
	if err == nil || !strings.Contains(err.Error(), "1 drifted method(s)") || !strings.Contains(log, "drift    (*MemoryNoteStore).Get") {
		t.Fatalf("drift must fail check: %v\n%s", err, log)
	}
}

func TestCheckPlannedRemovals(t *testing.T) {
	p := newRC(t)
	p.gen("get save", "yes", "postgres")
	p.gen("get save", "yes", "memory") // switch back: sql files would be removed
	p.gen("get save", "yes", "postgres")
	writeSpec(t, p.dir, fmt.Sprintf(rcSpec, "get save", "yes", "memory"), "")
	log, err := checkRun(t, filepath.Join(p.dir, "spec.sexp"), p.out, true)
	if err == nil || !strings.Contains(log, "remove   db/schema.sql") {
		t.Fatalf("planned removals must be reported: %v\n%s", err, log)
	}
	if _, err := os.Stat(filepath.Join(p.out, "db", "schema.sql")); err != nil {
		t.Fatal("check must not delete anything")
	}
}

// TestSweepMarkerOnlyOnFirstLine: sqlc copies comments from db/query.sql
// into its Go, so a tilegen marker later in a file must not make tilegen
// think the file is its own (it would delete sqlc's output).
func TestSweepMarkerOnlyOnFirstLine(t *testing.T) {
	p := newRC(t)
	p.gen("get", "yes", "postgres")
	sqlcFile := filepath.Join(p.out, "internal", "db", "query.sql.go")
	os.MkdirAll(filepath.Dir(sqlcFile), 0o755)
	os.WriteFile(sqlcFile, []byte("// Code generated by sqlc. DO NOT EDIT.\n\npackage db\n\n// "+generatedMarker+"\nfunc F() {}\n"), 0o644)
	p.gen("get", "yes", "postgres")
	if _, err := os.Stat(sqlcFile); err != nil {
		t.Fatal("tilegen deleted sqlc's output because a copied marker appeared after line 1")
	}
	if q := read(t, p.out, "db/query.sql"); !strings.Contains(q, "DO NOT EDIT.\n-- (the empty statement below") || !strings.Contains(q, "\n;\n") {
		t.Errorf("query.sql header should end with an empty statement:\n%s", q)
	}
}

// ---- tilegen prompt ----

func promptOut(t *testing.T, spec, out string, args ...string) (string, error) {
	t.Helper()
	var stdout strings.Builder
	err := promptCmd(append([]string{"-out", out, spec}, args...), &stdout, io.Discard)
	return stdout.String(), err
}

func TestPromptListsAndRenders(t *testing.T) {
	p := newRC(t)
	p.gen("get save", "yes", "memory")
	sp := filepath.Join(p.dir, "spec.sexp")

	list, err := promptOut(t, sp, p.out)
	if err != nil || !strings.Contains(list, "2 open task(s)") || !strings.Contains(list, "notes.MemoryNoteStore.Get ") || !strings.Contains(list, "notes/memory_note_store.go:") {
		t.Fatalf("listing: %v\n%s", err, list)
	}
	pr, err := promptOut(t, sp, p.out, "notes.MemoryNoteStore.Get")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# Task notes.MemoryNoteStore.Get",
		"func (s *MemoryNoteStore) Get(ctx context.Context, id int64) (*Note, error)",
		"Intent: Look up id in s.m while holding s.mu.",
		"Reply with the complete method, signature and body, in a single ```go code block",
		"### notes/memory_note_store.go",
		`panic("tilegen:hole notes.MemoryNoteStore.Get")`, // the file itself, with the hole
		"### notes/notes_gen.go",
		"type NoteStore interface",
		"### go.mod",
	} {
		if !strings.Contains(pr, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestPromptSeesThePlanAndWritesNothing(t *testing.T) {
	p := newRC(t)
	p.gen("get", "yes", "memory")
	writeSpec(t, p.dir, fmt.Sprintf(rcSpec, "get count", "yes", "memory"), "") // not regenerated
	before := snapshot(t, p.out)
	pr, err := promptOut(t, filepath.Join(p.dir, "spec.sexp"), p.out, "notes.MemoryNoteStore.Count")
	if err != nil || !strings.Contains(pr, "func (s *MemoryNoteStore) Count(ctx context.Context) (int64, error)") {
		t.Fatalf("prompt should use the current spec even before regenerating: %v\n%s", err, pr)
	}
	if after := snapshot(t, p.out); len(after) != len(before) {
		t.Fatal("prompt must not write anything")
	}
}

func TestPromptUnknownIDSuggests(t *testing.T) {
	p := newRC(t)
	p.gen("get", "yes", "memory")
	_, err := promptOut(t, filepath.Join(p.dir, "spec.sexp"), p.out, "notes.MemoryNoteStore.Gt")
	if err == nil || !strings.Contains(err.Error(), "(did you mean notes.MemoryNoteStore.Get?)") {
		t.Fatalf("want a suggestion, got %v", err)
	}
}

func TestPromptFilelessAndDrift(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, `(project p (module example.com/p) (go 1.22)
  (package a (llm "Add a Search function.") (workflow Checkout (step pay))))`, "")
	out := filepath.Join(dir, "out")
	if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	pr, err := promptOut(t, sp, out, "-all")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Write new Go code in package `a`", "Intent: Add a Search function.",
		"(workflow Checkout (step pay))", "Reply with complete Go files", "\n---\n"} {
		if !strings.Contains(pr, want) {
			t.Errorf("-all prompt missing %q", want)
		}
	}

	d := newRC(t)
	d.gen("get", "yes", "memory")
	d.fill("notes/memory_note_store.go", "notes.MemoryNoteStore.Get", "_ = ctx\n\treturn s.m[id], nil")
	d.gen("get", "no", "memory")
	pr, err = promptOut(t, filepath.Join(d.dir, "spec.sexp"), d.out, "notes.MemoryNoteStore.Get")
	if err != nil || !strings.Contains(pr, "no longer matches the spec. Change its signature to exactly this") ||
		!strings.Contains(pr, "func (s *MemoryNoteStore) Get(id int64) (*Note, error)") {
		t.Fatalf("drift prompt: %v\n%s", err, pr)
	}
}

func TestFlagsAfterPositionalArgs(t *testing.T) {
	p := newRC(t)
	p.gen("get", "yes", "memory")
	var log strings.Builder
	// flags after SPEC must still count
	if err := checkCmd([]string{"-out", p.out, filepath.Join(p.dir, "spec.sexp"), "-allow-holes"}, &log); err != nil {
		t.Fatalf("check SPEC -allow-holes: %v\n%s", err, log.String())
	}
}

// ---- tilegen fill ----

// fakeLLM writes a stand-in LLM script to dir. It answers each prompt from
// dir/<task-id>.<attempt>.txt, falling back to dir/<task-id>.txt, and saves
// every prompt it receives as dir/<task-id>.prompt.<attempt>.
func fakeLLM(t *testing.T, dir string, answers map[string]string) string {
	t.Helper()
	script := `D="` + dir + `"
cat > "$D/in.txt"
id=$(sed -n 's/^# Task //p' "$D/in.txt" | head -1)
n=$(cat "$D/$id.count" 2>/dev/null || echo 0); n=$((n+1)); echo $n > "$D/$id.count"
cp "$D/in.txt" "$D/$id.prompt.$n"
f="$D/$id.$n.txt"; [ -f "$f" ] || f="$D/$id.txt"
cat "$f"
`
	os.WriteFile(filepath.Join(dir, "llm.sh"), []byte(script), 0o755)
	fence := strings.Repeat("`", 3)
	for name, code := range answers {
		os.WriteFile(filepath.Join(dir, name), []byte("Here it is:\n"+fence+"go\n"+code+"\n"+fence+"\n"), 0o644)
	}
	return "sh " + filepath.Join(dir, "llm.sh")
}

func fillRun(t *testing.T, p *rcProject, llm string, args ...string) (string, error) {
	t.Helper()
	var log strings.Builder
	err := fillCmd(append([]string{"-out", p.out, "-llm", llm, filepath.Join(p.dir, "spec.sexp")}, args...), &log)
	return log.String(), err
}

func TestFillSucceedsRetriesAndRestores(t *testing.T) {
	p := newRC(t)
	p.gen("get list save", "yes", "memory")
	llmDir := t.TempDir()
	llm := fakeLLM(t, llmDir, map[string]string{
		"notes.MemoryNoteStore.Get.txt":    "func (s *MemoryNoteStore) Get(ctx context.Context, id int64) (*Note, error) {\n\ts.mu.Lock()\n\tdefer s.mu.Unlock()\n\treturn s.m[id], nil\n}",
		"notes.MemoryNoteStore.Save.1.txt": "func (s *MemoryNoteStore) Save(ctx context.Context, note *Note) error {\n\ts.m[note.ID] = nte\n\treturn nil\n}",
		"notes.MemoryNoteStore.Save.2.txt": "func (s *MemoryNoteStore) Save(ctx context.Context, note *Note) error {\n\ts.m[note.ID] = note\n\treturn nil\n}",
		"notes.MemoryNoteStore.List.txt":   "func (s *MemoryNoteStore) List(ctx context.Context, limit int) ([]*Note, error) {\n\treturn nil, nil\n}",
	})
	log, err := fillRun(t, p, llm)
	if err == nil || !strings.Contains(err.Error(), "notes.MemoryNoteStore.List") {
		t.Fatalf("List should fail: %v\n%s", err, log)
	}
	for _, want := range []string{"filled notes.MemoryNoteStore.Get (attempt 1)", "filled notes.MemoryNoteStore.Save (attempt 2)",
		"open   notes.MemoryNoteStore.List", "filled 2 of 3 (1 on the first try); 1 open task(s) remain"} {
		if !strings.Contains(log, want) {
			t.Errorf("log missing %q:\n%s", want, log)
		}
	}
	src := read(t, p.out, "notes/memory_note_store.go")
	if !strings.Contains(src, "return s.m[id], nil") || !strings.Contains(src, "s.m[note.ID] = note\n") ||
		!strings.Contains(src, `panic("tilegen:hole notes.MemoryNoteStore.List")`) {
		t.Errorf("want Get and Save filled and List's hole restored:\n%s", src)
	}
	retry, _ := os.ReadFile(filepath.Join(llmDir, "notes.MemoryNoteStore.Save.prompt.2"))
	if !strings.Contains(string(retry), "undefined: nte") || !strings.Contains(string(retry), "Your previous answer") {
		t.Errorf("the retry prompt should carry the compiler error and the previous answer:\n%s", retry)
	}
	if tasks := read(t, p.out, "tilegen.tasks.json"); strings.Contains(tasks, "MemoryNoteStore.Get") || !strings.Contains(tasks, "MemoryNoteStore.List") {
		t.Errorf("tasks.json should list only what is left:\n%s", tasks)
	}
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = p.out
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the project must build after fill: %v\n%s", err, out)
	}
}

func TestFillPatternsAndDryRun(t *testing.T) {
	p := newRC(t)
	p.gen("get save", "yes", "memory")
	llmDir := t.TempDir()
	llm := fakeLLM(t, llmDir, map[string]string{
		"notes.MemoryNoteStore.Get.txt": "func (s *MemoryNoteStore) Get(ctx context.Context, id int64) (*Note, error) {\n\treturn s.m[id], nil\n}",
	})
	if log, err := fillRun(t, p, llm, "-dry-run", "*.Get"); err != nil || !strings.Contains(log, "would fill 1 task(s)") {
		t.Fatalf("dry run: %v\n%s", err, log)
	}
	if _, err := os.Stat(filepath.Join(llmDir, "notes.MemoryNoteStore.Get.count")); err == nil {
		t.Fatal("dry run must not call the LLM")
	}
	if log, err := fillRun(t, p, llm, "*.Get"); err != nil || !strings.Contains(log, "filled 1 of 1") {
		t.Fatalf("pattern fill: %v\n%s", err, log)
	}
	if _, err := os.Stat(filepath.Join(llmDir, "notes.MemoryNoteStore.Save.count")); err == nil {
		t.Fatal("Save did not match the pattern and must not be asked")
	}
}

func TestFillNeedsACommandAndABuildingBaseline(t *testing.T) {
	p := newRC(t)
	p.gen("get", "yes", "memory")
	if _, err := fillRun(t, p, ""); err == nil || !strings.Contains(err.Error(), "no LLM command") {
		t.Fatalf("want a missing-command error, got %v", err)
	}
	os.WriteFile(filepath.Join(p.out, "notes", "broken.go"), []byte("package notes\n\nvar x = undefinedThing\n"), 0o644)
	if _, err := fillRun(t, p, "cat"); err == nil || !strings.Contains(err.Error(), "does not build before filling") {
		t.Fatalf("want a baseline build error, got %v", err)
	}
}

func TestFillFixesDrift(t *testing.T) {
	p := newRC(t)
	p.gen("get", "yes", "memory")
	p.fill("notes/memory_note_store.go", "notes.MemoryNoteStore.Get", "_ = ctx\n\treturn s.m[id], nil")
	writeSpec(t, p.dir, fmt.Sprintf(rcSpec, "get", "no", "memory"), "") // Get drifts
	llm := fakeLLM(t, t.TempDir(), map[string]string{
		"notes.MemoryNoteStore.Get.txt": "func (s *MemoryNoteStore) Get(id int64) (*Note, error) {\n\treturn s.m[id], nil\n}",
	})
	if log, err := fillRun(t, p, llm); err != nil || !strings.Contains(log, "filled notes.MemoryNoteStore.Get") {
		t.Fatalf("drift fill: %v\n%s", err, log)
	}
	if src := read(t, p.out, "notes/memory_note_store.go"); !strings.Contains(src, "func (s *MemoryNoteStore) Get(id int64) (*Note, error)") {
		t.Fatalf("drifted method should now have the new signature:\n%s", src)
	}
}

func TestFillWorkspaceConfig(t *testing.T) {
	for src, want := range map[string]string{
		`(workspace (fill (retries 9)))`:          "retries must be 0 to 5",
		`(workspace (fill (timeout "soon")))`:     "timeout must be a duration",
		`(workspace (fill (comand "claude -p")))`: "(did you mean command?)",
	} {
		if _, err := parseWS(t, src); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", src, want, err)
		}
	}
	ws, err := parseWS(t, `(workspace (fill (command "claude -p") (retries 2) (timeout "90s")))`)
	if err != nil || ws.Fill.Command != "claude -p" || ws.Fill.Retries != 2 || ws.Fill.Timeout != 90*time.Second || ws.Fill.Build != "go build ./..." {
		t.Fatalf("fill config: %+v, %v", ws.Fill, err)
	}
}

func TestOnlyDrift(t *testing.T) {
	drift := "# example.com/p/notes\nnotes/notes_gen.go:20:19: cannot use (*MemoryNoteStore)(nil) (value of type *MemoryNoteStore) as NoteStore value in variable declaration: *MemoryNoteStore does not implement NoteStore (wrong type for method Get)\n\t\thave Get(context.Context, int64) (*Note, error)\n\t\twant Get(int64) (*Note, error)"
	pending := map[string]bool{"MemoryNoteStore.Get": true}
	if !onlyDrift(drift, pending) {
		t.Error("a pending drift error should be tolerated")
	}
	if onlyDrift(drift, map[string]bool{"MemoryNoteStore.Save": true}) {
		t.Error("drift in a method with no pending task is a real error")
	}
	if onlyDrift(drift+"\nnotes/x.go:3:9: undefined: y", pending) {
		t.Error("any other error must stop fill")
	}
}

// ---- tile registry ----

func TestRegistryListsEveryTileAndBackend(t *testing.T) {
	var out strings.Builder
	if err := tilesCmd(nil, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"expand      entity", "select      catch-all", "llm/task",
		"memory", "postgres-sqlc", "events", "event-bus", "capability store      offered by memory, postgres-pgx, postgres-sqlc"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("tiles missing %q:\n%s", want, out.String())
		}
	}
}

func TestRegistrySexpIsData(t *testing.T) {
	var out strings.Builder
	if err := tilesCmd([]string{"-sexp"}, &out); err != nil {
		t.Fatal(err)
	}
	forms, err := Parse("tiles.sexp", out.String())
	if err != nil {
		t.Fatalf("tilegen tiles -sexp must parse back as S-expressions: %v", err)
	}
	if len(forms) != len(Registry()) {
		t.Fatalf("got %d forms for %d tiles", len(forms), len(Registry()))
	}
	for _, f := range forms {
		if f.Head() != "tile" || f.Find("pass") == nil || (f.Find("covers") == nil && f.Find("offers") == nil) {
			t.Errorf("malformed tile: %s", short(f))
		}
	}
	var pg *Node
	for _, f := range forms {
		if f.List[1].Atom == "postgres-sqlc" {
			pg = f
		}
	}
	if pg == nil || pg.Find("form").Flat() != "(form db-rows)" || pg.Find("covers").Flat() != "(covers (alias postgres))" ||
		pg.Find("offers").Flat() != "(offers store)" || pg.Find("requires").Flat() != "(requires (tool sqlc))" {
		t.Errorf("postgres tile: %s", pg.Flat())
	}
}

// TestBackendsComeFromTheRegistry: config, validation and the store tile all
// ask the registry, so a new backend needs no change in the core.
func TestBackendsComeFromTheRegistry(t *testing.T) {
	registerStoreBackend(&Backend{Name: "fake", Tile: "fake-store", Cost: Cost{{Dim: "llm-work", Value: 9}}, Packages: map[string]string{"fakedb": "internal/fakedb"},
		Implement: func(in StoreInput) (StoreParts, error) {
			return StoreParts{Params: L(Sym("params")), Body: "return &" + in.Impl + "{}", Hint: func(*Node) string { return "fake" }}, nil
		}})
	defer func() {
		delete(backends, "fake")
		delete(tileAliases, "fake-store")
		var keep []*Offer
		for _, o := range offers["store"] {
			if o.Tile != "fake-store" {
				keep = append(keep, o)
			}
		}
		offers["store"] = keep
	}()

	cfg, _ := Parse("c.sexp", "(config (storage fake))")
	if c, err := ParseConfig(cfg[0]); err != nil || c.Storage != "fake" {
		t.Fatalf("config should accept a registered backend: %v", err)
	}
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, `(project p (module example.com/p) (go 1.22)
  (package a (entity E (field ID int64) (store get))))
(config (storage fake))`, "")
	out := filepath.Join(dir, "out")
	if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if src := read(t, out, "a/fake_e_store.go"); !strings.Contains(src, "type FakeEStore struct") {
		t.Errorf("the fake backend should implement the store:\n%s", src)
	}
	sp2, _ := writeSpec(t, t.TempDir(), `(project p (module example.com/p) (go 1.22)
  (package fakedb (struct S (field X int)))
  (package a (entity E (field ID int64) (store get))))
(config (storage fake))`, "")
	if err := run(Options{Spec: sp2, Out: filepath.Join(dir, "out2")}, io.Discard); err == nil ||
		!strings.Contains(err.Error(), `package name "fakedb" is reserved: the fake-store tile puts its code in internal/fakedb`) {
		t.Errorf("a backend's packages should be reserved: %v", err)
	}
	bad, _ := Parse("c.sexp", "(config (storage fak))")
	if _, err := ParseConfig(bad[0]); err == nil || !strings.Contains(err.Error(), "(did you mean fake?)") {
		t.Errorf("unknown backends should suggest registered ones: %v", err)
	}
}

// ---- events ----

const eventsSpec = `(project app (module example.com/app) (go 1.22)
  (package sharing
    (events
      (event NoteShared (field NoteID int64) (field Email string))
      (event NoteUnshared (field NoteID int64)))))
(config (context-first %s))`

// TestEventsBusBehaves generates the bus and runs a behavior test inside
// the generated package: the tile has no holes, so its code must be right.
func TestEventsBusBehaves(t *testing.T) {
	for _, ctx := range []string{"yes", "no"} {
		dir := t.TempDir()
		sp, _ := writeSpec(t, dir, fmt.Sprintf(eventsSpec, ctx), "")
		out := filepath.Join(dir, "out")
		if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
			t.Fatal(err)
		}
		gen := read(t, out, "sharing/events_gen.go")
		c, h := "ctx context.Context, ", "context.Context, "
		if ctx == "no" {
			c, h = "", ""
		}
		for _, want := range []string{
			"// " + generatedMarker, "type NoteShared struct", "`json:\"note_id\"`",
			"PublishNoteShared(" + c + "e NoteShared) error",
			"OnNoteShared(h func(" + h + "NoteShared) error) func()",
			"var _ Bus = (*LocalBus)(nil)",
		} {
			if !strings.Contains(gen, want) {
				t.Errorf("context-first %s: events_gen.go missing %q", ctx, want)
			}
		}
		arg := "context.Background(), "
		hp := "_ context.Context, "
		if ctx == "no" {
			arg, hp = "", ""
		}
		test := strings.NewReplacer("ARG", arg, "HP", hp).Replace(`package sharing

import (
	"context"
	"errors"
	"testing"
)

var _ = context.Background

func TestBus(t *testing.T) {
	var b Bus = NewLocalBus()
	var got []string
	b.OnNoteShared(func(HPe NoteShared) error { got = append(got, "a"+e.Email); return nil })
	un := b.OnNoteShared(func(HPe NoteShared) error { got = append(got, "b"+e.Email); return nil })
	b.PublishNoteShared(ARGNoteShared{Email: "x"})
	un()
	un()
	b.PublishNoteShared(ARGNoteShared{Email: "y"})
	if len(got) != 3 || got[0] != "ax" || got[1] != "bx" || got[2] != "ay" {
		t.Fatalf("delivery: %v", got)
	}
	e1, e2 := errors.New("1"), errors.New("2")
	b.OnNoteUnshared(func(HPe NoteUnshared) error { return e1 })
	b.OnNoteUnshared(func(HPe NoteUnshared) error { return e2 })
	if err := b.PublishNoteUnshared(ARGNoteUnshared{}); !errors.Is(err, e1) || !errors.Is(err, e2) {
		t.Fatalf("errors not joined: %v", err)
	}
}
`)
		os.WriteFile(filepath.Join(out, "sharing", "bus_test.go"), []byte(test), 0o644)
		cmd := exec.Command("go", "test", "./sharing/")
		cmd.Dir = out
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("context-first %s: generated bus fails its behavior test: %v\n%s", ctx, err, b)
		}
	}
}

func TestEventsValidation(t *testing.T) {
	base := `(project p (module example.com/p) (go %s) (package a %s))`
	for _, c := range []struct{ goVer, form, want string }{
		{"1.22", `(events)`, "needs at least one (event ...)"},
		{"1.22", `(events (evnt E))`, "(did you mean event?)"},
		{"1.22", `(events (event E) (event E))`, `duplicate type "E"`},
		{"1.22", `(struct Bus) (events (event E))`, "generates Bus, but the package already declares it"},
		{"1.22", `(events (event E)) (events (event F))`, "one events form per package"},
		{"1.19", `(events (event E))`, "needs (go 1.20) or later"},
		{"1.22", `(evnts (event E))`, "(did you mean events?)"},
	} {
		if err := validateSrc(t, fmt.Sprintf(base, c.goVer, c.form), false); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want %q, got %v", c.form, c.want, err)
		}
	}
}

func TestPackageTilesJoinBeforeTheCatchAll(t *testing.T) {
	rules := Select.Rules
	if last := rules[len(rules)-1]; last.Name != "catch-all" {
		t.Fatalf("the catch-all must stay last, got %s", last.Name)
	}
	found := false
	for _, r := range rules {
		found = found || r.Name == "events"
	}
	if !found {
		t.Fatal("the events tile should have registered itself into select")
	}
}

// ---- selection: legality, cost, explain ----

func autoSpec(storage string) string {
	return `(project p (module example.com/p) (go 1.22)
  (package orders (entity Order (field ID int64) (field Total int64) (store get save (durable))))
  (package sessions (entity Session (field ID string) (store get))))
(config (storage ` + storage + `))`
}

func TestSelectionPicksTheCheapestLegalBackendPerStore(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, autoSpec("auto"), "")
	out := filepath.Join(dir, "out")
	if err := run(Options{Spec: sp, Out: out, Dump: true}, io.Discard); err != nil {
		t.Fatal(err)
	}
	read(t, out, "orders/postgres_order_store.go")   // durable: memory is illegal
	read(t, out, "sessions/memory_session_store.go") // no needs: memory is cheapest
	read(t, out, "sqlc.yaml")
	if schema := read(t, out, "db/schema.sql"); strings.Contains(schema, "sessions") {
		t.Error("memory stores must not get tables")
	}
	dump := dumpForms(t, read(t, out, ".tilegen/03-concretize.sexp"))
	for _, want := range []string{`(chosen postgres-sqlc (score 29) (own 22) (via row-mapper 7 "1 entity") (by auto))`, "(considered postgres-pgx (score 45))",
		`(illegal memory "an in-memory map loses its data on restart")`, "(chosen memory (score 12) (by auto))"} {
		if !dump[want] {
			t.Errorf("the dump should record %s", want)
		}
	}
}

func TestSelectionExplicitIllegalChoiceIsAnError(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, autoSpec("memory"), "")
	err := run(Options{Spec: sp, Out: filepath.Join(dir, "out")}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "the config chose memory, which is illegal for orders.OrderStore: an in-memory map loses its data on restart; legal: postgres-sqlc (score 29), postgres-pgx (score 45), or use auto") {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "spec.sexp:2:") {
		t.Errorf("the error should point at the store: %v", err)
	}
}

func TestPgxBackend(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, autoSpec("pgx"), "")
	out := filepath.Join(dir, "out")
	if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	src := read(t, out, "orders/pgx_order_store.go")
	if !strings.Contains(src, "pool *pgxpool.Pool") || !strings.Contains(src, `"github.com/jackc/pgx/v5/pgxpool"`) {
		t.Errorf("pgx store:\n%s", src)
	}
	for _, f := range []string{"db/query.sql", "sqlc.yaml"} {
		if _, err := os.Stat(filepath.Join(out, f)); err == nil {
			t.Errorf("pgx must not write %s", f)
		}
	}
	tasks := read(t, out, "tilegen.tasks.json")
	if !strings.Contains(tasks, "A query that fits: SELECT * FROM orders WHERE id = $1;") || strings.Contains(tasks, "Implement it as documented: Get") {
		t.Errorf("pgx hints:\n%s", tasks)
	}
}

func TestExplainCommand(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, autoSpec("auto"), "")
	var out strings.Builder
	if err := explainCmd([]string{sp}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"weights: llm-work 4, maintenance 3", "orders.OrderStore   (store)   needs: durable   chosen by: auto",
		"chosen  postgres-sqlc   score 29   llm 3·4 + maint 2·3 + dep 3·1 + run 1·1 = 22", "+ row-mapper 7 (1 entity)",
		"illegal memory          an in-memory map loses its data on restart", "sessions.SessionStore   (store)   needs: none"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("explain missing %q:\n%s", want, out.String())
		}
	}
	if err := explainCmd([]string{sp, "orders.OrderStor"}, &out, io.Discard); err == nil || !strings.Contains(err.Error(), "(did you mean orders.OrderStore?)") {
		t.Errorf("unknown store: %v", err)
	}
}

func TestNeedsValidation(t *testing.T) {
	base := okPrefix + `(entity E (field ID int64) (store get %s))))`
	for form, want := range map[string]string{
		`(durable yes)`: "expected (_)",
		`(durabel)`:     "(did you mean durable?)",
	} {
		if err := validateSrc(t, fmt.Sprintf(base, form), false); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", form, want, err)
		}
	}
	var out strings.Builder
	tilesCmd([]string{"-sexp", "memory"}, &out)
	if !strings.Contains(out.String(), `(illegal-when (durable) "an in-memory map loses its data on restart")`) {
		t.Errorf("the registry should show legality:\n%s", out.String())
	}
}

// ---- chain rules ----

// TestChainTipsTheChoice: sqlc is cheaper on its own, but its rows must be
// mapped to domain types; with enums and nullable fields the mapper costs
// enough that pgx, which scans straight into domain types, wins.
func TestChainTipsTheChoice(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, `(project p (module example.com/p) (go 1.22)
  (package orders (entity Order (field ID int64) (field Total int64) (store get (durable))))
  (package profiles
    (enum Plan free pro)
    (entity Profile (field ID int64) (field Plan Plan) (field Nick *string) (field Bio *string) (field Gone *time.Time)
      (store get (durable)))))`, "")
	var out strings.Builder
	if err := explainCmd([]string{sp}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"orders.OrderStore   (store)   needs: durable   chosen by: auto\n  chosen  postgres-sqlc   score 29",
		"profiles.ProfileStore   (store)   needs: durable   chosen by: auto\n  chosen  postgres-pgx    score 45",
		"postgres-sqlc   score 51", "+ row-mapper 29 (1 entity, 1 enum field, 3 nullable fields)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("explain missing %q:\n%s", want, got)
		}
	}
	if err := run(Options{Spec: sp, Out: filepath.Join(dir, "out")}, io.Discard); err != nil {
		t.Fatal(err)
	}
	read(t, filepath.Join(dir, "out"), "orders/postgres_order_store.go")
	read(t, filepath.Join(dir, "out"), "profiles/pgx_profile_store.go")
}

func TestChainCostsAndPaths(t *testing.T) {
	e := EntityShape{Fields: []FieldShape{{Name: "A", Enum: true}, {Name: "B", Nullable: true}, {Name: "C", JSONB: true}, {Name: "D"}}}
	steps, s, ok := convert("db-rows", domainForm, e, DefaultPolicy())
	if !ok || len(steps) != 1 || steps[0].Chain.Name != "row-mapper" {
		t.Fatalf("path: %v %v", steps, ok)
	}
	// units = 1 entity + 1 enum + 1 nullable + 2 JSONB = 5: llm 5*4 + maint 3*3 = 29
	if s != 29 || steps[0].Detail != "1 entity, 1 enum field, 1 nullable field, 1 JSONB field" {
		t.Errorf("cost %d, detail %q", s, steps[0].Detail)
	}
	if _, s, ok := convert(domainForm, domainForm, e, DefaultPolicy()); !ok || s != 0 {
		t.Error("no conversion needed for domain")
	}
	if _, _, ok := convert("xml", domainForm, e, DefaultPolicy()); ok {
		t.Error("no chain converts xml")
	}
}

func TestBackendWithoutAChainIsIllegal(t *testing.T) {
	registerStoreBackend(&Backend{Name: "xmlstore", Tile: "xml-store", Form: "xml", Cost: Cost{{Dim: "llm-work", Value: 0}},
		Implement: func(StoreInput) (StoreParts, error) { return StoreParts{}, nil }})
	defer func() {
		delete(backends, "xmlstore")
		delete(tileAliases, "xml-store")
		var keep []*Offer
		for _, o := range offers["store"] {
			if o.Tile != "xml-store" {
				keep = append(keep, o)
			}
		}
		offers["store"] = keep
	}()
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, `(project p (module example.com/p) (go 1.22) (package a (entity E (field ID int64) (store get))))`, "")
	var out strings.Builder
	if err := explainCmd([]string{sp}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "illegal xml-store       it produces xml, and no chain converts that to domain") {
		t.Errorf("a backend with no path to domain must be illegal, even at cost 0:\n%s", out.String())
	}
}

// dumpForms parses a -dump file and returns the flat text of every node in
// it, so tests can look for a form however the pretty-printer wrapped it.
func dumpForms(t *testing.T, text string) map[string]bool {
	t.Helper()
	forms, err := Parse("dump.sexp", text)
	if err != nil {
		t.Fatal(err)
	}
	all := map[string]bool{}
	var walk func(n *Node)
	walk = func(n *Node) {
		all[n.Flat()] = true
		for _, c := range n.List {
			walk(c)
		}
	}
	for _, f := range forms {
		walk(f)
	}
	return all
}

// ---- tilegen.lock ----

const lockSpec = `(project p (module example.com/p) (go 1.22)
  (package orders
    (enum Status placed paid)
    (entity Order (field ID int64) %s (store get (durable))))
  (package sessions (entity Session (field ID string) (store get %s))))`

func lockRun(t *testing.T, dir, orderFields, sessionNeeds string, reselect bool) string {
	t.Helper()
	sp, _ := writeSpec(t, dir, fmt.Sprintf(lockSpec, orderFields, sessionNeeds), "")
	var log strings.Builder
	if err := run(Options{Spec: sp, Out: filepath.Join(dir, "out"), Reselect: reselect}, &log); err != nil {
		t.Fatal(err)
	}
	return log.String()
}

func TestLockPinsAChoiceAgainstDrift(t *testing.T) {
	dir := t.TempDir()
	lockRun(t, dir, "", "", false)
	out := filepath.Join(dir, "out")
	lock := read(t, out, "tilegen.lock")
	if !strings.Contains(lock, "(tile orders.OrderStore postgres-sqlc)") || !strings.Contains(lock, "(tile sessions.SessionStore memory)") {
		t.Fatalf("lock:\n%s", lock)
	}
	// New enum and nullable fields make sqlc's mapper expensive: auto would move to pgx.
	heavy := "(field S Status) (field A *string) (field B *string) (field C *time.Time)"
	lockRun(t, dir, heavy, "", false)
	if _, err := os.Stat(filepath.Join(out, "orders", "pgx_order_store.go")); err == nil {
		t.Fatal("the lock should keep OrderStore on postgres")
	}
	var ex strings.Builder
	explainCmd([]string{"-out", out, filepath.Join(dir, "spec.sexp"), "orders.OrderStore"}, &ex, io.Discard)
	if !strings.Contains(ex.String(), "chosen by: lock") || !strings.Contains(ex.String(), "auto would now pick postgres-pgx (score 45)") {
		t.Errorf("explain should show the pin and the drift:\n%s", ex.String())
	}
	lockRun(t, dir, heavy, "", true) // -reselect
	read(t, out, "orders/pgx_order_store.go")
	if !strings.Contains(read(t, out, "tilegen.lock"), "(tile orders.OrderStore postgres-pgx)") {
		t.Error("-reselect should update the lock")
	}
}

func TestLockReselectsAnIllegalPin(t *testing.T) {
	dir := t.TempDir()
	lockRun(t, dir, "", "", false) // sessions pinned to memory
	log := lockRun(t, dir, "", "(durable)", false)
	if !strings.Contains(log, "tilegen.lock:5:3: tilegen.lock: memory is now illegal for sessions.SessionStore") {
		t.Errorf("want a positioned warning:\n%s", log)
	}
	if !strings.Contains(read(t, filepath.Join(dir, "out"), "tilegen.lock"), "(tile sessions.SessionStore postgres-sqlc)") {
		t.Error("the illegal pin should be re-selected")
	}
}

func TestLockUnknownBackendAndBadFile(t *testing.T) {
	dir := t.TempDir()
	lockRun(t, dir, "", "", false)
	out := filepath.Join(dir, "out")
	os.WriteFile(filepath.Join(out, "tilegen.lock"), []byte("(lock (tile sessions.SessionStore memroy))\n"), 0o644)
	if log := lockRun(t, dir, "", "", false); !strings.Contains(log, `tile "memroy" offers no store any more; re-selected (did you mean memory?)`) {
		t.Errorf("unknown backend in the lock:\n%s", log)
	}
	os.WriteFile(filepath.Join(out, "tilegen.lock"), []byte("(lock (tile x))\n"), 0o644)
	sp := filepath.Join(dir, "spec.sexp")
	if err := run(Options{Spec: sp, Out: out}, io.Discard); err == nil || !strings.Contains(err.Error(), "tilegen.lock:1:7: expected (tile NEED TILE)") {
		t.Errorf("a malformed lock must be a positioned error, got %v", err)
	}
}

func TestCheckFlagsAnUnpinnedStore(t *testing.T) {
	dir := t.TempDir()
	lockRun(t, dir, "", "", false)
	writeSpec(t, dir, strings.Replace(fmt.Sprintf(lockSpec, "", ""), "(package sessions", "(package carts (entity Cart (field ID int64) (store get)))\n  (package sessions", 1), "")
	log, err := checkRun(t, filepath.Join(dir, "spec.sexp"), filepath.Join(dir, "out"), true)
	if err == nil || !strings.Contains(log, "stale    tilegen.lock") {
		t.Errorf("a new store changes the lock, so check should fail: %v\n%s", err, log)
	}
}

// ---- policy ----

const polSpec = `(project p (module example.com/p) (go 1.22)
  (package orders
    (enum Status placed paid)
    (entity Order (field ID int64) (field S Status) (field A *string) (field B *string) (field C *time.Time)
      (store get (durable))))
  (package sessions (entity Session (field ID string) (store get))))`

func policyExplain(t *testing.T, policy string) string {
	t.Helper()
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, polSpec, "")
	args := []string{sp}
	if policy != "" {
		pf := filepath.Join(dir, "policy.sexp")
		os.WriteFile(pf, []byte(policy), 0o644)
		args = append([]string{"-policy", pf}, args...)
	}
	var out strings.Builder
	if err := explainCmd(args, &out, io.Discard); err != nil {
		t.Fatalf("%v", err)
	}
	return out.String()
}

// TestPolicyChangesTheArchitecture: one spec, two teams, different choices.
func TestPolicyChangesTheArchitecture(t *testing.T) {
	// Default: the heavy entity's row-mapper tips it to pgx.
	if got := policyExplain(t, ""); !strings.Contains(got, "chosen  postgres-pgx") {
		t.Errorf("default policy:\n%s", got)
	}
	// Team A values LLM work most and prefers sqlc within a margin.
	a := policyExplain(t, `(policy (weights (llm-work 6) (dependency 1)) (prefer postgres-sqlc) (margin 20))`)
	if !strings.Contains(a, "chosen  postgres-sqlc") || !strings.Contains(a, "policy: preferred, and within the margin of postgres-pgx") {
		t.Errorf("team A should keep sqlc:\n%s", a)
	}
	// Team B avoids sqlc outright.
	b := policyExplain(t, `(policy (weights (dependency 6) (llm-work 1)) (avoid postgres-sqlc "no codegen in CI"))`)
	if !strings.Contains(b, "chosen  postgres-pgx") || !strings.Contains(b, "illegal postgres-sqlc   avoided by the policy: no codegen in CI") {
		t.Errorf("team B should avoid sqlc, with the reason:\n%s", b)
	}
	if !strings.Contains(b, "avoid: postgres-sqlc (no codegen in CI)") {
		t.Errorf("explain should print the policy:\n%s", b)
	}
}

func TestPolicyAffectsChainCosts(t *testing.T) {
	got := policyExplain(t, `(policy (weights (llm-work 1) (maintenance 1)))`)
	if !strings.Contains(got, "+ row-mapper 8 (1 entity, 1 enum field, 3 nullable fields)") {
		t.Errorf("the chain must be priced by the policy too:\n%s", got)
	}
}

func TestPolicyInTheSpecAndValidation(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, polSpec+"\n(policy (avoid postgres-sqlc))", "")
	var out strings.Builder
	if err := explainCmd([]string{sp}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "illegal postgres-sqlc   avoided by the policy") {
		t.Errorf("a (policy ...) form in the spec should apply:\n%s", out.String())
	}
	for src, want := range map[string]string{
		`(policy (weights (llm-work 3)) (weights (runtime 1)))`: "duplicate (weights ...)",
		`(policy (weights (llm-werk 3)))`:                       "(did you mean llm-work?)",
		`(policy (weights (llm-work 500)))`:                     "must be 0 to 100",
		`(policy (margin -1))`:                                  "margin must be 0 to 1000",
		`(policy (preffer memory))`:                             "(did you mean prefer?)",
	} {
		n, _ := Parse("policy.sexp", src)
		if _, err := ParsePolicy(n[0]); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", src, want, err)
		}
	}
	n, _ := Parse("policy.sexp", `(policy (prefer postgres-sqlk) (avoid memroy))`)
	p, _ := ParsePolicy(n[0])
	err := p.checkTileNames()
	if err == nil || !strings.Contains(err.Error(), "(prefer postgres-sqlk): no such tile (did you mean postgres-sqlc?)") ||
		!strings.Contains(err.Error(), "(avoid memroy): no such tile (did you mean memory?)") {
		t.Errorf("unknown tile names: %v", err)
	}
}

func TestPolicyAvoidingEveryBackendIsAnError(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, polSpec, "")
	pf := filepath.Join(dir, "policy.sexp")
	os.WriteFile(pf, []byte(`(policy (avoid memory) (avoid postgres-sqlc) (avoid postgres-pgx))`), 0o644)
	err := run(Options{Spec: sp, Out: filepath.Join(dir, "out"), PolicyFile: pf}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "no tile can cover orders.OrderStore (store)") ||
		!strings.Contains(err.Error(), "postgres-sqlc: avoided by the policy") {
		t.Fatalf("want a clear error, got %v", err)
	}
}

// ---- tilegen import ----

// writeModule writes a Go module: files maps path -> source.
func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for p, src := range files {
		full := filepath.Join(dir, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const legacyMod = "module example.com/legacy\n\ngo 1.22\n"

const legacyCatalog = `package catalog

import "context"

type Status string

const (
	StatusDraft     Status = "draft"
	StatusPublished Status = "published"
	statusHidden    Status = "hidden"
)

type Product struct {
	ID     int64
	SKU    string ` + "`json:\"sku\"`" + `
	Status Status
	Tags   []string
	Meta   map[string]any
	Gone   *string
	secret string
}

type Repo interface {
	Find(ctx context.Context, id int64) (*Product, error)
	Upsert(ctx context.Context, p *Product) error
}

type Logger interface {
	Printf(format string, args ...any)
}

type PGRepo struct {
	conn   string
	logger Logger
}

func (r *PGRepo) Find(ctx context.Context, id int64) (*Product, error) { return nil, nil }
func (r *PGRepo) Upsert(ctx context.Context, p *Product) error        { return nil }

type Cache[K comparable, V any] struct{ m map[K]V }
type Handler func() error
type Alias = Product
`

func importSpec(t *testing.T, dir string, force bool) (map[string]string, string) {
	t.Helper()
	var out, log strings.Builder
	args := []string{dir}
	if force {
		args = append([]string{"-force"}, args...)
	}
	if err := importCmd(args, &out, &log); err != nil {
		t.Fatalf("import: %v", err)
	}
	files := map[string]string{}
	entries, _ := os.ReadDir(filepath.Join(dir, "spec"))
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join(dir, "spec", e.Name()))
		files[e.Name()] = string(b)
	}
	return files, out.String() + log.String()
}

// TestImportLiftsWhatTheTypeCheckerProves: every form comes from go/types.
func TestImportLifts(t *testing.T) {
	dir := writeModule(t, map[string]string{"go.mod": legacyMod, "catalog/catalog.go": legacyCatalog})
	files, out := importSpec(t, dir, false)
	spec := files["10-catalog.sexp"]
	for _, want := range []string{
		"(enum Status draft published)",                 // typed constants, in declaration order; the unexported one is left out
		"(field SKU string (tag \"json:\\\"sku\\\"\"))", // the tag as written
		"(field Tags []string)", "(field Meta map[string]any)", "(field Gone *string)",
		"(method Printf\n      (params (format string) (args ...any)))",                    // variadic
		"(implement Repo (as PGRepo)\n    (field conn string)\n    (field logger Logger))", // types.Implements
	} {
		if !strings.Contains(spec, want) {
			t.Errorf("spec missing %q:\n%s", want, spec)
		}
	}
	for _, unwanted := range []string{"hidden", "secret", "(struct PGRepo", "Cache", "Handler", "Alias"} {
		if strings.Contains(spec, unwanted) {
			t.Errorf("spec should not contain %q:\n%s", unwanted, spec)
		}
	}
	if !strings.Contains(files["00-project.sexp"], "(module example.com/legacy)") || !strings.Contains(files["00-project.sexp"], "(go 1.22)") {
		t.Errorf("project form:\n%s", files["00-project.sexp"])
	}
	notes := files["NOTES.md"]
	for _, want := range []string{"catalog.Cache: it is generic", "catalog.Handler: its underlying type is func() error",
		"catalog.Alias: it is a type alias", `catalog.Product: unexported field "secret"`} {
		if !strings.Contains(notes, want) {
			t.Errorf("NOTES.md missing %q:\n%s", want, notes)
		}
	}
	if !strings.Contains(out, "imported 5 of 8 exported type(s)") {
		t.Errorf("summary: %s", out)
	}
}

// TestImportRoundTrips: the imported spec generates a project that builds.
func TestImportRoundTrips(t *testing.T) {
	dir := writeModule(t, map[string]string{"go.mod": legacyMod, "catalog/catalog.go": legacyCatalog})
	importSpec(t, dir, false)
	out := filepath.Join(t.TempDir(), "regen")
	if err := run(Options{Spec: filepath.Join(dir, "spec"), Out: out}, io.Discard); err != nil {
		t.Fatalf("the imported spec must generate: %v", err)
	}
	for _, cmd := range [][]string{{"go", "mod", "tidy"}, {"go", "build", "./..."}, {"go", "vet", "./..."}} {
		c := exec.Command(cmd[0], cmd[1:]...)
		c.Dir = out
		if b, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v in the regenerated project: %v\n%s", cmd, err, b)
		}
	}
	gen := read(t, out, "catalog/catalog_gen.go")
	for _, want := range []string{"type Status string", "StatusPublished Status = \"published\"", "type Product struct",
		"Find(ctx context.Context, id int64) (*Product, error)", "var _ Repo = (*PGRepo)(nil)"} {
		if !strings.Contains(gen, want) {
			t.Errorf("regenerated file missing %q", want)
		}
	}
}

func TestImportRefusesCodeThatDoesNotTypeCheck(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":             legacyMod,
		"catalog/catalog.go": legacyCatalog,
		"bad/bad.go":         "package bad\n\nfunc F() int { return undefinedThing }\n",
	})
	var out, log strings.Builder
	err := importCmd([]string{dir}, &out, &log)
	if err == nil || !strings.Contains(err.Error(), "does not type-check") || !strings.Contains(err.Error(), "undefined: undefinedThing") {
		t.Fatalf("want a refusal naming the error, got %v", err)
	}
	if strings.Count(err.Error(), "undefined: undefinedThing") != 1 {
		t.Errorf("each error should be reported once:\n%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "spec")); !os.IsNotExist(statErr) {
		t.Error("a refused import must write nothing")
	}
	// -force imports the packages that do check, and says so.
	files, out2 := importSpec(t, dir, true)
	if !strings.Contains(out2, "warning: 1 type error(s)") {
		t.Errorf("-force should warn: %s", out2)
	}
	if _, ok := files["10-catalog.sexp"]; !ok || len(files) < 2 { // numbering is by files written, so catalog is first
		t.Errorf("-force should still import catalog: %v", sortedKeys(files))
	}
}

func TestImportSkipsGeneratedAndNeedsAModule(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":       legacyMod,
		"gen/gen.go":   "// " + generatedMarker + "\n\npackage gen\n\ntype Row struct{ ID int64 }\n",
		"real/real.go": "package real\n\ntype Thing struct{ ID int64 }\n",
	})
	files, _ := importSpec(t, dir, false)
	for name := range files {
		if strings.Contains(name, "gen") && name != "00-project.sexp" {
			t.Errorf("generated packages must not be imported: %s", name)
		}
	}
	if len(files) != 2 {
		t.Errorf("want the project form and real: %v", sortedKeys(files))
	}
	var out, log strings.Builder
	if err := importCmd([]string{t.TempDir()}, &out, &log); err == nil || !strings.Contains(err.Error(), "no go.mod") {
		t.Errorf("want a clear error outside a module, got %v", err)
	}
}

// TestImportSelfRoundTrip: importing a project tilegen generated recovers
// every type, and regenerating from that spec builds.
func TestImportSelfRoundTrip(t *testing.T) {
	first := t.TempDir()
	if err := run(Options{Spec: "examples/shop/spec.sexp", Config: "examples/shop/config.sexp", Out: first}, io.Discard); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(first, ".tilegen"))
	tidy1 := exec.Command("go", "mod", "tidy") // import type-checks, so dependencies must be fetched
	tidy1.Dir = first
	if b, err := tidy1.CombinedOutput(); err != nil {
		t.Skipf("go mod tidy needs the module proxy: %v\n%s", err, b)
	}
	var out, log strings.Builder
	if err := importCmd([]string{first}, &out, &log); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "imported 10 of 10 exported type(s)") {
		t.Errorf("a generated project should import completely: %s", out.String())
	}
	if notes := read(t, first, "spec/NOTES.md"); !strings.Contains(notes, "look like tilegen's event bus") {
		t.Errorf("the event bus should be reported, not lifted as plain structs:\n%s", notes)
	}
	spec := read(t, first, "spec/20-orders.sexp") // packages are sorted by import path
	for _, want := range []string{"(enum Status pending paid shipped cancelled)", "(implement OrderStore (as MemoryOrderStore)",
		"(interface Pricer", "(struct Order"} {
		if !strings.Contains(spec, want) {
			t.Errorf("missing %q:\n%s", want, spec)
		}
	}
	for _, unwanted := range []string{"(interface Bus", "LocalBus", "OrderPlaced"} {
		if strings.Contains(spec, unwanted) {
			t.Errorf("the events tile owns %s; it must not be lifted:\n%s", unwanted, spec)
		}
	}
	second := t.TempDir()
	if err := run(Options{Spec: filepath.Join(first, "spec"), Out: second}, io.Discard); err != nil {
		t.Fatalf("regenerating from the imported spec: %v", err)
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = second
	tidy.Run()
	build := exec.Command("go", "build", "./...")
	build.Dir = second
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the twice-round-tripped project must build: %v\n%s", err, b)
	}
}

// ---- API surface ----

func TestAPISurfaceInPrompts(t *testing.T) {
	p := newRC(t)
	p.gen("get save", "yes", "memory")
	// A memory store imports nothing outside the standard library.
	pr, err := promptOut(t, filepath.Join(p.dir, "spec.sexp"), p.out, "notes.MemoryNoteStore.Get")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pr, "## API surface") {
		t.Errorf("no third-party imports, so no API section:\n%s", pr)
	}
}

func TestAPISurfaceExtraction(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": "module example.com/api\n\ngo 1.22\n",
		"lib/lib.go": `// Package lib is a dependency.
package lib

import "context"

// Client talks to the service.
type Client struct {
	Addr    string
	Timeout int
	secret  string
}

// Do sends a request.
func (c *Client) Do(ctx context.Context, path string) (string, error) { return "", nil }

func (c *Client) helper() {}

// Store persists things.
type Store interface {
	Get(ctx context.Context, id int64) (string, error)
	Put(ctx context.Context, id int64, v string) error
}

// ErrMissing is returned when nothing is there.
var ErrMissing = context.Canceled

// Mode is how the client behaves.
type Mode string

const ModeFast Mode = "fast"

func New(addr string) *Client { return nil }

func unexported() {}
`,
	})
	text, err := loadPackageAPI(dir, "example.com/api/lib")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"package lib // example.com/api/lib",
		"// Client talks to the service.",
		"type Client struct {\n\tAddr string\n\tTimeout int\n}", // exported fields only
		"func (*Client) Do(ctx context.Context, path string) (string, error)",
		"type Store interface {\n\tGet(ctx context.Context, id int64) (string, error)",
		"// ErrMissing is returned when nothing is there.",
		"type Mode string",
		"func New(addr string) *Client",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("API surface missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"secret", "helper", "unexported", "return nil"} { // no unexported, no bodies
		if strings.Contains(text, unwanted) {
			t.Errorf("API surface should not contain %q:\n%s", unwanted, text)
		}
	}
}

func TestAPISurfaceScopeAndCache(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":     "module example.com/api\n\ngo 1.22\n",
		"lib/lib.go": "package lib\n\n// Thing is a thing.\ntype Thing struct{ ID int64 }\n",
		"app/app.go": "package app\n\nimport (\n\t\"context\"\n\t\"example.com/api/lib\"\n)\n\nvar _ = context.Background\nvar _ lib.Thing\n",
	})
	r := &Report{out: dir, staged: map[string][]byte{}, unlinked: map[string]bool{}}
	// The standard library is never included; the project's own packages
	// are already context files.
	if got := apiSurface(dir, []string{"app/app.go"}, r, "example.com/api"); got != "" {
		t.Errorf("own and standard packages should be skipped:\n%s", got)
	}
	got := apiSurface(dir, []string{"app/app.go"}, r, "example.com/other")
	if !strings.Contains(got, "### example.com/api/lib") || !strings.Contains(got, "type Thing struct {\n\tID int64\n}") {
		t.Errorf("a third-party package should be included:\n%s", got)
	}
	cache := filepath.Join(dir, apiCacheDir)
	entries, err := os.ReadDir(cache)
	if err != nil || len(entries) != 1 {
		t.Fatalf("want one cache entry, got %v %v", entries, err)
	}
	// A second call reads the cache: corrupt it and the change shows.
	os.WriteFile(filepath.Join(cache, entries[0].Name()), []byte("FROM CACHE"), 0o644)
	if got := apiSurface(dir, []string{"app/app.go"}, r, "example.com/other"); !strings.Contains(got, "FROM CACHE") {
		t.Errorf("the second call should hit the cache:\n%s", got)
	}
}

func TestAPISurfaceIncludesPackagesTheIntentNames(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":     "module example.com/api\n\ngo 1.22\n\nrequire example.com/dep v1.0.0\n",
		"app/app.go": "package app\n",
	})
	// A package named in prose but not yet imported is resolved through go.mod.
	if got := mentionedPackages([]string{"Map dep.ErrNoRows to ErrNotFound."}, nil, dir); len(got) != 1 || got[0] != "example.com/dep" {
		t.Errorf("want example.com/dep, got %v", got)
	}
	if got := mentionedPackages([]string{"Return time.Now() and errors.Join(...)"}, nil, dir); len(got) != 0 {
		t.Errorf("the standard library is never included: %v", got)
	}
	if got := mentionedPackages([]string{"Map dep.ErrNoRows"}, []string{"example.com/dep"}, dir); len(got) != 0 {
		t.Errorf("already imported, so not added twice: %v", got)
	}
}

// ---- the solver's guarantees ----
//
// Four properties, each checked against a registry of synthetic
// capabilities and offers, so the tests do not depend on which real tiles
// happen to exist.

// withTestRegistry installs capabilities and offers for one test and
// restores the real registry afterwards.
func withTestRegistry(t *testing.T, caps []*Capability, os []*Offer) {
	t.Helper()
	savedCaps, savedOffers := capabilities, offers
	capabilities, offers = map[string]*Capability{}, map[string][]*Offer{}
	t.Cleanup(func() { capabilities, offers = savedCaps, savedOffers })
	for _, c := range caps {
		RegisterCapability(c)
	}
	for _, o := range os {
		RegisterOffer(o)
	}
}

func testNeed(id, capability string, reqs ...string) *Need {
	return &Need{ID: id, Capability: capability, Requirements: reqs}
}

func noShape(*Need) EntityShape { return EntityShape{} }

func testCtx() *Ctx {
	return &Ctx{Cfg: DefaultConfig(), Policy: DefaultPolicy(), Lock: map[string]LockEntry{}}
}

// brute enumerates every legal covering of a need and returns the lowest
// total cost, independently of solve.
func brute(t *testing.T, n *Need, c *Ctx) (int, bool) {
	t.Helper()
	best, found := 1<<30, false
	for _, o := range offersOf(n.Capability) {
		if offerIllegalFor(o, n.Requirements) != "" {
			continue
		}
		if _, avoided := c.Policy.Avoid[o.Tile]; avoided {
			continue
		}
		want := n.Want
		if want == "" {
			want = domainForm
		}
		_, chain, ok := convert(offerForm(o), want, EntityShape{}, c.Policy)
		if !ok {
			continue
		}
		own, _ := c.Policy.score(o.Cost)
		total, legal := own+chain, true
		if o.Children != nil {
			for _, kid := range o.Children(n) {
				kidCost, kidOK := brute(t, kid, c)
				if !kidOK {
					legal = false
					break
				}
				total += kidCost
			}
		}
		if legal && total < best {
			best, found = total, true
		}
	}
	return best, found
}

// TestSolverOptimality: over a registry with nested needs, solve's answer
// equals the true minimum found by enumerating every legal covering. This
// is what makes "cheapest legal covering" a claim rather than a hope.
func TestSolverOptimality(t *testing.T) {
	// A "service" needs a store and a cache; each has several offers with
	// different costs, so the cheapest service depends on its children.
	withTestRegistry(t,
		[]*Capability{
			{Name: "service", Requirements: []string{"durable"}},
			{Name: "store", Requirements: []string{"durable"}},
			{Name: "cache"},
		},
		[]*Offer{
			// Two services: "thin" is cheap itself but needs an expensive
			// store; "fat" costs more but needs only a cache. Greedy on own
			// cost would pick thin; the total says otherwise.
			{Tile: "thin-service", Capability: "service", Cost: Cost{{Dim: "llm-work", Value: 1}},
				Children: func(n *Need) []*Need {
					return []*Need{testNeed(n.ID+".store", "store", n.Requirements...)}
				}},
			{Tile: "fat-service", Capability: "service", Cost: Cost{{Dim: "llm-work", Value: 3}},
				Children: func(n *Need) []*Need { return []*Need{testNeed(n.ID+".cache", "cache")} }},
			{Tile: "cheap-store", Capability: "store", Cost: Cost{{Dim: "llm-work", Value: 2}},
				IllegalFor: map[string]string{"durable": "in memory"}},
			{Tile: "durable-store", Capability: "store", Cost: Cost{{Dim: "llm-work", Value: 20}}},
			{Tile: "small-cache", Capability: "cache", Cost: Cost{{Dim: "llm-work", Value: 1}}},
		})
	c := testCtx()

	// Without (durable): thin 1*4 + cheap-store 2*4 = 12 beats fat 3*4 + cache 1*4 = 16.
	cov, err := solve(testNeed("a.Svc", "service"), c, noShape)
	if err != nil {
		t.Fatal(err)
	}
	if cov.Offer.Tile != "thin-service" || cov.Score != 12 {
		t.Errorf("want thin-service at 12, got %s at %d", cov.Offer.Tile, cov.Score)
	}
	// With (durable): thin must use durable-store, 4 + 80 = 84, so fat (16) wins.
	cov, err = solve(testNeed("a.Svc", "service", "durable"), c, noShape)
	if err != nil {
		t.Fatal(err)
	}
	if cov.Offer.Tile != "fat-service" || cov.Score != 16 {
		t.Errorf("a child's cost must decide the parent: got %s at %d", cov.Offer.Tile, cov.Score)
	}
	if len(cov.Children) != 1 || cov.Children[0].Offer.Tile != "small-cache" {
		t.Errorf("the covering should include its children: %+v", cov.Children)
	}
	// And in general, solve equals brute force.
	for _, reqs := range [][]string{nil, {"durable"}} {
		n := testNeed("a.Svc", "service", reqs...)
		cov, err := solve(n, c, noShape)
		want, ok := brute(t, n, c)
		if err != nil || !ok {
			t.Fatalf("reqs %v: %v", reqs, err)
		}
		if cov.Score != want {
			t.Errorf("reqs %v: solve gave %d, the cheapest legal covering is %d", reqs, cov.Score, want)
		}
	}
}

// TestSolverDeterminism: the same inputs always give the same covering,
// whatever order maps happen to iterate in.
func TestSolverDeterminism(t *testing.T) {
	withTestRegistry(t,
		[]*Capability{{Name: "thing", Requirements: []string{"durable"}}},
		[]*Offer{
			{Tile: "b-tile", Capability: "thing", Cost: Cost{{Dim: "llm-work", Value: 2}}},
			{Tile: "a-tile", Capability: "thing", Cost: Cost{{Dim: "llm-work", Value: 2}}}, // a tie, on purpose
			{Tile: "c-tile", Capability: "thing", Cost: Cost{{Dim: "llm-work", Value: 5}}},
		})
	c := testCtx()
	first := ""
	for i := 0; i < 50; i++ {
		cov, err := solve(testNeed("p.T", "thing"), c, noShape)
		if err != nil {
			t.Fatal(err)
		}
		got := cov.Offer.Tile + " " + strings.Join(tileNamesOf(cov), ",")
		if i == 0 {
			first = got
		} else if got != first {
			t.Fatalf("run %d differs: %q vs %q", i, got, first)
		}
	}
	if !strings.HasPrefix(first, "a-tile ") {
		t.Errorf("ties must break by tile name, got %q", first)
	}
}

// TestSolverLegalityBeforeCost: an illegal offer is never scored, however
// cheap it is, and the reason is reported.
func TestSolverLegalityBeforeCost(t *testing.T) {
	withTestRegistry(t,
		[]*Capability{{Name: "thing", Requirements: []string{"durable"}}},
		[]*Offer{
			{Tile: "free-but-illegal", Capability: "thing", Cost: Cost{{Dim: "llm-work", Value: 0}},
				IllegalFor: map[string]string{"durable": "it forgets"}},
			{Tile: "costly-but-legal", Capability: "thing", Cost: Cost{{Dim: "llm-work", Value: 9}}},
		})
	c := testCtx()
	cov, err := solve(testNeed("p.T", "thing", "durable"), c, noShape)
	if err != nil {
		t.Fatal(err)
	}
	if cov.Offer.Tile != "costly-but-legal" {
		t.Errorf("a cheaper illegal tile must not win: %s", cov.Offer.Tile)
	}
	if len(cov.Ranked) != 1 || len(cov.Illegal) != 1 || cov.Illegal[0].Reason != "it forgets" {
		t.Errorf("the illegal tile should be reported with its reason: %+v", cov.Illegal)
	}
	// Nothing legal at all is an error naming every rejection.
	withTestRegistry(t,
		[]*Capability{{Name: "thing", Requirements: []string{"durable"}}},
		[]*Offer{{Tile: "only", Capability: "thing", Cost: Cost{{Dim: "llm-work", Value: 0}},
			IllegalFor: map[string]string{"durable": "it forgets"}}})
	if _, err := solve(testNeed("p.T", "thing", "durable"), c, noShape); err == nil ||
		!strings.Contains(err.Error(), "no tile can cover p.T (thing)") || !strings.Contains(err.Error(), "only: it forgets") {
		t.Errorf("want an error naming the rejection, got %v", err)
	}
}

// TestSolverTotality: the real registry always has an offer for every
// capability, so no spec can ask for something nothing provides.
func TestSolverTotality(t *testing.T) {
	if err := checkRegistry(); err != nil {
		t.Fatal(err)
	}
	for _, name := range capabilityNames() {
		if len(offersOf(name)) == 0 {
			t.Errorf("capability %q has no offers", name)
		}
	}
	for _, c := range capabilities {
		for _, r := range c.Requirements {
			if !contains(knownRequirements(), r) {
				t.Errorf("requirement %q of %q is not in the shared vocabulary", r, c.Name)
			}
		}
	}
}

// ---- event-bus transports compete ----

func TestEventBusTransportsCompete(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, `(project p (module example.com/p) (go 1.22)
  (require (nats github.com/nats-io/nats.go v1.37.0))
  (package local (events (event Ping (field ID int64))))
  (package fleet (events (cross-process) (event JobStarted (field JobID int64)))))`, "")
	var out strings.Builder
	if err := explainCmd([]string{sp}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"fleet.Bus   (event-bus)   needs: cross-process   chosen by: auto\n  chosen  nats-bus",
		"illegal local-bus       an in-process bus only reaches handlers in this program",
		"local.Bus   (event-bus)   needs: none   chosen by: auto\n  chosen  local-bus       score 1",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("explain missing %q:\n%s", want, out.String())
		}
	}
	o := filepath.Join(dir, "out")
	if err := run(Options{Spec: sp, Out: o}, io.Discard); err != nil {
		t.Fatal(err)
	}
	// local-bus generates complete code; nats-bus scaffolds holes.
	if gen := read(t, o, "local/events_gen.go"); !strings.Contains(gen, "type LocalBus struct") || strings.Contains(gen, holePrefix) {
		t.Errorf("local-bus should generate complete code:\n%s", gen)
	}
	if _, err := os.Stat(filepath.Join(o, "local", "local_bus.go")); err == nil {
		t.Error("a complete transport needs no scaffolded file")
	}
	impl := read(t, o, "fleet/nats_bus.go")
	for _, want := range []string{"type NatsBus struct {\n\tconn    *nats.Conn", "func NewNatsBus(conn *nats.Conn, subject string) *NatsBus",
		`panic("tilegen:hole fleet.NatsBus.PublishJobStarted")`} {
		if !strings.Contains(impl, want) {
			t.Errorf("nats_bus.go missing %q:\n%s", want, impl)
		}
	}
	if gen := read(t, o, "fleet/events_gen.go"); !strings.Contains(gen, "var _ Bus = (*NatsBus)(nil)") {
		t.Error("the generated file should assert the transport satisfies Bus")
	}
	if tasks := read(t, o, "tilegen.tasks.json"); !strings.Contains(tasks, `publish it on s.subject+\".JobStarted\"`) {
		t.Errorf("nats tasks should carry its hints:\n%s", tasks)
	}
	if mod := read(t, o, "go.mod"); !strings.Contains(mod, "github.com/nats-io/nats.go v1.37.0") {
		t.Error("the transport's module should be required")
	}
	if lock := read(t, o, "tilegen.lock"); !strings.Contains(lock, "(tile fleet.Bus nats-bus)") || !strings.Contains(lock, "(tile local.Bus local-bus)") {
		t.Errorf("both buses should be pinned:\n%s", lock)
	}
}

func TestEventBusRequirementValidation(t *testing.T) {
	for form, want := range map[string]string{
		`(events (durabel) (event E))`:  "(did you mean durable?)",
		`(events (nonsense) (event E))`: "event-bus does not take the requirement (nonsense)",
	} {
		err := validateSrc(t, okPrefix+form+`))`, false)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", form, want, err)
		}
	}
}

// ---- the plan graph ----

func planGraphOf(t *testing.T, spec, out string) (*PlanGraph, [][]string) {
	t.Helper()
	cp, err := compile(Options{Spec: spec, Out: out}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Emit(cp.nodes, cp.o.Out, cp.c, false)
	if err != nil {
		t.Fatal(err)
	}
	g := buildPlanGraph(r, cp.c)
	levels, err := g.Levels()
	if err != nil {
		t.Fatalf("the plan should be acyclic: %v", err)
	}
	return g, levels
}

// levelOf returns which level a node landed in.
func levelOf(levels [][]string, id string) int {
	for i, level := range levels {
		for _, n := range level {
			if n == id {
				return i
			}
		}
	}
	return -1
}

// TestPlanGraphOrdersGeneratedWork: a postgres store's tasks must come
// after sqlc has run, a memory store's need not. Every edge is inferred.
func TestPlanGraphOrdersGeneratedWork(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, `(project p (module example.com/p) (go 1.22)
  (package orders (entity Order (field ID int64) (store get save (durable))))
  (package sessions (entity Session (field ID string) (store get))))`, "")
	g, levels := planGraphOf(t, sp, filepath.Join(dir, "out"))

	sqlc := levelOf(levels, toolNode("sqlc generate"))
	dbPkg := levelOf(levels, "dir:internal/db")
	pgTask := levelOf(levels, taskNode("orders.PostgresOrderStore.Get"))
	memTask := levelOf(levels, taskNode("sessions.MemorySessionStore.Get"))
	schema := levelOf(levels, fileNode("db/schema.sql"))
	for name, lvl := range map[string]int{"sqlc": sqlc, "internal/db": dbPkg, "postgres task": pgTask, "memory task": memTask, "schema": schema} {
		if lvl < 0 {
			t.Fatalf("%s is missing from the plan", name)
		}
	}
	if !(schema < sqlc && sqlc < dbPkg && dbPkg < pgTask) {
		t.Errorf("want schema < sqlc < internal/db < postgres task, got %d %d %d %d", schema, sqlc, dbPkg, pgTask)
	}
	if memTask > sqlc || memTask >= dbPkg {
		t.Errorf("a memory store's task needs nothing generated, so it must not wait for sqlc's output: task %d, sqlc %d, internal/db %d", memTask, sqlc, dbPkg)
	}
	// Files are credited to the tile responsible for them.
	if n := g.Nodes[fileNode("db/schema.sql")]; n == nil || n.By != "postgres-sqlc" {
		t.Errorf("schema.sql should be credited to postgres-sqlc, got %+v", n)
	}
	if n := g.Nodes[fileNode("sessions/memory_session_store.go")]; n == nil || n.By != "memory" {
		t.Errorf("the memory store file should be credited to memory, got %+v", n)
	}
	// The solver's coverings are in the graph too.
	if n := g.Nodes["need:orders.OrderStore"]; n == nil || !strings.Contains(n.Note, "store, chosen by auto") {
		t.Errorf("the covering should be shown: %+v", n)
	}
}

func TestPlanGraphDot(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, `(project p (module example.com/p) (go 1.22)
  (package a (entity E (field ID int64) (store get))))`, "")
	g, _ := planGraphOf(t, sp, filepath.Join(dir, "out"))
	dot := g.Dot()
	if !strings.HasPrefix(dot, "digraph tilegen {") || !strings.HasSuffix(dot, "}\n") {
		t.Fatalf("not a DOT document:\n%s", dot)
	}
	if !strings.Contains(dot, `"file:a/a_gen.go"`) || !strings.Contains(dot, " -> ") {
		t.Errorf("DOT should have nodes and edges:\n%s", dot)
	}
	// Every edge names nodes the document declares.
	for _, line := range strings.Split(dot, "\n") {
		from, to, isEdge := strings.Cut(line, " -> ")
		if !isEdge {
			continue
		}
		for _, id := range []string{strings.TrimSpace(from), strings.TrimSuffix(strings.TrimSpace(to), ";")} {
			if !strings.Contains(dot, id+" [label=") {
				t.Errorf("edge names an undeclared node %s", id)
			}
		}
	}
}

func TestPlanGraphCycleIsReported(t *testing.T) {
	g := newPlanGraph()
	for _, id := range []string{"a", "b", "c", "loose"} {
		g.add(&PlanNode{ID: id, Kind: "file", Name: id})
	}
	g.dep("a", "b")
	g.dep("b", "c")
	g.dep("c", "a") // a cycle
	levels, err := g.Levels()
	if err == nil || !strings.Contains(err.Error(), "dependency cycle among 3 node(s): a, b, c") {
		t.Fatalf("want a cycle naming its nodes, got %v", err)
	}
	if len(levels) != 1 || levels[0][0] != "loose" {
		t.Errorf("nodes outside the cycle should still be ordered: %v", levels)
	}
}

func TestPlanGraphIsDeterministicAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, `(project p (module example.com/p) (go 1.22)
  (package a (entity E (field ID int64) (store get save (durable))) (events (event Happened (field ID int64)))))`, "")
	out := filepath.Join(dir, "out")
	first := ""
	for i := 0; i < 10; i++ {
		g, levels := planGraphOf(t, sp, out)
		got := g.Dot() + fmt.Sprint(levels)
		if i == 0 {
			first = got
		} else if got != first {
			t.Fatal("the plan graph must be deterministic")
		}
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("tilegen plan must write nothing")
	}
}

// ---- -json on the read-only commands ----

func jsonOf[T any](t *testing.T, run func(w io.Writer) error) T {
	t.Helper()
	var buf bytes.Buffer
	err := run(&buf)
	var out T
	if jsonErr := json.Unmarshal(buf.Bytes(), &out); jsonErr != nil {
		t.Fatalf("not valid JSON (%v): %v\n%s", err, jsonErr, buf.String())
	}
	return out
}

func TestExplainJSON(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, autoSpec("auto"), "")
	got := jsonOf[ExplainJSON](t, func(w io.Writer) error {
		return explainCmd([]string{"-json", sp}, w, io.Discard)
	})
	if got.Policy.Weights["llm-work"] != 4 {
		t.Errorf("the policy should be reported: %+v", got.Policy)
	}
	var orders *CoverageJSON
	for i := range got.Coverage {
		if got.Coverage[i].Need == "orders.OrderStore" {
			orders = &got.Coverage[i]
		}
	}
	if orders == nil {
		t.Fatalf("every need should appear: %+v", got.Coverage)
	}
	if orders.Chosen != "postgres-sqlc" || orders.Capability != "store" || orders.ChosenBy != "auto" ||
		len(orders.Requirements) != 1 || orders.Requirements[0] != "durable" {
		t.Errorf("coverage: %+v", orders)
	}
	if len(orders.Illegal) != 1 || orders.Illegal[0].Tile != "memory" || orders.Illegal[0].Reason == "" {
		t.Errorf("rejections should carry their reasons: %+v", orders.Illegal)
	}
	var sqlc *CandidateJSON
	for i := range orders.Candidates {
		if orders.Candidates[i].Tile == "postgres-sqlc" {
			sqlc = &orders.Candidates[i]
		}
	}
	if sqlc == nil || sqlc.Own != 22 || len(sqlc.Chain) != 1 || sqlc.Chain[0].Rule != "row-mapper" {
		t.Errorf("the chain should be broken out: %+v", sqlc)
	}
	if !strings.Contains(orders.Position, "spec.sexp:") {
		t.Errorf("the need should carry its position: %q", orders.Position)
	}
}

// TestJSONAgreesWithText: both renderings come from one computation, so a
// disagreement means the two have drifted.
func TestJSONAgreesWithText(t *testing.T) {
	p := newRC(t)
	p.gen("get save", "yes", "memory")
	sp := filepath.Join(p.dir, "spec.sexp")
	writeSpec(t, p.dir, fmt.Sprintf(rcSpec, "get save count", "yes", "memory"), "") // now out of date

	res := jsonOf[CheckJSON](t, func(w io.Writer) error {
		saved := os.Stdout
		r, wr, _ := os.Pipe()
		os.Stdout = wr
		err := run(Options{Spec: sp, Out: p.out, Check: true, AllowHoles: true, JSON: true}, io.Discard)
		wr.Close()
		os.Stdout = saved
		io.Copy(w, r)
		return err
	})
	text, textErr := checkRun(t, sp, p.out, true)

	if res.OK {
		t.Fatal("a changed spec should not be ok")
	}
	for _, f := range res.Stale {
		if !strings.Contains(text, "stale    "+f) {
			t.Errorf("JSON reports %s stale, the text report does not:\n%s", f, text)
		}
	}
	if len(res.Stubs) == 0 || !strings.Contains(res.Stubs[0], "Count") {
		t.Errorf("the appended stub should be listed: %v", res.Stubs)
	}
	if got := strings.Count(text, "stale    "); got != len(res.Stale) {
		t.Errorf("text has %d stale lines, JSON has %d", got, len(res.Stale))
	}
	if textErr == nil || !strings.Contains(textErr.Error(), strings.Join(res.Problems, ", ")) {
		t.Errorf("both should report the same problems: JSON %v, text %v", res.Problems, textErr)
	}
}

func TestPlanAndTilesJSON(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, `(project p (module example.com/p) (go 1.22)
  (package orders (entity Order (field ID int64) (store get (durable)))))`, "")
	plan := jsonOf[PlanJSON](t, func(w io.Writer) error {
		return planCmd([]string{"-json", "-out", filepath.Join(dir, "out"), sp}, w, io.Discard)
	})
	if len(plan.Levels) < 3 || len(plan.Cycle) != 0 {
		t.Fatalf("want several levels and no cycle: %d levels, cycle %v", len(plan.Levels), plan.Cycle)
	}
	kinds := map[string]bool{}
	var sqlcLevel, taskLevel = -1, -1
	for i, level := range plan.Levels {
		for _, n := range level {
			kinds[n.Kind] = true
			if n.ID == toolNode("sqlc generate") {
				sqlcLevel = i
			}
			if n.ID == taskNode("orders.PostgresOrderStore.Get") {
				taskLevel = i
			}
		}
	}
	for _, k := range []string{"file", "tool", "task", "need", "tile"} {
		if !kinds[k] {
			t.Errorf("no %s node in the plan", k)
		}
	}
	if sqlcLevel < 0 || taskLevel <= sqlcLevel {
		t.Errorf("the postgres task must come after sqlc: sqlc %d, task %d", sqlcLevel, taskLevel)
	}

	tiles := jsonOf[TilesJSON](t, func(w io.Writer) error { return tilesCmd([]string{"-json"}, w) })
	if len(tiles.Tiles) < 20 {
		t.Errorf("want the whole registry, got %d tiles", len(tiles.Tiles))
	}
	var store *CapabilityJSON
	for i := range tiles.Capabilities {
		if tiles.Capabilities[i].Name == "store" {
			store = &tiles.Capabilities[i]
		}
	}
	if store == nil || len(store.OfferedBy) != 3 || len(store.Requirements) != 1 {
		t.Fatalf("capabilities should list their offers and requirements: %+v", store)
	}
	for _, tl := range tiles.Tiles {
		if tl.Name == "memory" {
			if tl.Cost["llm-work"] != 2 || tl.IllegalWhen["durable"] == "" {
				t.Errorf("the memory tile should carry its cost and legality: %+v", tl)
			}
		}
	}
}

// ---- preferences: strength and provenance ----

func TestPreferenceStrengths(t *testing.T) {
	// weak: wins ties and anything within the margin, as before.
	weak := policyExplain(t, `(policy (prefer postgres-sqlc (strength weak)) (margin 20))`)
	if !strings.Contains(weak, "chosen  postgres-sqlc") || !strings.Contains(weak, "preferred, and within the margin of postgres-pgx") {
		t.Errorf("weak:\n%s", weak)
	}
	// strong: wins however much cheaper the alternative is, and says so.
	strong := policyExplain(t, `(policy (prefer postgres-sqlc (strength strong) (source team "we know sqlc")))`)
	if !strings.Contains(strong, "chosen  postgres-sqlc") ||
		!strings.Contains(strong, "strongly preferred; cost alone would have picked postgres-pgx") ||
		!strings.Contains(strong, "[source: team, we know sqlc]") {
		t.Errorf("strong:\n%s", strong)
	}
	// required: a constraint, so everything else is illegal with the source
	// as the reason. Cost never enters into it.
	req := policyExplain(t, `(policy (prefer postgres-sqlc (strength required) (source client "contract §4")))`)
	if !strings.Contains(req, "chosen  postgres-sqlc") ||
		!strings.Contains(req, "illegal postgres-pgx    the policy requires postgres-sqlc (source: client, contract §4)") ||
		!strings.Contains(req, "illegal memory          the policy requires postgres-sqlc") {
		t.Errorf("required:\n%s", req)
	}
	// (prefer X) with no strength still means weak.
	if got := policyExplain(t, `(policy (prefer postgres-sqlc) (margin 20))`); !strings.Contains(got, "chosen  postgres-sqlc") {
		t.Errorf("a bare prefer should still be weak:\n%s", got)
	}
}

func TestRequiredPreferenceCanBeImpossible(t *testing.T) {
	// Requiring a tile that is illegal for this need fails, rather than
	// silently falling back: the client asked for something impossible.
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, polSpec, "")
	pf := filepath.Join(dir, "policy.sexp")
	os.WriteFile(pf, []byte(`(policy (prefer memory (strength required) (source client "no databases")))`), 0o644)
	err := run(Options{Spec: sp, Out: filepath.Join(dir, "out"), PolicyFile: pf}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "no tile can cover orders.OrderStore") ||
		!strings.Contains(err.Error(), "an in-memory map loses its data on restart") {
		t.Fatalf("want a clear failure naming both reasons, got %v", err)
	}
}

func TestPreferenceValidation(t *testing.T) {
	for src, want := range map[string]string{
		`(policy (prefer x (strength maybe)))`: "strength must be one of required, strong, weak",
		`(policy (prefer x (strength strng)))`: "(did you mean strong?)",
		`(policy (prefer x (surce client)))`:   "(did you mean source?)",
		`(policy (prefer (strength strong)))`:  "prefer needs at least one tile name",
	} {
		n, _ := Parse("policy.sexp", src)
		if _, err := ParsePolicy(n[0]); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", src, want, err)
		}
	}
	n, _ := Parse("policy.sexp", `(policy (prefer a b (strength strong) (source client "why")))`)
	p, err := ParsePolicy(n[0])
	if err != nil || len(p.Prefer) != 2 || p.Prefer["a"].Strength != "strong" || p.Prefer["b"].Source != "client" || p.Prefer["a"].Why != "why" {
		t.Fatalf("several tiles should share one preference: %+v, %v", p.Prefer, err)
	}
}

// TestCostProvenance: a number a tile declares says where it came from, and
// explain and -json both report it, so an estimate is not mistaken for a
// measurement.
func TestCostProvenance(t *testing.T) {
	got := policyExplain(t, "")
	if !strings.Contains(got, "cost    postgres-pgx llm-work: 7 (derived: the LLM writes every query and scan by hand)") {
		t.Errorf("explain should show provenance:\n%s", got)
	}
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, polSpec, "")
	out := jsonOf[ExplainJSON](t, func(w io.Writer) error {
		return explainCmd([]string{"-json", sp}, w, io.Discard)
	})
	var found bool
	for _, cov := range out.Coverage {
		for _, cand := range cov.Candidates {
			for _, term := range cand.Cost {
				if cand.Tile == "postgres-pgx" && term.Dimension == "llm-work" {
					found = true
					if term.Value != 7 || term.Weight != 4 || term.Source != "derived" || term.Note == "" {
						t.Errorf("the JSON term should carry value, weight and provenance: %+v", term)
					}
				}
			}
		}
	}
	if !found {
		t.Error("costs should appear in the JSON")
	}
	// A registry entry shows it too.
	var tiles strings.Builder
	tilesCmd([]string{"-sexp", "postgres-pgx"}, &tiles)
	if !strings.Contains(tiles.String(), `(llm-work 7 (source derived "the LLM writes every query and scan by hand"))`) {
		t.Errorf("the registry should carry provenance:\n%s", tiles.String())
	}
}

// TestWorkspaceOnlySpec: up, status and down need only the workspace, so a
// spec folder with no project yet still opens its session.
func TestWorkspaceOnlySpec(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "spec"), 0o755)
	os.WriteFile(filepath.Join(dir, "spec", "workspace.sexp"),
		[]byte("(workspace (name demo) (out ..) (tmux (session demo (window work (dir .)))))"), 0o644)
	ws, err := loadWorkspace(filepath.Join(dir, "spec"))
	if err != nil {
		t.Fatalf("a workspace-only spec should load: %v", err)
	}
	if ws.Session != "demo" || ws.Out != canonical(dir) && ws.Out != dir {
		t.Errorf("workspace: %+v", ws)
	}
	// A session needs no git repository; only worktrees do.
	r := &scriptRunner{answers: map[string]string{}}
	if err := Up(ws, r, io.Discard, true); err != nil {
		t.Fatalf("opening a session should not need a repository: %v", err)
	}
	if len(r.did) == 0 || !strings.HasPrefix(r.did[0], "tmux new-session") {
		t.Errorf("want the session created, got %q", r.did)
	}
	// Generation still says what is missing, and points at what works.
	err = run(Options{Spec: filepath.Join(dir, "spec"), Out: filepath.Join(dir, "out")}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "no (project NAME ...) form") ||
		!strings.Contains(err.Error(), "`tilegen up` still works") {
		t.Fatalf("want a helpful generation error, got %v", err)
	}
}

// ---- panes ----

func TestWindowPanes(t *testing.T) {
	ws, err := parseWS(t, `(workspace (out ..)
	  (worktrees (worktree feat))
	  (tmux (session s
	    (window demo (dir .)
	      (split horizontal)
	      (pane (run "left"))
	      (pane (dir sub) (run "right")))
	    (window stacked (dir .)
	      (split vertical)
	      (pane (run "top"))
	      (pane (worktree feat) (run "bottom")))
	    (window withrun (dir .) (run "first")
	      (pane (run "second"))))))`)
	if err != nil {
		t.Fatal(err)
	}
	demo := ws.Windows[0]
	if demo.Split != "horizontal" || len(demo.Panes) != 2 || demo.Panes[1].Dir != "/base/sub" || demo.Panes[1].Run != "right" {
		t.Fatalf("panes: %+v", demo)
	}
	if ws.Windows[1].Panes[1].Dir != "/base.wt/feat" {
		t.Errorf("a pane should take a worktree: %+v", ws.Windows[1].Panes[1])
	}

	r := &scriptRunner{answers: map[string]string{
		"git worktree list": "worktree /base\n\nworktree /base.wt/feat", // both already there
		"tmux list-panes":   "%0",
		"tmux split-window": "%1", // the id of the pane just made
	}}
	if err := Up(ws, r, io.Discard, true); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"tmux new-session -d -s s -n demo -c /base",
		"tmux send-keys -t %0 left Enter",
		"tmux send-keys -t %1 right Enter", // the pane id split-window printed
		"tmux select-layout -t =s:demo even-horizontal",
		"tmux new-window -d -t =s: -n stacked -c /base",
		"tmux send-keys -t %0 top Enter",
		"tmux send-keys -t %1 bottom Enter",
		"tmux select-layout -t =s:stacked even-vertical",
		"tmux new-window -d -t =s: -n withrun -c /base",
		"tmux send-keys -t =s:withrun first Enter", // the window's own run is pane 0
		"tmux send-keys -t %1 second Enter",
	}
	if strings.Join(r.did, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands:\n%s\nwant:\n%s", strings.Join(r.did, "\n"), strings.Join(want, "\n"))
	}
}

func TestPaneValidation(t *testing.T) {
	for src, want := range map[string]string{
		`(workspace (out ..) (tmux (session s (window a (split sideways) (pane)))))`:  "split must be horizontal (side by side) or vertical (stacked)",
		`(workspace (out ..) (tmux (session s (window a (split horizntal) (pane)))))`: "(did you mean horizontal?)",
		`(workspace (out ..) (tmux (session s (window a (pane (worktree nope))))))`:   `no worktree named "nope"`,
		`(workspace (out ..) (tmux (session s (window a (pane (runn "x"))))))`:        "(did you mean run?)",
	} {
		if _, err := parseWS(t, src); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", src, want, err)
		}
	}
	// A window without panes is unchanged.
	ws, err := parseWS(t, `(workspace (out ..) (tmux (session s (window a (dir .) (run "x")))))`)
	if err != nil || len(ws.Windows[0].Panes) != 0 || ws.Windows[0].Run != "x" {
		t.Fatalf("a plain window should not gain panes: %+v, %v", ws.Windows, err)
	}
}

// TestPanesWithRealTmux: the commands are right, and so is the result.
func TestPanesWithRealTmux(t *testing.T) {
	for _, tool := range []string{"tmux"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " not installed")
		}
	}
	sock, err := os.MkdirTemp("/tmp", "tg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { exec.Command("tmux", "kill-server").Run(); os.RemoveAll(sock) })
	t.Setenv("TMUX_TMPDIR", sock)
	t.Setenv("TMUX", "")

	root := canonical(t.TempDir())
	n, _ := Parse("ws.sexp", `(workspace (out .)
	  (tmux (session tgpanes (window split (dir .) (split vertical)
	    (pane (run "echo ONE")) (pane (run "echo TWO")) (pane (run "echo THREE"))))))`)
	ws, err := ParseWorkspace(n[0], root)
	if err != nil {
		t.Fatal(err)
	}
	if err := Up(ws, execRunner{log: io.Discard}, io.Discard, true); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("tmux", "list-panes", "-t", "=tgpanes:split", "-F", "#{pane_index}").Output()
	if err != nil || len(strings.Fields(string(out))) != 3 {
		t.Fatalf("want three panes, got %q, %v", out, err)
	}
	ids, _ := exec.Command("tmux", "list-panes", "-t", "=tgpanes:split", "-F", "#{pane_id}").Output()
	for i, id := range strings.Fields(string(ids)) {
		want := []string{"ONE", "TWO", "THREE"}[i]
		b, _ := exec.Command("tmux", "capture-pane", "-p", "-t", id).Output()
		if !strings.Contains(string(b), want) {
			t.Errorf("pane %d (%s) should have run %s:\n%s", i, id, want, b)
		}
	}
}

// ---- the http tile ----

const httpSpec = `(project p (module example.com/p) (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0))
  (package links
    (entity Link (field ID uuid.UUID) (field URL string) (store get list save delete))
    (http
      (route GET    "/links"      (list Link))
      (route GET    "/links/{id}" (get Link))
      (route POST   "/links"      (save Link))
      (route DELETE "/links/{id}" (delete Link)))))`

func TestHTTPTile(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, httpSpec, "")
	out := filepath.Join(dir, "out")
	if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	gen := read(t, out, "links/http_gen.go")
	for _, want := range []string{
		"// " + generatedMarker,
		"type Handler struct {\n\tlinks LinkStore\n}",
		"func NewHandler(links LinkStore) *Handler",
		`mux.HandleFunc("GET /links/{id}", h.GetLink)`, // method and wildcard: net/http routes it
		`mux.HandleFunc("DELETE /links/{id}", h.DeleteLink)`,
		"id, err := uuid.Parse(r.PathValue(\"id\"))", // the ID type decides the parse
		"h.statusForLink(err)",                       // errors go through the hole
		"if err := h.validateLink(&v); err != nil {", // so does validation
		"w.WriteHeader(http.StatusNoContent)",        // delete
		`"github.com/google/uuid"`,                   // the import the body needs
	} {
		if !strings.Contains(gen, want) {
			t.Errorf("http_gen.go missing %q", want)
		}
	}
	if strings.Contains(gen, holePrefix) {
		t.Error("the routing layer should have no holes")
	}
	impl := read(t, out, "links/http.go")
	for _, want := range []string{
		"func (h *Handler) validateLink(link *Link) error",
		"func (h *Handler) statusForLink(err error) int",
		`panic("tilegen:hole links.Handler.validateLink")`,
	} {
		if !strings.Contains(impl, want) {
			t.Errorf("http.go missing %q:\n%s", want, impl)
		}
	}
	tasks := read(t, out, "tilegen.tasks.json")
	if !strings.Contains(tasks, "ErrNotFound to 404") || !strings.Contains(tasks, "sent to the client with 400") {
		t.Errorf("the two judgment calls should carry their intent:\n%s", tasks)
	}
	if strings.Count(tasks, "links.Handler.") != 2 {
		t.Errorf("one pair of holes per resource, not per route:\n%s", tasks)
	}
}

// TestHTTPTileServes: the generated routing must actually route, so the
// test fills the two holes and exercises every status the tile promises.
func TestHTTPTileServes(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, httpSpec, "")
	out := filepath.Join(dir, "out")
	if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	fill := func(file string, repl map[string]string, header string) {
		p := filepath.Join(out, file)
		b, _ := os.ReadFile(p)
		s := string(b)
		for hole, body := range repl {
			s = strings.Replace(s, fmt.Sprintf("panic(%q)", holePrefix+hole), body, 1)
		}
		if header != "" {
			s = strings.Replace(s, "package links\n", "package links\n\n"+header+"\n", 1)
		}
		os.WriteFile(p, []byte(s), 0o644)
	}
	lock := "s.mu.Lock()\n\tdefer s.mu.Unlock()\n\t"
	fill("links/memory_link_store.go", map[string]string{
		"links.MemoryLinkStore.Get":    lock + "v, ok := s.m[id]\n\tif !ok {\n\t\treturn nil, ErrNotFound\n\t}\n\treturn v, nil",
		"links.MemoryLinkStore.List":   lock + "out := make([]*Link, 0, len(s.m))\n\tfor _, v := range s.m {\n\t\tout = append(out, v)\n\t}\n\treturn out, nil",
		"links.MemoryLinkStore.Save":   lock + "s.m[link.ID] = link\n\treturn nil",
		"links.MemoryLinkStore.Delete": lock + "delete(s.m, id)\n\treturn nil",
	}, "")
	fill("links/http.go", map[string]string{
		"links.Handler.validateLink":  "if link.URL == \"\" {\n\t\treturn errors.New(\"url is required\")\n\t}\n\treturn nil",
		"links.Handler.statusForLink": "if errors.Is(err, ErrNotFound) {\n\t\treturn http.StatusNotFound\n\t}\n\treturn http.StatusInternalServerError",
	}, "import (\n\t\"errors\"\n\t\"net/http\"\n)")

	test := `package links

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestServes(t *testing.T) {
	mux := NewHandler(NewMemoryLinkStore()).Routes()
	do := func(m, p, b string) int {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(m, p, strings.NewReader(b)))
		return rec.Code
	}
	id := uuid.New()
	for _, c := range []struct {
		name, method, path, body string
		want                     int
	}{
		{"save", "POST", "/links", ` + "`" + `{"id":"` + "`" + `+id.String()+` + "`" + `","url":"https://x"}` + "`" + `, 200},
		{"get", "GET", "/links/" + id.String(), "", 200},
		{"list", "GET", "/links", "", 200},
		{"missing", "GET", "/links/" + uuid.New().String(), "", http.StatusNotFound},
		{"bad id", "GET", "/links/not-a-uuid", "", http.StatusBadRequest},
		{"invalid", "POST", "/links", ` + "`" + `{"id":"` + "`" + `+uuid.New().String()+` + "`" + `","url":""}` + "`" + `, http.StatusBadRequest},
		{"unknown field", "POST", "/links", ` + "`" + `{"nope":1}` + "`" + `, http.StatusBadRequest},
		{"delete", "DELETE", "/links/" + id.String(), "", http.StatusNoContent},
		{"wrong method", "PATCH", "/links", "", http.StatusMethodNotAllowed},
		{"unknown path", "GET", "/nope", "", http.StatusNotFound},
	} {
		if got := do(c.method, c.path, c.body); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}
`
	os.WriteFile(filepath.Join(out, "links", "serves_test.go"), []byte(test), 0o644)
	for _, args := range [][]string{{"mod", "tidy"}, {"test", "./links/"}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = out
		if b, err := cmd.CombinedOutput(); err != nil {
			if args[0] == "mod" {
				t.Skipf("go mod tidy needs the module proxy: %v", err)
			}
			t.Fatalf("the generated API must serve: %v\n%s", err, b)
		}
	}
}

func TestHTTPValidation(t *testing.T) {
	base := `(project p (module example.com/p) (go %s)
  (package a (entity E (field ID int64) (store get list)) (http %s)))`
	for _, c := range []struct{ goVer, form, want string }{
		{"1.22", `(route GET "/e" (get E))`, "needs {id} in the path"},
		{"1.22", `(route GET "/e/{id}" (list E))`, "acts on the collection, so the path should not have {id}"},
		{"1.22", `(route get "/e" (list E))`, "route method must be GET"},
		{"1.22", `(route GET "e" (list E))`, "route path must start with /"},
		{"1.22", `(route GET "/e" (lst E))`, "(did you mean list?)"},
		{"1.22", `(route GET "/e" (list E)) (route GET "/e" (list E))`, "duplicate route GET /e"},
		{"1.22", ``, "needs at least one (route ...)"},
		{"1.21", `(route GET "/e" (list E))`, "needs (go 1.22) or later"},
		{"1.22", `(rout GET "/e" (list E))`, "(did you mean route?)"},
	} {
		err := validateSrc(t, fmt.Sprintf(base, c.goVer, c.form), false)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want %q, got %v", c.form, c.want, err)
		}
	}
	// A route whose entity has no store is an error at generation.
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, `(project p (module example.com/p) (go 1.22)
  (package a (struct E (field ID int64)) (http (route GET "/e" (list E)))))`, "")
	if err := run(Options{Spec: sp, Out: filepath.Join(dir, "out")}, io.Discard); err == nil ||
		!strings.Contains(err.Error(), "has no store in this package") {
		t.Errorf("want a clear error, got %v", err)
	}
}

// ---- the boundary between the layers ----

// TestGenerationIgnoresFilledHoles pins the property the whole design
// rests on: the deterministic layer produces the same scaffolding whether
// or not anything ever filled a hole. Generation must not read, react to,
// or depend on filled code. If this ever fails, the two layers have begun
// to know about each other.
func TestGenerationIgnoresFilledHoles(t *testing.T) {
	spec := `(project p (module example.com/p) (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0))
  (package links
    (entity Link (field ID uuid.UUID) (field URL string) (store get list save (durable)))
    (http (route GET "/links/{id}" (get Link)) (route POST "/links" (save Link)))
    (events (event LinkSaved (field LinkID uuid.UUID)))))`

	// Generate twice into separate trees; fill every hole in the second.
	untouched, filled := t.TempDir(), t.TempDir()
	for _, out := range []string{untouched, filled} {
		dir := t.TempDir()
		sp, _ := writeSpec(t, dir, spec, "")
		if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	fillEvery(t, filled)

	// Regenerate both. The generated files must be byte-identical.
	for _, out := range []string{untouched, filled} {
		dir := t.TempDir()
		sp, _ := writeSpec(t, dir, spec, "")
		if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"links/links_gen.go", "links/http_gen.go", "links/events_gen.go", "db/schema.sql", "db/query.sql", "sqlc.yaml", "tilegen.lock"} {
		a, b := read(t, untouched, f), read(t, filled, f)
		if a != b {
			t.Errorf("%s differs once holes are filled: the layers must stay independent\n--- untouched\n%s\n--- filled\n%s", f, a, b)
		}
	}
	// And the filled code itself is never rewritten.
	if src := read(t, filled, "links/http.go"); strings.Contains(src, holePrefix) {
		t.Error("regeneration must not restore a filled hole")
	}
}

// fillEvery replaces every hole in a tree with a trivial body, the way any
// filler would.
func fillEvery(t *testing.T, out string) {
	t.Helper()
	n := 0
	filepath.Walk(out, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		src, _ := os.ReadFile(p)
		s := string(src)
		if !strings.Contains(s, holePrefix) {
			return nil
		}
		for {
			i := strings.Index(s, `panic("`+holePrefix)
			if i < 0 {
				break
			}
			j := strings.Index(s[i:], "\")") + i + 2
			s = s[:i] + "panic(\"filled by a test\")" + s[j:]
			n++
		}
		return os.WriteFile(p, []byte(s), 0o644)
	})
	if n == 0 {
		t.Fatal("the spec should have produced holes to fill")
	}
}

func TestPromptNamesAvailableModules(t *testing.T) {
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, `(project p (module example.com/p) (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0))
  (package links
    (entity Link (field ID uuid.UUID) (store get save (durable)))
    (http (route GET "/links/{id}" (get Link)))))`, "")
	out := filepath.Join(dir, "out")
	if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	pr, err := promptOut(t, sp, out, "links.Handler.statusForLink")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"these modules, which are the only ones available:",
		"    github.com/google/uuid v1.6.0",
		"Do not import any other module.",
	} {
		if !strings.Contains(pr, want) {
			t.Errorf("prompt missing %q:\n%s", want, pr)
		}
	}
	// The tile asks for the backend's error package on a statusFor hole,
	// so the task carries it; whether its API can be read depends on the
	// module being downloaded, which is not this test's business.
	tasks := read(t, out, "tilegen.tasks.json")
	if !strings.Contains(tasks, `"api_packages"`) || !strings.Contains(tasks, "github.com/jackc/pgx/v5/pgconn") {
		t.Errorf("a statusFor task should name the driver's error package:\n%s", tasks)
	}
	if strings.Count(tasks, "pgx/v5/pgconn") != 1 {
		t.Errorf("only statusFor should ask for it:\n%s", tasks)
	}

	// And only when the store behind it has a driver: a memory-backed
	// resource has no driver errors to map.
	dir2 := t.TempDir()
	sp2, _ := writeSpec(t, dir2, `(project p (module example.com/p) (go 1.22)
  (package s
    (entity S (field ID string) (store get save))
    (http (route GET "/s/{id}" (get S)))))`, "")
	out2 := filepath.Join(dir2, "out")
	if err := run(Options{Spec: sp2, Out: out2}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if tasks := read(t, out2, "tilegen.tasks.json"); strings.Contains(tasks, "api_packages") {
		t.Errorf("a memory-backed resource needs no driver API:\n%s", tasks)
	}

	// A validate hole is about the entity, not the driver, so it gets no
	// driver API: prompts should carry what the hole needs and no more.
	pr, err = promptOut(t, sp, out, "links.Handler.validateLink")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pr, "pgconn") {
		t.Errorf("a validate hole should not carry the driver's API:\n%s", pr)
	}
}
