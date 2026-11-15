// Scaffolded by tilegen. This file is yours: tilegen never overwrites it.

package tiles

import (
	"fmt"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

// The leveldb tile, registered from a (tile-spec leveldb ...) form.
// embedded key-value: LevelDB in pure Go, a directory of sorted tables, values as JSON
func init() {
	compile.RegisterStoreBackend(&compile.Backend{
		Name: "leveldb",
		Tile: "leveldb",
		Doc:  "embedded key-value: LevelDB in pure Go, a directory of sorted tables, values as JSON",
		Cost: compile.Cost{
			{Dim: "llm-work", Value: 5, Source: "derived", Note: "key encoding, prefix iteration and JSON, all by hand"},
			{Dim: "maintenance", Value: 4, Source: "derived", Note: "no schema and no migrations, and a library that changes rarely: its last tagged release is v1.0.0"},
			{Dim: "dependency", Value: 1, Source: "derived", Note: "goleveldb and snappy, and no build-time tool"},
			{Dim: "runtime", Value: 2, Source: "derived", Note: "an LSM tree with background compaction"},
		},
		IllegalWhen: map[string]string{
			"cross-process":   "goleveldb takes a file lock on the directory; a second process cannot open the same store",
			"lookup-by-field": "one keyspace, one key; a secondary index would be written and repaired by hand",
		},
		Unverified: map[string]string{
			"inbox":  "an inbox is one transaction over a handler's writes and the record that it ran; a Batch or a Transaction commits several writes atomically, and this tile does not generate one",
			"outbox": "an outbox is one transaction over the entity and the event; a Batch or a Transaction commits several writes atomically, and this tile does not generate one",
		},
		Satisfies: map[string]string{
			"durable":   "every write this tile asks for sets WriteOptions.Sync: the journal is synced before the call returns",
			"no-broker": "a library and a directory; there is no server",
		},
		Imports: map[string]string{
			"leveldb": "github.com/syndtr/goleveldb/leveldb",
		},
		Topics:       []string{"leveldb", "embedded-database"},
		ErrorPackage: "leveldb",
		Implement:    (&leveldbTile{}).Implement,
	})
}

// leveldbTile carries the parts of the leveldb tile that are judgment rather than
// declaration. It holds nothing: it gives the two holes an address.
type leveldbTile struct {
}

func (s *leveldbTile) Implement(in compile.StoreInput) (compile.StoreParts, error) {
	return compile.StoreParts{
		Fields: []*sexp.Node{
			sexp.L(sexp.Sym("field"), sexp.Sym("db"), sexp.Sym("*leveldb.DB")),
			sexp.L(sexp.Sym("field"), sexp.Sym("prefix"), sexp.Sym("string")),
		},
		Params: sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("db"), sexp.Sym("*leveldb.DB"))),
		Body:   fmt.Sprintf("return &%s{db: db, prefix: %q}", in.Impl, compile.LowerFirstWord(in.Entity)+"s/"),
		Hint:   s.Hint,
	}, nil
}

func (s *leveldbTile) Hint(meth *sexp.Node) string {
	kind, _ := compile.MethodOp(meth)
	switch kind {
	case "get":
		return "Call s.db.Get(key, nil). The key is []byte(s.prefix) followed by the ID as a string. Map leveldb.ErrNotFound to ErrNotFound; otherwise JSON-unmarshal the bytes into the value."
	case "list":
		return "Iterate s.db.NewIterator(util.BytesPrefix([]byte(s.prefix)), nil) with Next(), JSON-unmarshal each Value() before moving on (the slice is reused), then Release the iterator and return its Error(). Keys come back in order. util is github.com/syndtr/goleveldb/leveldb/util."
	case "save":
		return "JSON-marshal the value and call s.db.Put(key, bytes, &opt.WriteOptions{Sync: true}). Sync is what makes the write durable before it returns. opt is github.com/syndtr/goleveldb/leveldb/opt."
	case "delete":
		return "Call s.db.Delete(key, &opt.WriteOptions{Sync: true}). Deleting a missing key is not an error."
	case "count":
		return "Iterate the same prefix range as List and count the keys."
	}
	// The *-by ops never reach here: this store is keyed by ID alone, and
	// illegal-when rules the tile out of any store that asks for one.
	return compile.CustomHint(meth)
}
