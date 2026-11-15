(tile cassandra
  (offers store)
  (doc "Cassandra: one table per entity keyed by id, the value as JSON in a blob")
  (engine cassandra)
  (import gocql github.com/gocql/gocql)
  (error-package gocql)
  (topics cassandra nosql)

  (satisfies (cross-process)
    "a cluster every process reaches over the network")
  (illegal-when (lookup-by-field)
    "the value is one opaque blob found by its partition key; a query on anything inside it has nothing to use")
  (illegal-when (no-broker)
    "a Cassandra cluster to run")
  (unverified (durable)
    "a write is acknowledged by as many replicas as the session's consistency level asks, and each appends it to a commit log that is synced to disk periodically by default; what one node's crash can lose is the cluster's configuration")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; a logged batch applies all of its statements eventually, without isolation, and this tile does not generate one")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; a logged batch applies all of its statements eventually, without isolation, and this tile does not generate one")

  (cost
    (llm-work 5 (source derived "CQL by hand, an iterator to close, and results that arrive in token order"))
    (maintenance 5 (source derived "the table (id text PRIMARY KEY, data blob) is created outside this code, a list or a count scans every partition, and a delete leaves a tombstone"))
    (dependency 2 (source derived "gocql and a few small modules"))
    (runtime 2 (source derived "a round trip per call"))
    (operations 8 (source derived "a cluster of at least three nodes to run, repair, compact, back up and upgrade"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (session *gocql.Session) (table string))
  (new (params (session *gocql.Session))
    "return &{impl}{session: session, table: \"{names}\"}")

  (hint get
    "The table is (id text PRIMARY KEY, data blob), created outside this code. Call s.session.Query(\"SELECT data FROM \"+s.table+\" WHERE id = ?\", the ID as a string).WithContext(ctx).Scan(&data) with data a []byte. When errors.Is(err, gocql.ErrNotFound), return ErrNotFound. Otherwise JSON-unmarshal data.")

  (hint list
    "Take iter := s.session.Query(\"SELECT data FROM \"+s.table).WithContext(ctx).Iter(), loop while iter.Scan(&data), unmarshalling each, and return iter.Close()'s error. Rows arrive in token order, which is not ID order: sort the result by ID. This reads every partition.")

  (hint save
    "JSON-marshal the value and call s.session.Query(\"INSERT INTO \"+s.table+\" (id, data) VALUES (?, ?)\", the ID as a string, the bytes).WithContext(ctx).Exec(). An INSERT replaces a row with the same key.")

  (hint delete
    "Call s.session.Query(\"DELETE FROM \"+s.table+\" WHERE id = ?\", the ID as a string).WithContext(ctx).Exec(). Deleting a missing row is not an error.")

  (hint count
    "Call s.session.Query(\"SELECT count(*) FROM \"+s.table).WithContext(ctx).Scan(&n) with n an int64. It reads every partition, and times out on a large table."))
