(tile cockroach-json
  (offers store)
  (doc "CockroachDB through pgx: a table per entity, the value as one JSONB document")
  (alias cockroach)
  (engine cockroachdb)
  (import pgxpool github.com/jackc/pgx/v5/pgxpool)
  (error-package github.com/jackc/pgx/v5)
  (topics cockroachdb sql)

  (satisfies (durable)
    "a committed statement has been written by a majority of the range's replicas")
  (satisfies (cross-process)
    "a cluster other processes connect to over the network")
  (satisfies (lookup-by-field)
    "a WHERE on a key inside the JSONB document; it reads every row unless an inverted or computed-column index is added")
  (illegal-when (no-broker)
    "a CockroachDB cluster to run or pay for")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; a transaction can hold both, and this tile does not generate it")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; a transaction can hold both, and this tile does not generate it")

  (cost
    (llm-work 5 (source derived "six statements by hand, rows to scan, and the JSON"))
    (maintenance 5 (source derived "one column holds the whole value: nothing checks a field's type or that it is there, and the table is created outside this code"))
    (dependency 1 (source derived "pgx only"))
    (runtime 2 (source derived "a round of consensus for every write"))
    (operations 7 (source derived "a cluster of at least three nodes to run, or a managed one to pay for"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (pool *pgxpool.Pool) (table string))
  (new (params (pool *pgxpool.Pool))
    "return &{impl}{pool: pool, table: \"{names}\"}")

  (hint get
    "The table is (id TEXT PRIMARY KEY, data JSONB NOT NULL), created outside this code. Call s.pool.QueryRow(ctx, \"SELECT data FROM \"+s.table+\" WHERE id = $1\", the ID as a string).Scan(&data) with data a []byte. When errors.Is(err, pgx.ErrNoRows), return ErrNotFound. Otherwise JSON-unmarshal data.")

  (hint list
    "Call rows, err := s.pool.Query(ctx, \"SELECT data FROM \"+s.table+\" ORDER BY id\"), defer rows.Close(), Scan and unmarshal each row while rows.Next(), and return rows.Err() at the end.")

  (hint save
    "JSON-marshal the value and call s.pool.Exec(ctx, \"UPSERT INTO \"+s.table+\" (id, data) VALUES ($1, $2)\", the ID as a string, the bytes). UPSERT is CockroachDB's own: it inserts the row or replaces it. A single statement is retried by the server when it conflicts, so no retry loop is needed here.")

  (hint delete
    "Call s.pool.Exec(ctx, \"DELETE FROM \"+s.table+\" WHERE id = $1\", the ID as a string). Deleting a missing row is not an error.")

  (hint count
    "Call s.pool.QueryRow(ctx, \"SELECT COUNT(*) FROM \"+s.table).Scan(&n). It reads the whole table.")

  (hint list-by
    "Call s.pool.Query(ctx, \"SELECT data FROM \"+s.table+\" WHERE data->>'KEY' = $1 ORDER BY id\", {param}), where KEY is the key the entity's JSON uses for {field} and the value is compared as text. Scan and unmarshal each row as List does.")

  (hint get-by
    "Call s.pool.QueryRow(ctx, \"SELECT data FROM \"+s.table+\" WHERE data->>'KEY' = $1 ORDER BY id LIMIT 1\", {param}).Scan(&data), where KEY is the key the entity's JSON uses for {field}. When errors.Is(err, pgx.ErrNoRows), return ErrNotFound.")

  (hint count-by
    "Call s.pool.QueryRow(ctx, \"SELECT COUNT(*) FROM \"+s.table+\" WHERE data->>'KEY' = $1\", {param}).Scan(&n), where KEY is the key the entity's JSON uses for {field}.")

  (hint exists-by
    "Count as CountBy would, with the same WHERE on {field} and {param}, and report whether the count is above zero.")

  (hint delete-by
    "Call s.pool.Exec(ctx, \"DELETE FROM \"+s.table+\" WHERE data->>'KEY' = $1\", {param}), where KEY is the key the entity's JSON uses for {field}."))
