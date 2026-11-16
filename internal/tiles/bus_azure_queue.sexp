(tile azure-queue
  (offers event-bus)
  (doc "Azure Queue Storage: a queue per event type, polled by competing consumers")
  (alias azqueue)
  (type-name AzureQueueBus)
  (engine azure-queue)
  (import azqueue github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue)
  (topics azure queue-storage)

  (satisfies (durable)
    "an enqueue that returns has been written to the storage account's replicas")
  (satisfies (cross-process)
    "a service every process reaches over HTTPS")
  (satisfies (at-least-once)
    "a dequeued message is hidden, not removed: one that is not deleted before its visibility timeout appears again")
  (satisfies (consumer-groups)
    "consumers polling one queue share its messages")
  (illegal-when (replay)
    "a message is gone once it is deleted; nothing can be read a second time")
  (illegal-when (ordered)
    "the service promises no order")
  (illegal-when (fan-out)
    "a queue hands each message to one consumer")
  (illegal-when (no-broker)
    "a cloud service the program cannot start without: a storage account, the queues, credentials and a network path to them")

  (cost
    (llm-work 5 (source derived "a polling loop with a pause and a clean stop, and delete-on-success, by hand"))
    (maintenance 4 (source derived "a queue to create per event type, visibility timeouts to tune, and poison messages to move aside by hand"))
    (dependency 3 (source derived "the Azure SDK's core and storage modules"))
    (runtime 3 (source derived "an HTTPS round trip per message, and polling with no long poll"))
    (operations 2 (source derived "nothing to patch; access policy and cost to watch"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus backed by Azure Queue Storage. Each event type has a\nqueue of its own, and the consumers of a queue share its messages.")
  (holds (service *azqueue.ServiceClient))
  (about-new
    "New{impl} returns a {impl} over the given service client. An event\ntype's queue is its name in lower case, created outside this code.")
  (new (params (service *azqueue.ServiceClient))
    "return &{impl}{service: service}")

  (hint publish
    "Take q := s.service.NewQueueClient(strings.ToLower(\"{event}\")): a queue's name is lower case. Encode e as JSON and call q.EnqueueMessage(ctx, string(the JSON), nil). Return its error.")

  (hint on
    "Take q := s.service.NewQueueClient(strings.ToLower(\"{event}\")). In a goroutine, loop until the context ends on q.DequeueMessages(ctx, &azqueue.DequeueMessagesOptions{NumberOfMessages: a pointer to int32(10), VisibilityTimeout: a pointer to int32(30)}). There is no long poll: when resp.Messages is empty, wait a second before asking again. For each message decode *m.MessageText as JSON into {event}, call h, and call q.DeleteMessage(ctx, *m.MessageID, *m.PopReceipt, nil) only when h returns nil, so a failed message appears again after its visibility timeout. Return a func that cancels the context and waits for the goroutine."))
