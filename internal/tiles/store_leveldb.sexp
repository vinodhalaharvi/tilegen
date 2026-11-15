(tile leveldb
  (offers store)
  (doc "embedded key-value: LevelDB in pure Go, a directory of sorted tables, values as JSON")
  (import leveldb github.com/syndtr/goleveldb/leveldb)
  (error-package leveldb)
  (topics leveldb embedded-database)

  (satisfies (durable)
    "every write this tile asks for sets WriteOptions.Sync: the journal is synced before the call returns")
  (satisfies (no-broker)
    "a library and a directory; there is no server")
  (illegal-when (cross-process)
    "goleveldb takes a file lock on the directory; a second process cannot open the same store")
  (illegal-when (lookup-by-field)
    "one keyspace, one key; a secondary index would be written and repaired by hand")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; a Batch or a Transaction commits several writes atomically, and this tile does not generate one")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; a Batch or a Transaction commits several writes atomically, and this tile does not generate one")

  (cost
    (llm-work 5 (source derived "key encoding, prefix iteration and JSON, all by hand"))
    (maintenance 4 (source derived "no schema and no migrations, and a library that changes rarely: its last tagged release is v1.0.0"))
    (dependency 1 (source derived "goleveldb and snappy, and no build-time tool"))
    (runtime 2 (source derived "an LSM tree with background compaction")))

  (holds (db *leveldb.DB) (prefix string))
  (new (params (db *leveldb.DB))
    "return &{impl}{db: db, prefix: \"{names}/\"}")

  (hint get
    "Call s.db.Get(key, nil). The key is []byte(s.prefix) followed by the ID as a string. Map leveldb.ErrNotFound to ErrNotFound; otherwise JSON-unmarshal the bytes into the value.")
  (hint list
    "Iterate s.db.NewIterator(util.BytesPrefix([]byte(s.prefix)), nil) with Next(), JSON-unmarshal each Value() before moving on (the slice is reused), then Release the iterator and return its Error(). Keys come back in order. util is github.com/syndtr/goleveldb/leveldb/util.")
  (hint save
    "JSON-marshal the value and call s.db.Put(key, bytes, &opt.WriteOptions{Sync: true}). Sync is what makes the write durable before it returns. opt is github.com/syndtr/goleveldb/leveldb/opt.")
  (hint delete
    "Call s.db.Delete(key, &opt.WriteOptions{Sync: true}). Deleting a missing key is not an error.")
  (hint count
    "Iterate the same prefix range as List and count the keys."))
