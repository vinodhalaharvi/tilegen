// Scaffolded by tilegen. This file is yours: tilegen never overwrites it.

package tiles

import (
	"fmt"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

// The etcd tile, registered from a (tile-spec etcd ...) form.
// etcd: a replicated key-value store, one key per entity under a prefix, values as JSON
func init() {
	compile.RegisterStoreBackend(&compile.Backend{
		Name: "etcd",
		Tile: "etcd",
		Doc:  "etcd: a replicated key-value store, one key per entity under a prefix, values as JSON",
		Cost: compile.Cost{
			{Dim: "llm-work", Value: 5, Source: "derived", Note: "get, put, delete and a prefix range, with the JSON and the key encoding by hand"},
			{Dim: "maintenance", Value: 7, Source: "derived", Note: "no schema, and it is built for a little data that matters a great deal: a request is capped near 1.5 MiB and the whole keyspace at a few GiB by default, which a store of entities grows past"},
			{Dim: "dependency", Value: 4, Source: "derived", Note: "the client brings gRPC and protobuf with it"},
			{Dim: "runtime", Value: 2, Source: "derived", Note: "every write is a round of consensus"},
			{Dim: "operations", Value: 7, Source: "derived", Note: "a quorum to keep healthy: members, backups, compaction and defragmentation"},
		},
		IllegalWhen: map[string]string{
			"lookup-by-field": "keys only: a range over a key prefix is the one query, so finding by another field means a second index kept by hand",
			"no-broker":       "a cluster to run: an odd number of members, with snapshots, compaction and defragmentation",
		},
		Unverified: map[string]string{
			"inbox":  "an inbox is one transaction over a handler's writes and the record that it ran; a Txn can put both, and this tile does not generate one",
			"outbox": "an outbox is one transaction over the entity and the event; a Txn can put both, and this tile does not generate one",
		},
		Satisfies: map[string]string{
			"cross-process": "a cluster every process reaches over gRPC",
			"durable":       "a Put returns after a quorum of the cluster has the write in its log",
		},
		Imports: map[string]string{
			"clientv3": "go.etcd.io/etcd/client/v3",
		},
		Topics:       []string{"etcd", "key-value"},
		ErrorPackage: "clientv3",
		Engine:       "etcd",
		Implement:    (&etcdTile{}).Implement,
	})
}

// etcdTile carries the parts of the etcd tile that are judgment rather than
// declaration. It holds nothing: it gives the two holes an address.
type etcdTile struct {
}

func (s *etcdTile) Implement(in compile.StoreInput) (compile.StoreParts, error) {
	return compile.StoreParts{
		Fields: []*sexp.Node{
			sexp.L(sexp.Sym("field"), sexp.Sym("client"), sexp.Sym("*clientv3.Client")),
			sexp.L(sexp.Sym("field"), sexp.Sym("prefix"), sexp.Sym("string")),
		},
		Params: sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("client"), sexp.Sym("*clientv3.Client"))),
		Body:   fmt.Sprintf("return &%s{client: client, prefix: %q}", in.Impl, compile.LowerFirstWord(in.Entity)+"s/"),
		Hint:   s.Hint,
	}, nil
}

func (s *etcdTile) Hint(meth *sexp.Node) string {
	kind, _ := compile.MethodOp(meth)
	switch kind {
	case "get":
		return "Call s.client.Get(ctx, key). The key is s.prefix followed by the ID as a string. When the response's Kvs is empty return ErrNotFound; otherwise JSON-unmarshal Kvs[0].Value into the value."
	case "list":
		return "Call s.client.Get(ctx, s.prefix, clientv3.WithPrefix()) and JSON-unmarshal each of the response's Kvs. A range read comes back in key order."
	case "save":
		return "JSON-marshal the value and call s.client.Put(ctx, key, string(bytes)), which replaces whatever was there. The key is s.prefix followed by the ID as a string."
	case "delete":
		return "Call s.client.Delete(ctx, key). The key is s.prefix followed by the ID as a string. Deleting a missing key is not an error: the response's Deleted is 0."
	case "count":
		return "Call s.client.Get(ctx, s.prefix, clientv3.WithPrefix(), clientv3.WithCountOnly()) and return the response's Count."
	}
	// The *-by ops never reach here: this store is keyed by ID alone, and
	// illegal-when rules the tile out of any store that asks for one.
	return compile.CustomHint(meth)
}
