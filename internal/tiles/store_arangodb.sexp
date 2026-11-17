(tile arangodb
  (offers store)
  (doc "ArangoDB: a collection per entity, a document per value")
  (alias arango)
  (engine arangodb)
  (import arangodb github.com/arangodb/go-driver/v2/arangodb)
  (import utils github.com/arangodb/go-driver/v2/utils)
  (error-package github.com/arangodb/go-driver/v2/arangodb/shared)
  (topics arangodb aql)

  (satisfies (cross-process)
    "a server other processes connect to over the network")
  (satisfies (lookup-by-field)
    "an AQL filter on the field; it reads every document unless an index on that attribute is added")
  (illegal-when (no-broker)
    "an ArangoDB server or cluster to run")
  (unverified (durable)
    "a write is acknowledged before it is synced to disk unless the collection was created with waitForSync, which is outside this code")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; a stream transaction can hold both, and this tile does not generate it")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; a stream transaction can hold both, and this tile does not generate it")

  (cost
    (llm-work 5 (source derived "document calls for one value, AQL with bind variables for the rest, a wrapper for the key, and a cursor to drain"))
    (maintenance 4 (source derived "no schema, the collection is created outside this code, and a filter reads every document until an index is added"))
    (dependency 3 (source derived "the driver and its HTTP and encoding modules"))
    (runtime 2 (source derived "an HTTP round trip per call"))
    (operations 6 (source derived "a server or a cluster to run, with backups and upgrades"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (db arangodb.Database) (col arangodb.Collection))
  (new (params (db arangodb.Database) (col arangodb.Collection))
    "return &{impl}{db: db, col: col}")

  (hint get
    "A document is stored as struct{ Key string `json:\"_key\"`; Data {entity} `json:\"data\"` }: ArangoDB wants its key in _key, and the entity sits beside it. Declare one and call s.col.ReadDocument(ctx, the ID as a string, &doc). When shared.IsNotFound(err), return ErrNotFound. Return the Data. col is this entity's collection, opened by the caller with db.GetCollection.")

  (hint list
    "Call cur, err := s.db.Query(ctx, \"FOR d IN @@col SORT d._key RETURN d.data\", &arangodb.QueryOptions{BindVars: map[string]interface{}{\"@col\": s.col.Name()}}) and defer cur.Close(). While cur.HasMore(), read the next {entity} with cur.ReadDocument(ctx, &v).")

  (hint save
    "Wrap the value as struct{ Key string `json:\"_key\"`; Data {entity} `json:\"data\"` } with Key the ID as a string, and call s.col.CreateDocumentWithOptions(ctx, the wrapper, &arangodb.CollectionDocumentCreateOptions{OverwriteMode: utils.NewType(arangodb.CollectionDocumentCreateOverwriteModeReplace)}). The overwrite mode makes it replace a document that is already there; without it a second save is a conflict.")

  (hint delete
    "Call s.col.DeleteDocument(ctx, the ID as a string). When shared.IsNotFound(err) it was already gone: treat that as success.")

  (hint count
    "Call s.col.Count(ctx).")

  (hint list-by
    "Call s.db.Query(ctx, \"FOR d IN @@col FILTER d.data.KEY == @v SORT d._key RETURN d.data\", &arangodb.QueryOptions{BindVars: map[string]interface{}{\"@col\": s.col.Name(), \"v\": {param}}}), where KEY is the key the entity's JSON uses for {field}. Drain the cursor as List does.")

  (hint get-by
    "Run the ListBy query for {field} with LIMIT 1 before RETURN. When cur.HasMore() is false, return ErrNotFound; otherwise read the one document.")

  (hint count-by
    "Call s.db.Query(ctx, \"FOR d IN @@col FILTER d.data.KEY == @v COLLECT WITH COUNT INTO n RETURN n\", with the same bind variables as ListBy), where KEY is the key the entity's JSON uses for {field}, and read the single int64 with cur.ReadDocument(ctx, &n).")

  (hint exists-by
    "Count as CountBy would, with the same FILTER on {field} and {param}, and report whether the count is above zero.")

  (hint delete-by
    "Call s.db.Query(ctx, \"FOR d IN @@col FILTER d.data.KEY == @v REMOVE d IN @@col\", with the same bind variables as ListBy), where KEY is the key the entity's JSON uses for {field}, and close the cursor."))
