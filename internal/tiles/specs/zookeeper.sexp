(tile-spec zookeeper
  (kind store-backend)
  (doc "ZooKeeper: one znode per entity under a parent path, data as JSON")
  (engine zookeeper)
  (import zk github.com/go-zookeeper/zk)
  (error-package zk)
  (topics zookeeper coordination)

  (satisfies (durable)
    "a write is acknowledged once a quorum of servers has logged it to disk")
  (satisfies (cross-process)
    "an ensemble every process reaches over the network; a client reads its own writes")
  (illegal-when (lookup-by-field)
    "a znode is found by its path; there is no query over its data")
  (illegal-when (no-broker)
    "a ZooKeeper ensemble to run")
  (unverified (outbox)
    "an outbox is one transaction over the entity and the event; Multi commits several operations atomically, and this tile does not generate one")
  (unverified (inbox)
    "an inbox is one transaction over a handler's writes and the record that it ran; Multi commits several operations atomically, and this tile does not generate one")

  (cost
    (llm-work 5 (source derived "paths, versions, a create-or-set for every save and a read per child for every list"))
    (maintenance 7 (source derived "no schema, and it is built for a little data that matters a great deal: a znode holds under 1 MiB, the whole tree lives in each server's memory, and listing reads children one by one"))
    (dependency 1 (source derived "the zk client only"))
    (runtime 2 (source derived "a quorum write, and a list is one call per znode"))
    (operations 7 (source derived "an ensemble of three or five to keep in quorum, with snapshots and logs to purge"))
    (uncertainty 2 (source derived "written from the library's documentation; no store generated from this tile has been run against the real thing")))

  (holds (conn *zk.Conn) (parent string))
  (new (params (conn *zk.Conn))
    "return &{impl}{conn: conn, parent: \"/{names}\"}")

  (hint get
    "Call s.conn.Get(s.parent + \"/\" + the ID as a string). When errors.Is(err, zk.ErrNoNode), return ErrNotFound. Otherwise JSON-unmarshal the data. The methods take no context; honour ctx by checking ctx.Err() first.")

  (hint list
    "Call s.conn.Children(s.parent); zk.ErrNoNode there means nothing was ever saved, so return an empty list. Sort the names, Get each child, skip one that has gone (zk.ErrNoNode) and unmarshal the rest.")

  (hint save
    "JSON-marshal the value and call s.conn.Set(path, the bytes, -1), where -1 matches any version. On zk.ErrNoNode the znode does not exist yet: s.conn.Create(path, the bytes, 0, zk.WorldACL(zk.PermAll)), creating s.parent first the same way with empty data if Create reports zk.ErrNoNode for it, and going back to Set if Create reports zk.ErrNodeExists.")

  (hint delete
    "Call s.conn.Delete(s.parent + \"/\" + the ID as a string, -1). It returns zk.ErrNoNode for a missing znode: treat that as success.")

  (hint count
    "Call s.conn.Children(s.parent) and return how many names came back; zk.ErrNoNode means zero."))
