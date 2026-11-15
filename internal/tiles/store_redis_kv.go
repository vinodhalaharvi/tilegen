// Scaffolded by tilegen. This file is yours: tilegen never overwrites it.

package tiles

import (
	"fmt"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

// The redis-kv tile, registered from a (tile-spec redis-kv ...) form.
// Redis: one string key per entity under a prefix, values as JSON
func init() {
	compile.RegisterStoreBackend(&compile.Backend{
		Name: "rediskv",
		Tile: "redis-kv",
		Doc:  "Redis: one string key per entity under a prefix, values as JSON",
		Cost: compile.Cost{
			{Dim: "llm-work", Value: 4, Source: "derived", Note: "get, set and delete by key, a SCAN for the rest, and the JSON by hand"},
			{Dim: "maintenance", Value: 4, Source: "derived", Note: "no schema, and every key lives in memory: the data set has to fit there"},
			{Dim: "dependency", Value: 2, Source: "derived", Note: "go-redis and little else"},
			{Dim: "runtime", Value: 1, Source: "derived", Note: "one round trip per call, served from memory"},
			{Dim: "operations", Value: 4, Source: "derived", Note: "a server to run, with memory limits, persistence and failover to decide"},
		},
		IllegalWhen: map[string]string{
			"lookup-by-field": "keys only: finding by another field means a second index, a set or a sorted set, kept by hand",
			"no-broker":       "a Redis to run, with memory to size and persistence to configure",
		},
		Unverified: map[string]string{
			"durable": "what survives a restart depends on the server's persistence (snapshots, or the append-only file and how often it is synced); the default can lose the last seconds of writes, and this tile cannot see the setting",
			"inbox":   "an inbox is one transaction over a handler's writes and the record that it ran; MULTI and EXEC can queue both, and this tile does not generate one",
			"outbox":  "an outbox is one transaction over the entity and the event; MULTI and EXEC can queue both, and this tile does not generate one",
		},
		Satisfies: map[string]string{
			"cross-process": "a server every process reaches over the network",
		},
		Imports: map[string]string{
			"redis": "github.com/redis/go-redis/v9",
		},
		Topics:       []string{"redis", "key-value"},
		ErrorPackage: "redis",
		Engine:       "redis",
		Implement:    (&redisKvTile{}).Implement,
	})
}

// redisKvTile carries the parts of the redis-kv tile that are judgment rather than
// declaration. It holds nothing: it gives the two holes an address.
type redisKvTile struct {
}

func (s *redisKvTile) Implement(in compile.StoreInput) (compile.StoreParts, error) {
	return compile.StoreParts{
		Fields: []*sexp.Node{
			sexp.L(sexp.Sym("field"), sexp.Sym("client"), sexp.Sym("*redis.Client")),
			sexp.L(sexp.Sym("field"), sexp.Sym("prefix"), sexp.Sym("string")),
		},
		Params: sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("client"), sexp.Sym("*redis.Client"))),
		Body:   fmt.Sprintf("return &%s{client: client, prefix: %q}", in.Impl, compile.LowerFirstWord(in.Entity)+"s:"),
		Hint:   s.Hint,
	}, nil
}

func (s *redisKvTile) Hint(meth *sexp.Node) string {
	kind, _ := compile.MethodOp(meth)
	switch kind {
	case "get":
		return "Call s.client.Get(ctx, key).Bytes(). The key is s.prefix followed by the ID as a string. Map redis.Nil to ErrNotFound; otherwise JSON-unmarshal the bytes into the value."
	case "list":
		return "Walk the keys with s.client.Scan(ctx, 0, s.prefix+\"*\", 0).Iterator(), never KEYS. Read each with Get, skip one that has gone (redis.Nil) since the scan saw it, JSON-unmarshal the rest, and sort the result by ID: SCAN returns keys in no order."
	case "save":
		return "JSON-marshal the value and call s.client.Set(ctx, key, bytes, 0).Err(). The key is s.prefix followed by the ID as a string. An expiration of 0 keeps it until it is deleted."
	case "delete":
		return "Call s.client.Del(ctx, key).Err(). The key is s.prefix followed by the ID as a string. Deleting a missing key is not an error."
	case "count":
		return "Count the keys that s.client.Scan(ctx, 0, s.prefix+\"*\", 0).Iterator() yields, and return the iterator's Err()."
	}
	// The *-by ops never reach here: this store is keyed by ID alone, and
	// illegal-when rules the tile out of any store that asks for one.
	return compile.CustomHint(meth)
}
