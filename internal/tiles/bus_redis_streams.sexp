(tile redis-streams
  (offers event-bus)
  (doc "Redis Streams: an append-only log with consumer groups")
  (alias redis)
  (type-name RedisBus)
  (engine redis)
  (import redis github.com/redis/go-redis/v9)
  (topics redis)

  (satisfies (cross-process)
    "clients in other processes read the stream over the network")
  (satisfies (replay)
    "a stream is read from any id, so a late reader can start at the beginning")
  (satisfies (ordered)
    "one stream is an ordered sequence of ids")
  (satisfies (at-least-once)
    "an unacknowledged entry stays in the pending list and can be claimed again")
  (satisfies (fan-out)
    "several groups on one stream each see every entry")
  (satisfies (consumer-groups)
    "a group shares entries among its consumers")
  (illegal-when (durable)
    "durability is an operator setting: under the default append-only policy a second of writes can be lost on a crash")
  (illegal-when (no-broker)
    "there is a Redis to run")

  (cost
    (llm-work 4)
    (maintenance 3)
    (dependency 2)
    (runtime 2)
    (operations 3 (source derived "one server, but stream trimming and group lag to watch")))

  (about
    "{impl} is a Bus backed by Redis Streams. Events are appended to one stream, so a handler that starts later can read back what it missed.")
  (holds (rdb *redis.Client) (stream string))
  (about-new
    "New{impl} returns a {impl} over the given client.")
  (new (params (rdb *redis.Client) (stream string))
    "return &{impl}{rdb: rdb, stream: stream}")

  (hint publish
    "XADD e to s.stream as JSON under a field named \"{event}\". Return the error from the command.")
  (hint on
    "Read s.stream with XREADGROUP, decode entries carrying \"{event}\" as JSON into {event}, call h, and XACK only when h returns nil so a failed entry stays in the pending list. Return a func that stops the reader."))
