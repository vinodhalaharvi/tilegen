package main

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
	"github.com/vinodhalaharvi/tilegen/internal/tiles"
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
	// The copy has badger once: as the Go file just generated, in place of
	// the data file tilegen ships.
	if err := os.Remove(filepath.Join(copyOf, "internal", "tiles", "specs", "badger.sexp")); err != nil {
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

// specTiles are the tiles that are data: the file, and the tile it holds.
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

// A tile that is data is the tile tilegen runs with: what the file says is
// what is registered, down to each hint.
func TestDataTilesAreWhatIsRegistered(t *testing.T) {
	for spec, tile := range specTiles(t) {
		src, err := os.ReadFile(spec)
		if err != nil {
			t.Fatal(err)
		}
		read, err := tiles.LoadTiles(spec, string(src))
		if err != nil || len(read) != 1 {
			t.Errorf("%s: %d tiles, %v", spec, len(read), err)
			continue
		}
		reg := compile.Backends[shortNameOf(tile)]
		if reg == nil {
			t.Errorf("%s is not registered as a store", tile)
			continue
		}
		a, b := *read[0], *reg
		a.Implement, b.Implement = nil, nil
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s: the file and the registry disagree:\n%+v\n%+v", tile, a, b)
		}
		offered := false
		for _, o := range compile.Offers["store"] {
			offered = offered || (o.Tile == tile && o.Doc == reg.Doc)
		}
		if !offered {
			t.Errorf("%s is not offered as a store", tile)
		}
		// Each plain method has its hint, and nothing in it is left unfilled.
		parts, err := reg.Implement(compile.StoreInput{Entity: "LineItem", Impl: "XLineItemStore", IDType: "string"})
		if err != nil || len(parts.Fields) == 0 || !strings.Contains(parts.Body, "XLineItemStore") {
			t.Errorf("%s: parts %+v, %v", tile, parts, err)
			continue
		}
		for _, op := range []string{"get", "list", "save", "delete", "count"} {
			hint := parts.Hint(sexp.L(sexp.Sym("method"), sexp.Sym("M"), sexp.L(sexp.Sym("op"), sexp.Sym(op))))
			if hint == "" {
				t.Errorf("%s has no hint for %s", tile, op)
			}
			for _, left := range []string{"{impl}", "{entity}", "{name}", "{names}", "{id}", "{field}", "{param}"} {
				if strings.Contains(hint+parts.Body, left) {
					t.Errorf("%s: %s is left unfilled", tile, left)
				}
			}
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

// A tile-spec that is whole has nothing to scaffold: inside a project it
// is refused, with where it belongs.
func TestAWholeTileSpecIsNotScaffolded(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("internal", "tiles", "specs", "pebble.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	whole := strings.Replace(string(src), "(tile-spec pebble", "(tile-spec rocks", 1)
	dir := t.TempDir()
	sp, _ := writeSpec(t, dir, "(project tiles (module example.com/tiles) (go 1.22)\n  (package main (dir \".\")\n"+whole+"))", "")
	err = run(compile.Options{Spec: sp, Out: filepath.Join(dir, "out")}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "(tile-spec rocks ...) is whole") || !strings.Contains(err.Error(), "there is nothing to scaffold") {
		t.Fatalf("got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "out", "store_rocks.go")); statErr == nil {
		t.Error("a file was written for it")
	}
}
