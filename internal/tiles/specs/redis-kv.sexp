; The redis-kv tile. `tilegen -out . internal/tiles/specs/redis-kv.sexp` regenerates its
; registration in internal/tiles/store_redis_kv.go; the two holes there are filled by hand.

(project tilegen-tiles
  (module github.com/vinodhalaharvi/tilegen)
  (go 1.26)
  (require (redis github.com/redis/go-redis/v9 v9.23.0))

  (package tiles
    (dir "internal/tiles")

    (tile-spec redis-kv
      (kind store-backend)
      (doc "Redis: one string key per entity under a prefix, values as JSON")
      (alias rediskv)
      (engine redis)
      (import redis github.com/redis/go-redis/v9)
      (error-package redis)
      (topics redis key-value)

      (satisfies (cross-process)
        "a server every process reaches over the network")
      (illegal-when (no-broker)
        "a Redis to run, with memory to size and persistence to configure")
      (illegal-when (lookup-by-field)
        "keys only: finding by another field means a second index, a set or a sorted set, kept by hand")
      (unverified (durable)
        "what survives a restart depends on the server's persistence (snapshots, or the append-only file and how often it is synced); the default can lose the last seconds of writes, and this tile cannot see the setting")
      (unverified (outbox)
        "an outbox is one transaction over the entity and the event; MULTI and EXEC can queue both, and this tile does not generate one")
      (unverified (inbox)
        "an inbox is one transaction over a handler's writes and the record that it ran; MULTI and EXEC can queue both, and this tile does not generate one")

      (cost
        (llm-work 4 (source derived "get, set and delete by key, a SCAN for the rest, and the JSON by hand"))
        (maintenance 4 (source derived "no schema, and every key lives in memory: the data set has to fit there"))
        (dependency 2 (source derived "go-redis and little else"))
        (runtime 1 (source derived "one round trip per call, served from memory"))
        (operations 4 (source derived "a server to run, with memory limits, persistence and failover to decide"))))))
