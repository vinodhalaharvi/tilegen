(tile minio
  (offers store)
  (doc "MinIO or any S3-compatible server: one JSON object per entity under a prefix in a bucket")
  (engine minio)
  (import minio github.com/minio/minio-go/v7)
  (error-package minio)
  (topics minio s3 object-storage)

  (satisfies (cross-process)
    "a server every process reaches over HTTP")
  (illegal-when (lookup-by-field)
    "an object is found by its key; there is no query over what is inside it")
  (illegal-when (no-broker)
    "an object server to run, with disks, erasure sets and upgrades")
  (unverified (durable)
    "a PutObject is acknowledged once the server has written it, and what survives a lost disk depends on how the server is deployed: one disk, or an erasure set")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; the S3 API has no transaction over two objects")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; the S3 API has no transaction over two objects")

  (cost
    (llm-work 6 (source derived "a GetObject whose error only appears on the first read, a channel of listed objects, and a GET per object behind every list"))
    (maintenance 5 (source derived "no schema and no query: a list or a count walks every key under the prefix"))
    (dependency 2 (source derived "minio-go and a few small modules"))
    (runtime 3 (source derived "a round trip per object, and a list is one per page of keys plus one per object"))
    (operations 6 (source derived "an object server to run: disks, healing, upgrades and backups"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (client *minio.Client) (bucket string) (prefix string))
  (new (params (client *minio.Client) (bucket string))
    "return &{impl}{client: client, bucket: bucket, prefix: \"{names}/\"}")

  (hint get
    "Call s.client.GetObject(ctx, s.bucket, s.prefix + the ID as a string + \".json\", minio.GetObjectOptions{}) and defer its Close. GetObject itself does not fail for a missing key: the error comes from reading, so io.ReadAll the object and, when minio.ToErrorResponse(err).Code is \"NoSuchKey\", return ErrNotFound. Then JSON-unmarshal the bytes.")

  (hint list
    "Range over s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: s.prefix, Recursive: true}). Each minio.ObjectInfo has an Err to check and a Key, not the value: read each object as Get does. Keys come back in order.")

  (hint save
    "JSON-marshal the value and call s.client.PutObject(ctx, s.bucket, the same key as Get, bytes.NewReader(the bytes), int64(len(the bytes)), minio.PutObjectOptions{ContentType: \"application/json\"}). It replaces the whole object.")

  (hint delete
    "Call s.client.RemoveObject(ctx, s.bucket, the same key as Get, minio.RemoveObjectOptions{}). Removing a missing object is not an error.")

  (hint count
    "Range over the same ListObjects channel as List, check each Err, and count, without reading any object."))
