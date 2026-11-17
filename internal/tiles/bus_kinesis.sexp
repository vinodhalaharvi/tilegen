(tile kinesis
  (offers event-bus)
  (doc "Amazon Kinesis Data Streams: one stream, every event a record, read shard by shard")
  (type-name KinesisBus)
  (engine kinesis)
  (import kinesis github.com/aws/aws-sdk-go-v2/service/kinesis)
  (import types github.com/aws/aws-sdk-go-v2/service/kinesis/types)
  (import aws github.com/aws/aws-sdk-go-v2/aws)
  (topics kinesis aws streams)

  (satisfies (durable)
    "a PutRecord that returns has stored the record in three availability zones")
  (satisfies (cross-process)
    "a regional service every process reaches over HTTPS")
  (satisfies (replay)
    "a record stays for the stream's retention period, a day unless raised, and a reader can start from the oldest")
  (satisfies (ordered)
    "records with one partition key go to one shard and are read in the order they were written")
  (satisfies (at-least-once)
    "the consumer this tile describes keeps its place in memory: after a restart it reads the stream again from the oldest record, so nothing is missed and much is handled twice")
  (satisfies (fan-out)
    "every reader of a shard sees every record in it")
  (illegal-when (consumer-groups)
    "sharing shards among instances needs a lease table, as the Kinesis Client Library keeps; every instance of this consumer reads every shard")
  (illegal-when (no-broker)
    "a cloud service the program cannot start without: an account, the stream, credentials and a network path to it")

  (cost
    (llm-work 7 (source derived "shards to list, an iterator per shard to follow, a pause between reads, an envelope to tell event types apart, and a clean stop"))
    (maintenance 6 (source derived "shards split and merge, an iterator expires after five minutes unused, read limits per shard, and a position kept only in memory"))
    (dependency 4 (source derived "the AWS SDK's module tree"))
    (runtime 3 (source derived "an HTTPS round trip per record written, and polling every shard whether or not there is anything to read"))
    (operations 3 (source derived "nothing to patch; shards or capacity mode, IAM and cost to watch"))
    (uncertainty 2 (source derived "written from the library's documentation; no bus generated from this tile has been run against the real thing")))

  (about
    "{impl} is a Bus backed by one Amazon Kinesis data stream. Every event\ntype shares the stream, and a consumer reads all of it.")
  (holds (client *kinesis.Client) (stream string))
  (about-new
    "New{impl} returns a {impl} that writes to and reads from the stream\nof the given name, created outside this code.")
  (new (params (client *kinesis.Client) (stream string))
    "return &{impl}{client: client, stream: stream}")

  (hint publish
    "Every event type shares the stream, so wrap the event: encode struct{ Type string `json:\"type\"`; Data json.RawMessage `json:\"data\"` } with Type \"{event}\" and Data the event's JSON. Call s.client.PutRecord(ctx, &kinesis.PutRecordInput{StreamName: aws.String(s.stream), Data: the envelope's bytes, PartitionKey: aws.String(the id of the entity the event is about)}). Records with one partition key keep their order. Return its error.")

  (hint on
    "Call s.client.ListShards(ctx, &kinesis.ListShardsInput{StreamName: aws.String(s.stream)}) and start one goroutine per shard. Each calls s.client.GetShardIterator with the shard's ShardId and ShardIteratorType types.ShardIteratorTypeTrimHorizon, which starts at the oldest record, then loops: s.client.GetRecords(ctx, &kinesis.GetRecordsInput{ShardIterator: the iterator}), handle out.Records in order, continue from out.NextShardIterator, and wait a second between calls, as the service allows five reads a second per shard. A nil NextShardIterator means the shard has been closed: end that goroutine. For each record decode the envelope from r.Data, skip one whose Type is not \"{event}\", decode Data into {event} and call h; when h fails, wait and call it again with the same record before moving on, since moving on would lose it. Return a func that cancels the context and waits for the goroutines."))
