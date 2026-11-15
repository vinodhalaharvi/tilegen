(tile redis-pubsub
  (offers event-bus)
  (doc "Redis publish/subscribe: a channel per event type, heard by whoever is listening")
  (alias pubsub)
  (type-name RedisPubSubBus)
  (engine redis)
  (import redis github.com/redis/go-redis/v9)
  (topics redis pubsub)

  (satisfies (cross-process)
    "every client connected to the server can subscribe")
  (satisfies (ordered)
    "a channel's messages reach a subscriber in the order the server took them")
  (satisfies (fan-out)
    "every subscriber to a channel gets each message")
  (illegal-when (durable)
    "a message goes to whoever is subscribed at that moment and is kept nowhere")
  (illegal-when (replay)
    "nothing is stored, so there is nothing to read back")
  (illegal-when (at-least-once)
    "a subscriber that is away, slow or disconnected simply misses the message: delivery is at most once")
  (illegal-when (consumer-groups)
    "every subscriber gets every message; nothing shares them out")
  (illegal-when (no-broker)
    "a Redis to run")

  (cost
    (llm-work 2 (source derived "a publish, and a channel to range over"))
    (maintenance 2 (source derived "nothing to declare; a missed message leaves no trace to find it by"))
    (dependency 2 (source derived "go-redis and its few modules"))
    (runtime 1 (source derived "a round trip per publish"))
    (operations 4 (source derived "a server to run"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus backed by Redis publish/subscribe. An event reaches the\nhandlers listening when it is published, and nobody else.")
  (holds (client *redis.Client) (prefix string))
  (about-new
    "New{impl} returns a {impl} over the given client, with every channel\nunder prefix.")
  (new (params (client *redis.Client) (prefix string))
    "return &{impl}{client: client, prefix: prefix}")

  (hint publish
    "Encode e as JSON and call s.client.Publish(ctx, s.prefix+\"{event}\", the bytes).Err(). It succeeds even when nobody is subscribed.")

  (hint on
    "Take sub := s.client.Subscribe(ctx, s.prefix+\"{event}\") and, in a goroutine, range over sub.Channel(): decode each m.Payload as JSON into {event} and call h. A failure from h is one to record: the message will not come again. Return a func that calls sub.Close(), which ends the range, and waits for the goroutine."))
