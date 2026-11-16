(tile kafka
  (offers event-bus)
  (doc "Kafka: a partitioned log, ordered per key, read back at will")
  (type-name KafkaBus)
  (import kgo github.com/twmb/franz-go/pkg/kgo)
  (topics kafka)

  (satisfies (durable)
    "a produce is acknowledged once the partition's replicas have the record")
  (satisfies (cross-process)
    "consumers in other processes read the topic over the network")
  (satisfies (replay)
    "a consumer group can seek to any offset still inside retention")
  (satisfies (ordered)
    "records with the same key go to one partition, and a partition is an ordered log")
  (satisfies (at-least-once)
    "an uncommitted offset is redelivered to whoever takes the partition next")
  (satisfies (fan-out)
    "several consumer groups on one topic each see every record")
  (satisfies (consumer-groups)
    "a group shares partitions among its members and rebalances them")
  (illegal-when (no-broker)
    "a cluster to run, partitions to size, and retention and consumer lag to watch")

  (cost
    (llm-work 5)
    (maintenance 5)
    (dependency 4 (source derived "a client library with a large transitive graph"))
    (runtime 4)
    (operations 9 (source derived "the heaviest thing in the registry to run: brokers, partitions, rebalances, lag, and someone on call for them")))

  (about
    "{impl} is a Bus backed by Kafka. Events are published to one topic, keyed so that everything about one entity stays in order.")
  (holds (client *kgo.Client) (topic string))
  (about-new
    "New{impl} returns a {impl} over the given client.")
  (new (params (client *kgo.Client) (topic string))
    "return &{impl}{client: client, topic: topic}")

  (hint publish
    "Encode e as JSON and produce it to s.topic with kgo, using the entity's id as the record key so that everything about one entity stays in order. The record's type header is \"{event}\". Return the produce error.")
  (hint on
    "Consume s.topic with a consumer group, decode records whose type header is \"{event}\" as JSON into {event}, and call h for each. Commit only after h returns nil, so a failed handler sees the record again. Return a func that stops the consumer."))

; The same kafka, as an inbound edge: different work, priced on its own.
(tile kafka
  (offers transport)
  (doc "Kafka as an inbound edge: consume a topic, at your own offset")
  (type-name KafkaSource)
  (import kgo github.com/twmb/franz-go/pkg/kgo)
  (topics kafka)

  (satisfies (cross-process)
    "the producers are other systems entirely; that is what an inbound edge is")
  (satisfies (durable)
    "a record is on the partition's replicas before it is acknowledged")
  (satisfies (replay)
    "a consumer can start at any offset still inside retention")
  (satisfies (ordered)
    "a partition is an ordered log, and one key goes to one partition")
  (satisfies (at-least-once)
    "an uncommitted offset is redelivered after a rebalance or a restart")
  (illegal-when (no-broker)
    "a cluster to run, partitions to size, and retention and consumer lag to watch")

  (cost
    (llm-work 7 (source derived "a consumer group, offset commits after the handler, and a rebalance callback: more than the publish side"))
    (maintenance 5)
    (dependency 4 (source derived "a client library with a large transitive graph"))
    (runtime 4)
    (operations 9 (source derived "the heaviest thing in the registry to run: brokers, partitions, rebalances, lag, and someone on call for them"))))
