(tile mssql-json
  (offers store)
  (doc "SQL Server through database/sql: a table per entity, the value as one JSON document")
  (alias mssqljson)
  (engine mssql)
  (import mssql github.com/microsoft/go-mssqldb)
  (error-package sql)
  (topics sql-server sql)

  (satisfies (durable)
    "a committed statement is on disk, and on its replicas where there are any")
  (satisfies (cross-process)
    "a server other processes connect to over the network")
  (satisfies (lookup-by-field)
    "a WHERE on a key inside the JSON document; it reads every row unless an index on that expression is added")
  (illegal-when (no-broker)
    "a SQL Server to run, with backups, connections and upgrades")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; this database can hold both in one, and this tile does not generate it")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; this database can hold both in one, and this tile does not generate it")

  (cost
    (llm-work 5 (source derived "six statements by hand, rows to scan and close, and the JSON"))
    (maintenance 5 (source derived "one column holds the whole value: nothing checks a field's type or that it is there, a renamed field leaves old rows behind, and the table is created outside this code"))
    (dependency 2 (source derived "the driver and its few modules"))
    (runtime 1 (source derived "a statement per call"))
    (operations 6 (source derived "a server to run, with backups, connections and upgrades"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (db *sql.DB) (table string))
  (new (params (db *sql.DB))
    "return &{impl}{db: db, table: \"{names}\"}")

  (hint get
    "The table is (id NVARCHAR(255) PRIMARY KEY, data NVARCHAR(MAX) NOT NULL), created outside this code. Call s.db.QueryRowContext(ctx, \"SELECT data FROM \"+s.table+\" WHERE id = @p1\", the ID as a string).Scan(&data) with data a []byte. When errors.Is(err, sql.ErrNoRows), return ErrNotFound. Otherwise JSON-unmarshal data.")

  (hint list
    "Call s.db.QueryContext(ctx, \"SELECT data FROM \"+s.table+\" ORDER BY id\"), defer rows.Close(), Scan and unmarshal each row while rows.Next(), and return rows.Err() at the end.")

  (hint save
    "JSON-marshal the value and call s.db.ExecContext(ctx, \"MERGE T WITH (HOLDLOCK) AS t USING (SELECT @p1 AS id, @p2 AS data) AS s ON t.id = s.id WHEN MATCHED THEN UPDATE SET data = s.data WHEN NOT MATCHED THEN INSERT (id, data) VALUES (s.id, s.data);\", the ID as a string, string(the bytes)), with the table's name in place of T. It inserts the row or replaces its document.")

  (hint delete
    "Call s.db.ExecContext(ctx, \"DELETE FROM \"+s.table+\" WHERE id = @p1\", the ID as a string). Deleting a missing row is not an error.")

  (hint count
    "Call s.db.QueryRowContext(ctx, \"SELECT COUNT(*) FROM \"+s.table).Scan(&n).")

  (hint list-by
    "Call s.db.QueryContext(ctx, \"SELECT data FROM \"+s.table+\" WHERE JSON_VALUE(data, '$.KEY') = @p1 ORDER BY id\", {param}), where KEY is the key the entity's JSON uses for {field} (the field's name unless a json tag says otherwise), and the value is compared as text. Scan and unmarshal each row as List does.")

  (hint get-by
    "Call s.db.QueryRowContext(ctx, \"SELECT data FROM \"+s.table+\" WHERE JSON_VALUE(data, '$.KEY') = @p1 ORDER BY id\", {param}).Scan(&data), where KEY is the key the entity's JSON uses for {field} (the field's name unless a json tag says otherwise), and the value is compared as text. QueryRow takes the first row; when errors.Is(err, sql.ErrNoRows), return ErrNotFound.")

  (hint count-by
    "Call s.db.QueryRowContext(ctx, \"SELECT COUNT(*) FROM \"+s.table+\" WHERE JSON_VALUE(data, '$.KEY') = @p1\", {param}).Scan(&n), where KEY is the key the entity's JSON uses for {field} (the field's name unless a json tag says otherwise), and the value is compared as text.")

  (hint exists-by
    "Count as CountBy would, with the same WHERE on {field} and {param}, and report whether the count is above zero.")

  (hint delete-by
    "Call s.db.ExecContext(ctx, \"DELETE FROM \"+s.table+\" WHERE JSON_VALUE(data, '$.KEY') = @p1\", {param}), where KEY is the key the entity's JSON uses for {field} (the field's name unless a json tag says otherwise), and the value is compared as text."))
