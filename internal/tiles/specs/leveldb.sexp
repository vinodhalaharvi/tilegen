; The leveldb tile. `tilegen -out . internal/tiles/specs/leveldb.sexp` regenerates its
; registration in internal/tiles/store_leveldb.go; the two holes there are filled by hand.

(project tilegen-tiles
  (module github.com/vinodhalaharvi/tilegen)
  (go 1.26)
  (require (leveldb github.com/syndtr/goleveldb v1.0.0))

  (package tiles
    (dir "internal/tiles")

    (tile-spec leveldb
      (kind store-backend)
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
        (runtime 2 (source derived "an LSM tree with background compaction"))))))
