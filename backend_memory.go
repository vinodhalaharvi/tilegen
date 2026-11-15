package main

import (
	"fmt"
	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

// The memory backend: a map guarded by a mutex. Useful for tests and fakes.
func init() {
	compile.RegisterStoreBackend(&compile.Backend{
		Name:   "memory",
		Tile:   "memory",
		Engine: compile.NoEngine,
		Doc:    "in-memory store: a map guarded by a mutex",
		Cost:   compile.Cost{{Dim: "llm-work", Value: 2}, {Dim: "maintenance", Value: 1}, {Dim: "dependency", Value: 0}, {Dim: "runtime", Value: 1}},
		IllegalWhen: map[string]string{
			"durable":       "an in-memory map loses its data on restart",
			"cross-process": "the map lives in this process's heap; another process has its own, empty one",
			"outbox":        "nothing is committed, so there is no transaction for an event to share",
			"inbox":         "nothing is committed, so a handler's writes and the record that it ran cannot share a transaction",
		},
		Satisfies: map[string]string{
			"lookup-by-field": "a scan over the map: correct at any size, slow at a large one",
			"no-broker":       "a map and a mutex; there is nothing to run",
		},
		Implement: func(in compile.StoreInput) (compile.StoreParts, error) {
			return compile.StoreParts{
				Fields: []*sexp.Node{
					sexp.L(sexp.Sym("field"), sexp.Sym("mu"), sexp.Sym("sync.Mutex")),
					sexp.L(sexp.Sym("field"), sexp.Sym("m"), sexp.Str(fmt.Sprintf("map[%s]*%s", in.IDType, in.Entity))),
				},
				Params: sexp.L(sexp.Sym("params")),
				Body:   fmt.Sprintf("return &%s{m: make(map[%s]*%s)}", in.Impl, in.IDType, in.Entity),
				Hint:   compile.MemoryHint,
			}, nil
		},
	})
}
