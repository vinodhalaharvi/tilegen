(tile nats-jetstream
  (offers event-bus)
  (doc "NATS JetStream: the same server with a log, so events are kept and can be read back")
  (alias jetstream)
  (type-name JetStreamBus)
  (engine nats)
  (import jetstream github.com/nats-io/nats.go/jetstream)
  (topics nats jetstream)

  (satisfies (durable)
    "a publish is acknowledged only once the stream has stored the message")
  (satisfies (cross-process)
    "consumers in other processes read the stream over the network")
  (satisfies (replay)
    "a consumer can start at any sequence or time in the stream")
  (satisfies (ordered)
    "a stream is an ordered log, and a consumer reads it in sequence")
  (satisfies (at-least-once)
    "an unacknowledged message is redelivered until it is acknowledged or expires")
  (satisfies (fan-out)
    "several consumers on one stream each see every message")
  (satisfies (consumer-groups)
    "a queue consumer shares its messages among its members")
  (illegal-when (no-broker)
    "a server to run, with streams and consumers to size and retain")

  (cost
    (llm-work 5 (source derived "publish with an ack, and a consumer with explicit acknowledgement"))
    (maintenance 3)
    (dependency 4)
    (runtime 2)
    (operations 4 (source derived "one binary with no dependencies, but streams, consumers and retention to configure")))

  (about
    "{impl} is a Bus backed by NATS JetStream. Events go to a stream, so a\nconsumer that was not running still receives them, and a consumer can read\nback from any point.")
  (holds (js jetstream.JetStream) (subject string))
  (about-new
    "New{impl} returns a {impl} publishing under the given subject prefix.\nThe stream covering that prefix must already exist.")
  (new (params (js jetstream.JetStream) (subject string))
    "return &{impl}{js: js, subject: subject}")

  (hint publish
    "Encode e as JSON and publish it on s.subject+\".{event}\" with s.js.Publish, which returns only once the stream has stored the message. Return that error. Do not reach for a plain connection publish: this tile was chosen because the spec asked for durability, and an unacknowledged publish does not provide it.")
  (hint on
    "Create or look up a consumer on the stream covering s.subject+\".{event}\", decode each message as JSON into {event}, and call h. Acknowledge only after h returns nil, and negatively acknowledge otherwise, so a failed handler sees the message again. Return a func that stops the consumer."))
