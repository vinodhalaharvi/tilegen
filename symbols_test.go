//go:build !js

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// A small module with one of everything the reader has to get right.
var symbolFixture = map[string]string{
	"go.mod": "module example.com/kv\n\ngo 1.22\n",
	"kv.go": `// Package kv is a key-value store. It is small.
package kv

import (
	"errors"
	"io"
)

// Reader reads a value. It never blocks.
type Reader interface {
	// Get returns the value under key.
	Get(key string) ([]byte, error)
}

// Store reads, writes and closes.
type Store interface {
	Reader
	io.Closer
	Put(key string, value []byte, opts ...Option) error
}

// Number is a constraint, not something to implement.
type Number interface{ ~int | ~float64 }

// File is a Store kept in one file.
type File struct {
	Path   string
	hidden int
}

// Open opens the store at path.
func Open(path string) (*File, error) { return &File{Path: path}, nil }

// Must is Open that panics.
func Must(f *File, err error) *File { return f }

func (f *File) Get(key string) ([]byte, error) { return nil, ErrNotFound }

// Put writes a value.
func (f *File) Put(key string, value []byte, opts ...Option) error { return nil }
func (f *File) Close() error                                        { return nil }
func (f *File) secret()                                             {}

// Mem is a Reader only: it has no Put.
type Mem map[string][]byte

func (m Mem) Get(key string) ([]byte, error) { return m[key], nil }

// Option configures a Put.
type Option func(*File)

// Handle is another name for File.
type Handle = File

// Set is generic.
type Set[T comparable] struct{ Items map[T]struct{} }

func (s *Set[T]) Add(v T) {}

// Mode is how a store is opened.
type Mode int

const (
	ReadOnly Mode = iota
	ReadWrite
)

// ErrNotFound is returned when a key has no value.
var ErrNotFound = errors.New("kv: not found")

// Default is not an error.
var Default = Mem{}

// ConflictError says a write lost a race.
type ConflictError struct{ Key string }

func (e *ConflictError) Error() string { return e.Key }

func unexported() {}
`,
	"codec/codec.go": `// Package codec turns values into bytes.
package codec

// Codec encodes and decodes.
type Codec interface {
	Encode(v any) ([]byte, error)
}

// JSON is the JSON codec.
type JSON struct{}

func (JSON) Encode(v any) ([]byte, error) { return nil, nil }
`,
	"internal/wal/wal.go":   "package wal\n\ntype Log struct{}\n",
	"cmd/kvctl/main.go":     "package main\n\nfunc main() {}\n",
	"examples/basic/doc.go": "package basic\n\ntype Demo struct{}\n",
	"kv_test.go":            "package kv\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n\ntype OnlyInTests struct{}\n",
}

func fixtureSymbols(t *testing.T) *ModuleSymbols {
	t.Helper()
	dir := writeModule(t, symbolFixture)
	m, err := readModule(dir, "example.com/kv", "v0.1.0", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// symbolsStubbed says a test has chosen what reading a module returns.
var symbolsStubbed bool

// stubSymbols makes every reader of symbols in tilegen see this answer.
func stubSymbols(t *testing.T, m *ModuleSymbols, err error) *int {
	t.Helper()
	calls := new(int)
	old := readSymbols
	readSymbols = func(mod, version string) (*ModuleSymbols, error) {
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
	t.Cleanup(func() { readSymbols, symbolsStubbed = old, false })
	return calls
}

func iface(t *testing.T, p PackageSymbols, name string) InterfaceSym {
	t.Helper()
	for _, i := range p.Interfaces {
		if i.Name == name {
			return i
		}
	}
	t.Fatalf("no interface %s in %s", name, p.Path)
	return InterfaceSym{}
}

func typ(t *testing.T, p PackageSymbols, name string) TypeSym {
	t.Helper()
	for _, x := range p.Types {
		if x.Name == name {
			return x
		}
	}
	t.Fatalf("no type %s in %s", name, p.Path)
	return TypeSym{}
}

func names[T any](xs []T, name func(T) string) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = name(x)
	}
	return out
}

func funcNames(fs []FuncSym) []string {
	return names(fs, func(f FuncSym) string { return f.Name })
}

// What the reader says is what the type checker knows.
func TestSymbolsAreWhatTheTypeCheckerKnows(t *testing.T) {
	m := fixtureSymbols(t)
	if m.Module != "example.com/kv" || m.Version != "v0.1.0" {
		t.Fatalf("module %s %s", m.Module, m.Version)
	}
	// The packages a program could import, the module's own first; not
	// internal ones, commands, examples or tests.
	if got := names(m.Packages, func(p PackageSymbols) string { return p.Path }); !reflect.DeepEqual(got, []string{"example.com/kv", "example.com/kv/codec"}) {
		t.Fatalf("packages read: %v", got)
	}
	kv := m.Packages[0]
	if kv.Name != "kv" || kv.Doc != "Package kv is a key-value store." {
		t.Errorf("package: %q %q", kv.Name, kv.Doc)
	}

	// An interface is its whole method set: its own methods, and the ones
	// it embeds, from this package and from another.
	store := iface(t, kv, "Store")
	if got := funcNames(store.Methods); !reflect.DeepEqual(got, []string{"Close", "Get", "Put"}) {
		t.Errorf("Store's methods: %v", got)
	}
	put := store.Methods[2]
	if !put.Variadic || put.signature() != "Put(key string, value []byte, opts ...Option) error" {
		t.Errorf("Put: %s", put.signature())
	}
	if get := iface(t, kv, "Reader").Methods[0]; get.Doc != "Get returns the value under key." || !reflect.DeepEqual(get.Results, []string{"[]byte", "error"}) {
		t.Errorf("Reader.Get: %+v", get)
	}

	// Which type satisfies which interface is types.Implements: File does
	// both through pointer methods, Mem has no Put, a generic type and an
	// alias-free check are left alone.
	if got := store.ImplementedBy; !reflect.DeepEqual(got, []string{"File", "Handle"}) {
		t.Errorf("Store is implemented by %v", got)
	}
	if got := iface(t, kv, "Reader").ImplementedBy; !reflect.DeepEqual(got, []string{"File", "Handle", "Mem"}) {
		t.Errorf("Reader is implemented by %v", got)
	}
	if n := iface(t, kv, "Number"); !n.Constraint || len(n.ImplementedBy) != 0 || len(n.Methods) != 0 {
		t.Errorf("Number is a constraint: %+v", n)
	}

	file := typ(t, kv, "File")
	if file.Kind != "struct" || !reflect.DeepEqual(file.Fields, []ParamSym{{Name: "Path", Type: "string"}}) {
		t.Errorf("File's exported fields: %+v", file.Fields)
	}
	if got := funcNames(file.Methods); !reflect.DeepEqual(got, []string{"Close", "Get", "Put"}) {
		t.Errorf("File's methods: %v", got)
	}
	if !reflect.DeepEqual(file.Implements, []string{"Reader", "Store"}) || !reflect.DeepEqual(file.Constructors, []string{"Must", "Open"}) || file.IsError {
		t.Errorf("File: implements %v, from %v", file.Implements, file.Constructors)
	}
	if file.Methods[2].Doc != "Put writes a value." {
		t.Errorf("a method's doc: %q", file.Methods[2].Doc)
	}
	if mem := typ(t, kv, "Mem"); mem.Kind != "map" || mem.Underlying != "map[string][]byte" || !reflect.DeepEqual(mem.Implements, []string{"Reader"}) {
		t.Errorf("Mem: %+v", mem)
	}
	if h := typ(t, kv, "Handle"); h.Alias != "File" || len(h.Methods) != 0 {
		t.Errorf("Handle is an alias: %+v", h)
	}
	if s := typ(t, kv, "Set"); s.TypeParams != "[T comparable]" || len(s.Implements) != 0 || !reflect.DeepEqual(funcNames(s.Methods), []string{"Add"}) {
		t.Errorf("Set: %+v", s)
	}
	if o := typ(t, kv, "Option"); o.Kind != "func" || o.Underlying != "func(*File)" {
		t.Errorf("Option: %+v", o)
	}
	if c := typ(t, kv, "ConflictError"); !c.IsError {
		t.Error("ConflictError has an Error method and is not marked an error")
	}
	if md := typ(t, kv, "Mode"); md.Kind != "int" {
		t.Errorf("Mode: %+v", md)
	}

	// Functions, and values: a sentinel error is known by its type.
	if got := funcNames(kv.Funcs); !reflect.DeepEqual(got, []string{"Must", "Open"}) {
		t.Errorf("funcs: %v", got)
	}
	vals := map[string]ValueSym{}
	for _, v := range kv.Values {
		vals[v.Name] = v
	}
	if v := vals["ErrNotFound"]; !v.IsError || v.Kind != "var" || v.Type != "error" {
		t.Errorf("ErrNotFound: %+v", v)
	}
	if v := vals["Default"]; v.IsError || v.Type != "Mem" {
		t.Errorf("Default: %+v", v)
	}
	if v := vals["ReadWrite"]; v.Kind != "const" || v.Type != "Mode" || v.Value != "1" {
		t.Errorf("ReadWrite: %+v", v)
	}

	// Nothing unexported, and nothing from a test file.
	raw, _ := json.Marshal(m)
	for _, not := range []string{"hidden", "secret", "unexported", "OnlyInTests", "wal", "kvctl", "Demo"} {
		if bytes.Contains(raw, []byte(`"`+not+`"`)) {
			t.Errorf("the symbols carry %q", not)
		}
	}

	codec := m.Packages[1]
	if got := iface(t, codec, "Codec").ImplementedBy; !reflect.DeepEqual(got, []string{"JSON"}) {
		t.Errorf("Codec is implemented by %v", got)
	}
}

func TestSymbolsAnswerQuestions(t *testing.T) {
	m := fixtureSymbols(t)
	for _, ref := range []string{"kv.Store", "kv.Store.Put", "kv.Store.Close", "kv.File", "kv.File.Path", "kv.File.Get", "kv.Open", "kv.ErrNotFound", "kv.ReadOnly", "example.com/kv.Store", "codec.JSON", "example.com/kv/codec.Codec.Encode"} {
		if !m.has(ref) {
			t.Errorf("%s is exported and has says no", ref)
		}
	}
	for _, ref := range []string{"kv.Stor", "kv.Store.Delete", "kv.File.hidden", "kv.Open.Path", "kv.unexported", "wal.Log", "kv.", "Store", "other.Store", "kv.OnlyInTests", "codec.Store"} {
		if m.has(ref) {
			t.Errorf("%s is not exported and has says yes", ref)
		}
	}

	// A draft is held to the module it names.
	ok := &Offer{Tile: "kv", Module: "example.com/kv", Symbols: []string{"kv.Store", "kv.File.Put", "kv.ErrNotFound"}}
	if err := checkCited(ok, m); err != nil {
		t.Errorf("real symbols were refused: %v", err)
	}
	bad := &Offer{Tile: "kv", Module: "example.com/kv", Symbols: []string{"kv.Store", "kv.Stores", "kv.Transaction"}}
	err := checkCited(bad, m)
	if err == nil || !strings.Contains(err.Error(), "example.com/kv v0.1.0 does not export kv.Stores (did you mean kv.Store?), kv.Transaction") {
		t.Errorf("invented symbols: %v", err)
	}

	// The index is names; a detail is one symbol in full.
	idx := m.index()
	if len(idx.Packages) != 2 || !reflect.DeepEqual(idx.Packages[0].Errors, []string{"ErrNotFound"}) || idx.Packages[0].Values != 3 {
		t.Errorf("index: %+v", idx.Packages[0])
	}
	for _, i := range idx.Packages[0].Interfaces {
		if i.Name == "Store" && (i.Methods != 3 || !reflect.DeepEqual(i.ImplementedBy, []string{"File", "Handle"})) {
			t.Errorf("index of Store: %+v", i)
		}
	}
	if d, ok := m.detail("kv.Store"); !ok || d.Interface == nil || d.Package != "example.com/kv" || len(d.Interface.Methods) != 3 {
		t.Errorf("detail of kv.Store: %+v", d)
	}
	if d, ok := m.detail("kv.Open"); !ok || d.Func == nil || d.Func.signature() != "Open(path string) (*File, error)" {
		t.Errorf("detail of kv.Open: %+v", d)
	}
	if d, ok := m.detail("kv.ErrNotFound"); !ok || d.Value == nil || !d.Value.IsError {
		t.Errorf("detail of kv.ErrNotFound: %+v", d)
	}
	if d, ok := m.detail("codec.JSON"); !ok || d.Type == nil {
		t.Errorf("detail of codec.JSON: %+v", d)
	}
	if _, ok := m.detail("kv.Nope"); ok {
		t.Error("detail of a symbol that is not there")
	}

	// The listing for a prompt says what matters, and says when it stops.
	text := m.text(1 << 20)
	for _, want := range []string{
		"example.com/kv v0.1.0, as the Go type checker reads it:",
		"package kv // example.com/kv",
		"  interface Store (implemented by File, Handle)\n    Close() error\n    Get(key string) ([]byte, error)\n    Put(key string, value []byte, opts ...Option) error\n",
		"  type File struct, from Must, Open, implements Reader, Store; methods: Close, Get, Put\n",
		"  error ErrNotFound\n",
		"package codec // example.com/kv/codec",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the listing has no %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Number") {
		t.Error("the listing offers a constraint as something to implement")
	}
	short := m.text(300)
	if len(short) > 300+len("... (more symbols than fit here)\n") || !strings.Contains(short, "more symbols than fit here") {
		t.Errorf("a listing over its budget: %d chars\n%s", len(short), short)
	}

	// Kinds.
	only := fixtureSymbols(t)
	only.only([]string{"interface"})
	if p := only.Packages[0]; len(p.Interfaces) != 3 || p.Types != nil || p.Funcs != nil || p.Values != nil {
		t.Errorf("-kind interface kept: %d interfaces, %d types, %d funcs, %d values", len(p.Interfaces), len(p.Types), len(p.Funcs), len(p.Values))
	}
	if _, err := parseKinds("interface,typ"); err == nil || !strings.Contains(err.Error(), "did you mean type?") {
		t.Errorf("a mistyped kind: %v", err)
	}
}

func TestSymbolsRefuseWhatTheyCannotRead(t *testing.T) {
	for mod, want := range map[string]string{
		"":                "not a module path",
		"not a module":    "not a module path",
		"../../etc":       "not a module path",
		"-modfile=x":      "not a module path",
		"example.com/ok;": "not a module path",
	} {
		if _, err := moduleSymbols(mod, ""); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("module %q: %v", mod, err)
		}
	}
	for _, v := range []string{"v1 && rm", "-u", "v1@v2", "a\nb"} {
		if _, err := moduleSymbols("example.com/kv", v); err == nil || !strings.Contains(err.Error(), "is not a version") {
			t.Errorf("version %q: %v", v, err)
		}
	}
	// A module that does not type-check is said so, not half read.
	broken := writeModule(t, map[string]string{"go.mod": "module example.com/b\n\ngo 1.22\n", "b.go": "package b\n\nvar X int = \"no\"\n"})
	if _, err := readModule(broken, "example.com/b", "v0", nil, nil); err == nil || !strings.Contains(err.Error(), "no package of example.com/b type-checks") {
		t.Errorf("a broken module: %v", err)
	}
	empty := writeModule(t, map[string]string{"go.mod": "module example.com/e\n\ngo 1.22\n", "cmd/x/main.go": "package main\n\nfunc main() {}\n"})
	if _, err := readModule(empty, "example.com/e", "v0", nil, nil); err == nil || !strings.Contains(err.Error(), "has no package a program can import") {
		t.Errorf("a module of commands: %v", err)
	}
}

// Reading a module leaves the directory it was read from as it was.
func TestReadingSymbolsChangesNothing(t *testing.T) {
	dir := writeModule(t, symbolFixture)
	before := snapshot(t, dir)
	if _, err := readModule(dir, "example.com/kv", "v0.1.0", nil, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshot(t, dir)) {
		t.Error("reading symbols changed the module")
	}
}

// A real module, fetched with the go tool: the one tilegen itself requires.
func TestSymbolsOfARealModule(t *testing.T) {
	if testing.Short() {
		t.Skip("fetches a module")
	}
	m, err := moduleSymbols("golang.org/x/mod", "v0.41.0")
	if err != nil {
		t.Skipf("the module could not be fetched here: %v", err)
	}
	if m.Version != "v0.41.0" {
		t.Errorf("resolved %s", m.Version)
	}
	for _, ref := range []string{"module.CheckPath", "module.Version.Path", "modfile.File.AddRequire", "semver.Compare", "golang.org/x/mod/modfile.Parse"} {
		if !m.has(ref) {
			t.Errorf("x/mod exports %s and has says no", ref)
		}
	}
	for _, p := range m.Packages {
		if strings.Contains(p.Path, "/internal/") {
			t.Errorf("an internal package was read: %s", p.Path)
		}
	}
	// Asked twice, fetched once.
	n := 0
	read := rememberSymbols(func(mod, v string) (*ModuleSymbols, error) { n++; return m, nil }, 1<<40)
	read("a", "v1")
	read("a", "v1")
	read("a", "v2")
	if n != 2 {
		t.Errorf("fetched %d times for two versions", n)
	}
}

func TestSymbolsCommand(t *testing.T) {
	m := fixtureSymbols(t)
	old := moduleSymbolsForCmd
	moduleSymbolsForCmd = func(mod, v string) (*ModuleSymbols, error) {
		if mod != "example.com/kv" || v != "v0.1.0" {
			return nil, errors.New("asked for " + mod + "@" + v)
		}
		return fixtureSymbols(t), nil
	}
	t.Cleanup(func() { moduleSymbolsForCmd = old })

	var out, errOut bytes.Buffer
	if err := symbolsCmd([]string{"example.com/kv@v0.1.0"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if out.String() != strings.TrimRight(m.text(1<<30), "\n")+"\n" {
		t.Errorf("the listing:\n%s", out.String())
	}
	out.Reset()
	if err := symbolsCmd([]string{"-json", "-kind", "interface", "example.com/kv@v0.1.0"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	var got []ModuleSymbols
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || len(got) != 1 || len(got[0].Packages[0].Interfaces) != 3 || got[0].Packages[0].Types != nil {
		t.Errorf("-json -kind interface: %v\n%s", err, out.String())
	}
	if err := symbolsCmd(nil, &out, &errOut); err == nil {
		t.Error("no module given was accepted")
	}
	if err := symbolsCmd([]string{"-kind", "enum", "example.com/kv"}, &out, &errOut); err == nil || !strings.Contains(err.Error(), "-kind takes") {
		t.Errorf("a kind that is not one: %v", err)
	}
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

	offers, err := parseDrafts(with("(symbols kv.Store kv.File.Put example.com/kv.ErrNotFound)"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(offers[0].Symbols, []string{"kv.Store", "kv.File.Put", "example.com/kv.ErrNotFound"}) {
		t.Errorf("cited: %v", offers[0].Symbols)
	}
	if offers, err = parseDrafts(with("")); err != nil || offers[0].Symbols != nil {
		t.Errorf("a draft that cites nothing: %v", err)
	}
	// What a draft says round-trips through the form tilegen shows.
	offers, _ = parseDrafts(with("(symbols kv.Store kv.Open)"))
	if form := draftForm(offers[0]); !strings.Contains(form, "\n  (symbols kv.Store kv.Open)") {
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
		if _, err := parseDrafts(with(c.symbols)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want %q, got: %v", name, c.want, err)
		}
	}
	noModule := strings.Replace(with("(symbols kv.Store)"), "(module example.com/kv)", "", 1)
	if _, err := parseDrafts(noModule); err == nil || !strings.Contains(err.Error(), "cites symbols and names no (module ...)") {
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
	d := draftModule(ctx, "sk-ant-test", "example.com/kv", "v0.1.0")
	if d.Error != "" || d.Name != "kv-file" || calls.Load() != 1 || d.Symbols != len(m.names()) || d.FromMemory != "" {
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
	d = draftModule(ctx, "sk-ant-test", "example.com/kv", "v0.1.0")
	if d.Error != "" || d.Name != "kv-file" || calls.Load() != 2 || !strings.Contains(*last, "does not export kv.Transaction") {
		t.Fatalf("an invented symbol, corrected: %+v after %d call(s)", d, calls.Load())
	}
	calls, _ = fakeClaude(t, invented)
	d = draftModule(ctx, "sk-ant-test", "example.com/kv", "v0.1.0")
	if d.Tile != "" || calls.Load() != 2 || !strings.Contains(d.Error, "does not export kv.Transaction") {
		t.Fatalf("an invented symbol, kept: %+v", d)
	}
	// Shown the symbols, a draft that cites none is not accepted.
	calls, last = fakeClaude(t, uncited, good)
	d = draftModule(ctx, "sk-ant-test", "example.com/kv", "v0.1.0")
	if d.Error != "" || calls.Load() != 2 || !strings.Contains(*last, "cite in (symbols ...) the ones the claims rest on") {
		t.Fatalf("an uncited draft: %+v after %d call(s)", d, calls.Load())
	}

	// When the module cannot be read the draft says it is from memory, and
	// cannot pretend to cite anything.
	stubSymbols(t, nil, errors.New("cannot fetch example.com/kv@v0.1.0: no network"))
	calls, last = fakeClaude(t, uncited)
	d = draftModule(ctx, "sk-ant-test", "example.com/kv", "v0.1.0")
	if d.Error != "" || d.Name != "kv-file" || d.Symbols != 0 || !strings.Contains(d.FromMemory, "could not be read (cannot fetch example.com/kv@v0.1.0: no network)") {
		t.Fatalf("a draft from memory: %+v", d)
	}
	if strings.Contains(*last, "Symbols:") {
		t.Error("no symbols were read and the request claims some")
	}
	calls, _ = fakeClaude(t, good)
	d = draftModule(ctx, "sk-ant-test", "example.com/kv", "v0.1.0")
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
	if err := importTiles(dir, "tilespecs", false, true, &out); err != nil {
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
	if _, err := Parse("kv.sexp", string(kv)); err != nil {
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
	if err := importTiles(plain, "tilespecs", false, false, &out); err != nil {
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
	read := func(mod, v string) (*ModuleSymbols, error) {
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

	// symbols: the index, then the ones asked for
	sym := symbols(read)
	out, err := sym(ctx, SymbolsIn{Module: "example.com/kv", Version: "v0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Index == nil || out.Details != nil || out.Version != "v0.1.0" || !reflect.DeepEqual(*out.Index, m.index()) {
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
	slow := symbols(func(string, string) (*ModuleSymbols, error) { select {} })
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
