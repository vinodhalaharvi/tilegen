; The dynamodb tile. `tilegen -out . internal/tiles/specs/dynamodb.sexp` regenerates its
; registration in internal/tiles/store_dynamodb.go; the two holes there are filled by hand.

(project tilegen-tiles
  (module github.com/vinodhalaharvi/tilegen)
  (go 1.26)
  (require (dynamodb github.com/aws/aws-sdk-go-v2/service/dynamodb v1.74.0))

  (package tiles
    (dir "internal/tiles")

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
        (operations 3 (source derived "nothing to patch or back up by hand, but capacity, IAM and cost to watch"))))))
