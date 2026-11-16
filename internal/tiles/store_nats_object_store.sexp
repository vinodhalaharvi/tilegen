(tile nats-object-store
  (offers store)
  (doc "NATS JetStream object store: a bucket per entity, an object per value, as JSON")
  (alias natsobj)
  (engine nats)
  (import jetstream github.com/nats-io/nats.go/jetstream)
  (error-package jetstream)
  (topics nats jetstream object-store)

  (satisfies (cross-process)
    "a server every process reaches over the network")
  (illegal-when (lookup-by-field)
    "an object is found by its name; there is no index on what it holds")
  (illegal-when (no-broker)
    "a NATS server with JetStream to run")
  (unverified (durable)
    "an object is acknowledged once the bucket's stream has stored it, and what survives a restart depends on the bucket: file or memory storage, and how many replicas")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; JetStream has no transaction over two objects")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; JetStream has no transaction over two objects")

  (cost
    (llm-work 4 (source derived "put, get, delete and a listing, with the JSON by hand"))
    (maintenance 4 (source derived "no schema, an object store is built for large values and is heavy for small ones, and a list reads every object"))
    (dependency 2 (source derived "nats.go and its few modules"))
    (runtime 2 (source derived "an object is chunked into messages on the way in and out"))
    (operations 4 (source derived "a NATS server with JetStream storage to run, size and back up"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (objects jetstream.ObjectStore))
  (new (params (objects jetstream.ObjectStore))
    "return &{impl}{objects: objects}")

  (hint get
    "Call s.objects.GetBytes(ctx, the ID as a string). When errors.Is(err, jetstream.ErrObjectNotFound), return ErrNotFound. Otherwise JSON-unmarshal the bytes. The bucket is this entity's own, so the name is the ID alone.")

  (hint list
    "Call s.objects.List(ctx); jetstream.ErrNoObjectsFound means an empty bucket, so return an empty list. Sort the infos by Name, skip one that is Deleted, and GetBytes and unmarshal each of the rest.")

  (hint save
    "JSON-marshal the value and call s.objects.PutBytes(ctx, the ID as a string, the bytes). It replaces the object of that name.")

  (hint delete
    "Call s.objects.Delete(ctx, the ID as a string). When errors.Is(err, jetstream.ErrObjectNotFound) it was already gone: treat that as success.")

  (hint count
    "Call s.objects.List(ctx) and count the infos that are not Deleted; jetstream.ErrNoObjectsFound means zero."))
