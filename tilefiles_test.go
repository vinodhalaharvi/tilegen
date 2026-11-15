package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
	"github.com/vinodhalaharvi/tilegen/internal/tiles"
)

// specTiles are the tiles that are data: the file, and the tile it holds.
func specTiles(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("internal", "tiles", "*.sexp"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no tile files found: %v", err)
	}
	out := map[string]string{}
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		read, err := tiles.LoadTiles(file, string(src))
		if err != nil || len(read) != 1 {
			t.Fatalf("%s: %d tiles, %v", file, len(read), err)
		}
		out[file] = read[0].Tile
	}
	return out
}

// loaded reads each tile file as tilegen does when it starts.
func loaded(t *testing.T) []*tiles.Tile {
	t.Helper()
	var out []*tiles.Tile
	for file := range specTiles(t) {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		read, err := tiles.LoadTiles(file, string(src))
		if err != nil || len(read) != 1 {
			t.Fatalf("%s: %d tiles, %v", file, len(read), err)
		}
		out = append(out, read[0])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tile < out[j].Tile })
	return out
}

// A tile that is data is the tile tilegen runs with: what the file says is
// what is registered, down to each hint.
func TestDataTilesAreWhatIsRegistered(t *testing.T) {
	offered := map[string]int{}
	for _, tile := range loaded(t) {
		offered[tile.Offers]++
		var reg *compile.Offer
		for _, o := range compile.Offers[tile.Offers] {
			if o.Tile == tile.Tile {
				reg = o
			}
		}
		if reg == nil || reg.Doc == "" || compile.TileAliases[tile.Tile] != tile.Name {
			t.Errorf("%s is not offered as %s under the name %s", tile.Tile, tile.Offers, tile.Name)
			continue
		}
		if tile.Store == nil {
			// The offer is the registration: compare all of it but the code.
			a, b := *tile.Offer, *reg
			a.Impl, b.Impl = nil, nil
			if !reflect.DeepEqual(a, b) {
				t.Errorf("%s: the file and the registry disagree:\n%+v\n%+v", tile.Tile, a, b)
			}
			continue
		}
		store := compile.Backends[tile.Name]
		if store == nil {
			t.Errorf("%s is not registered as a store", tile.Tile)
			continue
		}
		a, b := *tile.Store, *store
		a.Implement, b.Implement = nil, nil
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s: the file and the registry disagree:\n%+v\n%+v", tile.Tile, a, b)
		}
		// Each plain method has its hint, and nothing in it is left unfilled.
		parts, err := store.Implement(compile.StoreInput{Entity: "LineItem", Impl: "XLineItemStore", IDType: "string"})
		if err != nil || len(parts.Fields) == 0 || !strings.Contains(parts.Body, "XLineItemStore") {
			t.Errorf("%s: parts %+v, %v", tile.Tile, parts, err)
			continue
		}
		for _, op := range []string{"get", "list", "save", "delete", "count"} {
			hint := parts.Hint(sexp.L(sexp.Sym("method"), sexp.Sym("M"), sexp.L(sexp.Sym("op"), sexp.Sym(op))))
			if hint == "" {
				t.Errorf("%s has no hint for %s", tile.Tile, op)
			}
			for _, left := range []string{"{impl}", "{entity}", "{name}", "{names}", "{id}", "{field}", "{param}"} {
				if strings.Contains(hint+parts.Body, left) {
					t.Errorf("%s: %s is left unfilled", tile.Tile, left)
				}
			}
		}
	}
	for _, capability := range tiles.Loadable() {
		if offered[capability] == 0 {
			t.Errorf("no tile kept as a file offers %s, so nothing shows that one can", capability)
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

// Every tile kept as a file compiles for real: pinned in a small spec, it
// generates a project that builds and vets against the library it names.
// A type the library does not have, or an import path that is wrong,
// fails here.
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
      (store get list save delete count))
    (events
      (event NoteSaved (field ID string)))
    (auth
      (doc "Principal is whoever a request is from."))))
`
	// What a config says to pin a tile, by what the tile offers.
	pin := map[string]string{"store": "storage", "event-bus": "events", "auth": "auth"}
	for _, tile := range loaded(t) {
		t.Run(tile.Tile, func(t *testing.T) {
			t.Parallel()
			if pin[tile.Offers] == "" {
				t.Fatalf("no way to pin a tile that offers %s", tile.Offers)
			}
			dir := t.TempDir()
			spec := filepath.Join(dir, "spec.sexp")
			config := filepath.Join(dir, "config.sexp")
			if err := os.WriteFile(spec, []byte(notes), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(config, []byte("(config ("+pin[tile.Offers]+" "+tile.Name+"))\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(dir, "out")
			if err := run(compile.Options{Spec: spec, Config: config, Out: out}, io.Discard); err != nil {
				t.Fatalf("pinned to %s: %v", tile.Name, err)
			}
			// The tile was used: its type is in what was generated.
			lock, _ := os.ReadFile(filepath.Join(out, "tilegen.lock"))
			if !strings.Contains(string(lock), tile.Tile) {
				t.Fatalf("the lock does not name %s:\n%s", tile.Tile, lock)
			}
			for _, args := range [][]string{{"mod", "tidy"}, {"build", "./..."}, {"vet", "./..."}} {
				cmd := exec.Command("go", args...)
				cmd.Dir = out
				if b, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("a project on %s: go %s: %v\n%s", tile.Tile, strings.Join(args, " "), err, b)
				}
			}
		})
	}
}

// A tile is a file of its own. Inside a project it is refused, with where
// it belongs, rather than becoming a hole for a model to fill.
func TestATileSpecIsNotPartOfAProject(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("internal", "tiles", "store_pebble.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	tile := strings.Replace(string(src), "(tile pebble", "(tile rocks", 1)
	for name, spec := range map[string]string{
		"in a package": "(project p (module example.com/p) (go 1.22)\n  (package main (dir \".\")\n" + tile + "))",
		"at the top":   "(project p (module example.com/p) (go 1.22)\n  (package main (dir \".\")))\n" + tile,
	} {
		dir := t.TempDir()
		sp, _ := writeSpec(t, dir, spec, "")
		err = run(compile.Options{Spec: sp, Out: filepath.Join(dir, "out")}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "(tile ...) is not part of a project: a tile is a file of its own, in a folder tilegen is given with -tiles DIR") {
			t.Errorf("%s: got %v", name, err)
		}
		if left, _ := os.ReadDir(filepath.Join(dir, "out")); len(left) != 0 {
			t.Errorf("%s: %d entries were written", name, len(left))
		}
	}
}

// A folder that cannot be a team's tiles is refused whole: nothing in it
// is registered.
func TestOwnTilesAreRefusedForWhatIsWrong(t *testing.T) {
	pebble, err := os.ReadFile(filepath.Join("internal", "tiles", "store_pebble.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	as := func(name string) string {
		return strings.Replace(string(pebble), "(tile pebble", "(tile "+name, 1)
	}
	before := len(compile.Backends)
	for name, c := range map[string]struct {
		files map[string]string
		want  string
	}{
		"empty":          {map[string]string{"README.md": "x"}, "no .sexp files in it"},
		"a name taken":   {map[string]string{"a.sexp": as("rocks"), "b.sexp": string(pebble)}, "tilegen already has a tile called pebble; give this one a name of its own"},
		"a short name":   {map[string]string{"a.sexp": strings.Replace(as("rocks"), "(offers store)", "(offers store) (alias pgx)", 1)}, "tilegen already has a tile called pgx"},
		"another kind's": {map[string]string{"a.sexp": as("kafka")}, "tilegen already has a tile called kafka"},
		"the same twice": {map[string]string{"a.sexp": as("rocks"), "b.sexp": as("rocks")}, "rocks is also the name of a tile in"},
		"a tile unfinished": {map[string]string{"a.sexp": as("rocks"), "b.sexp": strings.Replace(as("stone"), `(hint get`, `(hint get "TODO") (hint list`, 1)},
			"is still a TODO"},
	} {
		dir := t.TempDir()
		for file, src := range c.files {
			if err := os.WriteFile(filepath.Join(dir, file), []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		added, err := tiles.LoadDir(dir)
		if err == nil || !strings.Contains(err.Error(), c.want) || len(added) != 0 {
			t.Errorf("%s: want %q, got %v (%v added)", name, c.want, err, added)
		}
	}
	if _, err := tiles.LoadDir(filepath.Join(t.TempDir(), "none")); err == nil || !strings.Contains(err.Error(), "not a folder of tile files") {
		t.Errorf("a folder that is not there: %v", err)
	}
	if len(compile.Backends) != before || compile.Backends["rocks"] != nil {
		t.Errorf("a refused folder registered something: %d backends, was %d", len(compile.Backends), before)
	}

	// And the flag: only at the front, once per folder, and it needs its folder.
	if rest, err := ownTiles([]string{"explain", "-tiles", "x"}); err != nil || strings.Join(rest, " ") != "explain -tiles x" {
		t.Errorf("-tiles after the command belongs to the command: %v %v", rest, err)
	}
	if rest, err := ownTiles(nil); err != nil || len(rest) != 0 {
		t.Errorf("no arguments: %v %v", rest, err)
	}
	for _, args := range [][]string{{"-tiles"}, {"--tiles"}} {
		if _, err := ownTiles(args); err == nil || !strings.Contains(err.Error(), "-tiles needs a folder") {
			t.Errorf("%v: %v", args, err)
		}
	}
	for _, args := range [][]string{{"-tiles", "/no/such"}, {"-tiles=/no/such", "tiles"}, {"--tiles=/no/such"}} {
		if _, err := ownTiles(args); err == nil || !strings.Contains(err.Error(), "not a folder of tile files") {
			t.Errorf("%v: %v", args, err)
		}
	}
}

// A team's own tile, in a folder of its own, is a tile like any built in:
// listed, explained, and generated from. This runs tilegen itself, since a
// tile once registered stays for the life of the program.
func TestOwnTilesTakePart(t *testing.T) {
	if testing.Short() {
		t.Skip("builds tilegen")
	}
	bin := filepath.Join(t.TempDir(), "tilegen")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	pebble, err := os.ReadFile(filepath.Join("internal", "tiles", "store_pebble.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	own := t.TempDir()
	rocks := strings.NewReplacer("(tile pebble", "(tile rocks", "(offers store)", "(offers store)\n  (alias rk)",
		"(llm-work 5", "(llm-work 0", "(maintenance 3", "(maintenance 0", "(dependency 3", "(dependency 0", "(runtime 2", "(runtime 0").Replace(string(pebble))
	if err := os.WriteFile(filepath.Join(own, "rocks.sexp"), []byte(rocks), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.sexp")
	if err := os.WriteFile(spec, []byte(`(project notes (module example.com/notes) (go 1.22)
  (package notes (entity Note (field ID string) (field Title string) (store get save (durable)))))
`), 0o644); err != nil {
		t.Fatal(err)
	}
	tilegen := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// Without the folder there is no such tile; with it, it is listed, and
	// being free it wins the store.
	if out, _ := tilegen("tiles"); strings.Contains(out, "rocks") {
		t.Errorf("rocks is listed without its folder:\n%s", out)
	}
	if out, err := tilegen("-tiles", own, "tiles"); err != nil || !strings.Contains(out, "rocks") {
		t.Errorf("rocks is not listed: %v\n%s", err, out)
	}
	out, err := tilegen("-tiles="+own, "explain", "spec.sexp")
	if err != nil || !strings.Contains(out, "chosen  rocks") {
		t.Fatalf("explain with the folder: %v\n%s", err, out)
	}
	if out, err := tilegen("-tiles", own, "-out", "out", "spec.sexp"); err != nil {
		t.Fatalf("generate: %v\n%s", err, out)
	}
	store, err := os.ReadFile(filepath.Join(dir, "out", "notes", "rk_note_store.go"))
	if err != nil {
		found, _ := filepath.Glob(filepath.Join(dir, "out", "notes", "*"))
		t.Fatalf("the store on rocks: %v (have %v)", err, found)
	}
	for _, want := range []string{"type RkNoteStore struct", "db     *pebble.DB", `return &RkNoteStore{db: db, prefix: "notes/"}`} {
		if !strings.Contains(string(store), want) {
			t.Errorf("the generated store has no %q:\n%s", want, store)
		}
	}
	// Its hints are what the model is told for each method.
	if tasks, _ := os.ReadFile(filepath.Join(dir, "out", "tilegen.tasks.json")); !strings.Contains(string(tasks), "Map pebble.ErrNotFound to ErrNotFound") {
		t.Errorf("the tasks do not carry the tile's hint for get:\n%s", tasks)
	}
	// A later run without the folder does not know the tile the lock
	// names: it says so, and chooses again from the tiles it has.
	if out, err := tilegen("-out", "out", "spec.sexp"); err != nil || !strings.Contains(out, `tile "rocks" offers no store any more; re-selected`) {
		t.Errorf("a project locked to rocks, generated without it: %v\n%s", err, out)
	}
}
