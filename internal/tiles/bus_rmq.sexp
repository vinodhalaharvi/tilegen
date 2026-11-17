(tile rmq
  (offers event-bus)
  (doc "rmq: a Redis list per event type, with deliveries acknowledged or rejected one by one")
  (type-name RMQBus)
  (engine redis)
  (import rmq github.com/adjust/rmq/v5)
  (topics rmq redis queue)

  (satisfies (cross-process)
    "consumers in other processes take deliveries from the same Redis")
  (satisfies (consumer-groups)
    "the consumers of a queue share its deliveries")
  (illegal-when (replay)
    "an acknowledged delivery is removed; nothing can be read a second time")
  (illegal-when (ordered)
    "deliveries are prefetched in batches and handled by whichever consumer has them")
  (illegal-when (fan-out)
    "a delivery goes to one consumer")
  (illegal-when (no-broker)
    "a Redis to run")
  (unverified (durable)
    "a delivery is an entry in a Redis list: what survives a restart depends on the server's persistence, which this tile does not configure")
  (unverified (at-least-once)
    "a rejected delivery moves to the queue's rejected list, and one held by a consumer that died stays in its unacked list; each comes back only when something returns it, with ReturnRejected or a Cleaner, which is the caller's to schedule")

  (cost
    (llm-work 4 (source derived "open a queue, publish bytes, and a consumer func that acknowledges or rejects"))
    (maintenance 5 (source derived "a cleaner to run on a schedule, rejected deliveries to return or purge, and a queue that can be set consuming only once"))
    (dependency 2 (source derived "rmq and the Redis client under it"))
    (runtime 2 (source derived "a round trip per delivery, and consumers polling"))
    (operations 4 (source derived "a Redis to run, with memory to size and persistence to configure"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus backed by rmq. Each event type is a queue in Redis, and\na delivery is acknowledged once its handler succeeds.")
  (holds (conn rmq.Connection))
  (about-new
    "New{impl} returns a {impl} on the given rmq connection, which the\ncaller opened on its Redis client.")
  (new (params (conn rmq.Connection))
    "return &{impl}{conn: conn}")

  (hint publish
    "Call q, err := s.conn.OpenQueue(\"{event}\"), encode e as JSON and call q.PublishBytes(the bytes). The connection takes no context, so check ctx.Err() first. Return the first error.")

  (hint on
    "Call q, err := s.conn.OpenQueue(\"{event}\") and q.StartConsuming(10, time.Second): at most ten deliveries are held unacknowledged, and an empty queue is polled every second. StartConsuming fails on a queue this connection is already consuming, so an event type takes one handler. Call q.AddConsumerFunc(\"{event}\", func(d rmq.Delivery) {...}), decoding []byte(d.Payload()) as JSON into {event}, calling h, and then d.Ack() when it returns nil or d.Reject() when it does not; a rejected delivery waits in the queue's rejected list until something returns it. Return a func that waits on <-q.StopConsuming()."))
