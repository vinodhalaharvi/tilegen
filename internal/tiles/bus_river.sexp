(tile river
  (offers event-bus)
  (doc "River: each event a job in Postgres, worked by one worker and retried when it fails")
  (type-name RiverBus)
  (engine postgres)
  (import river github.com/riverqueue/river)
  (import pgx github.com/jackc/pgx/v5)
  (topics river postgres jobs)

  (satisfies (durable)
    "a job is a row, committed before Publish returns")
  (satisfies (cross-process)
    "every process with a client on the database can insert and work jobs")
  (satisfies (at-least-once)
    "a job whose worker returns an error, or dies, is worked again, up to its attempt limit")
  (satisfies (consumer-groups)
    "the clients working a queue share its jobs")
  (illegal-when (replay)
    "a finished job is kept for a while and then deleted; it cannot be worked a second time")
  (illegal-when (ordered)
    "jobs are taken by whichever worker is free, and a retry runs later than the jobs behind it")
  (illegal-when (fan-out)
    "a job goes to one worker, and a kind has one worker type")
  (illegal-when (no-broker)
    "a Postgres to run, though nothing beyond the one a project on a Postgres store already has")

  (cost
    (llm-work 4 (source derived "a job type around each event, a typed worker to register, and an insert"))
    (maintenance 4 (source derived "River's own tables to migrate with each upgrade, workers registered before the client starts, and a kind that must never be renamed"))
    (dependency 4 (source derived "River, its pgx driver and their modules"))
    (runtime 2 (source derived "an insert per event, and workers polling and listening"))
    (operations 5 (source derived "a server to run, with backups, connections and upgrades"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus backed by River. Each event is a job in Postgres, worked\nby one worker and retried when its handler fails.")
  (holds (client "*river.Client[pgx.Tx]") (workers *river.Workers))
  (about-new
    "New{impl} inserts through client and registers workers on workers, the\nsame set the client was configured with. Every handler is registered before\nthe caller starts the client.")
  (new (params (client "*river.Client[pgx.Tx]") (workers *river.Workers))
    "return &{impl}{client: client, workers: workers}")

  (hint publish
    "River wants job arguments that name their kind. Declare, at package level in this file, type {event}Job struct{ {event} } with a method Kind() string that returns \"{event}\"; embedding keeps the event's own JSON. Call s.client.Insert(ctx, the event wrapped in a {event}Job, nil), dereferencing the event first if it is a pointer, and return its error.")

  (hint on
    "Using the {event}Job type that Publish declares, call river.AddWorker(s.workers, river.WorkFunc(func(ctx context.Context, job *river.Job[{event}Job]) error {...})), calling h with job.Args.{event} and returning what it returns: an error makes River work the job again later. AddWorker panics when a kind is registered twice, and workers cannot be taken off, so return a func that does nothing, and say so in a comment."))
