(tile aerospike
  (offers store)
  (doc "Aerospike: a set per entity, a record per value, as JSON in one bin")
  (engine aerospike)
  (import aerospike github.com/aerospike/aerospike-client-go/v8)
  (error-package aerospike)
  (topics aerospike)

  (satisfies (cross-process)
    "a cluster other processes connect to over the network")
  (illegal-when (lookup-by-field)
    "the value is one bin of JSON, found by its key; a query on a field would need that field in a bin of its own and a secondary index")
  (illegal-when (no-broker)
    "an Aerospike cluster to run")
  (unverified (durable)
    "a write is acknowledged from memory unless the namespace is configured to commit to its device first, which is the cluster's setting")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; Aerospike has multi-record transactions on strongly consistent namespaces, and this tile does not generate one")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; Aerospike has multi-record transactions on strongly consistent namespaces, and this tile does not generate one")

  (cost
    (llm-work 5 (source derived "keys to build, an error type of the client's own, a scan read from a channel, and the JSON"))
    (maintenance 4 (source derived "no schema, the namespace is fixed in the cluster's configuration, and a list or a count scans the whole set"))
    (dependency 3 (source derived "the client and its few modules"))
    (runtime 1 (source derived "one round trip to the node that owns the key"))
    (operations 7 (source derived "a cluster to run, with namespaces sized to its memory and devices"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (client *aerospike.Client) (namespace string) (set string))
  (new (params (client *aerospike.Client) (namespace string))
    "return &{impl}{client: client, namespace: namespace, set: \"{names}\"}")

  (hint get
    "Build key, err := aerospike.NewKey(s.namespace, s.set, the ID as a string) and call rec, err := s.client.Get(nil, key). The client's methods return aerospike.Error, which is an error. When errors.Is(err, aerospike.ErrKeyNotFound), return ErrNotFound. The value is the bin \"data\": JSON-unmarshal rec.Bins[\"data\"].([]byte). The client takes no context, so check ctx.Err() first.")

  (hint list
    "Call rs, err := s.client.ScanAll(nil, s.namespace, s.set) and defer rs.Close(). Range over rs.Results(): a result with a non-nil Err ends the scan with that error; otherwise JSON-unmarshal res.Record.Bins[\"data\"].([]byte). A scan promises no order, so sort the values by ID before returning.")

  (hint save
    "JSON-marshal the value, build the key with aerospike.NewKey(s.namespace, s.set, the ID as a string), and call s.client.Put(nil, key, aerospike.BinMap{\"data\": the bytes}). It replaces the record's bin.")

  (hint delete
    "Build the key and call s.client.Delete(nil, key). Its first result says whether the record existed; a missing record is not an error.")

  (hint count
    "Take policy := aerospike.NewScanPolicy() and set policy.IncludeBinData = false, so that no value is sent back. Call s.client.ScanAll(policy, s.namespace, s.set) and count the results, returning the first non-nil Err."))
