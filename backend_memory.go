package main

import "fmt"

// The memory backend: a map guarded by a mutex. Useful for tests and fakes.
func init() {
	registerStoreBackend(&Backend{
		Name: "memory",
		Tile: "memory",
		Doc:  "in-memory store: a map guarded by a mutex",
		Cost: Cost{{Dim: "llm-work", Value: 2}, {Dim: "maintenance", Value: 1}, {Dim: "dependency", Value: 0}, {Dim: "runtime", Value: 1}},
		IllegalWhen: map[string]string{
			"durable":       "an in-memory map loses its data on restart",
			"cross-process": "the map lives in this process's heap; another process has its own, empty one",
		},
		Satisfies: map[string]string{
			"lookup-by-field": "a scan over the map: correct at any size, slow at a large one",
			"no-broker":       "a map and a mutex; there is nothing to run",
		},
		Implement: func(in StoreInput) (StoreParts, error) {
			return StoreParts{
				Fields: []*Node{
					L(Sym("field"), Sym("mu"), Sym("sync.Mutex")),
					L(Sym("field"), Sym("m"), Str(fmt.Sprintf("map[%s]*%s", in.IDType, in.Entity))),
				},
				Params: L(Sym("params")),
				Body:   fmt.Sprintf("return &%s{m: make(map[%s]*%s)}", in.Impl, in.IDType, in.Entity),
				Hint:   memoryHint,
			}, nil
		},
	})
}
