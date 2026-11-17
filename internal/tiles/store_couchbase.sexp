(tile couchbase
  (offers store)
  (doc "Couchbase through gocb: a collection per entity, a document per value")
  (engine couchbase)
  (import gocb github.com/couchbase/gocb/v2)
  (error-package gocb)
  (topics couchbase gocb)

  (satisfies (durable)
    "every write this tile asks for waits until a majority of the bucket's nodes have it on disk")
  (satisfies (cross-process)
    "a cluster other processes connect to over the network")
  (satisfies (lookup-by-field)
    "a SQL++ query on the field; it needs an index on the collection, created outside this code, and fails without one")
  (illegal-when (no-broker)
    "a Couchbase cluster to run or pay for")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; Couchbase has multi-document transactions, and this tile does not generate one")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; Couchbase has multi-document transactions, and this tile does not generate one")

  (cost
    (llm-work 5 (source derived "key-value calls for one document, SQL++ for everything else, and two kinds of result to read"))
    (maintenance 5 (source derived "the collection and its indexes are created outside this code, and a list or a count fails until a primary index exists"))
    (dependency 4 (source derived "gocb and the core client under it"))
    (runtime 3 (source derived "a write waits for disk on a majority of nodes"))
    (operations 7 (source derived "a cluster to run, size and rebalance, or a managed one to pay for"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (scope *gocb.Scope) (coll *gocb.Collection))
  (new (params (scope *gocb.Scope))
    "return &{impl}{scope: scope, coll: scope.Collection(\"{names}\")}")

  (hint get
    "Call res, err := s.coll.Get(the ID as a string, &gocb.GetOptions{Context: ctx}). When errors.Is(err, gocb.ErrDocumentNotFound), return ErrNotFound. Otherwise decode with res.Content(&v). The collection is created outside this code.")

  (hint list
    "Call rows, err := s.scope.Query(\"SELECT d.* FROM `\"+s.coll.Name()+\"` AS d ORDER BY META(d).id\", &gocb.QueryOptions{Context: ctx, ScanConsistency: gocb.QueryScanConsistencyRequestPlus}). RequestPlus makes the query wait for the index to catch up with writes already made. Defer rows.Close(); while rows.Next(), decode with rows.Row(&v); return rows.Err() at the end. The query needs a primary index on the collection and fails without one.")

  (hint save
    "Call s.coll.Upsert(the ID as a string, the value, &gocb.UpsertOptions{Context: ctx, DurabilityLevel: gocb.DurabilityLevelPersistToMajority}). The client encodes the value as JSON itself. The durability level is what makes the write wait for disk; without it Upsert returns once one node has the document in memory.")

  (hint delete
    "Call s.coll.Remove(the ID as a string, &gocb.RemoveOptions{Context: ctx, DurabilityLevel: gocb.DurabilityLevelPersistToMajority}). When errors.Is(err, gocb.ErrDocumentNotFound) it was already gone: treat that as success.")

  (hint count
    "Query \"SELECT COUNT(*) AS n FROM `\"+s.coll.Name()+\"`\" with the same options as List and read the single row with rows.One(&row), row a struct with N int64 tagged json:\"n\".")

  (hint list-by
    "Call s.scope.Query(\"SELECT d.* FROM `\"+s.coll.Name()+\"` AS d WHERE d.`KEY` = $1 ORDER BY META(d).id\", &gocb.QueryOptions{Context: ctx, PositionalParameters: []interface{}{{param}}, ScanConsistency: gocb.QueryScanConsistencyRequestPlus}), where KEY is the key the entity's JSON uses for {field}. Read the rows as List does.")

  (hint get-by
    "Run the ListBy query for {field} with LIMIT 1 added, and read it with rows.One(&v). When errors.Is(err, gocb.ErrNoResult), return ErrNotFound.")

  (hint count-by
    "Query \"SELECT COUNT(*) AS n FROM `\"+s.coll.Name()+\"` AS d WHERE d.`KEY` = $1\" with {param} as the positional parameter, where KEY is the key the entity's JSON uses for {field}, and read the single row with rows.One.")

  (hint exists-by
    "Count as CountBy would, with the same WHERE on {field} and {param}, and report whether the count is above zero.")

  (hint delete-by
    "Query \"DELETE FROM `\"+s.coll.Name()+\"` AS d WHERE d.`KEY` = $1\" with {param} as the positional parameter, where KEY is the key the entity's JSON uses for {field}, and return rows.Err() after rows.Close()."))
