(tile nsq
  (offers event-bus)
  (doc "NSQ: a topic per event type, a channel per group of consumers")
  (type-name NSQBus)
  (engine nsq)
  (import nsq github.com/nsqio/go-nsq)
  (topics nsq)

  (satisfies (cross-process)
    "consumers in other processes read a topic's channels over the network")
  (satisfies (at-least-once)
    "a message not finished before its timeout is requeued and delivered again")
  (satisfies (fan-out)
    "every channel on a topic gets a copy of each message")
  (satisfies (consumer-groups)
    "consumers on one channel share its messages")
  (illegal-when (replay)
    "a message is gone once it is finished; nothing can be read a second time")
  (illegal-when (ordered)
    "NSQ promises no order: requeues and several nsqd nodes interleave messages")
  (illegal-when (no-broker)
    "nsqd to run on each producing host, and nsqlookupd for consumers to find them")
  (unverified (durable)
    "nsqd holds messages in memory and writes to disk only what overflows --mem-queue-size, so a message in memory is lost if nsqd dies; setting that to 0 is the operator's choice, not this tile's")

  (cost
    (llm-work 4 (source derived "a producer call and a consumer with a handler and a clean stop"))
    (maintenance 3 (source derived "channels to name consistently, and timeouts and requeue limits to tune"))
    (dependency 1 (source derived "go-nsq and a few small modules"))
    (runtime 2 (source derived "a TCP round trip per publish"))
    (operations 5 (source derived "nsqd on each producing host and nsqlookupd to run and watch"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus backed by NSQ. Each event type is a topic, and every\nservice reads it on a channel of its own.")
  (holds (producer *nsq.Producer) (lookupd string) (channel string))
  (about-new
    "New{impl} publishes through producer, and consumes on channel from the\nnsqd nodes that lookupd, an nsqlookupd HTTP address, knows about.")
  (new (params (producer *nsq.Producer) (lookupd string) (channel string))
    "return &{impl}{producer: producer, lookupd: lookupd, channel: channel}")

  (hint publish
    "Encode e as JSON and call s.producer.Publish(\"{event}\", the bytes). The topic is the event's type name.")
  (hint on
    "Make consumer, err := nsq.NewConsumer(\"{event}\", s.channel, nsq.NewConfig()). Call consumer.AddHandler(nsq.HandlerFunc(func(m *nsq.Message) error {...})), decoding m.Body as JSON into {event} and returning what h returns: nil finishes the message and an error requeues it. Then consumer.ConnectToNSQLookupd(s.lookupd). Return a func that calls consumer.Stop() and waits on consumer.StopChan."))
