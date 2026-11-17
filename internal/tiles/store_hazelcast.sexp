(tile hazelcast
  (offers store)
  (doc "Hazelcast: a distributed map per entity, values as JSON")
  (engine hazelcast)
  (import hazelcast github.com/hazelcast/hazelcast-go-client)
  (import serialization github.com/hazelcast/hazelcast-go-client/serialization)
  (import predicate github.com/hazelcast/hazelcast-go-client/predicate)
  (error-package hazelcast)
  (topics hazelcast map)

  (satisfies (cross-process)
    "a cluster every process connects to as a client")
  (satisfies (lookup-by-field)
    "a predicate on an attribute of the JSON value; it is evaluated on every entry unless the map has an index on it")
  (illegal-when (durable)
    "a map lives in its members' memory: a cluster that stops forgets it, unless a persistence feature this tile does not configure is licensed and turned on")
  (illegal-when (no-broker)
    "a Hazelcast cluster to run")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; Hazelcast has transactions over maps, and this tile does not generate one")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; Hazelcast has transactions over maps, and this tile does not generate one")

  (cost
    (llm-work 4 (source derived "map calls that take and return interface values, a JSON value type of the client's own, and predicates"))
    (maintenance 4 (source derived "no schema, and memory to size for everything the map holds"))
    (dependency 2 (source derived "the client and its few modules"))
    (runtime 1 (source derived "one round trip to the member that owns the key"))
    (operations 6 (source derived "a cluster of JVMs to run, size and upgrade"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (m *hazelcast.Map))
  (new (params (m *hazelcast.Map))
    "return &{impl}{m: m}")

  (hint get
    "Call val, err := s.m.Get(ctx, the ID as a string). A nil val means there is no such entry: return ErrNotFound. Otherwise the value is a serialization.JSON, which is a []byte: JSON-unmarshal val.(serialization.JSON). m is this entity's own map, taken by the caller with client.GetMap.")

  (hint list
    "Call vals, err := s.m.GetValues(ctx) and JSON-unmarshal each val.(serialization.JSON). A map promises no order, so sort the values by ID before returning.")

  (hint save
    "JSON-marshal the value and call s.m.Set(ctx, the ID as a string, serialization.JSON(the bytes)). Storing it as serialization.JSON, not as plain bytes, is what lets the cluster read its attributes for a predicate.")

  (hint delete
    "Call s.m.Delete(ctx, the ID as a string). Deleting a missing entry is not an error.")

  (hint count
    "Call s.m.Size(ctx).")

  (hint list-by
    "Call vals, err := s.m.GetValuesWithPredicate(ctx, predicate.Equal(KEY, {param})), where KEY is the key the entity's JSON uses for {field}. Unmarshal and sort as List does.")

  (hint get-by
    "Call s.m.GetValuesWithPredicate(ctx, predicate.Equal(KEY, {param})), where KEY is the key the entity's JSON uses for {field}. With no values, return ErrNotFound; otherwise unmarshal them and return the one with the lowest ID.")

  (hint count-by
    "Call s.m.GetValuesWithPredicate(ctx, predicate.Equal(KEY, {param})), where KEY is the key the entity's JSON uses for {field}, and return how many came back.")

  (hint exists-by
    "Count as CountBy would, with the same predicate on {field} and {param}, and report whether the count is above zero.")

  (hint delete-by
    "Call s.m.RemoveAll(ctx, predicate.Equal(KEY, {param})), where KEY is the key the entity's JSON uses for {field}."))
