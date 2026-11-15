// Scaffolded by tilegen. This file is yours: tilegen never overwrites it.

package tiles

import (
	"fmt"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

// The pebble tile, registered from a (tile-spec pebble ...) form.
// embedded key-value: Pebble, an LSM store in a directory, values as JSON
func init() {
	compile.RegisterStoreBackend(&compile.Backend{
		Name: "pebble",
		Tile: "pebble",
		Doc:  "embedded key-value: Pebble, an LSM store in a directory, values as JSON",
		Cost: compile.Cost{
			{Dim: "llm-work", Value: 5, Source: "derived", Note: "key encoding, bounded iterators and the closer every read hands back, all by hand"},
			{Dim: "maintenance", Value: 3, Source: "derived", Note: "no schema and no migrations: a field added to the struct changes stored values silently"},
			{Dim: "dependency", Value: 3, Source: "derived", Note: "Pebble and the CockroachDB libraries it is built on, but no server and no build-time tool"},
			{Dim: "runtime", Value: 2, Source: "derived", Note: "an LSM tree with background compaction"},
		},
		IllegalWhen: map[string]string{
			"cross-process":   "Pebble locks its directory; a second process cannot open the same store",
			"lookup-by-field": "one keyspace, one key; a secondary index would be written and repaired by hand",
		},
		Unverified: map[string]string{
			"inbox":  "an inbox is one transaction over a handler's writes and the record that it ran; a Batch commits several writes atomically, and this tile does not generate one",
			"outbox": "an outbox is one transaction over the entity and the event; a Batch commits several writes atomically, and this tile does not generate one",
		},
		Satisfies: map[string]string{
			"durable":   "every write this tile asks for passes pebble.Sync: it is in the write-ahead log and synced before the call returns",
			"no-broker": "a library and a directory; there is no server",
		},
		Imports: map[string]string{
			"pebble": "github.com/cockroachdb/pebble",
		},
		Topics:       []string{"pebble", "embedded-database"},
		ErrorPackage: "pebble",
		Implement:    (&pebbleTile{}).Implement,
	})
}

// pebbleTile carries the parts of the pebble tile that are judgment rather than
// declaration. It holds nothing: it gives the two holes an address.
type pebbleTile struct {
}

func (s *pebbleTile) Implement(in compile.StoreInput) (compile.StoreParts, error) {
	return compile.StoreParts{
		Fields: []*sexp.Node{
			sexp.L(sexp.Sym("field"), sexp.Sym("db"), sexp.Sym("*pebble.DB")),
			sexp.L(sexp.Sym("field"), sexp.Sym("prefix"), sexp.Sym("string")),
		},
		Params: sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("db"), sexp.Sym("*pebble.DB"))),
		Body:   fmt.Sprintf("return &%s{db: db, prefix: %q}", in.Impl, compile.LowerFirstWord(in.Entity)+"s/"),
		Hint:   s.Hint,
	}, nil
}

func (s *pebbleTile) Hint(meth *sexp.Node) string {
	kind, _ := compile.MethodOp(meth)
	switch kind {
	case "get":
		return "Call s.db.Get(key). The key is []byte(s.prefix) followed by the ID as a string. Map pebble.ErrNotFound to ErrNotFound. JSON-unmarshal the value and then Close the closer that came back with it: the bytes are valid only until then."
	case "list":
		return "Open an iterator with s.db.NewIter(&pebble.IterOptions{LowerBound: []byte(s.prefix), UpperBound: the prefix with its last byte incremented}), walk it from First() with Next(), JSON-unmarshal each Value(), and Close the iterator, returning its error. Keys come back in order."
	case "save":
		return "JSON-marshal the value and call s.db.Set(key, bytes, pebble.Sync). Sync is what makes the write durable before it returns; do not pass NoSync."
	case "delete":
		return "Call s.db.Delete(key, pebble.Sync). Deleting a missing key is not an error."
	case "count":
		return "Walk the same bounded iterator as List and count the keys without reading the values."
	}
	// The *-by ops never reach here: this store is keyed by ID alone, and
	// illegal-when rules the tile out of any store that asks for one.
	return compile.CustomHint(meth)
}
