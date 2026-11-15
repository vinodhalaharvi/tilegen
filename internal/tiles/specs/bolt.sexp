(tile-spec bolt
  (kind store-backend)
  (doc "embedded key-value: one file, a bucket per entity, values as JSON")
  (import bbolt go.etcd.io/bbolt)
  (error-package bbolt)
  (topics bbolt embedded-database)

  (satisfies (durable)
    "one memory-mapped file; a committed write transaction is fsynced before it returns")
  (satisfies (no-broker)
    "a file and a library; there is no server")
  (illegal-when (cross-process)
    "bbolt takes an exclusive lock on the file; a second process cannot open it")
  (illegal-when (lookup-by-field)
    "a bucket has one key; a secondary index would be written and repaired by hand")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; this tile does not generate one")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; this tile does not generate one")

  (cost
    (llm-work 7 (source derived "every read, write and scan written by hand, including the key encoding"))
    (maintenance 6 (source derived "no schema and no migrations: a field added to the struct changes stored documents silently, and nothing reports it"))
    (dependency 1 (source derived "bbolt only, and no build-time tool"))
    (runtime 1))

  (holds (db *bbolt.DB) (bucket string))
  (new (params (db *bbolt.DB))
    "return &{impl}{db: db, bucket: \"{names}\"}")

  (hint get
    "In a View transaction, read the key from the bucket and unmarshal the JSON into the value. Return ErrNotFound when the key is absent.")
  (hint list
    "In a View transaction, walk the bucket with ForEach, unmarshal each value, and return them in key order.")
  (hint save
    "In an Update transaction, marshal the value to JSON and Put it under its ID, replacing whatever was there.")
  (hint delete
    "In an Update transaction, Delete the key. Deleting a missing key is not an error in bbolt.")
  (hint count
    "In a View transaction, return the bucket's Stats().KeyN."))
