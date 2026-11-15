package main

import (
	"encoding/json"
	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
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
	tile, err := os.ReadFile(filepath.Join(gen, "internal", "tiles", "tile_badger.go"))
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
	if err := os.WriteFile(filepath.Join(copyOf, "internal", "tiles", "tile_badger.go"), tile, 0o644); err != nil {
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
