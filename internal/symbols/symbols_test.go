//go:build !js

package symbols_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/symbols"
	"github.com/vinodhalaharvi/tilegen/internal/symbols/symbolstest"
)

func iface(t *testing.T, p symbols.PackageSymbols, name string) symbols.InterfaceSym {
	t.Helper()
	for _, i := range p.Interfaces {
		if i.Name == name {
			return i
		}
	}
	t.Fatalf("no interface %s in %s", name, p.Path)
	return symbols.InterfaceSym{}
}

func typ(t *testing.T, p symbols.PackageSymbols, name string) symbols.TypeSym {
	t.Helper()
	for _, x := range p.Types {
		if x.Name == name {
			return x
		}
	}
	t.Fatalf("no type %s in %s", name, p.Path)
	return symbols.TypeSym{}
}

func names[T any](xs []T, name func(T) string) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = name(x)
	}
	return out
}

func funcNames(fs []symbols.FuncSym) []string {
	return names(fs, func(f symbols.FuncSym) string { return f.Name })
}

// What the reader says is what the type checker knows.
func TestSymbolsAreWhatTheTypeCheckerKnows(t *testing.T) {
	m := symbolstest.Read(t)
	if m.Module != "example.com/kv" || m.Version != "v0.1.0" {
		t.Fatalf("module %s %s", m.Module, m.Version)
	}
	// The packages a program could import, the module's own first; not
	// internal ones, commands, examples or tests.
	if got := names(m.Packages, func(p symbols.PackageSymbols) string { return p.Path }); !reflect.DeepEqual(got, []string{"example.com/kv", "example.com/kv/codec"}) {
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
	if !put.Variadic || put.Signature() != "Put(key string, value []byte, opts ...Option) error" {
		t.Errorf("Put: %s", put.Signature())
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
	if file.Kind != "struct" || !reflect.DeepEqual(file.Fields, []symbols.ParamSym{{Name: "Path", Type: "string"}}) {
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
	vals := map[string]symbols.ValueSym{}
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
	m := symbolstest.Read(t)
	for _, ref := range []string{"kv.Store", "kv.Store.Put", "kv.Store.Close", "kv.File", "kv.File.Path", "kv.File.Get", "kv.Open", "kv.ErrNotFound", "kv.ReadOnly", "example.com/kv.Store", "codec.JSON", "example.com/kv/codec.Codec.Encode"} {
		if !m.Has(ref) {
			t.Errorf("%s is exported and has says no", ref)
		}
	}
	for _, ref := range []string{"kv.Stor", "kv.Store.Delete", "kv.File.hidden", "kv.Open.Path", "kv.unexported", "wal.Log", "kv.", "Store", "other.Store", "kv.OnlyInTests", "codec.Store"} {
		if m.Has(ref) {
			t.Errorf("%s is not exported and has says yes", ref)
		}
	}

	// A draft is held to the module it names.
	ok := &compile.Offer{Tile: "kv", Module: "example.com/kv", Symbols: []string{"kv.Store", "kv.File.Put", "kv.ErrNotFound"}}
	if err := symbols.CheckCited(ok, m); err != nil {
		t.Errorf("real symbols were refused: %v", err)
	}
	bad := &compile.Offer{Tile: "kv", Module: "example.com/kv", Symbols: []string{"kv.Store", "kv.Stores", "kv.Transaction"}}
	err := symbols.CheckCited(bad, m)
	if err == nil || !strings.Contains(err.Error(), "example.com/kv v0.1.0 does not export kv.Stores (did you mean kv.Store?), kv.Transaction") {
		t.Errorf("invented symbols: %v", err)
	}

	// The index is names; a detail is one symbol in full.
	idx := m.Index()
	if len(idx.Packages) != 2 || !reflect.DeepEqual(idx.Packages[0].Errors, []string{"ErrNotFound"}) || idx.Packages[0].Values != 3 {
		t.Errorf("index: %+v", idx.Packages[0])
	}
	for _, i := range idx.Packages[0].Interfaces {
		if i.Name == "Store" && (i.Methods != 3 || !reflect.DeepEqual(i.ImplementedBy, []string{"File", "Handle"})) {
			t.Errorf("index of Store: %+v", i)
		}
	}
	if d, ok := m.Detail("kv.Store"); !ok || d.Interface == nil || d.Package != "example.com/kv" || len(d.Interface.Methods) != 3 {
		t.Errorf("detail of kv.Store: %+v", d)
	}
	if d, ok := m.Detail("kv.Open"); !ok || d.Func == nil || d.Func.Signature() != "Open(path string) (*File, error)" {
		t.Errorf("detail of kv.Open: %+v", d)
	}
	if d, ok := m.Detail("kv.ErrNotFound"); !ok || d.Value == nil || !d.Value.IsError {
		t.Errorf("detail of kv.ErrNotFound: %+v", d)
	}
	if d, ok := m.Detail("codec.JSON"); !ok || d.Type == nil {
		t.Errorf("detail of codec.JSON: %+v", d)
	}
	if _, ok := m.Detail("kv.Nope"); ok {
		t.Error("detail of a symbol that is not there")
	}

	// The listing for a prompt says what matters, and says when it stops.
	text := m.Text(1 << 20)
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
	short := m.Text(300)
	if len(short) > 300+len("... (more symbols than fit here)\n") || !strings.Contains(short, "more symbols than fit here") {
		t.Errorf("a listing over its budget: %d chars\n%s", len(short), short)
	}

	// Kinds.
	only := symbolstest.Read(t)
	only.Only([]string{"interface"})
	if p := only.Packages[0]; len(p.Interfaces) != 3 || p.Types != nil || p.Funcs != nil || p.Values != nil {
		t.Errorf("-kind interface kept: %d interfaces, %d types, %d funcs, %d values", len(p.Interfaces), len(p.Types), len(p.Funcs), len(p.Values))
	}
	if _, err := symbols.ParseKinds("interface,typ"); err == nil || !strings.Contains(err.Error(), "did you mean type?") {
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
		if _, err := symbols.FetchModule(mod, ""); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("module %q: %v", mod, err)
		}
	}
	for _, v := range []string{"v1 && rm", "-u", "v1@v2", "a\nb"} {
		if _, err := symbols.FetchModule("example.com/kv", v); err == nil || !strings.Contains(err.Error(), "is not a version") {
			t.Errorf("version %q: %v", v, err)
		}
	}
	// A module that does not type-check is said so, not half read.
	broken := writeModule(t, map[string]string{"go.mod": "module example.com/b\n\ngo 1.22\n", "b.go": "package b\n\nvar X int = \"no\"\n"})
	if _, err := symbols.ReadModule(broken, "example.com/b", "v0", nil, nil); err == nil || !strings.Contains(err.Error(), "no package of example.com/b type-checks") {
		t.Errorf("a broken module: %v", err)
	}
	empty := writeModule(t, map[string]string{"go.mod": "module example.com/e\n\ngo 1.22\n", "cmd/x/main.go": "package main\n\nfunc main() {}\n"})
	if _, err := symbols.ReadModule(empty, "example.com/e", "v0", nil, nil); err == nil || !strings.Contains(err.Error(), "has no package a program can import") {
		t.Errorf("a module of commands: %v", err)
	}
}

// Reading a module leaves the directory it was read from as it was.
func TestReadingSymbolsChangesNothing(t *testing.T) {
	dir := symbolstest.Module(t)
	before := snapshot(t, dir)
	if _, err := symbols.ReadModule(dir, "example.com/kv", "v0.1.0", nil, nil); err != nil {
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
	m, err := symbols.FetchModule("golang.org/x/mod", "v0.41.0")
	if err != nil {
		t.Skipf("the module could not be fetched here: %v", err)
	}
	if m.Version != "v0.41.0" {
		t.Errorf("resolved %s", m.Version)
	}
	for _, ref := range []string{"module.CheckPath", "module.Version.Path", "modfile.File.AddRequire", "semver.Compare", "golang.org/x/mod/modfile.Parse"} {
		if !m.Has(ref) {
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
	read := symbols.Remember(func(mod, v string) (*symbols.ModuleSymbols, error) { n++; return m, nil }, 1<<40)
	read("a", "v1")
	read("a", "v1")
	read("a", "v2")
	if n != 2 {
		t.Errorf("fetched %d times for two versions", n)
	}
}

func TestSymbolsCommand(t *testing.T) {
	m := symbolstest.Read(t)
	old := symbols.CmdFetch
	symbols.CmdFetch = func(mod, v string) (*symbols.ModuleSymbols, error) {
		if mod != "example.com/kv" || v != "v0.1.0" {
			return nil, errors.New("asked for " + mod + "@" + v)
		}
		return symbolstest.Read(t), nil
	}
	t.Cleanup(func() { symbols.CmdFetch = old })

	var out, errOut bytes.Buffer
	if err := symbols.Cmd([]string{"example.com/kv@v0.1.0"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if out.String() != strings.TrimRight(m.Text(1<<30), "\n")+"\n" {
		t.Errorf("the listing:\n%s", out.String())
	}
	out.Reset()
	if err := symbols.Cmd([]string{"-json", "-kind", "interface", "example.com/kv@v0.1.0"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	var got []symbols.ModuleSymbols
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || len(got) != 1 || len(got[0].Packages[0].Interfaces) != 3 || got[0].Packages[0].Types != nil {
		t.Errorf("-json -kind interface: %v\n%s", err, out.String())
	}
	if err := symbols.Cmd(nil, &out, &errOut); err == nil {
		t.Error("no module given was accepted")
	}
	if err := symbols.Cmd([]string{"-kind", "enum", "example.com/kv"}, &out, &errOut); err == nil || !strings.Contains(err.Error(), "-kind takes") {
		t.Errorf("a kind that is not one: %v", err)
	}
}

// writeModule writes files to a temporary directory and returns it.
func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// snapshot is every file under dir, by path.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		out[p] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
