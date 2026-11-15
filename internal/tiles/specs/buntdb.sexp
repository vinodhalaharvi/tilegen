(tile-spec buntdb
  (kind store-backend)
  (doc "embedded key-value: BuntDB, in memory with an append-only file, values as JSON")
  (import buntdb github.com/tidwall/buntdb)
  (error-package buntdb)
  (topics buntdb embedded-database)

  (satisfies (no-broker)
    "a library and a file; there is no server")
  (illegal-when (cross-process)
    "the whole database lives in one process's memory, and the file is not locked against a second one")
  (illegal-when (lookup-by-field)
    "an index is declared over a pattern of keys and a comparison of values; this tile declares none")
  (unverified (durable)
    "by default the file is synced once a second, so a crash can lose the last second of writes; SyncPolicy buntdb.Always closes that, and it is the caller's Config, not this tile's")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; an Update is one, and this tile does not generate it")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; an Update is one, and this tile does not generate it")

  (cost
    (llm-work 4 (source derived "View and Update closures over string keys and values, with the JSON by hand"))
    (maintenance 3 (source derived "no schema and no migrations, and every value is held in memory"))
    (dependency 1 (source derived "buntdb and a few small modules, and no build-time tool"))
    (runtime 1 (source derived "a map and a B-tree in memory"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (db *buntdb.DB) (prefix string))
  (new (params (db *buntdb.DB))
    "return &{impl}{db: db, prefix: \"{names}:\"}")

  (hint get
    "In s.db.View(func(tx *buntdb.Tx) error {...}), call tx.Get(s.prefix + the ID as a string). When errors.Is(err, buntdb.ErrNotFound), return ErrNotFound. Keys and values are strings: JSON-unmarshal []byte(the value).")

  (hint list
    "In s.db.View, call tx.AscendKeys(s.prefix+\"*\", func(key, value string) bool {...}), unmarshalling each value and returning true to go on. Keys come back in order. Carry an unmarshal error out through a variable: the callback cannot return one.")

  (hint save
    "JSON-marshal the value and, in s.db.Update(func(tx *buntdb.Tx) error {...}), call tx.Set(s.prefix + the ID as a string, string(the bytes), nil). It replaces what was there.")

  (hint delete
    "In s.db.Update, call tx.Delete(the same key as Get). It returns buntdb.ErrNotFound for a missing key: treat that as success.")

  (hint count
    "In s.db.View, walk tx.AscendKeys(s.prefix+\"*\", ...) and count, without unmarshalling."))
