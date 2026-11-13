package main

import (
	"fmt"
	"path"
	"strings"
)

// (events
//   (event OrderPlaced (field OrderID uuid.UUID))
//   (outbox Order))
//
// says an Order is saved and its events are published as one act: both
// happen or neither does. No bus can promise that together with a
// database, so the event is first written to a table in the same
// transaction as the entity, and a relay publishes it from there.
//
// Two things follow. The entity's store must be one that can share a
// transaction with that table, so (outbox Order) becomes an outbox
// requirement on Order's store and selection answers it. And delivery is
// at least once: the relay publishes, then deletes, and a crash between
// the two publishes again. Handlers must tolerate a repeat.
//
// Everything is generated: which rows go in one transaction is not a
// judgment call. The store's own Save stays whatever it is, hole or not;
// the outbox calls it inside the transaction.

// outboxRequirements is the requirement an (outbox Entity) in the
// package's events puts on that entity's store.
func outboxRequirements(pkg *Node, entity string) []string {
	for _, ev := range pkg.FindAll("events") {
		for _, ob := range ev.FindAll("outbox") {
			if len(ob.List) == 2 && ob.List[1].Atom == entity {
				return []string{"outbox"}
			}
		}
	}
	return nil
}

func validateOutbox(v *validator, it *Node, types map[string]bool) {
	if b := v.shape(it, "(outbox ?entity)"); b == nil {
		return
	}
	for _, name := range []string{"Outbox", "OutboxRelay", "OutboxDB"} {
		if types[name] && !types["outbox:"+name] {
			v.bad(it, "(outbox ...) generates %s, but the package already declares it", name)
		}
		types[name], types["outbox:"+name] = true, true
	}
	if key := "outbox " + it.List[1].Atom; types[key] {
		v.bad(it, "duplicate (%s)", key)
	} else {
		types[key] = true
	}
}

func selectEventsAndOutbox(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	out, err := selectEvents(m, b, n)
	if err != nil || n.Find("outbox") == nil {
		return out, err
	}
	more, err := selectOutbox(m.C, n)
	return append(out, more...), err
}

func selectOutbox(c *Ctx, n *Node) ([]*Node, error) {
	pkg := c.Pkg
	if !c.Cfg.ContextFirst {
		return nil, fmt.Errorf("%s: (outbox ...) needs stores that take a context.Context; remove (context-first false) from the config", n.Pos)
	}
	var events, entities []string
	for _, ev := range n.FindAll("event") {
		events = append(events, ev.List[1].Atom)
	}
	for _, ob := range n.FindAll("outbox") {
		e := ob.List[1].Atom
		iface := pkg.Ifaces[e+"Store"]
		if iface == nil {
			return nil, fmt.Errorf("%s: (outbox %s): %s has no store in this package; add (store save) to the entity", ob.Pos, e, e)
		}
		if !storeHas(iface, "save") {
			return nil, fmt.Errorf("%s: (outbox %s) saves a %s with its events, so %sStore needs save; add it to the entity's (store ...)", ob.Pos, e, e, e)
		}
		cov := c.Cover[pkg.Name+"."+e+"Store"]
		be, _ := cov.Offer.Impl.(*Backend)
		if be == nil || be.Tile != "postgres-sqlc" {
			return nil, fmt.Errorf("%s: (outbox %s): %sStore is on %s, which does not generate an outbox; postgres-sqlc does",
				ob.Pos, e, e, cov.Offer.Tile)
		}
		entities = append(entities, e)
	}

	// One table per package, so two packages' relays never take each
	// other's rows. BIGSERIAL orders rows by when they were written.
	table := pkg.Name + "_outbox"
	q := camelUpper(pkg.Name) + "Outbox" // InsertOrdersOutbox
	nodes := []*Node{
		L(Sym("sql/table"), Str(table), Str(fmt.Sprintf(
			"CREATE TABLE %s (\n  id BIGSERIAL PRIMARY KEY,\n  topic TEXT NOT NULL,\n  payload JSONB NOT NULL,\n  created_at TIMESTAMPTZ NOT NULL DEFAULT now()\n);", table))),
		L(Sym("sql/query"), Str("Insert"+q), Str(fmt.Sprintf(
			"-- name: Insert%s :exec\nINSERT INTO %s (topic, payload) VALUES ($1, $2);", q, table))),
		// SKIP LOCKED lets several relays run without waiting on each
		// other: each takes rows no other holds.
		L(Sym("sql/query"), Str("Next"+q), Str(fmt.Sprintf(
			"-- name: Next%s :many\nSELECT id, topic, payload FROM %s ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED;", q, table))),
		L(Sym("sql/query"), Str("Delete"+q), Str(fmt.Sprintf(
			"-- name: Delete%s :exec\nDELETE FROM %s WHERE id = ANY($1::bigint[]);", q, table))),
	}

	gen, err := goFile(c, path.Join(pkg.Dir, "outbox_gen.go"), "generated", "",
		[]*Node{L(Sym("go/raw"), Str(outboxText(pkg.Name, q, entities, events)))})
	if err != nil {
		return nil, err
	}
	for _, imp := range [][2]string{{"db", c.Module + "/internal/db"}, {"pgx", "github.com/jackc/pgx/v5"}} {
		gen.List = append(gen.List, L(Sym("import"), Sym(imp[0]), Str(imp[1])))
	}
	return append(nodes, gen), nil
}

// camelUpper is a package name as part of a Go identifier: orders -> Orders.
func camelUpper(s string) string {
	c := camel(s)
	return strings.ToUpper(c[:1]) + c[1:]
}

func outboxText(pkg, q string, entities, events []string) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w(`// OutboxDB is what the outbox needs from a connection pool: a way to begin
// a transaction. *pgxpool.Pool is one.
type OutboxDB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Outbox saves an entity and records an event in one transaction: both are
// committed or neither is. The event is not published here. OutboxRelay
// publishes it afterwards, at least once.
type Outbox struct {
	db OutboxDB
}

// NewOutbox returns an Outbox over db.
func NewOutbox(db OutboxDB) *Outbox {
	return &Outbox{db: db}
}

`)
	for _, e := range entities {
		for _, ev := range events {
			arg := lowerFirstWord(e)
			w("// Save%sAndPublish%s saves %s and records e for publishing, atomically.\n"+
				"func (o *Outbox) Save%sAndPublish%s(ctx context.Context, %s *%s, e %s) error {\n"+
				"\treturn o.commit(ctx, %q, e, func(q *db.Queries) error {\n\t\treturn NewPostgres%sStore(q).Save(ctx, %s)\n\t})\n}\n\n",
				e, ev, arg, e, ev, arg, e, ev, ev, e, arg)
		}
	}
	w(`// commit runs write and records the event in one transaction.
func (o *Outbox) commit(ctx context.Context, topic string, e any, write func(*db.Queries) error) error {
	payload, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("%[1]s: outbox: encode %%s: %%w", topic, err)
	}
	tx, err := o.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // does nothing once committed
	q := db.New(tx)
	if err := write(q); err != nil {
		return err
	}
	if err := q.Insert%[2]s(ctx, db.Insert%[2]sParams{Topic: topic, Payload: payload}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// OutboxRelay publishes recorded events to the Bus, oldest first, and
// deletes each once the Bus has taken it. A crash after publishing and
// before deleting publishes again on restart: delivery is at least once,
// so handlers must tolerate a repeat. One relay keeps events in the order
// they were recorded; several share the work and give that up.
type OutboxRelay struct {
	db  OutboxDB
	bus Bus

	Batch int32         // events taken per transaction
	Every time.Duration // how long to wait when there is nothing to publish
}

// NewOutboxRelay returns a relay that takes 100 events at a time and looks
// again every second when idle.
func NewOutboxRelay(db OutboxDB, bus Bus) *OutboxRelay {
	return &OutboxRelay{db: db, bus: bus, Batch: 100, Every: time.Second}
}

// Run publishes until ctx is done, and returns ctx's error. A failure to
// publish is retried after Every; onError, if not nil, is told about it.
func (r *OutboxRelay) Run(ctx context.Context, onError func(error)) error {
	for {
		n, err := r.Drain(ctx)
		if err != nil && onError != nil && ctx.Err() == nil {
			onError(err)
		}
		if err == nil && n > 0 {
			continue // there may be more
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.Every):
		}
	}
}

// Drain publishes one batch and reports how many events it published. If
// the Bus refuses one, the events before it are still deleted and the
// rest stay for the next call.
func (r *OutboxRelay) Drain(ctx context.Context) (int, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	q := db.New(tx)
	rows, err := q.Next%[2]s(ctx, r.Batch)
	if err != nil {
		return 0, err
	}
	var done []int64
	var failed error
	for _, row := range rows {
		if failed = r.publish(ctx, row.Topic, row.Payload); failed != nil {
			break
		}
		done = append(done, row.ID)
	}
	if len(done) > 0 {
		if err := q.Delete%[2]s(ctx, done); err != nil {
			return 0, err
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
	}
	return len(done), failed
}

func (r *OutboxRelay) publish(ctx context.Context, topic string, payload []byte) error {
	switch topic {
`, pkg, q)
	for _, ev := range events {
		w("\tcase %q:\n\t\tvar e %s\n\t\tif err := json.Unmarshal(payload, &e); err != nil {\n\t\t\treturn fmt.Errorf(\"%s: outbox: decode %s: %%w\", err)\n\t\t}\n\t\treturn r.bus.Publish%s(ctx, e)\n", ev, ev, pkg, ev, ev)
	}
	w("\t}\n\treturn fmt.Errorf(\"%s: outbox: no event named %%q; it was recorded by a version of this program that had one\", topic)\n}", pkg)
	return b.String()
}
