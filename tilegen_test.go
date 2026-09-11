package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
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
