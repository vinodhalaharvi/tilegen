(tile sqs-fifo
  (offers event-bus)
  (doc "Amazon SQS FIFO: a queue per event type, in order within each entity's events")
  (alias sqsfifo)
  (type-name SQSFIFOBus)
  (engine sqs)
  (import sqs github.com/aws/aws-sdk-go-v2/service/sqs)
  (topics sqs aws)

  (satisfies (durable)
    "a SendMessage that returns has stored the message redundantly across availability zones")
  (satisfies (cross-process)
    "a regional service every process reaches over HTTPS")
  (satisfies (ordered)
    "a FIFO queue delivers the messages of one message group in the order they were sent")
  (satisfies (at-least-once)
    "a message not deleted before its visibility timeout is delivered again")
  (satisfies (consumer-groups)
    "consumers polling one queue share its message groups")
  (illegal-when (replay)
    "a message is gone once it is deleted; nothing can be read a second time")
  (illegal-when (fan-out)
    "a queue hands each message to one consumer; reaching several subscribers takes an SNS FIFO topic in front with a queue each")
  (illegal-when (no-broker)
    "a cloud service the program cannot start without: an account, the queues, credentials and a network path to them")

  (cost
    (llm-work 5 (source derived "a polling loop with a clean stop, delete-on-success, and a group and a deduplication id for every message"))
    (maintenance 5 (source derived "a .fifo queue to create per event type, and a message that keeps failing holds back the rest of its group until it reaches the dead-letter queue"))
    (dependency 4 (source derived "the AWS SDK's module tree"))
    (runtime 3 (source derived "an HTTPS round trip per send, a long poll per batch, and a lower throughput ceiling than a standard queue"))
    (operations 2 (source derived "nothing to patch; queues, IAM and cost to watch"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus backed by Amazon SQS FIFO queues. Each event type has a\nqueue of its own, and one entity's events are handled in the order sent.")
  (holds (client *sqs.Client) (queues "map[string]string"))
  (about-new
    "New{impl} returns a {impl} that sends each event type to the queue URL\nqueues gives for its name. Each is a FIFO queue, its name ending in .fifo.")
  (new (params (client *sqs.Client) (queues "map[string]string"))
    "return &{impl}{client: client, queues: queues}")

  (hint publish
    "Encode e as JSON and call s.client.SendMessage with QueueUrl aws.String(s.queues[\"{event}\"]), MessageBody aws.String(the JSON), MessageGroupId aws.String(the id of the entity the event is about), so that one entity's events stay in order, and MessageDeduplicationId aws.String(a value unique to this event), unless the queue has content-based deduplication turned on. An event type with no queue in s.queues is an error to return.")

  (hint on
    "Start a goroutine that long-polls s.client.ReceiveMessage on s.queues[\"{event}\"] with WaitTimeSeconds 20 and MaxNumberOfMessages 10. For each message, in the order received, decode aws.ToString(m.Body) as JSON into {event}, call h, and call s.client.DeleteMessage with the message's ReceiptHandle only when h returns nil. A message that is not deleted comes back after its visibility timeout and holds back the rest of its group until then: that is what keeps the order. Return a func that cancels the polling context and waits for the goroutine to end."))
