package main

import "fmt"

// The bolt backend: bbolt, one file, a bucket per entity, values stored as
// JSON. Durable without a server, which is what makes it worth having next
// to memory and sqlite, and narrow in two ways it declares rather than
// discovers: the file takes an exclusive lock, and a bucket is keyed by one
// key, so finding an entity by any other field means a second index that
// somebody writes and keeps repaired.
func init() {
	registerStoreBackend(&Backend{
		Name: "bolt",
		Tile: "bolt",
		Doc:  "embedded key-value: one file, a bucket per entity, values as JSON",
		Cost: Cost{
			{Dim: "llm-work", Value: 7, Source: "derived", Note: "every read, write and scan written by hand, including the key encoding"},
			{Dim: "maintenance", Value: 6, Source: "derived", Note: "no schema and no migrations: a field added to the struct changes stored documents silently, and nothing reports it"},
			{Dim: "dependency", Value: 1, Source: "derived", Note: "bbolt only, and no build-time tool"},
			{Dim: "runtime", Value: 1},
		},
		IllegalWhen: map[string]string{
			"cross-process":   "bbolt takes an exclusive lock on the file; a second process cannot open it",
			"lookup-by-field": "a bucket has one key; a secondary index would be written and repaired by hand",
		},
		Imports:      map[string]string{"bbolt": "go.etcd.io/bbolt"},
		Topics:       []string{"bbolt", "embedded-database"},
		ErrorPackage: "bbolt",
		Implement: func(in StoreInput) (StoreParts, error) {
			return StoreParts{
				Fields: []*Node{
					L(Sym("field"), Sym("db"), Sym("*bbolt.DB")),
					L(Sym("field"), Sym("bucket"), Sym("string")),
				},
				Params: L(Sym("params"), L(Sym("db"), Sym("*bbolt.DB"))),
				Body: fmt.Sprintf("return &%s{db: db, bucket: %q}",
					in.Impl, bucketName(in.Entity)),
				Hint: boltHint,
			}, nil
		},
	})
}

// bucketName is the bucket an entity's documents live in.
func bucketName(entity string) string { return lowerFirstWord(entity) + "s" }

// boltHint says what each method has to do in bbolt's terms: a read is a
// View, a write is an Update, and the value is JSON either way.
func boltHint(meth *Node) string {
	kind, f := methodOp(meth)
	p := firstParam(meth)
	switch kind {
	case "get":
		return "In a View transaction, read the key from the bucket and unmarshal the JSON into the value. Return ErrNotFound when the key is absent."
	case "list":
		return "In a View transaction, walk the bucket with ForEach, unmarshal each value, and return them in key order."
	case "save":
		return "In an Update transaction, marshal the value to JSON and Put it under its ID, replacing whatever was there."
	case "delete":
		return "In an Update transaction, Delete the key. Deleting a missing key is not an error in bbolt."
	case "count":
		return "In a View transaction, return the bucket's Stats().KeyN."
	case "list-by", "get-by", "count-by", "exists-by", "delete-by":
		return fmt.Sprintf("Walk the bucket and test %s against %s on each value. A bucket is keyed by ID alone, so this is a scan.", f, p)
	}
	return ""
}
