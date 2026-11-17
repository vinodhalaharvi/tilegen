(tile sqlx
  (offers store)
  (doc "sqlx over database/sql: a table per entity with a column per field, SQL by hand")
  (import sqlx github.com/jmoiron/sqlx)
  (error-package sql)
  (topics sqlx sql)

  (satisfies (lookup-by-field)
    "a WHERE on the field's column, and an index when the table has one")
  (unverified (durable)
    "it depends on the database the *sqlx.DB was opened on, which is the caller's")
  (unverified (cross-process)
    "it depends on the database the *sqlx.DB was opened on, which is the caller's")
  (unverified (no-broker)
    "it depends on the database the *sqlx.DB was opened on, which is the caller's")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; db.BeginTxx can hold both, and this tile does not generate it")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; db.BeginTxx can hold both, and this tile does not generate it")

  (cost
    (llm-work 5 (source derived "every statement by hand, with a column list kept in step with the struct and an upsert spelled for one database"))
    (maintenance 4 (source derived "the table is created outside this code, a field with no column fails at run time, and nothing checks the SQL before it runs"))
    (dependency 1 (source derived "sqlx only, and the driver the caller chooses"))
    (runtime 1 (source derived "a statement per call"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (db *sqlx.DB) (table string))
  (new (params (db *sqlx.DB))
    "return &{impl}{db: db, table: \"{names}\"}")

  (hint get
    "Declare var v {entity} and call err := s.db.GetContext(ctx, &v, s.db.Rebind(\"SELECT * FROM \"+s.table+\" WHERE id = ?\"), the ID). When errors.Is(err, sql.ErrNoRows), return ErrNotFound. Rebind turns ? into the placeholders the open database wants. sqlx matches a column to a field by the field's name in lower case, so SellerID is the column sellerid, unless the struct carries db tags; the table is created outside this code with columns named that way.")

  (hint list
    "Declare a slice of {entity} and call s.db.SelectContext(ctx, &values, \"SELECT * FROM \"+s.table+\" ORDER BY id\"). No rows is an empty slice, not an error.")

  (hint save
    "Call s.db.NamedExecContext(ctx, the statement, the value), with the statement an upsert that names every column and its :name parameter, each the field's name in lower case: \"INSERT INTO T (id, ...) VALUES (:id, ...) ON CONFLICT (id) DO UPDATE SET col = EXCLUDED.col, ...\" on Postgres and SQLite, or \"... ON DUPLICATE KEY UPDATE col = VALUES(col), ...\" on MySQL. Say in a comment which database it is written for.")

  (hint delete
    "Call s.db.ExecContext(ctx, s.db.Rebind(\"DELETE FROM \"+s.table+\" WHERE id = ?\"), the ID). Deleting a missing row is not an error.")

  (hint count
    "Declare var n int64 and call s.db.GetContext(ctx, &n, \"SELECT COUNT(*) FROM \"+s.table).")

  (hint list-by
    "Call s.db.SelectContext(ctx, &values, s.db.Rebind(\"SELECT * FROM \"+s.table+\" WHERE COLUMN = ? ORDER BY id\"), {param}), where COLUMN is {field} in lower case.")

  (hint get-by
    "Call s.db.GetContext(ctx, &v, s.db.Rebind(\"SELECT * FROM \"+s.table+\" WHERE COLUMN = ? ORDER BY id LIMIT 1\"), {param}), where COLUMN is {field} in lower case. When errors.Is(err, sql.ErrNoRows), return ErrNotFound. SQL Server spells the limit TOP 1 after SELECT.")

  (hint count-by
    "Call s.db.GetContext(ctx, &n, s.db.Rebind(\"SELECT COUNT(*) FROM \"+s.table+\" WHERE COLUMN = ?\"), {param}), where COLUMN is {field} in lower case.")

  (hint exists-by
    "Count as CountBy would, with the same WHERE on {field} and {param}, and report whether the count is above zero.")

  (hint delete-by
    "Call s.db.ExecContext(ctx, s.db.Rebind(\"DELETE FROM \"+s.table+\" WHERE COLUMN = ?\"), {param}), where COLUMN is {field} in lower case."))
