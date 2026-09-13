// Scaffolded by tilegen. This file is yours: tilegen never overwrites it.

package main

// The badger tile, registered from a (tile-spec badger ...) form.
// embedded key-value: an LSM store, one keyspace, values as JSON
func init() {
	registerStoreBackend(&Backend{
		Name: "badger",
		Tile: "badger",
		Doc:  "embedded key-value: an LSM store, one keyspace, values as JSON",
		Cost: Cost{
			{Dim: "llm-work", Value: 4, Source: "derived", Note: "id-keyed get, save and delete in a transaction; secondary indexes are excluded by illegal-when, not priced here"},
			{Dim: "maintenance", Value: 3, Source: "derived", Note: "no schema and no migrations: a field added to the struct changes stored values silently"},
			{Dim: "dependency", Value: 2, Source: "derived", Note: "badger and its own dependencies, but no server and no build-time tool"},
			{Dim: "runtime", Value: 2, Source: "derived", Note: "an LSM tree with background compaction, unlike bbolt's single file"},
		},
		IllegalWhen: map[string]string{
			"cross-process":   "badger takes a directory lock; a second process cannot open the same store",
			"lookup-by-field": "one keyspace, one key; a secondary index would be written and repaired by hand",
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
	panic("tilegen:hole main.badgerTile.Implement")
}

func (s *badgerTile) Hint(meth *Node) string {
	panic("tilegen:hole main.badgerTile.Hint")
}
