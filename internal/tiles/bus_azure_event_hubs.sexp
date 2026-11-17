(tile azure-event-hubs
  (offers event-bus)
  (doc "Azure Event Hubs: one hub, every event a record, read partition by partition")
  (alias eventhubs)
  (type-name EventHubsBus)
  (engine azure-event-hubs)
  (import azeventhubs github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2)
  (topics azure event-hubs streams)

  (satisfies (durable)
    "a send that returns has been written to the hub's replicas")
  (satisfies (cross-process)
    "a service every process reaches over the network")
  (satisfies (replay)
    "an event stays for the hub's retention period, and a reader can start from the earliest")
  (satisfies (ordered)
    "events with one partition key go to one partition and are read in the order they were sent")
  (satisfies (at-least-once)
    "the consumer this tile describes keeps its place in memory: after a restart it reads the hub again from the earliest event, so nothing is missed and much is handled twice")
  (satisfies (fan-out)
    "every reader of a partition sees every event in it")
  (illegal-when (consumer-groups)
    "sharing partitions among instances needs the SDK's Processor and a checkpoint store in Blob Storage, which this tile does not use; every instance of this consumer reads every partition")
  (illegal-when (no-broker)
    "a cloud service the program cannot start without: a namespace, the hub, credentials and a network path to it")

  (cost
    (llm-work 6 (source derived "a batch for every send, a client per partition to follow, a deadline on every receive, and a clean stop"))
    (maintenance 5 (source derived "a position kept only in memory, a hub's partition count fixed when it is made, and event types told apart by a property"))
    (dependency 3 (source derived "the Azure SDK's core, AMQP and Event Hubs modules"))
    (runtime 2 (source derived "a connection held open, and a batch per send"))
    (operations 3 (source derived "nothing to patch; throughput units, access policy and cost to watch"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus backed by one Azure event hub. Every event type shares\nthe hub, and a consumer reads all of it.")
  (holds (producer *azeventhubs.ProducerClient) (consumer *azeventhubs.ConsumerClient))
  (about-new
    "New{impl} sends through producer and reads through consumer, both\nopened by the caller on the same hub.")
  (new (params (producer *azeventhubs.ProducerClient) (consumer *azeventhubs.ConsumerClient))
    "return &{impl}{producer: producer, consumer: consumer}")

  (hint publish
    "Take key := the id of the entity the event is about and call batch, err := s.producer.NewEventDataBatch(ctx, &azeventhubs.EventDataBatchOptions{PartitionKey: &key}); events with one partition key keep their order. Encode e as JSON and call batch.AddEventData(&azeventhubs.EventData{Body: the bytes, Properties: map[string]any{\"type\": \"{event}\"}}, nil), then s.producer.SendEventDataBatch(ctx, batch, nil). Return the first error.")

  (hint on
    "Call props, err := s.consumer.GetEventHubProperties(ctx, nil) and start one goroutine per id in props.PartitionIDs. Each takes earliest := true and pc, err := s.consumer.NewPartitionClient(id, &azeventhubs.PartitionClientOptions{StartPosition: azeventhubs.StartPosition{Earliest: &earliest}}), defers pc.Close, and loops: call pc.ReceiveEvents with a context that times out after a second and a count of 100; it returns what arrived, and a context.DeadlineExceeded with no events only means none came. For each event skip one whose Properties[\"type\"] is not \"{event}\", decode Body as JSON into {event} and call h; when h fails, wait and call it again with the same event before moving on, since moving on would lose it. Return a func that cancels the context and waits for the goroutines."))
