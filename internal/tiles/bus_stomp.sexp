(tile stomp
  (offers event-bus)
  (doc "STOMP through go-stomp: a queue per event type on a broker such as ActiveMQ or RabbitMQ")
  (type-name StompBus)
  (import stomp github.com/go-stomp/stomp/v3)
  (topics stomp activemq)

  (satisfies (cross-process)
    "a broker every process connects to")
  (illegal-when (no-broker)
    "a broker that speaks STOMP to run")
  (unverified (durable)
    "it depends on the broker and how the destination is configured there; STOMP itself says nothing about storage")
  (unverified (replay)
    "it depends on the broker and how the destination is configured there")
  (unverified (ordered)
    "it depends on the broker and how the destination is configured there")
  (unverified (at-least-once)
    "a message is acknowledged only after its handler returns nil, and what a broker does with one that is refused, or was held by a connection that dropped, is the broker's own rule")
  (unverified (fan-out)
    "it depends on the broker and how the destination is configured there: a /queue/ gives each message to one subscriber, a /topic/ to all")
  (unverified (consumer-groups)
    "it depends on the broker and how the destination is configured there: subscribers to one /queue/ share its messages")

  (cost
    (llm-work 4 (source derived "send with a receipt, read a channel, and acknowledge or refuse each message"))
    (maintenance 4 (source derived "destination names and what they mean differ from broker to broker, and one connection carries every subscription"))
    (dependency 1 (source derived "go-stomp only"))
    (runtime 2 (source derived "a connection held open, and a receipt awaited per send"))
    (operations 5 (source derived "a broker to run, with its queues, storage and upgrades"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus over STOMP. Which broker carries the events, and what\nit promises, is decided where the connection it is given was opened.")
  (holds (conn *stomp.Conn))
  (about-new
    "New{impl} returns a {impl} on the given connection. An event type's\ndestination is /queue/ and its name.")
  (new (params (conn *stomp.Conn))
    "return &{impl}{conn: conn}")

  (hint publish
    "Encode e as JSON and call s.conn.Send(\"/queue/{event}\", \"application/json\", the bytes, stomp.SendOpt.Receipt). The receipt option makes Send wait until the broker says it has the message. The connection takes no context, so check ctx.Err() first. Return its error.")

  (hint on
    "Call sub, err := s.conn.Subscribe(\"/queue/{event}\", stomp.AckClientIndividual). In a goroutine, range over sub.C: a message with a non-nil Err means the subscription has failed, so stop. Otherwise decode msg.Body as JSON into {event}, call h, and call s.conn.Ack(msg) when it returns nil or s.conn.Nack(msg) when it does not. Return a func that calls sub.Unsubscribe() and waits for the goroutine."))
