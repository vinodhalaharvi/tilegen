(tile asynq
  (offers event-bus)
  (doc "asynq: each event a task on a Redis queue, handled by one worker and retried when it fails")
  (type-name AsynqBus)
  (engine redis)
  (import asynq github.com/hibiken/asynq)
  (topics asynq redis)

  (satisfies (cross-process)
    "workers in other processes take tasks from the same Redis")
  (satisfies (at-least-once)
    "a task whose handler returns an error, or whose worker dies, is retried")
  (satisfies (consumer-groups)
    "the workers of a queue share its tasks")
  (illegal-when (replay)
    "a task that succeeded is gone once its retention passes; nothing can be read a second time")
  (illegal-when (ordered)
    "tasks are taken by whichever worker is free, and a retry runs later than the tasks behind it")
  (illegal-when (fan-out)
    "a task goes to one worker, and a task type has one handler")
  (illegal-when (no-broker)
    "a Redis to run")
  (unverified (durable)
    "a task is a Redis entry: what survives a restart depends on the server's persistence, which this tile does not configure")

  (cost
    (llm-work 3 (source derived "enqueue a task, and register a handler"))
    (maintenance 4 (source derived "handlers are registered before the server starts and cannot be removed, and retries, timeouts and the dead set need watching"))
    (dependency 3 (source derived "asynq and the Redis client under it"))
    (runtime 2 (source derived "a round trip per task, and workers polling"))
    (operations 4 (source derived "a Redis to run, and an asynq server process for the workers"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus backed by asynq. Each event is a task on a Redis queue,\ntaken by one worker and retried when its handler fails.")
  (holds (client *asynq.Client) (mux *asynq.ServeMux))
  (about-new
    "New{impl} enqueues through client and registers handlers on mux, which\nthe caller hands to an asynq.Server once every handler is registered.")
  (new (params (client *asynq.Client) (mux *asynq.ServeMux))
    "return &{impl}{client: client, mux: mux}")

  (hint publish
    "Encode e as JSON and call s.client.EnqueueContext(ctx, asynq.NewTask(\"{event}\", the bytes)). The task type is the event's type name. Return its error.")

  (hint on
    "Call s.mux.HandleFunc(\"{event}\", func(ctx context.Context, t *asynq.Task) error {...}), decoding t.Payload() as JSON into {event} and returning what h returns: an error makes asynq retry the task later. A mux has no way to take a handler off, so return a func that does nothing, and say so in a comment."))
