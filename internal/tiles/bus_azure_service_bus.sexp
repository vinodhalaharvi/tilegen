(tile azure-service-bus
  (offers event-bus)
  (doc "Azure Service Bus: a topic per event type, a subscription per service")
  (alias servicebus)
  (type-name ServiceBus)
  (engine azure-service-bus)
  (import azservicebus github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus)
  (topics azure service-bus)

  (satisfies (durable)
    "a send that returns has been stored by the namespace")
  (satisfies (cross-process)
    "a service every process reaches over the network")
  (satisfies (at-least-once)
    "a received message is locked, and one that is abandoned or whose lock runs out is delivered again")
  (satisfies (fan-out)
    "every subscription on a topic gets a copy of each message")
  (satisfies (consumer-groups)
    "receivers on one subscription share its messages")
  (illegal-when (replay)
    "a message is gone once it is completed; nothing can be read a second time")
  (illegal-when (no-broker)
    "a cloud service the program cannot start without: a namespace, its topics and subscriptions, credentials and a network path to them")
  (unverified (ordered)
    "order is kept within a session, for messages that carry a session id on a subscription that requires one; this tile sets neither")

  (cost
    (llm-work 5 (source derived "a sender per call, a receive loop with a clean stop, and complete or abandon by the handler's result"))
    (maintenance 4 (source derived "topics, subscriptions, lock durations and a dead-letter queue to keep right"))
    (dependency 3 (source derived "the Azure SDK's core, AMQP and messaging modules"))
    (runtime 3 (source derived "an AMQP round trip per message"))
    (operations 2 (source derived "nothing to patch; the namespace, access policy and cost to watch"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus backed by Azure Service Bus. Each event type is a topic,\nand the instances of a service share one subscription to it.")
  (holds (client *azservicebus.Client) (subscription string))
  (about-new
    "New{impl} returns a {impl} over the given client. subscription names\nthis service's subscription on every topic it listens to.")
  (new (params (client *azservicebus.Client) (subscription string))
    "return &{impl}{client: client, subscription: subscription}")

  (hint publish
    "Call sender, err := s.client.NewSender(\"{event}\", nil), the topic named for the event, and defer sender.Close(ctx). Encode e as JSON and call sender.SendMessage(ctx, &azservicebus.Message{Body: the bytes}, nil); return its error.")

  (hint on
    "Call receiver, err := s.client.NewReceiverForSubscription(\"{event}\", s.subscription, nil). In a goroutine, loop on receiver.ReceiveMessages(ctx, 10, nil) until the context ends: for each message decode m.Body as JSON into {event}, call h, and receiver.CompleteMessage(ctx, m, nil) when it returns nil or receiver.AbandonMessage(ctx, m, nil) when it does not, so the message is delivered again. Return a func that cancels the context, waits for the goroutine and closes the receiver."))
