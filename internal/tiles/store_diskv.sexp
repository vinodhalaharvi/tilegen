(tile diskv
  (offers store)
  (doc "files on disk through diskv: one file per value, as JSON")
  (import diskv github.com/peterbourgon/diskv/v3)
  (error-package diskv)
  (topics diskv files)

  (satisfies (durable)
    "every write this tile asks for is synced to disk before it returns")
  (satisfies (no-broker)
    "a library and a directory; there is no server")
  (illegal-when (cross-process)
    "nothing coordinates two processes writing one directory, and each would keep a cache of its own")
  (illegal-when (lookup-by-field)
    "a value is found by its key, which is its file's name")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; there are no transactions over two files")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; there are no transactions over two files")

  (cost
    (llm-work 4 (source derived "read, write, erase and a channel of keys, with the JSON and a sort by hand"))
    (maintenance 4 (source derived "a file per value in one directory unless a transform spreads them, a key must be a legal file name, and a list reads every file"))
    (dependency 1 (source derived "diskv and a small B-tree module"))
    (runtime 2 (source derived "a file opened and synced on every write"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (d *diskv.Diskv) (prefix string))
  (new (params (d *diskv.Diskv))
    "return &{impl}{d: d, prefix: \"{names}-\"}")

  (hint get
    "Call s.d.Read(s.prefix + the ID as a string). When errors.Is(err, fs.ErrNotExist), return ErrNotFound. Otherwise JSON-unmarshal the bytes. The key becomes a file name, so an ID containing a path separator is an error to return before it reaches the disk.")

  (hint list
    "Range over s.d.KeysPrefix(s.prefix, nil), a channel of keys in no promised order: collect them, sort them, then Read and unmarshal each, skipping one that has gone (fs.ErrNotExist).")

  (hint save
    "JSON-marshal the value and call s.d.WriteStream(s.prefix + the ID as a string, bytes.NewReader(the bytes), true). The last argument syncs the file to disk before returning; Write alone does not.")

  (hint delete
    "Call s.d.Erase(s.prefix + the ID as a string). It returns an error for a missing key: when errors.Is(err, fs.ErrNotExist), treat that as success.")

  (hint count
    "Range over s.d.KeysPrefix(s.prefix, nil) and count the keys, without reading any file."))
