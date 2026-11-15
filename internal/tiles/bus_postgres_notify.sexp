(tile postgres-notify
  (offers event-bus)
  (doc "Postgres LISTEN/NOTIFY: a channel per event type, on the database already there")
  (alias pgnotify)
  (type-name PostgresNotifyBus)
  (engine postgres)
  (import pgxpool github.com/jackc/pgx/v5/pgxpool)
  (topics postgres listen-notify)

  (satisfies (cross-process)
    "every session connected to the database can listen")
  (satisfies (ordered)
    "notifications reach a listener in the order their transactions committed")
  (satisfies (fan-out)
    "every session listening on a channel gets each notification")
  (illegal-when (durable)
    "a notification goes to the sessions listening when the sending transaction commits and is kept nowhere")
  (illegal-when (replay)
    "nothing is stored, so there is nothing to read back")
  (illegal-when (at-least-once)
    "a listener that is disconnected when a notification is sent never sees it")
  (illegal-when (consumer-groups)
    "every listener gets every notification; nothing shares them out")
  (illegal-when (no-broker)
    "a Postgres to run, though nothing beyond the one a project on a Postgres store already has")

  (cost
    (llm-work 3 (source derived "a pg_notify call, and a connection held open in a loop with a clean stop"))
    (maintenance 3 (source derived "a payload must stay under 8000 bytes, and each listener holds one connection out of the pool for as long as it listens"))
    (dependency 1 (source derived "pgx only"))
    (runtime 1 (source derived "a statement per publish"))
    (operations 5 (source derived "a server to run, with backups, connections and upgrades"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus backed by Postgres LISTEN/NOTIFY. An event reaches the\nhandlers listening when its transaction commits, and nobody else.")
  (holds (pool *pgxpool.Pool))
  (about-new
    "New{impl} returns a {impl} over the given pool. Each handler keeps one\nof the pool's connections for as long as it listens.")
  (new (params (pool *pgxpool.Pool))
    "return &{impl}{pool: pool}")

  (hint publish
    "Encode e as JSON and call s.pool.Exec(ctx, \"select pg_notify($1, $2)\", strings.ToLower(\"{event}\"), string(the JSON)). The payload must be under 8000 bytes: return an error for a larger one rather than let the server refuse it.")

  (hint on
    "Call conn, err := s.pool.Acquire(ctx) and conn.Exec(ctx, \"listen \"+strings.ToLower(\"{event}\")): the channel is the event's name in lower case, a fixed identifier, never input. In a goroutine, loop on n, err := conn.Conn().WaitForNotification(ctx) until the context ends: decode n.Payload as JSON into {event} and call h. A failure from h is one to record: the notification will not come again. Return a func that cancels the context, waits for the goroutine and calls conn.Release()."))
