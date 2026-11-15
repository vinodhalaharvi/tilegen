(tile badger
  (offers store)
  (doc "embedded key-value: an LSM store, one keyspace, values as JSON")
  (import badger github.com/dgraph-io/badger/v4)
  (error-package badger)
  (topics badger embedded-database)

  (satisfies (durable)
    "a directory on disk with a write-ahead log replayed on open")
  (satisfies (no-broker)
    "a library and a directory; there is no server")
  (illegal-when (cross-process)
    "badger takes a directory lock; a second process cannot open the same store")
  (illegal-when (lookup-by-field)
    "one keyspace, one key; a secondary index would be written and repaired by hand")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; this tile does not generate one")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; this tile does not generate one")

  (cost
    (llm-work 4 (source derived "id-keyed get, save and delete in one transaction; secondary indexes are excluded by illegal-when, not priced here"))
    (maintenance 3 (source derived "no schema and no migrations: a field added to the struct changes stored values silently"))
    (dependency 2 (source derived "badger and its own dependencies, but no server and no build-time tool"))
    (runtime 2 (source derived "an LSM tree with background compaction, unlike bbolt's single file")))

  (holds (db *badger.DB) (prefix string))
  (new (params (db *badger.DB))
    "return &{impl}{db: db, prefix: \"{name}:\"}")

  (hint get
    "Fetch the value at s.prefix+id inside a s.db.View transaction, JSON-unmarshal its bytes into a fresh entity, and return it. Map badger.ErrKeyNotFound to ErrNotFound.")
  (hint list
    "In a s.db.View transaction, iterate keys with prefix []byte(s.prefix), JSON-unmarshal each item's value into a fresh entity, then sort the collected slice by ID before returning it.")
  (hint save
    "JSON-marshal the value and Set it at s.prefix+its ID inside a s.db.Update transaction, replacing any existing entry.")
  (hint delete
    "Delete s.prefix+id inside a s.db.Update transaction. badger.ErrKeyNotFound is not an error: deleting an absent key is a no-op.")
  (hint count
    "In a s.db.View transaction, iterate with an IteratorOptions whose PrefetchValues is false over prefix []byte(s.prefix), and count the keys."))
