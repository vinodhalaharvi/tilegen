(tile rocketmq
  (offers event-bus)
  (doc "Apache RocketMQ: a topic per event type, pushed to the consumers of a group")
  (type-name RocketMQBus)
  (engine rocketmq)
  (import rocketmq github.com/apache/rocketmq-client-go/v2)
  (import primitive github.com/apache/rocketmq-client-go/v2/primitive)
  (import consumer github.com/apache/rocketmq-client-go/v2/consumer)
  (topics rocketmq)

  (satisfies (cross-process)
    "brokers every process connects to")
  (satisfies (at-least-once)
    "a batch whose handler asks for a retry is delivered again later, and one that keeps failing ends in the group's dead-letter topic")
  (satisfies (fan-out)
    "every consumer group subscribed to a topic gets every message")
  (satisfies (consumer-groups)
    "the consumers of one group share a topic's queues")
  (illegal-when (ordered)
    "this tile sends ordinary messages, which the consumers of a group handle concurrently; ordered delivery is a different send and a different listener")
  (illegal-when (no-broker)
    "name servers and brokers to run")
  (unverified (durable)
    "SendSync returns once a broker has the message, and whether that means on disk and on a replica is the broker's flush and replication mode")
  (unverified (replay)
    "a group's position can be reset to an earlier time with the admin tools while the messages are still within retention, which is not something this code does")

  (cost
    (llm-work 4 (source derived "a synchronous send, and a listener that answers success or retry for a batch"))
    (maintenance 5 (source derived "every subscription must be made before the consumer starts, topics and groups are created on the cluster, and retries and dead letters need watching"))
    (dependency 4 (source derived "the client and its module tree"))
    (runtime 2 (source derived "a round trip per send, and long polling"))
    (operations 8 (source derived "name servers and brokers to run, replicate and upgrade"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus backed by Apache RocketMQ. Each event type is a topic,\nand the consumers of a group share its messages.")
  (holds (producer rocketmq.Producer) (push rocketmq.PushConsumer))
  (about-new
    "New{impl} sends through producer, already started, and subscribes on\npush, which the caller starts once every handler is registered.")
  (new (params (producer rocketmq.Producer) (push rocketmq.PushConsumer))
    "return &{impl}{producer: producer, push: push}")

  (hint publish
    "Encode e as JSON and call s.producer.SendSync(ctx, primitive.NewMessage(\"{event}\", the bytes)). The topic is the event's type name, created on the cluster outside this code. Return its error.")

  (hint on
    "Call s.push.Subscribe(\"{event}\", consumer.MessageSelector{}, func(ctx context.Context, msgs ...*primitive.MessageExt) (consumer.ConsumeResult, error) {...}). In the listener decode each m.Body as JSON into {event} and call h; when any call fails, return consumer.ConsumeRetryLater, nil, which has the whole batch delivered again later, so h must tolerate seeing an event twice. Otherwise return consumer.ConsumeSuccess, nil. Subscribe must be called before the consumer is started. Return a func that calls s.push.Unsubscribe(\"{event}\")."))
