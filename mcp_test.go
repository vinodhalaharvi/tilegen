//go:build !js

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vinodhalaharvi/weft/mcp"
	"github.com/vinodhalaharvi/weft/weft"
)

// The specs the MCP tests generate from: every single-file example, with
// the config or policy that ships beside it.
type mcpCase struct{ name, spec, config, policy string }

func mcpCases(t *testing.T) []mcpCase {
	t.Helper()
	file := func(rel string) string {
		if rel == "" {
			return ""
		}
		b, err := os.ReadFile(filepath.Join("examples", rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	var out []mcpCase
	for _, c := range []struct{ name, spec, config, policy string }{
		{"api", "api/spec.sexp", "", ""},
		{"api-grpc", "api/spec.sexp", "api/config.grpc.sexp", ""},
		{"app", "app/spec.sexp", "", ""},
		{"auth", "auth/spec.sexp", "", ""},
		{"auto", "auto/spec.sexp", "", ""},
		{"auto-policy", "auto/spec.sexp", "", "auto/policy.sexp"},
		{"outbox", "outbox/spec.sexp", "", ""},
		{"secrets", "secrets/spec.sexp", "", ""},
		{"secrets-vault", "secrets/spec.sexp", "secrets/config.vault.sexp", ""},
		{"shop", "shop/spec.sexp", "", ""},
		{"shop-postgres", "shop/spec.sexp", "shop/config.postgres.sexp", ""},
	} {
		out = append(out, mcpCase{c.name, file(c.spec), file(c.config), file(c.policy)})
	}
	return out
}

func (c mcpCase) in() ProjectIn {
	return ProjectIn{Name: c.name, SpecIn: SpecIn{Spec: c.spec, Config: c.config, Policy: c.policy}}
}

// viaCLI generates the case the way the command line does: files on disk,
// then run.
func viaCLI(t *testing.T, c mcpCase, out string) {
	t.Helper()
	dir := t.TempDir()
	put := func(name, text string) string {
		if text == "" {
			return ""
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	o := Options{Spec: put("spec.sexp", c.spec), Config: put("config.sexp", c.config), PolicyFile: put("policy.sexp", c.policy), Out: out}
	if err := run(o, io.Discard); err != nil {
		t.Fatalf("%s: the command line failed: %v", c.name, err)
	}
}

// A project made through the arrows is, file for file and byte for byte,
// the project the command line makes.
func TestMCPGenerateIsTheCommandLine(t *testing.T) {
	for _, c := range mcpCases(t) {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			p, err := generate(root)(context.Background(), c.in())
			if err != nil {
				t.Fatal(err)
			}
			cli := filepath.Join(t.TempDir(), c.name)
			viaCLI(t, c, cli)
			got, want := tree(t, p.Dir), tree(t, cli)
			if len(want) == 0 {
				t.Fatal("the command line wrote nothing")
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("the MCP project differs from the command line's: %d files against %d", len(got), len(want))
				for f, w := range want {
					if got[f] != w {
						t.Errorf("  %s differs", f)
					}
				}
			}
			if p.Written != len(got) {
				t.Errorf("reported %d files written; %d are on disk", p.Written, len(got))
			}
			if p.Dir != filepath.Join(root, c.name) {
				t.Errorf("wrote to %s, outside %s", p.Dir, root)
			}
		})
	}
}

// Generating twice is generating once.
func TestMCPGenerateIsIdempotent(t *testing.T) {
	root := t.TempDir()
	c := mcpCases(t)[0]
	g := generate(root)
	first, err := g(context.Background(), c.in())
	if err != nil {
		t.Fatal(err)
	}
	before := tree(t, first.Dir)
	second, err := g(context.Background(), c.in())
	if err != nil {
		t.Fatal(err)
	}
	if second.Written != 0 {
		t.Errorf("the second run wrote %d files; it should write none", second.Written)
	}
	if !reflect.DeepEqual(before, tree(t, second.Dir)) {
		t.Error("the second run changed the project")
	}
	// The lock the first run wrote now holds every choice in place.
	for i, d := range second.Decisions {
		f := first.Decisions[i]
		if d.Need != f.Need || d.Tile != f.Tile {
			t.Errorf("%s: %s the first time, %s the second", d.Need, f.Tile, d.Tile)
		}
		if d.ChosenBy != "lock" {
			t.Errorf("%s: the second run's choice is by %q, not the lock", d.Need, d.ChosenBy)
		}
	}
}

// Code a person wrote in a generated project survives generating again.
func TestMCPGenerateKeepsYourCode(t *testing.T) {
	root := t.TempDir()
	c := mcpCases(t)[0]
	g := generate(root)
	p, err := g(context.Background(), c.in())
	if err != nil {
		t.Fatal(err)
	}
	again, err := g(context.Background(), c.in())
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Kept) == 0 {
		t.Fatal("this example has no file that is kept; the test needs one")
	}
	mine := filepath.Join(p.Dir, again.Kept[0])
	b, err := os.ReadFile(mine)
	if err != nil {
		t.Fatal(err)
	}
	edited := string(b) + "\n// mine\n"
	if err := os.WriteFile(mine, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g(context.Background(), c.in()); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(mine); string(after) != edited {
		t.Errorf("%s was overwritten", again.Kept[0])
	}
}

// The laws the composition relies on, checked on tilegen's own stages:
// however the pipeline is grouped, and with identities put anywhere in
// it, the project is the same.
func TestMCPArrowLaws(t *testing.T) {
	var (
		rd weft.Arrow[placed, parsed]  = readText
		dc weft.Arrow[parsed, decided] = decideTiles
		ly weft.Arrow[decided, laid]   = layFiles
		wr weft.Arrow[laid, laid]      = writeFiles
	)
	type build func(root string) weft.Arrow[ProjectIn, Project]
	shapes := map[string]build{
		"left-nested": func(root string) weft.Arrow[ProjectIn, Project] {
			return weft.Map(weft.Compose(weft.Compose(weft.Compose(weft.Compose(place(root), rd), dc), ly), wr), report)
		},
		"right-nested": func(root string) weft.Arrow[ProjectIn, Project] {
			return weft.Map(weft.Compose(place(root), weft.Compose(rd, weft.Compose(dc, weft.Compose(ly, wr)))), report)
		},
		"split in the middle": func(root string) weft.Arrow[ProjectIn, Project] {
			return weft.Map(weft.Compose(weft.Compose(place(root), rd), weft.Compose(dc, weft.Compose(ly, wr))), report)
		},
		"identities throughout": func(root string) weft.Arrow[ProjectIn, Project] {
			return weft.Map(weft.Pipe6(weft.Id[ProjectIn](), place(root), weft.Compose(rd, weft.Id[parsed]()), weft.Compose(weft.Id[parsed](), dc), ly, weft.Compose(wr, weft.Id[laid]())), report)
		},
		"map fused": func(root string) weft.Arrow[ProjectIn, Project] {
			return weft.Compose(weft.Pipe5(place(root), rd, dc, ly, wr), weft.Pure(report))
		},
	}
	for _, c := range mcpCases(t) {
		ref := t.TempDir()
		want, err := generate(ref)(context.Background(), c.in())
		if err != nil {
			t.Fatal(err)
		}
		files := tree(t, want.Dir)
		for name, shape := range shapes {
			root := t.TempDir()
			got, err := shape(root)(context.Background(), c.in())
			if err != nil {
				t.Fatalf("%s, %s: %v", c.name, name, err)
			}
			if !reflect.DeepEqual(tree(t, got.Dir), files) {
				t.Errorf("%s: grouped %s, the files differ", c.name, name)
			}
			got.Dir, want.Dir = "", ""
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s: grouped %s, the report differs:\n got %+v\nwant %+v", c.name, name, got, want)
			}
		}
	}
}

// The stages before writeFiles have no effect: a failure in any of them
// leaves nothing on disk.
func TestMCPNothingIsWrittenUntilTheLastStage(t *testing.T) {
	for name, spec := range map[string]string{
		"does not parse":   "(project p (module example.com/p",
		"has no project":   "(package orders)",
		"cannot be solved": "(project p (module example.com/p) (go 1.22)\n (package o (events (bus (requires teleportation)) (event E (field ID string)))))",
		"is empty":         "   ",
	} {
		root := t.TempDir()
		_, err := generate(root)(context.Background(), ProjectIn{Name: "p", SpecIn: SpecIn{Spec: spec}})
		if err == nil {
			t.Errorf("a spec that %s was accepted", name)
			continue
		}
		if left, _ := os.ReadDir(root); len(left) != 0 {
			t.Errorf("a spec that %s left %d entries in the root", name, len(left))
		}
	}
}

// place is the only stage that says where files go, and it says no to
// anything outside the root or in someone else's folder.
func TestMCPPlaceConfinesToRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "escaped")
	for _, name := range []string{"", ".", "..", "../escaped", "a/b", `a\b`, "/etc", "..hidden", "a..b", ".git", "-rf", "Orders", "with space", "a\x00b", strings.Repeat("a", 64)} {
		_, err := generate(root)(context.Background(), ProjectIn{Name: name, SpecIn: SpecIn{Spec: mcpCases(t)[0].spec}})
		if err == nil {
			t.Errorf("the name %q was accepted", name)
		}
	}
	if left, _ := os.ReadDir(root); len(left) != 0 {
		t.Errorf("refused names left %d entries in the root", len(left))
	}
	if _, err := os.Stat(outside); err == nil {
		t.Error("a folder was created outside the root")
	}

	// A folder that is there and is not tilegen's is not touched.
	theirs := filepath.Join(root, "theirs")
	if err := os.MkdirAll(theirs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(theirs, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := generate(root)(context.Background(), ProjectIn{Name: "theirs", SpecIn: SpecIn{Spec: mcpCases(t)[0].spec}})
	if err == nil || !strings.Contains(err.Error(), "not a tilegen project") {
		t.Errorf("an existing folder was not refused: %v", err)
	}
	if got := tree(t, theirs); len(got) != 1 || got["main.go"] != "package main\n" {
		t.Errorf("the existing folder was changed: %v", got)
	}

	// An empty folder is free to use.
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := generate(root)(context.Background(), ProjectIn{Name: "empty", SpecIn: SpecIn{Spec: mcpCases(t)[0].spec}}); err != nil {
		t.Errorf("an empty folder was refused: %v", err)
	}
}

// A batch is each project generated on its own: the same files, the same
// reports, in the order asked, with a bad spec costing only itself.
func TestMCPManyIsEachOnItsOwn(t *testing.T) {
	cases := mcpCases(t)
	var in ManyIn
	for i, c := range cases {
		in.Projects = append(in.Projects, c.in())
		if i == 2 {
			in.Projects = append(in.Projects, ProjectIn{Name: "broken", SpecIn: SpecIn{Spec: "(project p (module"}})
			in.Projects = append(in.Projects, ProjectIn{Name: "../out", SpecIn: SpecIn{Spec: c.spec}})
		}
	}
	root := t.TempDir()
	out, err := many(root)(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Created != len(cases) || out.Failed != 2 || len(out.Projects) != len(in.Projects) {
		t.Fatalf("created %d, failed %d of %d; want %d and 2", out.Created, out.Failed, len(out.Projects), len(cases))
	}
	alone := t.TempDir()
	holes := 0
	for i, o := range out.Projects {
		if o.Name != in.Projects[i].Name {
			t.Fatalf("result %d is %q; the batch asked for %q there", i, o.Name, in.Projects[i].Name)
		}
		want, wantErr := generate(alone)(context.Background(), in.Projects[i])
		if wantErr != nil {
			if o.OK || o.Error != wantErr.Error() || o.Project != nil {
				t.Errorf("%s: alone it fails with %q; in the batch: ok=%v %q", o.Name, wantErr, o.OK, o.Error)
			}
			continue
		}
		if !o.OK || o.Project == nil {
			t.Errorf("%s: failed in the batch: %s", o.Name, o.Error)
			continue
		}
		if !reflect.DeepEqual(tree(t, o.Project.Dir), tree(t, want.Dir)) {
			t.Errorf("%s: the batch's files differ from generating it alone", o.Name)
		}
		got := *o.Project
		got.Dir, want.Dir = "", ""
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: the batch's report differs from generating it alone", o.Name)
		}
		holes += want.Holes
	}
	if out.Holes != holes {
		t.Errorf("the batch counts %d holes; its projects have %d", out.Holes, holes)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != len(cases) {
		t.Errorf("the root holds %d folders; %d projects were created", len(entries), len(cases))
	}
}

func TestMCPManyRefusesWhatItCannotDo(t *testing.T) {
	root := t.TempDir()
	spec := mcpCases(t)[0].spec
	one := ProjectIn{Name: "a", SpecIn: SpecIn{Spec: spec}}
	big := ManyIn{}
	for i := 0; i <= maxBatch; i++ {
		big.Projects = append(big.Projects, ProjectIn{Name: fmt.Sprintf("p%d", i), SpecIn: SpecIn{Spec: spec}})
	}
	for name, c := range map[string]struct {
		in   ManyIn
		want string
	}{
		"nothing":      {ManyIn{}, "no projects"},
		"a name twice": {ManyIn{Projects: []ProjectIn{one, one}}, "appears twice"},
		"too many":     {big, "several calls"},
	} {
		if _, err := many(root)(context.Background(), c.in); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if left, _ := os.ReadDir(root); len(left) != 0 {
		t.Errorf("refused batches left %d entries in the root", len(left))
	}
}

// Many projects at once, many times over: the race detector watches this.
func TestMCPManyAtOnce(t *testing.T) {
	cases := mcpCases(t)
	var in ManyIn
	for i := 0; i < 60; i++ {
		c := cases[i%len(cases)]
		p := c.in()
		p.Name = fmt.Sprintf("%s-%02d", c.name, i)
		in.Projects = append(in.Projects, p)
	}
	root := t.TempDir()
	out, err := many(root)(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Failed != 0 {
		for _, o := range out.Projects {
			if !o.OK {
				t.Errorf("%s: %s", o.Name, o.Error)
			}
		}
	}
	ref := map[string]map[string]string{}
	for i, o := range out.Projects {
		if !o.OK {
			continue
		}
		c := cases[i%len(cases)]
		if ref[c.name] == nil {
			dir := filepath.Join(t.TempDir(), c.name)
			viaCLI(t, c, dir)
			ref[c.name] = tree(t, dir)
		}
		if !reflect.DeepEqual(tree(t, o.Project.Dir), ref[c.name]) {
			t.Errorf("%s: made alongside others, it differs from the command line's", o.Name)
		}
	}
}

// explain decides and touches nothing, and what it says is what
// `tilegen explain -json` says.
func TestMCPExplainIsExplain(t *testing.T) {
	for _, c := range mcpCases(t) {
		got, err := explain()(context.Background(), SpecIn{Spec: c.spec, Config: c.config, Policy: c.policy})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		cp, err := compileRequest(specRequest{Spec: c.spec, Config: c.config, Policy: c.policy})
		if err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(explainJSON(cp, nil))
		if string(a) != string(b) || len(a) < 20 {
			t.Errorf("%s: explain through MCP differs from explain", c.name)
		}
	}
	if _, err := os.Stat("project"); err == nil {
		t.Error("explain created a folder")
	}
}

func TestMCPReferenceAndCatalogue(t *testing.T) {
	ref, err := reference()(context.Background(), struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## The spec", "## The config", "## Choosing tiles", "(project", "(config", "(policy"} {
		if !strings.Contains(ref.Reference, want) {
			t.Errorf("the reference has no %q", want)
		}
	}
	for _, not := range []string{"## Quick start", "## Help wanted", "## License"} {
		if strings.Contains(ref.Reference, not) {
			t.Errorf("the reference carries %q, which is not about writing a spec", not)
		}
	}
	cat, err := catalogue()(context.Background(), struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if cat.Version != version || len(cat.Tiles) == 0 || len(cat.Capabilities) == 0 {
		t.Errorf("the catalogue is empty: %d tiles, %d capabilities", len(cat.Tiles), len(cat.Capabilities))
	}
	if !reflect.DeepEqual(cat.TilesJSON, tilesJSON(Registry())) {
		t.Error("the catalogue is not `tilegen tiles -json`")
	}
}

// A panic in a stage is that call's error, and the next call is unharmed.
func TestMCPRecovers(t *testing.T) {
	boom := weft.Apply(weft.Arrow[int, int](func(_ context.Context, n int) (int, error) {
		if n == 0 {
			panic("boom")
		}
		return n, nil
	}), recovering[int, int]())
	if _, err := boom(context.Background(), 0); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("a panic was not turned into an error: %v", err)
	}
	if n, err := boom(context.Background(), 7); err != nil || n != 7 {
		t.Errorf("after a panic the arrow is broken: %d, %v", n, err)
	}
}

var mcpToolNames = []string{"tilegen_catalogue", "tilegen_explain", "tilegen_generate", "tilegen_generate_many", "tilegen_reference"}

// Through the protocol and back is the arrow itself: a tool called over
// MCP answers exactly what the arrow answers when called directly.
func TestMCPRoundTripIsTheArrow(t *testing.T) {
	ctx := context.Background()
	served, direct := t.TempDir(), t.TempDir()
	client, err := mcp.Connect(ctx, mcp.InMemory(mcpServer(served)))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	checkTools(t, client)
	roundTrips(t, ctx, caller{client: client}, served, direct)
}

// The same, against the real binary on real pipes.
func TestMCPOverStdio(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "tilegen")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	ctx := context.Background()
	served, direct := t.TempDir(), t.TempDir()
	tr, err := mcp.Stdio(bin, "mcp", "-root", served)
	if err != nil {
		t.Fatal(err)
	}
	client, err := mcp.Connect(ctx, tr)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	checkTools(t, client)
	roundTrips(t, ctx, caller{client: client, text: true}, served, direct)
}

// tree is a project's files by their path inside it.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for p, text := range snapshot(t, dir) {
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.ToSlash(rel)] = text
	}
	return out
}

// caller makes a typed arrow for a tool on some client.
type caller struct {
	client *mcp.Client
	text   bool // the transport hands results back as text (weft's stdio does)
}

func call[In, Out any](c caller, name string) weft.Arrow[In, Out] {
	if !c.text {
		return mcp.Tool[In, Out](c.client, name)
	}
	decode := func(_ context.Context, s string) (Out, error) {
		var out Out
		return out, json.Unmarshal([]byte(s), &out)
	}
	return weft.Compose(mcp.Tool[In, string](c.client, name), weft.Arrow[string, Out](decode))
}

func checkTools(t *testing.T, client *mcp.Client) {
	t.Helper()
	got := append([]string{}, client.Tools()...)
	sortStrings(got)
	if !reflect.DeepEqual(got, mcpToolNames) {
		t.Fatalf("the server offers %v; want %v", got, mcpToolNames)
	}
}

func sortStrings(s []string) {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

func roundTrips(t *testing.T, ctx context.Context, client caller, served, direct string) {
	t.Helper()
	asJSON := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(b), served, direct)
	}
	cases := mcpCases(t)

	// generate
	remote := call[ProjectIn, Project](client, "tilegen_generate")
	for _, c := range cases[:4] {
		got, err := remote(ctx, c.in())
		if err != nil {
			t.Fatalf("%s over MCP: %v", c.name, err)
		}
		want, err := generate(direct)(ctx, c.in())
		if err != nil {
			t.Fatal(err)
		}
		if asJSON(got) != asJSON(want) {
			t.Errorf("%s: over MCP the report differs:\n got %s\nwant %s", c.name, asJSON(got), asJSON(want))
		}
		if !reflect.DeepEqual(tree(t, got.Dir), tree(t, want.Dir)) {
			t.Errorf("%s: over MCP the files differ", c.name)
		}
	}

	// an error comes back as the same words
	_, err := remote(ctx, ProjectIn{Name: "../x", SpecIn: SpecIn{Spec: cases[0].spec}})
	_, wantErr := generate(direct)(ctx, ProjectIn{Name: "../x", SpecIn: SpecIn{Spec: cases[0].spec}})
	if err == nil || wantErr == nil {
		t.Fatalf("a name outside the root was accepted: %v, %v", err, wantErr)
	}
	quoted, _ := json.Marshal(wantErr.Error()) // weft's stdio client hands the message back as a JSON string
	if got := strings.ReplaceAll(err.Error(), served, direct); !strings.Contains(got, wantErr.Error()) && !strings.Contains(got, string(quoted)) {
		t.Errorf("over MCP the refusal reads %q; directly it reads %q", err, wantErr)
	}
	_, err = remote(ctx, ProjectIn{Name: "bad", SpecIn: SpecIn{Spec: "(project p (module"}})
	if err == nil || !strings.Contains(err.Error(), "spec.sexp:") {
		t.Errorf("a spec that does not parse should come back with its position: %v", err)
	}

	// many
	var in ManyIn
	for _, c := range cases[4:] {
		in.Projects = append(in.Projects, c.in())
	}
	in.Projects = append(in.Projects, ProjectIn{Name: "broken", SpecIn: SpecIn{Spec: "(nothing)"}})
	gotMany, err := call[ManyIn, ManyOut](client, "tilegen_generate_many")(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	wantMany, err := many(direct)(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if asJSON(gotMany) != asJSON(wantMany) {
		t.Errorf("over MCP the batch differs:\n got %s\nwant %s", asJSON(gotMany), asJSON(wantMany))
	}
	if gotMany.Failed != 1 || gotMany.Created != len(cases)-4 {
		t.Errorf("the batch created %d and failed %d", gotMany.Created, gotMany.Failed)
	}
	for i, o := range gotMany.Projects {
		if o.OK && !reflect.DeepEqual(tree(t, o.Project.Dir), tree(t, wantMany.Projects[i].Project.Dir)) {
			t.Errorf("%s: over MCP the batch's files differ", o.Name)
		}
	}

	// the read-only tools
	c := cases[0]
	gotEx, err := call[SpecIn, json.RawMessage](client, "tilegen_explain")(ctx, SpecIn{Spec: c.spec})
	if err != nil {
		t.Fatal(err)
	}
	wantEx, _ := explain()(ctx, SpecIn{Spec: c.spec})
	if !sameJSON(t, gotEx, wantEx) {
		t.Error("over MCP explain differs")
	}
	gotCat, err := call[struct{}, Catalogue](client, "tilegen_catalogue")(ctx, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	wantCat, _ := catalogue()(ctx, struct{}{})
	if asJSON(gotCat) != asJSON(wantCat) {
		t.Error("over MCP the catalogue differs")
	}
	gotRef, err := call[struct{}, Reference](client, "tilegen_reference")(ctx, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if wantRef, _ := reference()(ctx, struct{}{}); gotRef != wantRef {
		t.Error("over MCP the reference differs")
	}
}

func sameJSON(t *testing.T, raw json.RawMessage, v any) bool {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	enc, _ := json.Marshal(v)
	if err := json.Unmarshal(enc, &b); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(a, b)
}

// Every tool's input schema is JSON, an object, and closed.
func TestMCPSchemas(t *testing.T) {
	for name, s := range map[string]string{"none": noInput, "spec": specSchema, "one": oneSchema, "many": manySchema} {
		var v struct {
			Type       string         `json:"type"`
			Properties map[string]any `json:"properties"`
			Additional *bool          `json:"additionalProperties"`
		}
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			t.Errorf("%s: not JSON: %v", name, err)
			continue
		}
		if v.Type != "object" || v.Additional == nil || *v.Additional {
			t.Errorf("%s: want a closed object", name)
		}
	}
	if !projectNameOK.MatchString("orders-api") || !strings.Contains(oneSchema, projectNameOK.String()) {
		t.Error("the schema's name pattern is not the one place checks")
	}
	if !strings.Contains(manySchema, fmt.Sprintf(`"maxItems": %d`, maxBatch)) {
		t.Error("the schema's batch limit is not the one many checks")
	}
}

// A package's (dir ...) cannot leave the project: tilegen says so when it
// reads the spec, on the command line as much as here.
func TestDirStaysInTheProject(t *testing.T) {
	shop := mcpCases(t)[9].spec
	with := func(dir string) string {
		return strings.Replace(shop, "(package orders", `(package orders (dir "`+dir+`")`, 1)
	}
	for _, dir := range []string{"../../escaped", "..", "/tmp/abs", "a/../../up", `..\\up`, "c:/up"} {
		root := t.TempDir()
		_, err := generate(root)(context.Background(), ProjectIn{Name: "p", SpecIn: SpecIn{Spec: with(dir)}})
		if err == nil || !strings.Contains(err.Error(), "leaves the project") {
			t.Errorf("(dir %q) was not refused: %v", dir, err)
		}
		if left, _ := os.ReadDir(root); len(left) != 0 {
			t.Errorf("(dir %q) left %d entries in the root", dir, len(left))
		}
		if err := validateSrc(t, with(dir), false); err == nil {
			t.Errorf("(dir %q) passes validation", dir)
		}
	}
	for _, dir := range []string{".", "internal/orders", "pkg/orders/", "a/../orders"} {
		if _, err := generate(t.TempDir())(context.Background(), ProjectIn{Name: "p", SpecIn: SpecIn{Spec: with(dir)}}); err != nil {
			t.Errorf("(dir %q) was refused: %v", dir, err)
		}
	}
}

// confine holds even if a path gets past everything before it.
func TestMCPConfineRefusesAnEscapingPlan(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "p")
	for _, rel := range []string{"../x.go", "a/../../x.go", "/etc/x.go", ".."} {
		l := laid{rep: &Report{staged: map[string][]byte{"ok.go": nil, rel: nil}}}
		l.dir = dir
		if _, err := confine(context.Background(), l); err == nil || !strings.Contains(err.Error(), "outside its project folder") {
			t.Errorf("a plan with %q was let through: %v", rel, err)
		}
		l = laid{rep: &Report{unlinked: map[string]bool{rel: true}}}
		l.dir = dir
		if _, err := confine(context.Background(), l); err == nil {
			t.Errorf("a plan deleting %q was let through", rel)
		}
	}
	ok := laid{rep: &Report{staged: map[string][]byte{"a/b.go": nil, "go.mod": nil}, unlinked: map[string]bool{"old/c.go": true}}}
	ok.dir = dir
	if _, err := confine(context.Background(), ok); err != nil {
		t.Errorf("an ordinary plan was refused: %v", err)
	}
}

// A name that is a link, or a file, is not a folder to write into.
func TestMCPPlaceRefusesLinks(t *testing.T) {
	root, elsewhere := t.TempDir(), t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(root, "link")); err != nil {
		t.Skip("no symlinks here")
	}
	if err := os.WriteFile(filepath.Join(root, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"link", "file"} {
		_, err := generate(root)(context.Background(), ProjectIn{Name: name, SpecIn: SpecIn{Spec: mcpCases(t)[0].spec}})
		if err == nil || !strings.Contains(err.Error(), "not a folder") {
			t.Errorf("%s was not refused: %v", name, err)
		}
	}
	if left, _ := os.ReadDir(elsewhere); len(left) != 0 {
		t.Error("files were written through the link")
	}
}
