(tile-spec dynamodb
  (kind store-backend)
  (doc "DynamoDB: one table per entity keyed by id, items marshalled attribute by attribute")
  (engine dynamodb)
  (import dynamodb github.com/aws/aws-sdk-go-v2/service/dynamodb)
  (error-package github.com/aws/aws-sdk-go-v2/service/dynamodb/types)
  (topics dynamodb aws)

  (satisfies (durable)
    "a write is acknowledged once it is stored in more than one availability zone")
  (satisfies (cross-process)
    "a regional service every process reaches over HTTPS")
  (illegal-when (no-broker)
    "a cloud service the program cannot start without: an account, a table, credentials and a network path to them")
  (unverified (lookup-by-field)
    "a Query on another field needs a global secondary index, and a Scan with a filter reads the whole table; this tile creates neither")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; TransactWriteItems can write both, and this tile does not generate one")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; TransactWriteItems can write both, and this tile does not generate one")

  (cost
    (llm-work 8 (source derived "attribute values, the key schema, pagination and results in no order, written by hand for every method"))
    (maintenance 5 (source derived "no schema beyond the key, a list or a count is a paid read of the whole table, and tests need an emulator or a live table"))
    (dependency 4 (source derived "the AWS SDK's module tree"))
    (runtime 2 (source derived "an HTTPS round trip per call"))
    (operations 3 (source derived "nothing to patch or back up by hand, but capacity, IAM and cost to watch")))

  (holds (client *dynamodb.Client) (table string))
  (new (params (client *dynamodb.Client))
    "return &{impl}{client: client, table: \"{names}\"}")

  (hint get
    "Call s.client.GetItem with TableName aws.String(s.table) and Key map[string]types.AttributeValue{\"id\": &types.AttributeValueMemberS{Value: the ID as a string}}. An output with no Item means the key is absent: return ErrNotFound. Otherwise attributevalue.UnmarshalMap(out.Item, &value).")
  (hint list
    "Page through the table with dynamodb.NewScanPaginator(s.client, &dynamodb.ScanInput{TableName: aws.String(s.table)}), unmarshal each page with attributevalue.UnmarshalListOfMaps, and sort the result by ID: a Scan returns items in no useful order, and reads the whole table.")
  (hint save
    "attributevalue.MarshalMap the value, set the item's \"id\" attribute to the ID as a string (the table's key), and call s.client.PutItem, which replaces the whole item.")
  (hint delete
    "Call s.client.DeleteItem with the same TableName and Key as Get. Deleting a missing item is not an error.")
  (hint count
    "Page through a Scan with Select: types.SelectCount and add up each page's Count. DescribeTable's ItemCount is refreshed only every few hours and is not the answer."))
