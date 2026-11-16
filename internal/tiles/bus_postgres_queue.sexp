(tile postgres-queue
  (offers event-bus)
  (doc "a queue in a Postgres table: events are rows, taken one at a time with SKIP LOCKED")
  (alias pgqueue)
  (type-name PostgresQueueBus)
  (engine postgres)
  (import pgxpool github.com/jackc/pgx/v5/pgxpool)
  (import pgx github.com/jackc/pgx/v5)
  (topics postgres queue)

  (satisfies (durable)
    "an event is a row, committed before Publish returns")
  (satisfies (cross-process)
    "every process connected to the database can publish and consume")
  (satisfies (at-least-once)
    "a row is deleted in the transaction that handled it, so a handler that fails or a consumer that dies leaves it to be taken again")
  (satisfies (consumer-groups)
    "consumers take different rows: SKIP LOCKED passes over one another's")
  (illegal-when (replay)
    "a handled event's row is deleted; nothing can be read a second time")
  (illegal-when (ordered)
    "SKIP LOCKED lets a second consumer take the next row while the first is still on its own")
  (illegal-when (fan-out)
    "a row goes to one consumer")
  (illegal-when (no-broker)
    "a Postgres to run, though nothing beyond the one a project on a Postgres store already has")

  (cost
    (llm-work 5 (source derived "a polling loop with a clean stop, and a transaction around every event"))
    (maintenance 3 (source derived "the events table is created outside this code, a slow handler holds a transaction open, and the poll interval is a trade between lag and load"))
    (dependency 1 (source derived "pgx only"))
    (runtime 3 (source derived "a query per poll, whether or not there is anything to take"))
    (operations 5 (source derived "a server to run, with backups, connections and upgrades"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus kept in a Postgres table. An event is a row; a consumer\ntakes one, handles it and deletes it in a single transaction.")
  (holds (pool *pgxpool.Pool))
  (about-new
    "New{impl} returns a {impl} over the given pool. The table is\nevents (id BIGSERIAL PRIMARY KEY, type TEXT NOT NULL, payload JSONB NOT NULL),\ncreated outside this code.")
  (new (params (pool *pgxpool.Pool))
    "return &{impl}{pool: pool}")

  (hint publish
    "Encode e as JSON and call s.pool.Exec(ctx, \"INSERT INTO events (type, payload) VALUES ($1, $2)\", \"{event}\", the bytes). Return its error.")

  (hint on
    "In a goroutine, loop until the context ends. Each turn: tx, err := s.pool.Begin(ctx); then tx.QueryRow(ctx, \"SELECT id, payload FROM events WHERE type = $1 ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED\", \"{event}\").Scan(&id, &payload). When errors.Is(err, pgx.ErrNoRows) there is nothing to take: tx.Rollback(ctx) and wait a second before the next turn. Otherwise decode payload as JSON into {event} and call h. When h returns nil, tx.Exec(ctx, \"DELETE FROM events WHERE id = $1\", id) and tx.Commit(ctx); when it does not, tx.Rollback(ctx), which unlocks the row for another try, and wait before the next turn. Return a func that cancels the context and waits for the goroutine."))
