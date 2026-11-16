(tile redis-redigo
  (offers store)
  (doc "Redis through redigo: one string key per entity under a prefix, values as JSON")
  (alias redigo)
  (engine redis)
  (import redis github.com/gomodule/redigo/redis)
  (error-package redis)
  (topics redis redigo key-value)

  (satisfies (cross-process)
    "a server every process reaches over the network")
  (illegal-when (lookup-by-field)
    "a string key holds one value; a secondary index would be a second structure written and repaired by hand")
  (illegal-when (no-broker)
    "a Redis to run, with memory to size and persistence to configure")
  (unverified (durable)
    "what survives a restart depends on the server's persistence (snapshots, or the append-only file and how often it is synced), which this tile does not configure")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; MULTI/EXEC queues several commands atomically, and this tile does not generate one")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; MULTI/EXEC queues several commands atomically, and this tile does not generate one")

  (cost
    (llm-work 5 (source derived "a connection to take and return on every call, replies to convert by hand, a cursor scan, and JSON"))
    (maintenance 4 (source derived "no schema, and a list or a count walks the keyspace with SCAN"))
    (dependency 1 (source derived "redigo only"))
    (runtime 1 (source derived "a round trip per call"))
    (operations 4 (source derived "a server to run, with memory to size and persistence to configure"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (pool *redis.Pool) (prefix string))
  (new (params (pool *redis.Pool))
    "return &{impl}{pool: pool, prefix: \"{names}:\"}")

  (hint get
    "Take conn, err := s.pool.GetContext(ctx) and defer conn.Close(), which returns it to the pool. Call redis.Bytes(redis.DoContext(conn, ctx, \"GET\", s.prefix + the ID as a string)). When errors.Is(err, redis.ErrNil), return ErrNotFound. Otherwise JSON-unmarshal the bytes.")

  (hint list
    "On one connection from s.pool.GetContext(ctx), walk the keyspace: starting from cursor 0, call redis.Values(redis.DoContext(conn, ctx, \"SCAN\", cursor, \"MATCH\", s.prefix+\"*\", \"COUNT\", 100)) and redis.Scan(the values, &cursor, &keys) until the cursor is 0 again. SCAN may return a key twice and in no order, so collect the keys into a set and sort it. Read them with one MGET, redis.ByteSlices(redis.DoContext(conn, ctx, \"MGET\", the keys as ...any)), skipping a nil entry: that key has gone.")

  (hint save
    "JSON-marshal the value and, on a connection from s.pool.GetContext(ctx), call redis.DoContext(conn, ctx, \"SET\", s.prefix + the ID as a string, the bytes). It replaces what was there.")

  (hint delete
    "On a connection from s.pool.GetContext(ctx), call redis.DoContext(conn, ctx, \"DEL\", s.prefix + the ID as a string). Deleting a missing key is not an error.")

  (hint count
    "Walk the same SCAN as List, collect the keys into a set, and return its size."))
