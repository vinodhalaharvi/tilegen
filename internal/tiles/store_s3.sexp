(tile s3
  (offers store)
  (doc "S3: one JSON object per entity under a prefix in a bucket")
  (engine s3)
  (import s3 github.com/aws/aws-sdk-go-v2/service/s3)
  (error-package github.com/aws/aws-sdk-go-v2/service/s3/types)
  (topics s3 aws object-storage)

  (satisfies (durable)
    "a PutObject that returns has stored the object redundantly across availability zones")
  (satisfies (cross-process)
    "a regional service every process reaches over HTTPS, with read-after-write consistency")
  (illegal-when (lookup-by-field)
    "an object is found by its key; there is no query over what is inside it")
  (illegal-when (no-broker)
    "a cloud service the program cannot start without: an account, a bucket, credentials and a network path to them")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; S3 has no transaction over two objects")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; S3 has no transaction over two objects")

  (cost
    (llm-work 6 (source derived "paged listing, a GET per object behind every list, and typed errors, all by hand"))
    (maintenance 5 (source derived "no schema and no query: a list or a count reads every key under the prefix, and tests need an emulator or a live bucket"))
    (dependency 4 (source derived "the AWS SDK's module tree"))
    (runtime 4 (source derived "an HTTPS round trip per object, and a list is one per page of keys plus one per object"))
    (operations 2 (source derived "nothing to patch or back up by hand; a bucket policy, IAM and cost to watch"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (client *s3.Client) (bucket string) (prefix string))
  (new (params (client *s3.Client) (bucket string))
    "return &{impl}{client: client, bucket: bucket, prefix: \"{names}/\"}")

  (hint get
    "Call s.client.GetObject with Bucket aws.String(s.bucket) and Key aws.String(s.prefix + the ID as a string + \".json\"). When errors.As finds a *types.NoSuchKey, return ErrNotFound. Otherwise read out.Body to the end, Close it, and JSON-unmarshal the bytes.")

  (hint list
    "Page through s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(s.prefix)}). A page holds keys, not values: GetObject each key and unmarshal it. Keys come back in UTF-8 byte order, so the result is in ID order without sorting.")

  (hint save
    "JSON-marshal the value and call s.client.PutObject with the same Bucket and Key as Get, Body bytes.NewReader(the bytes) and ContentType aws.String(\"application/json\"). It replaces the whole object.")

  (hint delete
    "Call s.client.DeleteObject with the same Bucket and Key as Get. Deleting a missing object is not an error.")

  (hint count
    "Page through the same ListObjectsV2 paginator as List and add up aws.ToInt32(page.KeyCount) for each page, without fetching any object."))
