(tile zeromq
  (offers event-bus)
  (doc "ZeroMQ publish and subscribe in pure Go: sockets between processes, with nothing in the middle")
  (alias zmq)
  (type-name ZeroMQBus)
  (import zmq4 github.com/go-zeromq/zmq4)
  (topics zeromq zmq pubsub)

  (satisfies (cross-process)
    "a subscriber in another process dials the publisher's socket")
  (satisfies (fan-out)
    "every subscriber connected at the time gets every message it subscribed to")
  (satisfies (no-broker)
    "a library and two sockets; there is no server between them")
  (illegal-when (durable)
    "a message exists only on the wire and in socket buffers: one sent while a subscriber is away is never seen by it")
  (illegal-when (replay)
    "nothing is kept once it is sent")
  (illegal-when (at-least-once)
    "a publisher is never told whether anyone received a message, and a full buffer drops them")
  (illegal-when (consumer-groups)
    "every subscriber gets every message; none are shared out")
  (unverified (ordered)
    "one publisher's messages arrive in the order it sent them over one connection, and nothing orders the messages of two publishers")

  (cost
    (llm-work 4 (source derived "a topic frame and a payload frame, a socket per subscription, and a receive loop to stop cleanly"))
    (maintenance 4 (source derived "the publisher's address is configuration every subscriber must share, a slow subscriber silently loses messages, and nothing says when one has gone"))
    (dependency 2 (source derived "zmq4 and its few modules"))
    (runtime 1 (source derived "a write to a socket"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus over ZeroMQ PUB and SUB sockets. Events go straight from\nthe publishing process to whoever is connected, and are not kept.")
  (holds (pub zmq4.Socket) (endpoint string) (mu sync.Mutex))
  (about-new
    "New{impl} publishes on pub, a PUB socket the caller made with zmq4.NewPub\nand bound with Listen, and subscribes by dialing endpoint, its address.")
  (new (params (pub zmq4.Socket) (endpoint string))
    "return &{impl}{pub: pub, endpoint: endpoint}")

  (hint publish
    "Encode e as JSON and, holding s.mu, call s.pub.Send(zmq4.NewMsgFrom([]byte(\"{event}\"), the bytes)). The first frame is the topic subscribers filter on. The socket takes no context, so check ctx.Err() first. Return its error.")

  (hint on
    "Make sub := zmq4.NewSub(ctx) with a context you can cancel, call sub.Dial(s.endpoint) and sub.SetOption(zmq4.OptionSubscribe, \"{event}\"). In a goroutine, loop on msg, err := sub.Recv() until it fails. A subscription matches by prefix, so skip a message whose msg.Frames[0] is not exactly \"{event}\" or that has fewer than two frames; decode msg.Frames[1] as JSON into {event} and call h. There is no way to ask for a message again, so an error from h can only be dropped: say so in a comment. Return a func that cancels the context, closes sub and waits for the goroutine."))
