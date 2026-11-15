//go:build !js

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	_ "github.com/vinodhalaharvi/tilegen/internal/tiles" // a spec needs tiles to choose from
	"github.com/vinodhalaharvi/weft/weft"
)

// A panic in a stage is that call's error, and the next call is unharmed.
func TestMCPRecovers(t *testing.T) {
	boom := weft.Apply(weft.Arrow[int, int](func(_ context.Context, n int) (int, error) {
		if n == 0 {
			panic("boom")
		}
		return n, nil
	}), Recovering[int, int]())
	if _, err := boom(context.Background(), 0); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("a panic was not turned into an error: %v", err)
	}
	if n, err := boom(context.Background(), 7); err != nil || n != 7 {
		t.Errorf("after a panic the arrow is broken: %d, %v", n, err)
	}
}

// Every tool's input schema is JSON, an object, and closed.
func TestMCPSchemas(t *testing.T) {
	for name, s := range map[string]string{"none": NoInput, "explain": ExplainSchema, "one": OneSchema, "many": ManySchema, "gomod": GomodSchema, "symbols": SymbolsSchema, "check": CheckSchema} {
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
	if !ProjectNameOK.MatchString("orders-api") || !strings.Contains(OneSchema, ProjectNameOK.String()) {
		t.Error("the schema's name pattern is not the one place checks")
	}
	if !strings.Contains(SymbolsSchema, fmt.Sprintf(`"maxItems": %d`, MaxSymbolDetails)) {
		t.Error("the schema's limit on symbols is not the one the tool checks")
	}
	if !strings.Contains(ManySchema, fmt.Sprintf(`"maxItems": %d`, MaxBatch)) {
		t.Error("the schema's batch limit is not the one many checks")
	}
}

// Confine holds even if a path gets past everything before it.
func TestMCPConfineRefusesAnEscapingPlan(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "p")
	for _, rel := range []string{"../x.go", "a/../../x.go", "/etc/x.go", ".."} {
		l := Laid{Rep: &compile.Report{Staged: map[string][]byte{"ok.go": nil, rel: nil}}}
		l.Dir = dir
		if _, err := Confine(context.Background(), l); err == nil || !strings.Contains(err.Error(), "outside its project folder") {
			t.Errorf("a plan with %q was let through: %v", rel, err)
		}
		l = Laid{Rep: &compile.Report{Unlinked: map[string]bool{rel: true}}}
		l.Dir = dir
		if _, err := Confine(context.Background(), l); err == nil {
			t.Errorf("a plan deleting %q was let through", rel)
		}
	}
	ok := Laid{Rep: &compile.Report{Staged: map[string][]byte{"a/b.go": nil, "go.mod": nil}, Unlinked: map[string]bool{"old/c.go": true}}}
	ok.Dir = dir
	if _, err := Confine(context.Background(), ok); err != nil {
		t.Errorf("an ordinary plan was refused: %v", err)
	}
}

const notesSpec = `(project notes
  (module example.com/notes)
  (go 1.22)
  (package notes
    (entity Note
      (field ID string)
      (field Text string)
      (store get save (durable)))))
`

func files(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// One project, start to finish, with nothing but this package and the tiles.
func TestGenerateWritesAProjectUnderTheRoot(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	p, err := Generate(root)(ctx, ProjectIn{Name: "notes", SpecIn: SpecIn{Spec: notesSpec}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Dir != filepath.Join(root, "notes") || p.Name != "notes" {
		t.Errorf("written to %s", p.Dir)
	}
	got := files(t, p.Dir)
	if p.Written != len(got) || p.Written == 0 || got["go.mod"] == "" || got[compile.LockFile] == "" {
		t.Errorf("%d files reported, %d on disk", p.Written, len(got))
	}
	holes := 0
	for name, text := range got {
		if strings.HasSuffix(name, ".go") {
			holes += strings.Count(text, `panic("`+compile.HolePrefix)
		}
	}
	if p.Holes == 0 || p.Holes != holes {
		t.Errorf("%d holes reported; the files hold %d", p.Holes, holes)
	}
	if len(p.Decisions) != 1 || p.Decisions[0].Capability != "store" || p.Decisions[0].ChosenBy != "auto" || p.Decisions[0].Tile == "" {
		t.Errorf("the decisions: %+v", p.Decisions)
	}
	if n := len(p.Next); n < 2 || p.Next[n-1] != "go build ./..." || p.Next[n-2] != "go mod tidy" {
		t.Errorf("what to run next: %v", p.Next)
	}

	// Again: nothing is written, and the lock holds the choice.
	again, err := Generate(root)(ctx, ProjectIn{Name: "notes", SpecIn: SpecIn{Spec: notesSpec}})
	if err != nil {
		t.Fatal(err)
	}
	if again.Written != 0 || again.Decisions[0].Tile != p.Decisions[0].Tile || again.Decisions[0].ChosenBy != "lock" {
		t.Errorf("the second run: wrote %d, %+v", again.Written, again.Decisions)
	}
	if !reflect.DeepEqual(got, files(t, p.Dir)) {
		t.Error("the second run changed the project")
	}

	// A name is one folder, never a path.
	for _, name := range []string{"", "..", "../x", "a/b", "/abs", "Upper", "with space", ".hidden"} {
		if _, err := Generate(root)(ctx, ProjectIn{Name: name, SpecIn: SpecIn{Spec: notesSpec}}); err == nil {
			t.Errorf("the name %q was accepted", name)
		}
	}
	if left, _ := os.ReadDir(root); len(left) != 1 {
		t.Errorf("the root holds %d entries after the refusals", len(left))
	}
}

func TestManyReportsEachProject(t *testing.T) {
	root := t.TempDir()
	out, err := Many(root)(context.Background(), ManyIn{Projects: []ProjectIn{
		{Name: "a", SpecIn: SpecIn{Spec: notesSpec}},
		{Name: "broken", SpecIn: SpecIn{Spec: "(project p (module"}},
		{Name: "b", SpecIn: SpecIn{Spec: notesSpec, Config: "(config (storage bolt))"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Root != root || out.Created != 2 || out.Failed != 1 || len(out.Projects) != 3 {
		t.Fatalf("the batch: %+v", out)
	}
	a, broken, b := out.Projects[0], out.Projects[1], out.Projects[2]
	if a.Name != "a" || broken.Name != "broken" || b.Name != "b" {
		t.Errorf("the results are not in the order asked: %s %s %s", a.Name, broken.Name, b.Name)
	}
	if !a.OK || a.Project == nil || !b.OK || b.Project.Decisions[0].Tile != "bolt" || b.Project.Decisions[0].ChosenBy != "config" {
		t.Errorf("the two that should be made: %+v %+v", a, b)
	}
	if broken.OK || broken.Project != nil || !strings.Contains(broken.Error, "spec.sexp:1:") {
		t.Errorf("the one that should fail: %+v", broken)
	}
	if out.Holes != a.Project.Holes+b.Project.Holes {
		t.Errorf("the batch counts %d holes", out.Holes)
	}
	if _, err := os.Stat(filepath.Join(root, "broken")); err == nil {
		t.Error("the failed project left a folder")
	}
}

// The reference is the README's own words on writing a spec, whatever
// README the program was built with.
func TestReferenceIsTheReadmesSections(t *testing.T) {
	old := Readme
	t.Cleanup(func() { Readme = old })
	Readme = "# tilegen\n\nintro\n\n## Install\n\nbrew\n\n## The spec\n\nspec words\n\n### a sub-heading\n\nmore\n\n## The config\n\nconfig words\n\n## Other\n\nnot this\n\n## Choosing tiles\n\nchoosing words\n"
	ref, err := ReferenceTool()(context.Background(), struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	want := "## The spec\n\nspec words\n\n### a sub-heading\n\nmore\n\n## The config\n\nconfig words\n\n## Choosing tiles\n\nchoosing words"
	if ref.Reference != want || ref.Version != compile.Version {
		t.Errorf("the reference:\n%q\nwant\n%q", ref.Reference, want)
	}
	if got := readmeSections("## Not there"); got != "" {
		t.Errorf("a section the README lacks: %q", got)
	}
}

func TestServerOffersItsTools(t *testing.T) {
	if Server(t.TempDir()) == nil {
		t.Fatal("no server")
	}
	if !ProjectNameOK.MatchString("orders-api") || ProjectNameOK.MatchString("Orders") {
		t.Error("the name pattern")
	}
}
