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
  (illegal-when (lookup-by-field)
    "keys only: finding by another field means a second index, a set or a sorted set, kept by hand")
  (illegal-when (no-broker)
    "a Redis to run, with memory to size and persistence to configure")
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
    (operations 4 (source derived "a server to run, with memory limits, persistence and failover to decide")))

  (holds (client *redis.Client) (prefix string))
  (new (params (client *redis.Client))
    "return &{impl}{client: client, prefix: \"{names}:\"}")

  (hint get
    "Call s.client.Get(ctx, key).Bytes(). The key is s.prefix followed by the ID as a string. Map redis.Nil to ErrNotFound; otherwise JSON-unmarshal the bytes into the value.")
  (hint list
    "Walk the keys with s.client.Scan(ctx, 0, s.prefix+\"*\", 0).Iterator(), never KEYS. Read each with Get, skip one that has gone (redis.Nil) since the scan saw it, JSON-unmarshal the rest, and sort the result by ID: SCAN returns keys in no order.")
  (hint save
    "JSON-marshal the value and call s.client.Set(ctx, key, bytes, 0).Err(). The key is s.prefix followed by the ID as a string. An expiration of 0 keeps it until it is deleted.")
  (hint delete
    "Call s.client.Del(ctx, key).Err(). The key is s.prefix followed by the ID as a string. Deleting a missing key is not an error.")
  (hint count
    "Count the keys that s.client.Scan(ctx, 0, s.prefix+\"*\", 0).Iterator() yields, and return the iterator's Err()."))
