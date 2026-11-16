(tile sqlite-json
  (offers store)
  (doc "SQLite without cgo: a table per entity in one file, the value as one JSON document")
  (alias sqlitejson)
  (import sqlite modernc.org/sqlite)
  (error-package sql)
  (topics sqlite sql)

  (satisfies (durable)
    "the database is a file on disk, and a committed statement is in it")
  (satisfies (lookup-by-field)
    "a WHERE on a key inside the JSON document; it reads every row unless an index on that expression is added")
  (satisfies (no-broker)
    "one file and a library; there is no server")
  (illegal-when (cross-process)
    "a sqlite file serves one process; another would need its own connection to the same disk")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; this database can hold both in one, and this tile does not generate it")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; this database can hold both in one, and this tile does not generate it")

  (cost
    (llm-work 6 (source derived "SQL by hand in this database's own dialect: an upsert, lookups that reach inside the document, rows to scan and close, and the JSON"))
    (maintenance 5 (source derived "one column holds the whole value: nothing checks a field's type or that it is there, a renamed field leaves old rows behind, and the table is created outside this code"))
    (dependency 3 (source derived "a SQLite translated to Go: a large module, and no C compiler needed"))
    (runtime 1 (source derived "a statement per call"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (db *sql.DB) (table string))
  (new (params (db *sql.DB))
    "return &{impl}{db: db, table: \"{names}\"}")

  (hint get
    "The table is (id TEXT PRIMARY KEY, data TEXT NOT NULL), created outside this code. Call s.db.QueryRowContext(ctx, \"SELECT data FROM \"+s.table+\" WHERE id = ?\", the ID as a string).Scan(&data) with data a []byte. When errors.Is(err, sql.ErrNoRows), return ErrNotFound. Otherwise JSON-unmarshal data.")

  (hint list
    "Call s.db.QueryContext(ctx, \"SELECT data FROM \"+s.table+\" ORDER BY id\"), defer rows.Close(), Scan and unmarshal each row while rows.Next(), and return rows.Err() at the end.")

  (hint save
    "JSON-marshal the value and call s.db.ExecContext(ctx, \"INSERT INTO T (id, data) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET data = excluded.data\", the ID as a string, string(the bytes)), with the table's name in place of T. It inserts the row or replaces its document.")

  (hint delete
    "Call s.db.ExecContext(ctx, \"DELETE FROM \"+s.table+\" WHERE id = ?\", the ID as a string). Deleting a missing row is not an error.")

  (hint count
    "Call s.db.QueryRowContext(ctx, \"SELECT COUNT(*) FROM \"+s.table).Scan(&n).")

  (hint list-by
    "Call s.db.QueryContext(ctx, \"SELECT data FROM \"+s.table+\" WHERE json_extract(data, '$.KEY') = ? ORDER BY id\", {param}), where KEY is the key the entity's JSON uses for {field} (the field's name unless a json tag says otherwise), and the value is compared as text. Scan and unmarshal each row as List does.")

  (hint get-by
    "Call s.db.QueryRowContext(ctx, \"SELECT data FROM \"+s.table+\" WHERE json_extract(data, '$.KEY') = ? ORDER BY id\", {param}).Scan(&data), where KEY is the key the entity's JSON uses for {field} (the field's name unless a json tag says otherwise), and the value is compared as text. QueryRow takes the first row; when errors.Is(err, sql.ErrNoRows), return ErrNotFound.")

  (hint count-by
    "Call s.db.QueryRowContext(ctx, \"SELECT COUNT(*) FROM \"+s.table+\" WHERE json_extract(data, '$.KEY') = ?\", {param}).Scan(&n), where KEY is the key the entity's JSON uses for {field} (the field's name unless a json tag says otherwise), and the value is compared as text.")

  (hint exists-by
    "Count as CountBy would, with the same WHERE on {field} and {param}, and report whether the count is above zero.")

  (hint delete-by
    "Call s.db.ExecContext(ctx, \"DELETE FROM \"+s.table+\" WHERE json_extract(data, '$.KEY') = ?\", {param}), where KEY is the key the entity's JSON uses for {field} (the field's name unless a json tag says otherwise), and the value is compared as text."))
