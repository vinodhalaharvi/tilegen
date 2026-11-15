(tile valkey
  (offers store)
  (doc "Valkey: one string key per entity under a prefix, values as JSON")
  (engine valkey)
  (import valkey github.com/valkey-io/valkey-go)
  (error-package valkey)
  (topics valkey key-value)

  (satisfies (cross-process)
    "a server every process reaches over the network")
  (illegal-when (lookup-by-field)
    "a string key holds one value; a secondary index would be a second structure written and repaired by hand")
  (illegal-when (no-broker)
    "a Valkey to run, with memory to size and persistence to configure")
  (unverified (durable)
    "what survives a restart depends on the server's persistence (snapshots, or the append-only file and how often it is synced), which this tile does not configure")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; MULTI/EXEC queues several commands atomically, and this tile does not generate one")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; MULTI/EXEC queues several commands atomically, and this tile does not generate one")

  (cost
    (llm-work 5 (source derived "commands built one by one, a cursor scan, nil replies to tell from errors, and JSON by hand"))
    (maintenance 4 (source derived "no schema, and a list or a count walks the keyspace with SCAN"))
    (dependency 1 (source derived "valkey-go only"))
    (runtime 1 (source derived "a round trip per call, pipelined automatically"))
    (operations 4 (source derived "a server to run, with memory to size and persistence to configure"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (client valkey.Client) (prefix string))
  (new (params (client valkey.Client))
    "return &{impl}{client: client, prefix: \"{names}:\"}")

  (hint get
    "Call s.client.Do(ctx, s.client.B().Get().Key(s.prefix + the ID as a string).Build()).AsBytes(). When valkey.IsValkeyNil(err), return ErrNotFound. Otherwise JSON-unmarshal the bytes.")

  (hint list
    "Walk the keyspace with SCAN: starting from cursor 0, call s.client.Do(ctx, s.client.B().Scan().Cursor(cursor).Match(s.prefix+\"*\").Count(100).Build()).AsScanEntry() and carry entry.Cursor forward until it is 0 again. SCAN may return a key twice and in no order, so collect entry.Elements into a set, sort it, then read the values with one MGET (s.client.B().Mget().Key(keys...).Build(), then ToArray and each message's AsBytes), skipping a key that has gone (valkey.IsValkeyNil).")

  (hint save
    "JSON-marshal the value and call s.client.Do(ctx, s.client.B().Set().Key(s.prefix + the ID as a string).Value(valkey.BinaryString(the bytes)).Build()).Error(). It replaces what was there.")

  (hint delete
    "Call s.client.Do(ctx, s.client.B().Del().Key(s.prefix + the ID as a string).Build()).Error(). Deleting a missing key is not an error.")

  (hint count
    "Walk the same SCAN as List, collect the keys into a set, and return its size."))
