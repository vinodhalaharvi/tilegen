(tile pebble
  (offers store)
  (doc "embedded key-value: Pebble, an LSM store in a directory, values as JSON")
  (import pebble github.com/cockroachdb/pebble)
  (error-package pebble)
  (topics pebble embedded-database)

  (satisfies (durable)
    "every write this tile asks for passes pebble.Sync: it is in the write-ahead log and synced before the call returns")
  (satisfies (no-broker)
    "a library and a directory; there is no server")
  (illegal-when (cross-process)
    "Pebble locks its directory; a second process cannot open the same store")
  (illegal-when (lookup-by-field)
    "one keyspace, one key; a secondary index would be written and repaired by hand")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; a Batch commits several writes atomically, and this tile does not generate one")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; a Batch commits several writes atomically, and this tile does not generate one")

  (cost
    (llm-work 5 (source derived "key encoding, bounded iterators and the closer every read hands back, all by hand"))
    (maintenance 3 (source derived "no schema and no migrations: a field added to the struct changes stored values silently"))
    (dependency 3 (source derived "Pebble and the CockroachDB libraries it is built on, but no server and no build-time tool"))
    (runtime 2 (source derived "an LSM tree with background compaction")))

  (holds (db *pebble.DB) (prefix string))
  (new (params (db *pebble.DB))
    "return &{impl}{db: db, prefix: \"{names}/\"}")

  (hint get
    "Call s.db.Get(key). The key is []byte(s.prefix) followed by the ID as a string. Map pebble.ErrNotFound to ErrNotFound. JSON-unmarshal the value and then Close the closer that came back with it: the bytes are valid only until then.")
  (hint list
    "Open an iterator with s.db.NewIter(&pebble.IterOptions{LowerBound: []byte(s.prefix), UpperBound: the prefix with its last byte incremented}), walk it from First() with Next(), JSON-unmarshal each Value(), and Close the iterator, returning its error. Keys come back in order.")
  (hint save
    "JSON-marshal the value and call s.db.Set(key, bytes, pebble.Sync). Sync is what makes the write durable before it returns; do not pass NoSync.")
  (hint delete
    "Call s.db.Delete(key, pebble.Sync). Deleting a missing key is not an error.")
  (hint count
    "Walk the same bounded iterator as List and count the keys without reading the values."))
