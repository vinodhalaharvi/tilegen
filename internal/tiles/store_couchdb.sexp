(tile couchdb
  (offers store)
  (doc "CouchDB through kivik: a database per entity, a document per value")
  (engine couchdb)
  (import kivik github.com/go-kivik/kivik/v4)
  (error-package kivik)
  (topics couchdb kivik)

  (satisfies (durable)
    "CouchDB appends a write to the database file and syncs it before it answers")
  (satisfies (cross-process)
    "a server other processes reach over HTTP")
  (satisfies (lookup-by-field)
    "a Mango selector on the field; it reads every document unless an index on that field is added")
  (illegal-when (no-broker)
    "a CouchDB server or cluster to run")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; CouchDB has no transaction over two documents")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; CouchDB has no transaction over two documents")

  (cost
    (llm-work 6 (source derived "every write needs the document's current revision first, deletes too, and a selector query comes back a page at a time"))
    (maintenance 5 (source derived "no schema, two writers to one document conflict and one must retry, and design documents share the database with the values"))
    (dependency 2 (source derived "kivik and its CouchDB driver, which the caller imports to register it"))
    (runtime 3 (source derived "an HTTP round trip per call, and two per write"))
    (operations 5 (source derived "a server to run, with compaction, backups and upgrades"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (db *kivik.DB))
  (new (params (db *kivik.DB))
    "return &{impl}{db: db}")

  (hint get
    "Call err := s.db.Get(ctx, the ID as a string).ScanDoc(&v). When kivik.HTTPStatus(err) == http.StatusNotFound, return ErrNotFound. db is this entity's own database, opened by the caller with client.DB; the document's _id and _rev sit beside the entity's fields and are ignored on the way out.")

  (hint list
    "Call rs := s.db.AllDocs(ctx, kivik.IncludeDocs()) and defer rs.Close(). While rs.Next(), take id, _ := rs.ID(), skip one that starts with \"_design/\", and decode the rest with rs.ScanDoc(&v). Return rs.Err() at the end. AllDocs is in order of id.")

  (hint save
    "CouchDB replaces a document only when told its current revision. Call rev, err := s.db.GetRev(ctx, the ID as a string); a kivik.HTTPStatus(err) of http.StatusNotFound means the document is new and there is no revision to send. Then call s.db.Put(ctx, the ID as a string, the value, kivik.Rev(rev)), leaving the option out for a new document. An http.StatusConflict from Put means another writer got in between: return it as an error for the caller to retry.")

  (hint delete
    "Call rev, err := s.db.GetRev(ctx, the ID as a string); when kivik.HTTPStatus(err) == http.StatusNotFound it was already gone: return nil. Otherwise call s.db.Delete(ctx, the ID as a string, rev).")

  (hint count
    "Walk s.db.AllDocs(ctx) without IncludeDocs and count the rows whose rs.ID() does not start with \"_design/\". Return rs.Err() at the end.")

  (hint list-by
    "Call rs := s.db.Find(ctx, map[string]any{\"selector\": map[string]any{KEY: {param}}, \"limit\": 100}), where KEY is the key the entity's JSON uses for {field}. Decode each row with rs.ScanDoc(&v). Find answers a page at a time: after rs.Next() returns false, take meta, err := rs.Metadata() and ask again with \"bookmark\": meta.Bookmark added to the query, until a page comes back with fewer than 100 rows. Sort the values by ID before returning.")

  (hint get-by
    "Call s.db.Find with the same selector on {field} as ListBy and \"limit\": 1. When rs.Next() is false, return rs.Err() if it is set and ErrNotFound if it is not. Otherwise rs.ScanDoc(&v).")

  (hint count-by
    "Page through the same Find as ListBy for {field}, adding \"fields\": []string{\"_id\"} so only ids come back, and count the rows.")

  (hint exists-by
    "Call s.db.Find with the same selector on {field} as ListBy and \"limit\": 1, and report whether rs.Next() is true, returning rs.Err() when it is set.")

  (hint delete-by
    "Page through the same Find as ListBy for {field}; for each row take the id with rs.ID() and the revision with rs.Rev(), and call s.db.Delete(ctx, id, rev) once the page is read."))
