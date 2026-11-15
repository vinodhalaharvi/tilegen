package main

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
)

// A tile-spec generates a file of tilegen's own. Whatever tilegen's
// packages are called, that file has to compile where it is meant to go:
// this builds tilegen with a freshly generated tile in place of the one
// checked in.
func TestGeneratedTileBuildsInsideTilegen(t *testing.T) {
	if testing.Short() {
		t.Skip("builds tilegen")
	}
	gen := t.TempDir()
	if err := run(compile.Options{Spec: "examples/tilespec", Out: gen}, io.Discard); err != nil {
		t.Fatal(err)
	}
	tile, err := os.ReadFile(filepath.Join(gen, "internal", "tiles", "store_badger.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(tile), "tilegen:hole") != 2 {
		t.Fatalf("a fresh tile has two holes:\n%s", tile)
	}

	// The files a task tells the model to read are files tilegen has.
	raw, err := os.ReadFile(filepath.Join(gen, "tilegen.tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tasks struct {
		Tasks []struct {
			ID      string   `json:"id"`
			File    string   `json:"file"`
			Context []string `json:"context_files"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &tasks); err != nil || len(tasks.Tasks) != 2 {
		t.Fatalf("the tasks: %v\n%s", err, raw)
	}
	for _, task := range tasks.Tasks {
		if len(task.Context) == 0 {
			t.Errorf("%s names no file to read", task.ID)
		}
		for _, f := range task.Context {
			if f == task.File {
				continue
			}
			if _, err := os.Stat(f); err != nil {
				t.Errorf("%s tells the model to read %s, which tilegen does not have", task.ID, f)
			}
		}
	}

	// Every function of tilegen's a task names is one tilegen has.
	named := regexp.MustCompile(`\b(compile|sexp)\.([A-Z][A-Za-z0-9_]*)`).FindAllStringSubmatch(string(raw), -1)
	if len(named) < 5 {
		t.Errorf("the tasks name %d of tilegen's functions; the intents should say what to call", len(named))
	}
	sources := map[string]string{}
	for _, m := range named {
		pkg, name := m[1], m[2]
		if sources[pkg] == "" {
			files, _ := filepath.Glob(filepath.Join("internal", pkg, "*.go"))
			for _, f := range files {
				b, _ := os.ReadFile(f)
				sources[pkg] += string(b)
			}
		}
		declared := regexp.MustCompile(`(?m)^(func |type |var |const |\t)` + name + `\b|^func \([^)]*\) ` + name + `\(`)
		if !declared.MatchString(sources[pkg]) {
			t.Errorf("a task names %s.%s, which internal/%s does not declare", pkg, name, pkg)
		}
	}

	// A copy of what a build of tilegen reads.
	copyOf := t.TempDir()
	keep := func(rel string, d fs.DirEntry) bool {
		top := strings.Split(rel, string(filepath.Separator))[0]
		if d.IsDir() {
			return rel == "." || top == "internal" || top == "web"
		}
		switch {
		case top == "internal" || top == "web":
			return !strings.HasSuffix(rel, "_test.go")
		case rel == "go.mod" || rel == "go.sum" || rel == "README.md":
			return true
		}
		return filepath.Dir(rel) == "." && strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
	}
	err = filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !keep(p, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(copyOf, p), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(copyOf, p), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(copyOf, "internal", "tiles", "store_badger.go"), tile, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"build", "./..."}, {"vet", "./internal/tiles"}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = copyOf
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tilegen with a freshly generated tile: go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

// specTiles are the tiles that have a spec checked in beside them: the
// spec's path, and the tile it describes.
func specTiles(t *testing.T) map[string]string {
	t.Helper()
	specs, err := filepath.Glob(filepath.Join("internal", "tiles", "specs", "*.sexp"))
	if err != nil || len(specs) == 0 {
		t.Fatalf("no tile specs found: %v", err)
	}
	out := map[string]string{}
	for _, spec := range specs {
		out[spec] = strings.TrimSuffix(filepath.Base(spec), ".sexp")
	}
	return out
}

// registration is a tile file's init function: everything the spec states.
func registration(t *testing.T, src string) string {
	t.Helper()
	a := strings.Index(src, "func init() {\n")
	if a < 0 {
		t.Fatal("no init in the tile")
	}
	b := strings.Index(src[a:], "\n}\n")
	return src[a : a+b+3]
}

// A tile's spec and its Go file say the same thing: generating from the
// spec gives the registration that is checked in, and the holes it left
// have been filled.
func TestSpecsMatchTheirTiles(t *testing.T) {
	for spec, tile := range specTiles(t) {
		gen := t.TempDir()
		if err := run(compile.Options{Spec: spec, Out: gen}, io.Discard); err != nil {
			t.Errorf("%s: %v", spec, err)
			continue
		}
		made, _ := filepath.Glob(filepath.Join(gen, "internal", "tiles", "*.go"))
		if len(made) != 1 {
			t.Errorf("%s generated %d files in internal/tiles", spec, len(made))
			continue
		}
		fresh, err := os.ReadFile(made[0])
		if err != nil {
			t.Fatal(err)
		}
		kept, err := os.ReadFile(filepath.Join("internal", "tiles", filepath.Base(made[0])))
		if err != nil {
			t.Errorf("%s describes %s, and %s is not checked in", spec, tile, filepath.Base(made[0]))
			continue
		}
		if registration(t, string(fresh)) != registration(t, string(kept)) {
			t.Errorf("%s and %s have drifted: change the spec, generate, and keep the two holes' bodies", spec, filepath.Base(made[0]))
		}
		if strings.Contains(string(kept), compile.HolePrefix) {
			t.Errorf("%s still has an open hole", filepath.Base(made[0]))
		}
		if _, ok := compile.Backends[shortNameOf(tile)]; !ok {
			t.Errorf("%s is not registered as a store", tile)
		}
	}
}

// shortNameOf is the name a config pins a store tile by.
func shortNameOf(tile string) string {
	for name, b := range compile.Backends {
		if b.Tile == tile {
			return name
		}
	}
	return ""
}

// Every tile made from a spec compiles for real: pinned in a small spec,
// it generates a project that builds and vets against the library it
// names. A type the library does not have, or an import path that is
// wrong, fails here.
func TestSpecTilesCompileAgainstTheirLibraries(t *testing.T) {
	if testing.Short() {
		t.Skip("fetches each tile's library")
	}
	const notes = `(project notes
  (module example.com/notes)
  (go 1.22)
  (package notes
    (entity Note
      (field ID string)
      (field Title string)
      (field Body string)
      (store get list save delete count))))
`
	for _, tile := range specTiles(t) {
		short := shortNameOf(tile)
		t.Run(tile, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			spec := filepath.Join(dir, "spec.sexp")
			config := filepath.Join(dir, "config.sexp")
			if err := os.WriteFile(spec, []byte(notes), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(config, []byte("(config (storage "+short+"))\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(dir, "out")
			if err := run(compile.Options{Spec: spec, Config: config, Out: out}, io.Discard); err != nil {
				t.Fatalf("pinned to %s: %v", short, err)
			}
			for _, args := range [][]string{{"mod", "tidy"}, {"build", "./..."}, {"vet", "./..."}} {
				cmd := exec.Command("go", args...)
				cmd.Dir = out
				if b, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("a project on %s: go %s: %v\n%s", tile, strings.Join(args, " "), err, b)
				}
			}
		})
	}
}
