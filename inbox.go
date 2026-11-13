package main

import (
	"fmt"
	"path"
	"strings"
)

// (events
//   (event PaymentTaken (field ID uuid.UUID) (field Cents int64))
//   (inbox PaymentTaken ID))
//
// says each PaymentTaken takes effect once, however many times it is
// delivered, and that its ID field is what makes two deliveries the same
// event. It is the other half of at-least-once delivery: the bus, or an
// outbox relay, may deliver a second time, and the handler must not act
// a second time.
//
// A handler cannot check "have I seen this?" and then act: a crash between
// the two either loses the event or repeats it. So the record that the
// event was handled is written in the same transaction as whatever the
// handler writes. Both commit or neither does, and a repeat finds the
// record and does nothing.
//
// That transaction has to cover every store the handler may write, so
// (inbox ...) puts an inbox requirement on each store in the package, and
// selection answers it. Everything is generated; what the handler does
// with the event is the caller's own function.

// inboxRequirements is the requirement an (inbox ...) in the package's
// events puts on every store of the package.
func inboxRequirements(pkg *Node) []string {
	for _, ev := range pkg.FindAll("events") {
		if ev.Find("inbox") != nil {
			return []string{"inbox"}
		}
	}
	return nil
}

func validateInbox(v *validator, n, it *Node, types map[string]bool) {
	b := v.shape(it, "(inbox ?event ?field)")
	if b == nil {
		return
	}
	for _, name := range []string{"Inbox", "InboxDB"} {
		if types[name] && !types["inbox:"+name] {
			v.bad(it, "(inbox ...) generates %s, but the package already declares it", name)
		}
		types[name], types["inbox:"+name] = true, true
	}
	event, field := b.Atom("event"), b.Atom("field")
	if key := "inbox " + event; types[key] {
		v.bad(it, "duplicate (%s ...): an event has one identity", key)
	} else {
		types[key] = true
	}
	var events, fields []string
	found := false
	for _, ev := range n.FindAll("event") {
		if len(ev.List) < 2 {
			continue
		}
		events = append(events, ev.List[1].Atom)
		if ev.List[1].Atom != event {
			continue
		}
		for _, f := range ev.FindAll("field") {
			if len(f.List) > 1 {
				fields = append(fields, f.List[1].Atom)
			}
		}
		found = true
	}
	switch {
	case !found:
		v.bad(it, "(inbox %s ...) names an event, and these events have no %s%s", event, event, didYouMean(event, events))
	case !contains(fields, field):
		v.bad(it, "(inbox %s %s) names the field that tells two deliveries of one event apart, and %s has no field %s%s",
			event, field, event, field, didYouMean(field, fields))
	}
}

func selectInbox(c *Ctx, n *Node) ([]*Node, error) {
	pkg := c.Pkg
	if !c.Cfg.ContextFirst {
		return nil, fmt.Errorf("%s: (inbox ...) needs handlers that take a context.Context; remove (context-first false) from the config", n.Pos)
	}
	stores := 0
	for _, id := range sortedKeys(c.Cover) {
		cov := c.Cover[id]
		if cov.Need.Capability != "store" || !strings.HasPrefix(id, pkg.Name+".") {
			continue
		}
		stores++
		if be, _ := cov.Offer.Impl.(*Backend); be == nil || be.Tile != "postgres-sqlc" {
			return nil, fmt.Errorf("%s: (inbox ...): %s is on %s, which cannot share a transaction with the inbox; postgres-sqlc can",
				n.Pos, id, cov.Offer.Tile)
		}
	}
	if stores == 0 {
		return nil, fmt.Errorf("%s: (inbox ...) commits a handler's writes together with the record that it ran, so package %s needs an entity with a (store ...) for the handler to write",
			n.Pos, pkg.Name)
	}
	type keyed struct{ Event, Field string }
	var keys []keyed
	for _, in := range n.FindAll("inbox") {
		keys = append(keys, keyed{in.List[1].Atom, in.List[2].Atom})
	}

	table := pkg.Name + "_inbox"
	q := camelUpper(pkg.Name) + "Inbox"
	nodes := []*Node{
		L(Sym("sql/table"), Str(table), Str(fmt.Sprintf(
			"CREATE TABLE %s (\n  consumer TEXT NOT NULL,\n  key TEXT NOT NULL,\n  handled_at TIMESTAMPTZ NOT NULL DEFAULT now(),\n  PRIMARY KEY (consumer, key)\n);", table))),
		// The primary key does the work: a second delivery's insert waits
		// for the first's transaction, then finds its row and inserts none.
		L(Sym("sql/query"), Str("Mark"+q), Str(fmt.Sprintf(
			"-- name: Mark%s :execrows\nINSERT INTO %s (consumer, key) VALUES ($1, $2) ON CONFLICT DO NOTHING;", q, table))),
		L(Sym("sql/query"), Str("Prune"+q), Str(fmt.Sprintf(
			"-- name: Prune%s :execrows\nDELETE FROM %s WHERE handled_at < now() - ($1::bigint * interval '1 second');", q, table))),
	}

	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w(`// InboxDB is what the inbox needs from a connection pool: a way to begin a
// transaction. *pgxpool.Pool is one.
type InboxDB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Inbox makes a handler take effect once per event, however many times the
// event is delivered. The record that an event was handled is committed
// with whatever the handler wrote through q, or not at all.
type Inbox struct {
	db InboxDB
}

// NewInbox returns an Inbox over db.
func NewInbox(db InboxDB) *Inbox {
	return &Inbox{db: db}
}

`)
	for _, k := range keys {
		w("// Once%[1]s wraps h for Bus.On%[1]s. Two deliveries with the same %[2]s are\n"+
			"// one event: h runs for the first, and writes through q, in a transaction\n"+
			"// that also records it as handled. If h returns an error nothing is\n"+
			"// committed and the event may be delivered again. consumer names this\n"+
			"// handler, so two handlers of one event each run once.\n"+
			"func (in *Inbox) Once%[1]s(consumer string, h func(ctx context.Context, q *db.Queries, e %[1]s) error) func(context.Context, %[1]s) error {\n"+
			"\treturn func(ctx context.Context, e %[1]s) error {\n"+
			"\t\treturn in.once(ctx, consumer, \"%[1]s:\"+fmt.Sprint(e.%[2]s), func(q *db.Queries) error { return h(ctx, q, e) })\n\t}\n}\n\n",
			k.Event, k.Field)
	}
	w(`func (in *Inbox) once(ctx context.Context, consumer, key string, handle func(*db.Queries) error) error {
	tx, err := in.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // does nothing once committed
	q := db.New(tx)
	first, err := q.Mark%[1]s(ctx, db.Mark%[1]sParams{Consumer: consumer, Key: key})
	if err != nil {
		return err
	}
	if first == 0 {
		return nil // handled before: a repeat is not an error
	}
	if err := handle(q); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Prune forgets events handled longer ago than olderThan, and reports how
// many. Keep records for longer than a delivery can be delayed: an event
// redelivered after its record is gone is handled again.
func (in *Inbox) Prune(ctx context.Context, olderThan time.Duration) (int64, error) {
	tx, err := in.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	n, err := db.New(tx).Prune%[1]s(ctx, int64(olderThan/time.Second))
	if err != nil {
		return 0, err
	}
	return n, tx.Commit(ctx)
}`, q)

	gen, err := goFile(c, path.Join(pkg.Dir, "inbox_gen.go"), "generated", "", []*Node{L(Sym("go/raw"), Str(b.String()))})
	if err != nil {
		return nil, err
	}
	for _, imp := range [][2]string{{"db", c.Module + "/internal/db"}, {"pgx", "github.com/jackc/pgx/v5"}} {
		gen.List = append(gen.List, L(Sym("import"), Sym(imp[0]), Str(imp[1])))
	}
	return append(nodes, gen), nil
}
