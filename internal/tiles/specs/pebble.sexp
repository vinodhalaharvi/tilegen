; The pebble tile. `tilegen -out . internal/tiles/specs/pebble.sexp` regenerates its
; registration in internal/tiles/store_pebble.go; the two holes there are filled by hand.

(project tilegen-tiles
  (module github.com/vinodhalaharvi/tilegen)
  (go 1.26)
  (require (pebble github.com/cockroachdb/pebble v1.1.5))

  (package tiles
    (dir "internal/tiles")

    (tile-spec pebble
      (kind store-backend)
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
        (runtime 2 (source derived "an LSM tree with background compaction"))))))
