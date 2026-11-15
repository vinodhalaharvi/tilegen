(tile sqs
  (offers event-bus)
  (doc "Amazon SQS: a queue per event type, long-polled by competing consumers")
  (type-name SQSBus)
  (engine sqs)
  (import sqs github.com/aws/aws-sdk-go-v2/service/sqs)
  (topics sqs aws)

  (satisfies (durable)
    "a SendMessage that returns has stored the message redundantly across availability zones")
  (satisfies (cross-process)
    "a regional service every process reaches over HTTPS")
  (satisfies (at-least-once)
    "a message not deleted before its visibility timeout is delivered again")
  (satisfies (consumer-groups)
    "consumers polling one queue share its messages")
  (illegal-when (replay)
    "a message is gone once it is deleted; nothing can be read a second time")
  (illegal-when (ordered)
    "a standard queue delivers in no promised order; a FIFO queue orders within a message group, and this tile sets none")
  (illegal-when (fan-out)
    "a queue hands each message to one consumer; reaching several subscribers takes an SNS topic in front with a queue each")
  (illegal-when (no-broker)
    "a cloud service the program cannot start without: an account, the queues, credentials and a network path to them")

  (cost
    (llm-work 5 (source derived "a polling loop with a clean stop, and delete-on-success, by hand"))
    (maintenance 4 (source derived "a queue to create per event type, visibility timeouts to tune, and a dead-letter queue to watch"))
    (dependency 4 (source derived "the AWS SDK's module tree"))
    (runtime 3 (source derived "an HTTPS round trip per send, and a long poll per batch"))
    (operations 2 (source derived "nothing to patch; queues, IAM and cost to watch"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus backed by Amazon SQS. Each event type has a queue of its own,\nand the consumers of a queue share its messages.")
  (holds (client *sqs.Client) (queues "map[string]string"))
  (about-new
    "New{impl} returns a {impl} that sends each event type to the queue URL\nqueues gives for its name.")
  (new (params (client *sqs.Client) (queues "map[string]string"))
    "return &{impl}{client: client, queues: queues}")

  (hint publish
    "Encode e as JSON and call s.client.SendMessage with QueueUrl aws.String(s.queues[\"{event}\"]) and MessageBody aws.String(the JSON). An event type with no queue in s.queues is an error to return, not a message to drop.")
  (hint on
    "Start a goroutine that long-polls s.client.ReceiveMessage on s.queues[\"{event}\"] with WaitTimeSeconds 20 and MaxNumberOfMessages 10. For each message, decode aws.ToString(m.Body) as JSON into {event}, call h, and call s.client.DeleteMessage with the message's ReceiptHandle only when h returns nil, so a failed message comes back after its visibility timeout. Return a func that cancels the polling context and waits for the goroutine to end."))
