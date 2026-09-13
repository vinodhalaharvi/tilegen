; tilegen, applied to itself. Each (tile-spec ...) generates one tile for
; tilegen's own registry: the registration compiled, the judgment left open.
;
;   tilegen -out . examples/tilespec        # from the tilegen checkout
;   tilegen fill examples/tilespec          # the LLM writes the two holes
;   make check
;
; (dir ".") puts the tile beside the ones already there, and the module
; path is tilegen's own, so <out> is a real module: fill can build it, and
; a hole that does not compile is put back rather than shipped.
(project tilegen-tiles
  (module github.com/vinodhalaharvi/tilegen)
  (go 1.26)
  (require (badger github.com/dgraph-io/badger/v4 v4.5.1))

  (package main
    (dir ".")

    (tile-spec badger
      (kind store-backend)
      (doc "embedded key-value: an LSM store, one keyspace, values as JSON")
      (import badger github.com/dgraph-io/badger/v4)
      (error-package badger)
      (topics badger embedded-database)
      (illegal-when (cross-process)
        "badger takes a directory lock; a second process cannot open the same store")
      (illegal-when (lookup-by-field)
        "one keyspace, one key; a secondary index would be written and repaired by hand")
      (cost
        (llm-work 4
          (source derived "id-keyed get, save and delete in one transaction; secondary indexes are excluded by illegal-when, not priced here"))
        (maintenance 3
          (source derived "no schema and no migrations: a field added to the struct changes stored values silently"))
        (dependency 2
          (source derived "badger and its own dependencies, but no server and no build-time tool"))
        (runtime 2
          (source derived "an LSM tree with background compaction, unlike bbolt's single file"))))))
