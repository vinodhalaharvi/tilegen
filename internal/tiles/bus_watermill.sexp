(tile watermill
  (offers event-bus)
  (doc "Watermill: publish and subscribe through whichever Pub/Sub the caller built")
  (type-name WatermillBus)
  (import message github.com/ThreeDotsLabs/watermill/message)
  (import watermill github.com/ThreeDotsLabs/watermill)
  (topics watermill)

  (unverified (durable)
    "it depends on the Pub/Sub this is given, which is the caller's")
  (unverified (cross-process)
    "it depends on the Pub/Sub this is given, which is the caller's")
  (unverified (replay)
    "it depends on the Pub/Sub this is given, which is the caller's")
  (unverified (ordered)
    "it depends on the Pub/Sub this is given, which is the caller's")
  (unverified (at-least-once)
    "it depends on the Pub/Sub this is given, which is the caller's")
  (unverified (fan-out)
    "it depends on the Pub/Sub this is given, which is the caller's")
  (unverified (consumer-groups)
    "it depends on the Pub/Sub this is given, which is the caller's")
  (unverified (no-broker)
    "it depends on the Pub/Sub this is given, which is the caller's")

  (cost
    (llm-work 3 (source derived "wrap and unwrap a message, and acknowledge by the handler's result"))
    (maintenance 3 (source derived "what the bus promises is decided where the Pub/Sub is built, away from this code"))
    (dependency 3 (source derived "Watermill, and the Pub/Sub module the caller chooses"))
    (runtime 2 (source derived "a layer over the broker's own client"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus over Watermill. Which broker carries the events, and\nwhat it promises, is decided by the Publisher and Subscriber it is given.")
  (holds (pub message.Publisher) (sub message.Subscriber))
  (about-new
    "New{impl} returns a {impl} over a Watermill Publisher and Subscriber.")
  (new (params (pub message.Publisher) (sub message.Subscriber))
    "return &{impl}{pub: pub, sub: sub}")

  (hint publish
    "Encode e as JSON and call s.pub.Publish(\"{event}\", message.NewMessage(watermill.NewUUID(), the bytes)). The topic is the event's type name. Return its error.")

  (hint on
    "Call msgs, err := s.sub.Subscribe(ctx, \"{event}\") with a context you can cancel. In a goroutine, range over msgs: decode msg.Payload as JSON into {event}, call h, and msg.Ack() when it returns nil or msg.Nack() when it does not. Every message must get one or the other, or the subscriber stops delivering. Return a func that cancels the context and waits for the goroutine."))
