(tile etcd
  (offers store)
  (doc "etcd: a replicated key-value store, one key per entity under a prefix, values as JSON")
  (engine etcd)
  (import clientv3 go.etcd.io/etcd/client/v3)
  (error-package clientv3)
  (topics etcd key-value)

  (satisfies (durable)
    "a Put returns after a quorum of the cluster has the write in its log")
  (satisfies (cross-process)
    "a cluster every process reaches over gRPC")
  (illegal-when (lookup-by-field)
    "keys only: a range over a key prefix is the one query, so finding by another field means a second index kept by hand")
  (illegal-when (no-broker)
    "a cluster to run: an odd number of members, with snapshots, compaction and defragmentation")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; a Txn can put both, and this tile does not generate one")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; a Txn can put both, and this tile does not generate one")

  (cost
    (llm-work 5 (source derived "get, put, delete and a prefix range, with the JSON and the key encoding by hand"))
    (maintenance 7 (source derived "no schema, and it is built for a little data that matters a great deal: a request is capped near 1.5 MiB and the whole keyspace at a few GiB by default, which a store of entities grows past"))
    (dependency 4 (source derived "the client brings gRPC and protobuf with it"))
    (runtime 2 (source derived "every write is a round of consensus"))
    (operations 7 (source derived "a quorum to keep healthy: members, backups, compaction and defragmentation")))

  (holds (client *clientv3.Client) (prefix string))
  (new (params (client *clientv3.Client))
    "return &{impl}{client: client, prefix: \"{names}/\"}")

  (hint get
    "Call s.client.Get(ctx, key). The key is s.prefix followed by the ID as a string. When the response's Kvs is empty return ErrNotFound; otherwise JSON-unmarshal Kvs[0].Value into the value.")
  (hint list
    "Call s.client.Get(ctx, s.prefix, clientv3.WithPrefix()) and JSON-unmarshal each of the response's Kvs. A range read comes back in key order.")
  (hint save
    "JSON-marshal the value and call s.client.Put(ctx, key, string(bytes)), which replaces whatever was there. The key is s.prefix followed by the ID as a string.")
  (hint delete
    "Call s.client.Delete(ctx, key). The key is s.prefix followed by the ID as a string. Deleting a missing key is not an error: the response's Deleted is 0.")
  (hint count
    "Call s.client.Get(ctx, s.prefix, clientv3.WithPrefix(), clientv3.WithCountOnly()) and return the response's Count."))
