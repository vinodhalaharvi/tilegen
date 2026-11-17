(tile rqlite
  (offers store)
  (doc "rqlite: SQLite replicated by Raft, a table per entity, the value as one JSON document")
  (engine rqlite)
  (import gorqlite github.com/rqlite/gorqlite)
  (error-package gorqlite)
  (topics rqlite sqlite raft)

  (satisfies (durable)
    "a write is answered once a majority of the cluster's nodes have it in their Raft logs")
  (satisfies (cross-process)
    "a cluster other processes reach over HTTP")
  (satisfies (lookup-by-field)
    "a WHERE on a key inside the JSON document; it reads every row unless an index on that expression is added")
  (illegal-when (no-broker)
    "an rqlite node or cluster to run")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; rqlite can run several statements as one transaction in a single request, and this tile does not generate it")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; rqlite can run several statements as one transaction in a single request, and this tile does not generate it")

  (cost
    (llm-work 6 (source derived "SQL by hand, statements built as values, a result set with its own way of reading rows, and the JSON"))
    (maintenance 5 (source derived "one column holds the whole value: nothing checks a field's type or that it is there, and the table is created outside this code"))
    (dependency 1 (source derived "the client only"))
    (runtime 3 (source derived "an HTTP round trip per call, and a round of consensus for every write"))
    (operations 5 (source derived "a node, or a cluster of three, to run and back up"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (conn *gorqlite.Connection) (table string))
  (new (params (conn *gorqlite.Connection))
    "return &{impl}{conn: conn, table: \"{names}\"}")

  (hint get
    "The table is (id TEXT PRIMARY KEY, data TEXT NOT NULL), created outside this code. Call qr, err := s.conn.QueryOneParameterizedContext(ctx, gorqlite.ParameterizedStatement{Query: \"SELECT data FROM \"+s.table+\" WHERE id = ?\", Arguments: []interface{}{the ID as a string}}). When qr.Next() is false, return ErrNotFound. Otherwise qr.Scan(&data) with data a string, and JSON-unmarshal it.")

  (hint list
    "Call s.conn.QueryOneParameterizedContext with \"SELECT data FROM \"+s.table+\" ORDER BY id\" and no arguments; while qr.Next(), qr.Scan(&data) and unmarshal each.")

  (hint save
    "JSON-marshal the value and call s.conn.WriteOneParameterizedContext(ctx, gorqlite.ParameterizedStatement{Query: \"INSERT INTO \"+s.table+\" (id, data) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET data = excluded.data\", Arguments: []interface{}{the ID as a string, string(the bytes)}}). It inserts the row or replaces its document.")

  (hint delete
    "Call s.conn.WriteOneParameterizedContext with \"DELETE FROM \"+s.table+\" WHERE id = ?\" and the ID as a string. Deleting a missing row is not an error.")

  (hint count
    "Call s.conn.QueryOneParameterizedContext with \"SELECT COUNT(*) FROM \"+s.table; qr.Next() and qr.Scan(&n) with n an int64.")

  (hint list-by
    "Call s.conn.QueryOneParameterizedContext with \"SELECT data FROM \"+s.table+\" WHERE json_extract(data, '$.KEY') = ? ORDER BY id\" and {param} as the argument, where KEY is the key the entity's JSON uses for {field}. Read the rows as List does.")

  (hint get-by
    "Run the ListBy query for {field} with LIMIT 1 added. When qr.Next() is false, return ErrNotFound.")

  (hint count-by
    "Call s.conn.QueryOneParameterizedContext with \"SELECT COUNT(*) FROM \"+s.table+\" WHERE json_extract(data, '$.KEY') = ?\" and {param}, where KEY is the key the entity's JSON uses for {field}.")

  (hint exists-by
    "Count as CountBy would, with the same WHERE on {field} and {param}, and report whether the count is above zero.")

  (hint delete-by
    "Call s.conn.WriteOneParameterizedContext with \"DELETE FROM \"+s.table+\" WHERE json_extract(data, '$.KEY') = ?\" and {param}, where KEY is the key the entity's JSON uses for {field}."))
