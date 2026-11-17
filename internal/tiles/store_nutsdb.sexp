(tile nutsdb
  (offers store)
  (doc "NutsDB: an embedded key-value store, a bucket per entity, values as JSON")
  (import nutsdb github.com/nutsdb/nutsdb)
  (error-package nutsdb)
  (topics nutsdb embedded key-value)

  (satisfies (no-broker)
    "a library and a directory; there is no server")
  (illegal-when (cross-process)
    "the directory is locked by the one process that opened it")
  (illegal-when (lookup-by-field)
    "a value is found by its key; there is no index on what it holds")
  (unverified (durable)
    "a committed transaction is synced to disk when the database was opened with SyncEnable, which is the caller's option")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; one Update transaction can hold both, and this tile does not generate it")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; one Update transaction can hold both, and this tile does not generate it")

  (cost
    (llm-work 4 (source derived "a transaction closure around every call, several errors that all mean not found, and the JSON"))
    (maintenance 4 (source derived "no schema, the bucket must be created before first use, and every key is kept in memory"))
    (dependency 2 (source derived "NutsDB and its few modules"))
    (runtime 2 (source derived "an index in memory, and a write to the log"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (db *nutsdb.DB) (bucket string))
  (new (params (db *nutsdb.DB))
    "return &{impl}{db: db, bucket: \"{names}\"}")

  (hint get
    "Inside s.db.View(func(tx *nutsdb.Tx) error {...}), call data, err := tx.Get(s.bucket, []byte(the ID as a string)) and JSON-unmarshal data before the closure returns. When nutsdb.IsKeyNotFound(err) or nutsdb.IsBucketNotFound(err), return ErrNotFound. The bucket is created by the caller before first use, in an Update: tx.NewBucket(nutsdb.DataStructureBTree, the name).")

  (hint list
    "Inside s.db.View, call _, values, err := tx.GetAll(s.bucket), which returns them in key order. When nutsdb.IsBucketEmpty(err) or nutsdb.IsBucketNotFound(err), return an empty list. JSON-unmarshal each value inside the closure.")

  (hint save
    "JSON-marshal the value and, inside s.db.Update(func(tx *nutsdb.Tx) error {...}), call tx.Put(s.bucket, []byte(the ID as a string), the bytes, nutsdb.Persistent). The last argument is the time to live; Persistent keeps the value until it is deleted.")

  (hint delete
    "Inside s.db.Update, call tx.Delete(s.bucket, []byte(the ID as a string)). When nutsdb.IsKeyNotFound(err) it was already gone: return nil from the closure.")

  (hint count
    "Inside s.db.View, call keys, err := tx.GetKeys(s.bucket) and return len(keys); an empty or missing bucket is zero, as in List."))
