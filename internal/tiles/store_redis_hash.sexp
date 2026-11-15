(tile redis-hash
  (offers store)
  (doc "Redis: one hash per entity, a field per value, values as JSON")
  (alias redishash)
  (engine redis)
  (import redis github.com/redis/go-redis/v9)
  (error-package redis)
  (topics redis key-value)

  (satisfies (cross-process)
    "a server every process reaches over the network")
  (illegal-when (lookup-by-field)
    "a hash finds a value by its field name, which is the ID; there is no index on what a value holds")
  (illegal-when (no-broker)
    "a Redis to run, with memory to size and persistence to configure")
  (unverified (durable)
    "what survives a restart depends on the server's persistence (snapshots, or the append-only file and how often it is synced), which this tile does not configure")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; MULTI/EXEC queues several commands atomically, and this tile does not generate one")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; MULTI/EXEC queues several commands atomically, and this tile does not generate one")

  (cost
    (llm-work 3 (source derived "five hash commands and the JSON"))
    (maintenance 4 (source derived "no schema, and every value of an entity lives under one key: a list reads them all in one reply, and one key cannot be spread over a cluster's slots"))
    (dependency 2 (source derived "go-redis and its few modules"))
    (runtime 1 (source derived "a round trip per call"))
    (operations 4 (source derived "a server to run, with memory to size and persistence to configure"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (client *redis.Client) (key string))
  (new (params (client *redis.Client))
    "return &{impl}{client: client, key: \"{names}\"}")

  (hint get
    "Call s.client.HGet(ctx, s.key, the ID as a string).Bytes(). When errors.Is(err, redis.Nil), return ErrNotFound. Otherwise JSON-unmarshal the bytes.")

  (hint list
    "Call s.client.HGetAll(ctx, s.key).Result(), which returns every field and value as a map[string]string. Unmarshal each value and sort the result by ID: a hash keeps no order. A key that does not exist comes back as an empty map, not an error.")

  (hint save
    "JSON-marshal the value and call s.client.HSet(ctx, s.key, the ID as a string, the bytes).Err(). It replaces what was there.")

  (hint delete
    "Call s.client.HDel(ctx, s.key, the ID as a string).Err(). Deleting a missing field is not an error.")

  (hint count
    "Call s.client.HLen(ctx, s.key).Result() and return it as an int."))
