(tile nats-kv
  (offers store)
  (doc "NATS JetStream key-value: a bucket per entity, values as JSON")
  (alias natskv)
  (engine nats)
  (import jetstream github.com/nats-io/nats.go/jetstream)
  (error-package jetstream)
  (topics nats jetstream key-value)

  (satisfies (cross-process)
    "a server every process reaches over the network")
  (illegal-when (lookup-by-field)
    "a bucket has one key; there is no index on what a value holds")
  (illegal-when (no-broker)
    "a NATS server with JetStream to run")
  (unverified (durable)
    "a Put is acknowledged once the bucket's stream has stored it, and what survives a restart depends on the bucket: file or memory storage, and how many replicas")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; JetStream has no transaction over two keys")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; JetStream has no transaction over two keys")

  (cost
    (llm-work 4 (source derived "get, put, delete and a key listing, with the JSON by hand"))
    (maintenance 4 (source derived "no schema, a key may hold only letters, digits and - _ = . /, and a list or a count walks every key"))
    (dependency 2 (source derived "nats.go and its few modules"))
    (runtime 1 (source derived "a round trip per call"))
    (operations 4 (source derived "a NATS server with JetStream storage to run, size and back up"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (kv jetstream.KeyValue))
  (new (params (kv jetstream.KeyValue))
    "return &{impl}{kv: kv}")

  (hint get
    "Call s.kv.Get(ctx, the ID as a string). When errors.Is(err, jetstream.ErrKeyNotFound), return ErrNotFound: a deleted key reports the same. Otherwise JSON-unmarshal entry.Value().")

  (hint list
    "Call s.kv.ListKeys(ctx), range over lister.Keys(), Get each key and unmarshal it, and sort the result by ID: keys come back in no promised order. An empty bucket may be reported as jetstream.ErrNoKeysFound: treat that as an empty list.")

  (hint save
    "JSON-marshal the value and call s.kv.Put(ctx, the ID as a string, the bytes). The bucket is this entity's own, so the key is the ID alone.")

  (hint delete
    "Call s.kv.Delete(ctx, the ID as a string). It writes a delete marker, and deleting a missing key is not an error.")

  (hint count
    "Call s.kv.ListKeys(ctx) and count what lister.Keys() yields; jetstream.ErrNoKeysFound, if it comes back, means zero."))
