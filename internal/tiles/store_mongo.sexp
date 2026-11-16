(tile mongo
  (offers store)
  (doc "mongo: a collection per entity, documents rather than rows")
  (form bson-docs)
  (import mongo go.mongodb.org/mongo-driver/mongo)
  (error-package mongo)
  (topics mongodb nosql)

  (satisfies (cross-process)
    "a server other processes connect to over the network")
  (satisfies (lookup-by-field)
    "a filter on any field, and an index when you add one")
  (illegal-when (no-broker)
    "a mongo to run, with backups and a replica set for durability")
  (unverified (durable)
    "a write is durable only under a write concern and a replica set; the default is weaker, and this tile does not set it")
  (unverified (outbox)
    "a multi-document transaction needs a replica set, and this tile does not generate one")
  (unverified (inbox)
    "a multi-document transaction needs a replica set, and this tile does not generate one")

  (cost
    (llm-work 5 (source derived "filters and updates written by hand, but no schema to keep in step"))
    (maintenance 3)
    (dependency 3 (source derived "the driver, and a server to run"))
    (runtime 2)
    (operations 5 (source derived "a server to run, and a replica set if you want durable writes")))

  (holds (coll *mongo.Collection))
  (new (params (db *mongo.Database))
    "return &{impl}{coll: db.Collection(\"{names}\")}")

  (hint get
    "FindOne with a filter on _id, then Decode into the value. Return ErrNotFound when the driver reports no documents.")
  (hint list
    "Find with an empty filter, sorted by _id, then decode the cursor into a slice.")
  (hint save
    "ReplaceOne with a filter on _id and the Upsert option, so a new value is inserted and an existing one replaced.")
  (hint delete
    "DeleteOne with a filter on _id. Deleting a missing document is not an error.")
  (hint count
    "CountDocuments with an empty filter.")
  (hint list-by
    "Find with a filter matching {field} against {param}, sorted by _id, then decode the cursor.")
  (hint get-by
    "FindOne with a filter matching {field} against {param}, sorted by _id. Return ErrNotFound when there is no document.")
  (hint count-by
    "CountDocuments with a filter matching {field} against {param}.")
  (hint exists-by
    "CountDocuments with a filter matching {field} against {param} and a limit of 1.")
  (hint delete-by
    "DeleteMany with a filter matching {field} against {param}."))
