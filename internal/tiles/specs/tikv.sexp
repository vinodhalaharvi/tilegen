(tile-spec tikv
  (kind store-backend)
  (doc "TiKV: one key per entity under a prefix in the raw key-value API, values as JSON")
  (engine tikv)
  (import rawkv github.com/tikv/client-go/v2/rawkv)
  (error-package rawkv)
  (topics tikv distributed-kv)

  (satisfies (durable)
    "a Put that returns has been committed by a Raft majority of the key's region")
  (satisfies (cross-process)
    "a cluster every process reaches over gRPC, linearizable for a single key")
  (illegal-when (lookup-by-field)
    "one keyspace, one key; a secondary index would be written and repaired by hand")
  (illegal-when (no-broker)
    "a TiKV cluster and its placement drivers to run")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; the raw API has no transaction over two keys; the transactional API (txnkv) does, and this tile does not use it")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; the raw API has no transaction over two keys; the transactional API (txnkv) does, and this tile does not use it")

  (cost
    (llm-work 5 (source derived "key encoding, a scan in bounded pages and JSON, all by hand"))
    (maintenance 4 (source derived "no schema and no migrations: a field added to the struct changes stored values silently"))
    (dependency 6 (source derived "the TiKV client brings gRPC, protobuf, the placement-driver client and their trees"))
    (runtime 2 (source derived "a round of consensus for every write"))
    (operations 8 (source derived "at least three TiKV nodes and three placement drivers to run, balance, back up and upgrade"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (client *rawkv.Client) (prefix string))
  (new (params (client *rawkv.Client))
    "return &{impl}{client: client, prefix: \"{names}/\"}")

  (hint get
    "Call s.client.Get(ctx, []byte(s.prefix + the ID as a string)). A missing key is not an error: the value comes back nil, so return ErrNotFound then. Otherwise JSON-unmarshal it.")

  (hint list
    "Call s.client.Scan(ctx, start, end, rawkv.MaxRawKVScanLimit) with start []byte(s.prefix) and end the prefix with its last byte incremented. It returns keys and values in key order, at most the limit: while a call returns a full page, scan again from the last key with a zero byte appended.")

  (hint save
    "JSON-marshal the value and call s.client.Put(ctx, []byte(s.prefix + the ID as a string), the bytes). An empty value is refused, which JSON never is.")

  (hint delete
    "Call s.client.Delete(ctx, the same key as Get). Deleting a missing key is not an error.")

  (hint count
    "Scan the same range as List with the rawkv.ScanKeyOnly() option and count the keys, page by page."))
