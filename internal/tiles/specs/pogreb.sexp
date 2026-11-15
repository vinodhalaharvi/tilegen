(tile-spec pogreb
  (kind store-backend)
  (doc "embedded key-value: Pogreb, a hash table on disk, one database per entity, values as JSON")
  (import pogreb github.com/akrylysov/pogreb)
  (error-package pogreb)
  (topics pogreb embedded-database)

  (satisfies (no-broker)
    "a library and a directory; there is no server")
  (illegal-when (cross-process)
    "Pogreb locks its directory; a second process cannot open the same database")
  (illegal-when (lookup-by-field)
    "a hash table finds a value by its whole key, and nothing else")
  (unverified (durable)
    "a Put is written to the file and reaches the disk when the operating system flushes it, unless the database was opened to sync: Options.BackgroundSyncInterval -1 syncs after every write, and that is the caller's choice, not this tile's")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; Pogreb has no transactions")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; Pogreb has no transactions")

  (cost
    (llm-work 4 (source derived "get, put, delete and an iterator, with the JSON and a sort by hand"))
    (maintenance 4 (source derived "no schema, no ordering and no range scan: every list reads the whole database and sorts it"))
    (dependency 1 (source derived "pogreb only, and no build-time tool"))
    (runtime 1 (source derived "a hash lookup on disk"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (db *pogreb.DB))
  (new (params (db *pogreb.DB))
    "return &{impl}{db: db}")

  (hint get
    "Call s.db.Get([]byte(the ID as a string)). A missing key is not an error: the value comes back nil, so return ErrNotFound then. Otherwise JSON-unmarshal it. The database is this entity's own, so the key is the ID alone.")

  (hint list
    "Take it := s.db.Items() and call it.Next() until it returns pogreb.ErrIterationDone, unmarshalling each value; then sort by ID, because a hash table yields keys in no order.")

  (hint save
    "JSON-marshal the value and call s.db.Put([]byte(the ID as a string), the bytes). It replaces what was there.")

  (hint delete
    "Call s.db.Delete([]byte(the ID as a string)). Deleting a missing key is not an error.")

  (hint count
    "Return int(s.db.Count()): the database holds this entity and nothing else."))
