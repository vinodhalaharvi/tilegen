// Scaffolded by tilegen. This file is yours: tilegen never overwrites it.

package main

import "fmt"

// The badger tile, registered from a (tile-spec badger ...) form.
// embedded key-value: an LSM store, one keyspace, values as JSON
func init() {
	registerStoreBackend(&Backend{
		Name: "badger",
		Tile: "badger",
		Doc:  "embedded key-value: an LSM store, one keyspace, values as JSON",
		Cost: Cost{
			{Dim: "llm-work", Value: 4, Source: "derived", Note: "id-keyed get, save and delete in one transaction; secondary indexes are excluded by illegal-when, not priced here"},
			{Dim: "maintenance", Value: 3, Source: "derived", Note: "no schema and no migrations: a field added to the struct changes stored values silently"},
			{Dim: "dependency", Value: 2, Source: "derived", Note: "badger and its own dependencies, but no server and no build-time tool"},
			{Dim: "runtime", Value: 2, Source: "derived", Note: "an LSM tree with background compaction, unlike bbolt's single file"},
		},
		IllegalWhen: map[string]string{
			"cross-process":   "badger takes a directory lock; a second process cannot open the same store",
			"lookup-by-field": "one keyspace, one key; a secondary index would be written and repaired by hand",
		},
		Satisfies: map[string]string{
			"durable":   "a directory on disk with a write-ahead log replayed on open",
			"no-broker": "a library and a directory; there is no server",
		},
		Imports: map[string]string{
			"badger": "github.com/dgraph-io/badger/v4",
		},
		Topics:       []string{"badger", "embedded-database"},
		ErrorPackage: "badger",
		Implement:    (&badgerTile{}).Implement,
	})
}

// badgerTile carries the parts of the badger tile that are judgment rather than
// declaration. It holds nothing: it gives the two holes an address.
type badgerTile struct {
}

func (s *badgerTile) Implement(in StoreInput) (StoreParts, error) {
	return StoreParts{
		Fields: []*Node{
			L(Sym("field"), Sym("db"), Sym("*badger.DB")),
			L(Sym("field"), Sym("prefix"), Sym("string")),
		},
		Params: L(Sym("params"), L(Sym("db"), Sym("*badger.DB"))),
		Body:   fmt.Sprintf("return &%s{db: db, prefix: %q}", in.Impl, lowerFirstWord(in.Entity)+":"),
		Hint:   s.Hint,
	}, nil
}

func (s *badgerTile) Hint(meth *Node) string {
	kind, _ := methodOp(meth)
	switch kind {
	case "get":
		return "Fetch the value at s.prefix+id inside a s.db.View transaction, JSON-unmarshal its bytes into a fresh entity, and return it. Map badger.ErrKeyNotFound to ErrNotFound."
	case "list":
		return "In a s.db.View transaction, iterate keys with prefix []byte(s.prefix), JSON-unmarshal each item's value into a fresh entity, then sort the collected slice by ID before returning it."
	case "save":
		return "JSON-marshal the value and Set it at s.prefix+its ID inside a s.db.Update transaction, replacing any existing entry."
	case "delete":
		return "Delete s.prefix+id inside a s.db.Update transaction. badger.ErrKeyNotFound is not an error: deleting an absent key is a no-op."
	case "count":
		return "In a s.db.View transaction, iterate with an IteratorOptions whose PrefetchValues is false over prefix []byte(s.prefix), and count the keys."
	}
	// The *-by ops never reach here: badger is keyed by ID alone, and
	// illegal-when rules this tile out of any store that asks for one.
	return customHint(meth)
}
