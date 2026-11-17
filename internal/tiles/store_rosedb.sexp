(tile rosedb
  (offers store)
  (doc "RoseDB: an embedded log-structured key-value store, one key per entity under a prefix, values as JSON")
  (import rosedb github.com/rosedblabs/rosedb/v2)
  (error-package rosedb)
  (topics rosedb embedded key-value bitcask)

  (satisfies (no-broker)
    "a library and a directory; there is no server")
  (illegal-when (cross-process)
    "the directory is locked by the one process that opened it")
  (illegal-when (lookup-by-field)
    "a value is found by its key; there is no index on what it holds")
  (unverified (durable)
    "a write is synced to disk before it returns only when the database was opened with Sync, which is the caller's option")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; a Batch commits several writes atomically, and this tile does not generate one")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; a Batch commits several writes atomically, and this tile does not generate one")

  (cost
    (llm-work 4 (source derived "get, put and delete, a walk over the keys under a prefix, and the JSON"))
    (maintenance 4 (source derived "no schema, every key is kept in memory, and the log needs merging to give back space"))
    (dependency 1 (source derived "RoseDB and its write-ahead log"))
    (runtime 2 (source derived "an index in memory, and one disk read per get"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (db *rosedb.DB) (prefix string))
  (new (params (db *rosedb.DB))
    "return &{impl}{db: db, prefix: \"{names}/\"}")

  (hint get
    "Call data, err := s.db.Get([]byte(s.prefix + the ID as a string)). When errors.Is(err, rosedb.ErrKeyNotFound), return ErrNotFound. Otherwise JSON-unmarshal data. The database takes no context, so check ctx.Err() first.")

  (hint list
    "Call s.db.AscendGreaterOrEqual([]byte(s.prefix), func(k, v []byte) (bool, error) {...}), which walks keys in order from the prefix on. In the callback, return false, nil once bytes.HasPrefix(k, []byte(s.prefix)) no longer holds, which ends the walk; otherwise JSON-unmarshal v, keep it, and return true, nil. The walk has no error of its own, so keep an unmarshal error in a variable outside the callback and return it afterwards.")

  (hint save
    "JSON-marshal the value and call s.db.Put([]byte(s.prefix + the ID as a string), the bytes). It replaces what was there.")

  (hint delete
    "Call s.db.Delete([]byte(s.prefix + the ID as a string)). Deleting a missing key is not an error.")

  (hint count
    "Walk the same AscendGreaterOrEqual as List and count the keys, without decoding any value."))
