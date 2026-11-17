(tile bun
  (offers store)
  (doc "Bun: the entity as a model, on whatever SQL database the caller opened")
  (import bun github.com/uptrace/bun)
  (error-package sql)
  (topics bun orm sql)

  (satisfies (lookup-by-field)
    "a WHERE on the field's column, and an index when the table has one")
  (unverified (durable)
    "it depends on the database the *bun.DB was opened on, which is the caller's")
  (unverified (cross-process)
    "it depends on the database the *bun.DB was opened on, which is the caller's")
  (unverified (no-broker)
    "it depends on the database the *bun.DB was opened on, which is the caller's")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; db.RunInTx can hold both, and this tile does not generate it")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; db.RunInTx can hold both, and this tile does not generate it")

  (cost
    (llm-work 4 (source derived "a query builder call per method, and an upsert whose conflict clause is spelled differently on each database"))
    (maintenance 4 (source derived "the table follows the struct by convention and is created outside this code, and a renamed field is a column that no longer matches"))
    (dependency 3 (source derived "Bun, its dialect module and the driver the caller chooses"))
    (runtime 2 (source derived "reflection on every call, over the driver"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (db *bun.DB))
  (new (params (db *bun.DB))
    "return &{impl}{db: db}")

  (hint get
    "Declare var v {entity} and call err := s.db.NewSelect().Model(&v).Where(\"id = ?\", the ID).Scan(ctx). When errors.Is(err, sql.ErrNoRows), return ErrNotFound. Bun names the table and its columns from the struct in snake_case, and takes the ID field as the primary key; the table is the caller's to create, with db.NewCreateTable().Model((*{entity})(nil)).")

  (hint list
    "Declare a slice of {entity} and call s.db.NewSelect().Model(&values).Order(\"id\").Scan(ctx). No rows is an empty slice, not an error.")

  (hint save
    "Call s.db.NewInsert().Model(the value).On(\"CONFLICT (id) DO UPDATE\").Exec(ctx), adding one .Set(\"COLUMN = EXCLUDED.COLUMN\") for each column other than id. That is the form Postgres and SQLite take; on MySQL the clause is On(\"DUPLICATE KEY UPDATE\") and each Set is \"COLUMN = VALUES(COLUMN)\". Say in a comment which database the clause is written for.")

  (hint delete
    "Call s.db.NewDelete().Model((*{entity})(nil)).Where(\"id = ?\", the ID).Exec(ctx). Deleting a missing row is not an error.")

  (hint count
    "Call n, err := s.db.NewSelect().Model((*{entity})(nil)).Count(ctx).")

  (hint list-by
    "Call s.db.NewSelect().Model(&values).Where(\"? = ?\", bun.Ident(COLUMN), {param}).Order(\"id\").Scan(ctx), where COLUMN is {field} as Bun names it: snake_case, so SellerID is seller_id.")

  (hint get-by
    "Call s.db.NewSelect().Model(&v).Where(\"? = ?\", bun.Ident(COLUMN), {param}).Order(\"id\").Limit(1).Scan(ctx), where COLUMN is {field} in snake_case. When errors.Is(err, sql.ErrNoRows), return ErrNotFound.")

  (hint count-by
    "Call s.db.NewSelect().Model((*{entity})(nil)).Where(\"? = ?\", bun.Ident(COLUMN), {param}).Count(ctx), where COLUMN is {field} in snake_case.")

  (hint exists-by
    "Call s.db.NewSelect().Model((*{entity})(nil)).Where(\"? = ?\", bun.Ident(COLUMN), {param}).Exists(ctx), where COLUMN is {field} in snake_case.")

  (hint delete-by
    "Call s.db.NewDelete().Model((*{entity})(nil)).Where(\"? = ?\", bun.Ident(COLUMN), {param}).Exec(ctx), where COLUMN is {field} in snake_case."))
