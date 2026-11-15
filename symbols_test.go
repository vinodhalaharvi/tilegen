//go:build !js

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/importer"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
	"github.com/vinodhalaharvi/tilegen/internal/symbols"
	"github.com/vinodhalaharvi/tilegen/internal/symbols/symbolstest"
	"github.com/vinodhalaharvi/tilegen/internal/tiles"
)

func fixtureSymbols(t *testing.T) *symbols.ModuleSymbols {
	t.Helper()
	return symbolstest.Read(t)
}

// symbolsStubbed says a test has chosen what reading a module returns.
var symbolsStubbed bool

// stubSymbols makes every reader of symbols in tilegen see this answer.
func stubSymbols(t *testing.T, m *symbols.ModuleSymbols, err error) *int {
	t.Helper()
	calls := new(int)
	old := symbols.Read
	symbols.Read = func(mod, version string) (*symbols.ModuleSymbols, error) {
		*calls++
		if err != nil {
			return nil, err
		}
		if mod != m.Module {
			return nil, errors.New("cannot fetch " + mod + ": no such module in this test")
		}
		return m, nil
	}
	symbolsStubbed = true
	t.Cleanup(func() { symbols.Read, symbolsStubbed = old, false })
	return calls
}

// The draft form may cite symbols, and says what is wrong when it cannot.
func TestDraftCitesSymbols(t *testing.T) {
	const tile = `(tile kv-file
  (offers store)
  (doc "a key-value store in one file")
  (module example.com/kv)
  SYMBOLS
  (satisfies (durable) "Put returns after the write is on disk")
  (illegal-when (cross-process) "one process holds the file")
  (cost (llm-work 4) (maintenance 2) (dependency 1)))`
	with := func(s string) string { return strings.Replace(tile, "SYMBOLS", s, 1) }

	offers, err := compile.ParseDrafts(with("(symbols kv.Store kv.File.Put example.com/kv.ErrNotFound)"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(offers[0].Symbols, []string{"kv.Store", "kv.File.Put", "example.com/kv.ErrNotFound"}) {
		t.Errorf("cited: %v", offers[0].Symbols)
	}
	if offers, err = compile.ParseDrafts(with("")); err != nil || offers[0].Symbols != nil {
		t.Errorf("a draft that cites nothing: %v", err)
	}
	// What a draft says round-trips through the form tilegen shows.
	offers, _ = compile.ParseDrafts(with("(symbols kv.Store kv.Open)"))
	if form := importer.DraftForm(offers[0]); !strings.Contains(form, "\n  (symbols kv.Store kv.Open)") {
		t.Errorf("the form drops the symbols:\n%s", form)
	}
	for name, c := range map[string]struct{ symbols, want string }{
		"nothing cited":      {"(symbols)", "expected (symbols pkg.Name ...)"},
		"no package":         {"(symbols Store)", "a symbol is written pkg.Name"},
		"unexported":         {"(symbols kv.store)", "a symbol is written pkg.Name"},
		"too deep":           {"(symbols kv.File.Put.Now)", "a symbol is written pkg.Name"},
		"a list":             {"(symbols (kv.Store))", "a symbol is written pkg.Name"},
		"cited twice":        {"(symbols kv.Store kv.Store)", "kv.Store is cited twice"},
		"two symbols forms":  {"(symbols kv.Store) (symbols kv.Open)", "duplicate (symbols ...)"},
		"a mistyped heading": {"(symbol kv.Store)", "did you mean symbols?"},
	} {
		if _, err := compile.ParseDrafts(with(c.symbols)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want %q, got: %v", name, c.want, err)
		}
	}
	noModule := strings.Replace(with("(symbols kv.Store)"), "(module example.com/kv)", "", 1)
	if _, err := compile.ParseDrafts(noModule); err == nil || !strings.Contains(err.Error(), "cites symbols and names no (module ...)") {
		t.Errorf("symbols without a module: %v", err)
	}
}

const kvDraft = `(tile kv-file
  (offers store)
  (doc "a key-value store in one file")
  (module example.com/kv)
  (symbols CITED)
  (satisfies (durable) "Put returns after the write is on disk")
  (illegal-when (cross-process) "one process holds the file")
  (unverified (lookup-by-field) "it has keys only")
  (unverified (no-broker) "nothing to run, as far as its API shows")
  (unverified (outbox) "it has no transactions")
  (unverified (inbox) "it has no transactions")
  (cost (llm-work 4) (maintenance 2) (dependency 1)))`

// A draft is written from the module's symbols and held to them.
func TestClaudeDraftsFromSymbols(t *testing.T) {
	ctx := t.Context()
	m := fixtureSymbols(t)
	good := strings.Replace(kvDraft, "CITED", "kv.Store kv.File.Put", 1)
	invented := strings.Replace(kvDraft, "CITED", "kv.Store kv.Transaction", 1)
	uncited := strings.Replace(kvDraft, "(symbols CITED)\n", "", 1)

	// The request carries what the type checker read; a draft citing it passes.
	stubSymbols(t, m, nil)
	calls, last := fakeClaude(t, good)
	d := importer.DraftModule(ctx, "sk-ant-test", "example.com/kv", "v0.1.0")
	if d.Error != "" || d.Name != "kv-file" || calls.Load() != 1 || d.Symbols != len(m.Names()) || d.FromMemory != "" {
		t.Fatalf("a grounded draft: %+v after %d call(s)", d, calls.Load())
	}
	sent := strings.ReplaceAll(*last, `\n`, "\n")
	for _, want := range []string{"Module: example.com/kv\nVersion: v0.1.0\n\nSymbols:\nexample.com/kv v0.1.0, as the Go type checker reads it:", "interface Store (implemented by File, Handle)", "error ErrNotFound", "(symbols pkg.Name pkg.Type.Method ...)", "tilegen checks each one against the module"} {
		if !strings.Contains(sent, want) {
			t.Errorf("the request should carry %q", want)
		}
	}

	// A symbol the module does not have goes back with the reason, once.
	calls, last = fakeClaude(t, invented, good)
	d = importer.DraftModule(ctx, "sk-ant-test", "example.com/kv", "v0.1.0")
	if d.Error != "" || d.Name != "kv-file" || calls.Load() != 2 || !strings.Contains(*last, "does not export kv.Transaction") {
		t.Fatalf("an invented symbol, corrected: %+v after %d call(s)", d, calls.Load())
	}
	calls, _ = fakeClaude(t, invented)
	d = importer.DraftModule(ctx, "sk-ant-test", "example.com/kv", "v0.1.0")
	if d.Tile != "" || calls.Load() != 2 || !strings.Contains(d.Error, "does not export kv.Transaction") {
		t.Fatalf("an invented symbol, kept: %+v", d)
	}
	// Shown the symbols, a draft that cites none is not accepted.
	calls, last = fakeClaude(t, uncited, good)
	d = importer.DraftModule(ctx, "sk-ant-test", "example.com/kv", "v0.1.0")
	if d.Error != "" || calls.Load() != 2 || !strings.Contains(*last, "cite in (symbols ...) the ones the claims rest on") {
		t.Fatalf("an uncited draft: %+v after %d call(s)", d, calls.Load())
	}

	// When the module cannot be read the draft says it is from memory, and
	// cannot pretend to cite anything.
	stubSymbols(t, nil, errors.New("cannot fetch example.com/kv@v0.1.0: no network"))
	calls, last = fakeClaude(t, uncited)
	d = importer.DraftModule(ctx, "sk-ant-test", "example.com/kv", "v0.1.0")
	if d.Error != "" || d.Name != "kv-file" || d.Symbols != 0 || !strings.Contains(d.FromMemory, "could not be read (cannot fetch example.com/kv@v0.1.0: no network)") {
		t.Fatalf("a draft from memory: %+v", d)
	}
	if strings.Contains(*last, "Symbols:") {
		t.Error("no symbols were read and the request claims some")
	}
	calls, _ = fakeClaude(t, good)
	d = importer.DraftModule(ctx, "sk-ant-test", "example.com/kv", "v0.1.0")
	if d.Tile != "" || !strings.Contains(d.Error, "none can be cited") {
		t.Fatalf("citing symbols nobody read: %+v", d)
	}
}

// import -tiles -symbols puts the facts beside the TODOs.
func TestImportTilesWithSymbols(t *testing.T) {
	m := fixtureSymbols(t)
	stubSymbols(t, m, nil)
	dir := writeModule(t, map[string]string{"go.mod": "module example.com/app\n\ngo 1.22\n\nrequire (\n\texample.com/kv v0.1.0\n\texample.com/gone v1.0.0\n)\n"})
	var out bytes.Buffer
	if err := importer.Tiles(dir, "tilespecs", false, true, &out); err != nil {
		t.Fatal(err)
	}
	kv, err := os.ReadFile(filepath.Join(dir, "tilespecs", "kv.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"(tile-spec kv", "; What the module exports. `tilegen symbols example.com/kv@v0.1.0` prints all of it.", ";   interface Store (implemented by File, Handle)", ";     Put(key string, value []byte, opts ...Option) error", ";   error ErrNotFound"} {
		if !strings.Contains(string(kv), want) {
			t.Errorf("the skeleton has no %q", want)
		}
	}
	if _, err := sexp.Parse("kv.sexp", string(kv)); err != nil {
		t.Errorf("the skeleton with symbols no longer parses: %v", err)
	}
	for _, line := range strings.Split(string(kv), "\n") {
		if strings.HasSuffix(line, " ") {
			t.Errorf("trailing space: %q", line)
		}
	}
	gone, _ := os.ReadFile(filepath.Join(dir, "tilespecs", "gone.sexp"))
	if !strings.Contains(string(gone), "; The module's symbols could not be read: cannot fetch example.com/gone") {
		t.Errorf("a module that cannot be read should say so:\n%s", gone)
	}
	// Without the flag the skeleton is what go.mod states and nothing more.
	plain := writeModule(t, map[string]string{"go.mod": "module example.com/app\n\ngo 1.22\n\nrequire example.com/kv v0.1.0\n"})
	calls := stubSymbols(t, m, nil)
	if err := importer.Tiles(plain, "tilespecs", false, false, &out); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(plain, "tilespecs", "kv.sexp")); strings.Contains(string(b), "What the module exports") || *calls != 0 {
		t.Error("symbols were read without -symbols")
	}
}

// The MCP tools: a go.mod, a module's symbols, and a draft held to both.
func TestMCPSymbolTools(t *testing.T) {
	ctx := context.Background()
	m := fixtureSymbols(t)
	read := func(mod, v string) (*symbols.ModuleSymbols, error) {
		if mod != "example.com/kv" {
			return nil, errors.New("cannot fetch " + mod)
		}
		return m, nil
	}

	// gomod
	g, err := gomod()(ctx, GoModIn{GoMod: "module example.com/app\n\ngo 1.22\n\nrequire (\n\tgithub.com/jackc/pgx/v5 v5.7.2\n\texample.com/kv v0.1.0\n\tgolang.org/x/text v0.3.0 // indirect\n)\n"})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Known) != 1 || g.Known[0].Module != "github.com/jackc/pgx/v5" || len(g.Unknown) != 1 || g.Unknown[0].Module != "example.com/kv" || len(g.Running) != 1 || g.Running[0].Engine != "postgres" {
		t.Errorf("gomod: known %+v, unknown %+v, running %+v", g.Known, g.Unknown, g.Running)
	}
	for _, bad := range []string{"", "   ", "this is not a go.mod {"} {
		if _, err := gomod()(ctx, GoModIn{GoMod: bad}); err == nil {
			t.Errorf("gomod accepted %q", bad)
		}
	}
	if _, err := gomod()(ctx, GoModIn{GoMod: "this is not a go.mod {"}); err == nil || strings.Count(err.Error(), "not a go.mod") != 1 || !strings.HasPrefix(err.Error(), "that is not a go.mod: ") {
		t.Errorf("the refusal should say it once: %v", err)
	}

	// symbols: the index, then the ones asked for
	sym := symbolsTool(read)
	out, err := sym(ctx, SymbolsIn{Module: "example.com/kv", Version: "v0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Index == nil || out.Details != nil || out.Version != "v0.1.0" || !reflect.DeepEqual(*out.Index, m.Index()) {
		t.Errorf("the index: %+v", out)
	}
	out, err = sym(ctx, SymbolsIn{Module: " example.com/kv ", Symbols: []string{"kv.Store", "kv.Open", "kv.Stor", "codec.JSON"}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Index != nil || len(out.Details) != 3 || out.Details[0].Interface == nil || len(out.Details[0].Interface.Methods) != 3 ||
		!reflect.DeepEqual(out.Missing, []string{"kv.Stor (did you mean kv.Store?)"}) {
		t.Errorf("details: %+v", out)
	}
	if _, err := sym(ctx, SymbolsIn{}); err == nil || !strings.Contains(err.Error(), "no module") {
		t.Errorf("no module: %v", err)
	}
	if _, err := sym(ctx, SymbolsIn{Module: "example.com/gone"}); err == nil || !strings.Contains(err.Error(), "cannot fetch example.com/gone") {
		t.Errorf("a module that is not there: %v", err)
	}
	if _, err := sym(ctx, SymbolsIn{Module: "example.com/kv", Symbols: make([]string, maxSymbolDetails+1)}); err == nil || !strings.Contains(err.Error(), "several calls") {
		t.Errorf("too many symbols: %v", err)
	}
	gone, cancel := context.WithCancel(ctx)
	cancel()
	slow := symbolsTool(func(string, string) (*symbols.ModuleSymbols, error) { select {} })
	if _, err := slow(gone, SymbolsIn{Module: "example.com/kv"}); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled call should stop waiting: %v", err)
	}

	// check_tile
	check := checkTile(read)
	draft := func(cited string) string { return strings.Replace(kvDraft, "CITED", cited, 1) }
	c, err := check(ctx, CheckTileIn{Tile: draft("kv.Store kv.File.Put")})
	if err != nil || !c.OK || !c.Verified || c.Cited != 2 || c.Name != "kv-file" || c.Capability != "store" || c.Version != "v0.1.0" || c.Problems != nil {
		t.Errorf("a draft that stands: %+v %v", c, err)
	}
	c, _ = check(ctx, CheckTileIn{Tile: draft("kv.Store kv.Transaction")})
	if c.OK || c.Verified || len(c.Problems) != 1 || !strings.Contains(c.Problems[0], "does not export kv.Transaction") {
		t.Errorf("an invented symbol: %+v", c)
	}
	c, _ = check(ctx, CheckTileIn{Tile: strings.Replace(draft("kv.Store"), "(durable)", "(durabel)", 1)})
	if c.OK || len(c.Problems) == 0 || !strings.Contains(strings.Join(c.Problems, "\n"), "did you mean durable?") {
		t.Errorf("a draft with a bad form: %+v", c)
	}
	c, _ = check(ctx, CheckTileIn{Tile: strings.Replace(kvDraft, "(symbols CITED)\n", "", 1)})
	if !c.OK || c.Verified || c.Cited != 0 || !strings.Contains(c.Note, "cites no symbols") {
		t.Errorf("a draft that cites nothing: %+v", c)
	}
	c, _ = check(ctx, CheckTileIn{Tile: strings.Replace(draft("gone.Thing"), "example.com/kv", "example.com/gone", 1)})
	if c.OK || c.Verified || !strings.Contains(strings.Join(c.Problems, " "), "could not be read") {
		t.Errorf("a draft whose module cannot be read: %+v", c)
	}
	for _, bad := range []string{"", draft("kv.Store") + "\n" + strings.Replace(draft("kv.Store"), "kv-file", "kv-two", 1)} {
		if c, _ = check(ctx, CheckTileIn{Tile: bad}); c.OK || len(c.Problems) == 0 {
			t.Errorf("check accepted %q", bad)
		}
	}

	// explain with a draft in the running: it can win, and is marked.
	spec := "(project p (module example.com/p) (go 1.22)\n  (package notes\n    (entity Note (field ID string) (store get save (durable)))))"
	cheap := strings.Replace(strings.Replace(draft("kv.Store"), "(llm-work 4)", "(llm-work 0)", 1), "(maintenance 2)", "(maintenance 0)", 1)
	got, err := explain()(ctx, ExplainIn{SpecIn: SpecIn{Spec: spec}, Tiles: cheap})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got)
	if !bytes.Contains(raw, []byte(`"tile":"kv-file","draft":true`)) {
		t.Errorf("the draft is not among the candidates:\n%s", raw)
	}
	without, _ := explain()(ctx, ExplainIn{SpecIn: SpecIn{Spec: spec}})
	if rawWithout, _ := json.Marshal(without); bytes.Contains(rawWithout, []byte("kv-file")) {
		t.Error("a draft from one call is still there in the next")
	}
	if _, err := explain()(ctx, ExplainIn{SpecIn: SpecIn{Spec: spec}, Tiles: "(tile x (offers store))"}); err == nil {
		t.Error("a bad draft was accepted by explain")
	}
}

// A skeleton assumes nothing about the module, and cannot be used until
// every question in it is answered.
func TestSkeletonIsQuestionsUntilAnswered(t *testing.T) {
	dir := writeModule(t, map[string]string{"go.mod": "module example.com/app\n\ngo 1.22\n\nrequire example.com/kv v0.1.0\n"})
	var out bytes.Buffer
	if err := importer.Tiles(dir, "tilespecs", false, false, &out); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "tilespecs", "kv.sexp")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	skel := string(raw)

	// Nothing is guessed: no kind, and no number that could pass for one.
	for _, want := range []string{"(kind TODO)", `(doc "TODO")`, "(llm-work TODO ", "(maintenance TODO ", "(dependency TODO ", "(runtime TODO ",
		"One of: " + strings.Join(tiles.TileKinds, ", ") + ".", "this module is not a tile: delete this file", "store-backend: " + importer.KindComparison("store-backend")} {
		if !strings.Contains(skel, want) {
			t.Errorf("the skeleton has no %q:\n%s", want, skel)
		}
	}
	for _, req := range compile.KnownRequirements() {
		if !strings.Contains(skel, " "+req) {
			t.Errorf("the skeleton does not offer the requirement %s", req)
		}
	}
	for _, line := range strings.Split(skel, "\n") {
		if len(line) > 100 || strings.HasSuffix(line, " ") {
			t.Errorf("a line too long or with trailing space: %q", line)
		}
	}
	if cmp := importer.KindComparison("store-backend"); !strings.Contains(cmp, "llm-work runs from ") || importer.KindComparison("no-such-kind") != "" {
		t.Errorf("the comparison: %q", cmp)
	}

	// As written it is refused, one line per open question, and writes nothing.
	target := t.TempDir()
	err = run(compile.Options{Spec: file, Out: target}, io.Discard)
	if err == nil {
		t.Fatal("a skeleton with its TODOs open was generated from")
	}
	for _, want := range []string{"(kind TODO) is still a TODO", `(doc "TODO") is still a TODO`, "(cost (llm-work TODO", "answer it before this tile can be used"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal should say %q:\n%v", want, err)
		}
	}
	if strings.Count(err.Error(), "is still a TODO") != 3 || strings.Contains(err.Error(), "unknown tile kind") || strings.Contains(err.Error(), "whole number") {
		t.Errorf("each open question should be reported once, and nothing else:\n%v", err)
	}
	if left, _ := os.ReadDir(target); len(left) != 0 {
		t.Errorf("the refused skeleton wrote %d entries", len(left))
	}

	// Answered, it is a tile-spec like any other.
	answered := skel
	for old, new := range map[string]string{
		"(kind TODO)": "(kind store-backend)", `(doc "TODO")`: `(doc "a key-value store in one file")`,
		"(llm-work TODO": "(llm-work 4", "(maintenance TODO": "(maintenance 2", "(dependency TODO": "(dependency 1", "(runtime TODO": "(runtime 1",
		`"TODO: how much must be written by hand?"`: `"get, put and delete by key"`, `"TODO: what goes wrong over months?"`: `"no schema to migrate"`,
		`"TODO: what does it pull in, and what must be installed?"`: `"one module, nothing to install"`, `"TODO: what has to be running?"`: `"nothing"`,
	} {
		if !strings.Contains(answered, old) {
			t.Fatalf("the skeleton has no %s to answer", old)
		}
		answered = strings.Replace(answered, old, new, 1)
	}
	if err := os.WriteFile(file, []byte(answered), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(compile.Options{Spec: file, Out: target}, io.Discard); err != nil {
		t.Fatalf("an answered skeleton was refused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "internal", "tiles", "tile_kv.go")); err != nil {
		t.Errorf("an answered skeleton generated no tile: %v", err)
	}
	// A TODO left in one place is still caught, and words that merely
	// contain it are not.
	for _, n := range []*sexp.Node{{Atom: "TODO"}, {Atom: "TODO: later"}, {Atom: "TODO later"}, {IsList: true, List: []*sexp.Node{{Atom: "doc"}, {IsList: true, List: []*sexp.Node{{Atom: "TODO"}}}}}} {
		if !tiles.Unanswered(n) {
			t.Errorf("%s should count as unanswered", compile.Short(n))
		}
	}
	for _, n := range []*sexp.Node{{Atom: "TODOS"}, {Atom: "no TODO here"}, {Atom: "todo"}, {IsList: true, List: []*sexp.Node{{Atom: "doc"}, {Atom: "done"}}}} {
		if tiles.Unanswered(n) {
			t.Errorf("%s should not count as unanswered", compile.Short(n))
		}
	}
}
