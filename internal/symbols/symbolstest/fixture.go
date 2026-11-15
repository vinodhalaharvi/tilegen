// Package symbolstest is a small Go module to read symbols from in tests:
// one of everything the reader has to get right.
package symbolstest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vinodhalaharvi/tilegen/internal/symbols"
)

// Fixture is the module's files by path. Its module path is
// example.com/kv.
var Fixture = map[string]string{
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

// Module writes the fixture to a temporary directory and returns it.
func Module(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range Fixture {
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

// Read is the fixture's symbols, as version v0.1.0.
func Read(t testing.TB) *symbols.ModuleSymbols {
	t.Helper()
	m, err := symbols.ReadModule(Module(t), "example.com/kv", "v0.1.0", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
