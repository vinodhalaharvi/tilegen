(tile nats-core
  (offers event-bus)
  (doc "core NATS: subjects and queue groups, with nothing kept")
  (alias nats)
  (type-name NatsBus)
  (engine nats)
  (import nats github.com/nats-io/nats.go)
  (topics nats)

  (satisfies (cross-process)
    "subscribers in other processes receive what is published on a subject")
  (satisfies (fan-out)
    "every plain subscriber to a subject receives every message")
  (satisfies (consumer-groups)
    "a queue group delivers each message to one of its members")
  (illegal-when (durable)
    "core NATS is fire-and-forget: a message with no connected subscriber is gone")
  (illegal-when (replay)
    "core NATS keeps no stream; there is nothing to read back")
  (illegal-when (at-least-once)
    "core NATS delivers at most once and does not redeliver on failure")
  (illegal-when (no-broker)
    "there is a server to run, though only one binary and no state")
  (unverified (ordered)
    "messages from one publisher arrive in order on one connection, but nobody has checked what holds across a reconnect")

  (cost
    (llm-work 4 (source derived "subscribe and publish on a subject; there is nothing to acknowledge"))
    (maintenance 2)
    (dependency 4)
    (runtime 1)
    (operations 2 (source derived "one binary, no storage, nothing to size")))

  (about
    "{impl} is a Bus backed by core NATS. Events are published on a subject\nper event type, so handlers in other processes receive them. Nothing is\nkept: a message published with no subscriber connected is gone.")
  (holds (conn *nats.Conn) (subject string))
  (about-new
    "New{impl} returns a {impl} publishing under the given subject prefix.")
  (new (params (conn *nats.Conn) (subject string))
    "return &{impl}{conn: conn, subject: subject}")

  (hint publish
    "Encode e as JSON and publish it on s.subject+\".{event}\" with s.conn. Return the publish error. This is core NATS, so a successful publish means the server accepted the message, not that anyone received it.")
  (hint on
    "Subscribe to s.subject+\".{event}\" with s.conn, decode each message as JSON into {event}, and call h. Return a func that unsubscribes. Deliver errors from h to the connection's error handler, since NATS has no reply here."))

; The same nats-core, as an inbound edge: different work, priced on its own.
(tile nats-core
  (offers transport)
  (doc "core NATS: subjects and queue groups, with nothing kept")
  (alias nats)
  (type-name NatsSource)
  (engine nats)
  (import nats github.com/nats-io/nats.go)
  (topics nats)

  (satisfies (cross-process)
    "publishers in other processes reach this subject over the network")
  (illegal-when (durable)
    "core NATS is fire-and-forget: a message with no connected consumer is gone")
  (illegal-when (replay)
    "core NATS keeps no stream; JetStream is the same broker with a log")
  (illegal-when (at-least-once)
    "core NATS delivers at most once and does not redeliver on failure")
  (illegal-when (no-broker)
    "there is a server to run, though only one binary and no state")
  (unverified (ordered)
    "messages from one publisher arrive in order on one connection, but nobody has checked what holds across a reconnect")

  (cost
    (llm-work 2 (source derived "subscribe to a subject and decode; there are no offsets to keep"))
    (maintenance 1)
    (dependency 2)
    (runtime 1)
    (operations 2 (source derived "one binary, no storage, nothing to size"))))
