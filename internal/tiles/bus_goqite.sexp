(tile goqite
  (offers event-bus)
  (doc "goqite: a queue in a SQLite table, one per event type, with no server")
  (type-name GoqiteBus)
  (import goqite maragu.dev/goqite)
  (topics goqite sqlite queue)

  (satisfies (durable)
    "an event is a row in the database file, committed before Publish returns")
  (satisfies (at-least-once)
    "a message that is not deleted before its timeout is received again, up to the queue's receive limit of three unless raised; after that it stays in the table and is not delivered")
  (satisfies (no-broker)
    "a library and the SQLite file the program already has; there is no server")
  (illegal-when (cross-process)
    "a sqlite file serves one process; another would need its own connection to the same disk")
  (illegal-when (replay)
    "a handled message's row is deleted; nothing can be read a second time")
  (illegal-when (fan-out)
    "a message goes to one receiver")
  (illegal-when (consumer-groups)
    "receivers on one queue take different messages, but only inside the one process that has the file: the instances of a service cannot share it")
  (unverified (ordered)
    "messages are received oldest first, and one that timed out comes back behind those sent after it")

  (cost
    (llm-work 4 (source derived "a queue per event type, a receive loop with a clean stop, and delete-on-success"))
    (maintenance 3 (source derived "the goqite table is created outside this code from the library's schema, and messages past their receive limit stay in it until someone looks"))
    (dependency 3 (source derived "goqite, and a SQLite driver: a large module, or a C compiler in every build"))
    (runtime 3 (source derived "a query per poll, whether or not there is anything to take"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus kept in a SQLite table through goqite. Each event type is\na queue of its own, and a message is deleted once its handler succeeds.")
  (holds (db *sql.DB))
  (about-new
    "New{impl} returns a {impl} on the given database, which already has\ngoqite's table.")
  (new (params (db *sql.DB))
    "return &{impl}{db: db}")

  (hint publish
    "Take q := goqite.New(goqite.NewOpts{DB: s.db, Name: \"{event}\"}); a Queue is only a name over the shared table, so making one per call is cheap. Encode e as JSON and call q.Send(ctx, goqite.Message{Body: the bytes}). Return its error.")

  (hint on
    "Take q := goqite.New(goqite.NewOpts{DB: s.db, Name: \"{event}\"}). In a goroutine, loop on m, err := q.ReceiveAndWait(ctx, time.Second) with a context you can cancel; it polls until a message arrives and fails when the context ends, which ends the loop. Decode m.Body as JSON into {event}, call h, and call q.Delete(ctx, m.ID) only when h returns nil: a message left alone is received again after its timeout. Return a func that cancels the context and waits for the goroutine."))
