(tile sns-sqs
  (offers event-bus)
  (doc "Amazon SNS and SQS: a topic per event type, fanned out to a queue per subscriber")
  (alias snssqs)
  (type-name SNSSQSBus)
  (engine sns-sqs)
  (import sns github.com/aws/aws-sdk-go-v2/service/sns)
  (import sqs github.com/aws/aws-sdk-go-v2/service/sqs)
  (topics sns sqs aws)

  (satisfies (durable)
    "a Publish that returns has stored the message redundantly, and each subscribed queue stores its copy the same way")
  (satisfies (cross-process)
    "regional services every process reaches over HTTPS")
  (satisfies (at-least-once)
    "a message not deleted from its queue before the visibility timeout is delivered again")
  (satisfies (fan-out)
    "every queue subscribed to a topic gets its own copy of each message")
  (satisfies (consumer-groups)
    "consumers polling one queue share its messages")
  (illegal-when (replay)
    "a message is gone from a queue once it is deleted; nothing can be read a second time")
  (illegal-when (ordered)
    "a standard topic and queue deliver in no promised order; FIFO topics and queues order within a message group, and this tile sets none")
  (illegal-when (no-broker)
    "cloud services the program cannot start without: an account, the topics, queues and subscriptions, credentials and a network path to them")

  (cost
    (llm-work 5 (source derived "a publish, a polling loop with a clean stop, and delete-on-success, by hand"))
    (maintenance 5 (source derived "a topic per event type, a queue per subscriber, and the subscriptions and queue policies that join them, to create and keep in step"))
    (dependency 4 (source derived "the AWS SDK's module tree"))
    (runtime 3 (source derived "an HTTPS round trip per publish, and a long poll per batch"))
    (operations 2 (source derived "nothing to patch; topics, queues, IAM and cost to watch"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus backed by Amazon SNS and SQS. An event is published to\nits type's topic, and each service reads its own queue subscribed to it.")
  (holds (sns *sns.Client) (sqs *sqs.Client) (topics "map[string]string") (queues "map[string]string"))
  (about-new
    "New{impl} publishes each event type to the topic ARN topics gives for\nits name, and reads it from the queue URL queues gives. Each queue is\nsubscribed to its topic with raw message delivery turned on.")
  (new (params (sns *sns.Client) (sqs *sqs.Client) (topics "map[string]string") (queues "map[string]string"))
    "return &{impl}{sns: sns, sqs: sqs, topics: topics, queues: queues}")

  (hint publish
    "Encode e as JSON and call s.sns.Publish with TopicArn aws.String(s.topics[\"{event}\"]) and Message aws.String(the JSON). An event type with no topic in s.topics is an error to return, not a message to drop.")
  (hint on
    "Start a goroutine that long-polls s.sqs.ReceiveMessage on s.queues[\"{event}\"] with WaitTimeSeconds 20 and MaxNumberOfMessages 10. With raw message delivery on the subscription, a message's Body is the JSON as it was published: decode aws.ToString(m.Body) into {event}. Call h, and call s.sqs.DeleteMessage with the message's ReceiptHandle only when h returns nil, so a failed message comes back after its visibility timeout. Return a func that cancels the polling context and waits for the goroutine to end."))
