(tile amqp10
  (offers event-bus)
  (doc "AMQP 1.0 through go-amqp: a queue per event type on whichever broker speaks it")
  (type-name AMQP10Bus)
  (import amqp github.com/Azure/go-amqp)
  (topics amqp amqp-1.0 activemq artemis)

  (satisfies (cross-process)
    "a broker every process connects to")
  (satisfies (at-least-once)
    "a message is accepted only after its handler returns nil, and released back to the broker when it does not")
  (illegal-when (no-broker)
    "a broker that speaks AMQP 1.0 to run or pay for")
  (unverified (durable)
    "every message is sent marked durable, and whether it survives a restart depends on the broker and how the queue is configured there")
  (unverified (replay)
    "it depends on the broker and how the address is configured there")
  (unverified (ordered)
    "it depends on the broker and how the queue is configured there, and a released message comes back out of order")
  (unverified (fan-out)
    "it depends on the broker and how the address is configured there: a queue gives each message to one receiver, a topic to all")
  (unverified (consumer-groups)
    "it depends on the broker and how the address is configured there: receivers on one queue share its messages")

  (cost
    (llm-work 5 (source derived "a sender to open once per event type and keep, a receive loop, and an accept or a release for every message"))
    (maintenance 4 (source derived "what an address means differs from broker to broker, and links and sessions to reopen when the connection drops"))
    (dependency 1 (source derived "go-amqp only"))
    (runtime 2 (source derived "a connection held open, and a round trip per send"))
    (operations 5 (source derived "a broker to run, with its queues, storage and upgrades"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus over AMQP 1.0. Which broker carries the events, and\nwhat it promises, is decided where the session it is given was opened.")
  (holds (session *amqp.Session) (mu sync.Mutex) (senders "map[string]*amqp.Sender"))
  (about-new
    "New{impl} returns a {impl} on the given session. An event type's\naddress is its name.")
  (new (params (session *amqp.Session))
    "return &{impl}{session: session, senders: map[string]*amqp.Sender{}}")

  (hint publish
    "Take the sender for \"{event}\" from s.senders under s.mu, opening it the first time with s.session.NewSender(ctx, \"{event}\", nil) and keeping it. Encode e as JSON, build msg := amqp.NewMessage(the bytes), set msg.Header = &amqp.MessageHeader{Durable: true}, and call sender.Send(ctx, msg, nil), which returns once the broker has settled it. Return its error.")

  (hint on
    "Call r, err := s.session.NewReceiver(ctx, \"{event}\", nil) with a context you can cancel. In a goroutine, loop on msg, err := r.Receive(ctx, nil) until it fails: decode msg.GetData() as JSON into {event}, call h, and call r.AcceptMessage(ctx, msg) when it returns nil or r.ReleaseMessage(ctx, msg) when it does not, which gives the message back to the broker to deliver again. Return a func that cancels the context, closes the receiver and waits for the goroutine."))
