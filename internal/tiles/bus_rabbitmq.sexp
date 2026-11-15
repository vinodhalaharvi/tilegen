(tile rabbitmq
  (offers event-bus)
  (doc "RabbitMQ: exchanges, bindings and competing consumers")
  (type-name RabbitBus)
  (import amqp github.com/rabbitmq/amqp091-go)
  (topics rabbitmq amqp)

  (satisfies (cross-process)
    "consumers in other processes bind to the exchange over the network")
  (satisfies (at-least-once)
    "an unacknowledged message is requeued when the channel closes")
  (satisfies (fan-out)
    "several queues bound to one exchange each receive every message")
  (satisfies (consumer-groups)
    "several consumers on one queue compete for its messages")
  (illegal-when (replay)
    "a classic queue removes a message when it is acknowledged; there is nothing left to read back")
  (illegal-when (no-broker)
    "a broker to run, and exchanges and bindings to get right")
  (unverified (durable)
    "a durable exchange, a durable queue and persistent messages are all needed, and this tile declares none of them")
  (unverified (ordered)
    "one consumer on one queue sees publisher order, but nobody has checked what requeueing does to it")

  (cost
    (llm-work 4)
    (maintenance 4)
    (dependency 3)
    (runtime 3)
    (operations 5 (source derived "a broker to run, plus the topology to declare and keep in step")))

  (about
    "{impl} is a Bus backed by RabbitMQ. Events are published to one topic exchange, with the event type as the routing key.")
  (holds (ch *amqp.Channel) (exchange string))
  (about-new
    "New{impl} returns a {impl} over the given client.")
  (new (params (ch *amqp.Channel) (exchange string))
    "return &{impl}{ch: ch, exchange: exchange}")

  (hint publish
    "Publish e as JSON to s.exchange with routing key \"{event}\", marked persistent. Return the publish error.")
  (hint on
    "Declare a queue bound to s.exchange with routing key \"{event}\", consume it, decode each delivery as JSON into {event}, and call h. Ack on nil and Nack with requeue on error. Return a func that cancels the consumer."))
