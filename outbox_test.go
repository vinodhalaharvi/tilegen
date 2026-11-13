package main

import (
	"go/parser"
	"go/token"
	"io"
	"strings"
	"testing"
)

func outboxSpec(store, events string) string {
	return `(project ledger (module example.com/ledger) (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0))
  (package ledger
    (entity Payment (field ID uuid.UUID) (field Cents int64) (store ` + store + `))
    (entity Note (field ID string) (store get save))
    (events (event PaymentTaken (field ID uuid.UUID)) (event PaymentRefunded (field ID uuid.UUID)) ` + events + `)))`
}

func TestOutboxChoosesTheStore(t *testing.T) {
	// Nothing names a database. (outbox Payment) alone puts PaymentStore on
	// the one tile that can commit an entity and an event together, and
	// leaves Note, which asked for nothing, on the cheapest.
	out := t.TempDir()
	sp, _ := writeSpec(t, t.TempDir(), outboxSpec("get save", "(outbox Payment)"), "")
	if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	lock := read(t, out, "tilegen.lock")
	for _, want := range []string{"(tile ledger.PaymentStore postgres-sqlc)", "(tile ledger.NoteStore memory)", "(tile ledger.Bus local-bus)"} {
		if !strings.Contains(lock, want) {
			t.Errorf("lock missing %s:\n%s", want, lock)
		}
	}
	var log strings.Builder
	if err := explainCmd([]string{sp}, &log, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"needs: outbox", "nothing is committed, so there is no transaction for an event to share",
		"unverified: an outbox is one transaction over the entity and the event; this tile does not generate one"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("explain missing %q", want)
		}
	}
	// A config cannot put the store somewhere an outbox cannot be.
	sp, cp := writeSpec(t, t.TempDir(), outboxSpec("get save", "(outbox Payment)"), "(config (storage sqlite))")
	if err := run(Options{Spec: sp, Config: cp, Out: t.TempDir()}, io.Discard); err == nil || !strings.Contains(err.Error(), "this tile does not generate one") {
		t.Errorf("sqlite should be refused for an outbox: %v", err)
	}
}

func TestOutboxGeneratesTheTransactionAndTheRelay(t *testing.T) {
	out := t.TempDir()
	sp, _ := writeSpec(t, t.TempDir(), outboxSpec("get save", "(outbox Payment)"), "")
	if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	schema := read(t, out, "db/schema.sql")
	if !strings.Contains(schema, "CREATE TABLE ledger_outbox (\n  id BIGSERIAL PRIMARY KEY,\n  topic TEXT NOT NULL,\n  payload JSONB NOT NULL,") {
		t.Errorf("schema has no outbox table:\n%s", schema)
	}
	queries := read(t, out, "db/query.sql")
	for _, want := range []string{
		"-- name: InsertLedgerOutbox :exec\nINSERT INTO ledger_outbox (topic, payload) VALUES ($1, $2);",
		"SELECT id, topic, payload FROM ledger_outbox ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED;", // several relays never wait on each other
		"-- name: DeleteLedgerOutbox :exec\nDELETE FROM ledger_outbox WHERE id = ANY($1::bigint[]);",
	} {
		if !strings.Contains(queries, want) {
			t.Errorf("queries missing %q:\n%s", want, queries)
		}
	}
	src := read(t, out, "ledger/outbox_gen.go")
	for _, want := range []string{
		`"example.com/ledger/internal/db"`, `"github.com/jackc/pgx/v5"`,
		"func (o *Outbox) SavePaymentAndPublishPaymentTaken(ctx context.Context, payment *Payment, e PaymentTaken) error {",
		"func (o *Outbox) SavePaymentAndPublishPaymentRefunded(ctx context.Context, payment *Payment, e PaymentRefunded) error {",
		// The store's own Save runs inside the transaction, whoever wrote it.
		"return NewPostgresPaymentStore(q).Save(ctx, payment)",
		"if err := q.InsertLedgerOutbox(ctx, db.InsertLedgerOutboxParams{Topic: topic, Payload: payload}); err != nil {",
		"return tx.Commit(ctx)",
		"func (r *OutboxRelay) Run(ctx context.Context, onError func(error)) error {",
		"func (r *OutboxRelay) Drain(ctx context.Context) (int, error) {",
		"case \"PaymentRefunded\":\n\t\tvar e PaymentRefunded",
		"return r.bus.PublishPaymentTaken(ctx, e)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("outbox_gen.go missing %q", want)
		}
	}
	// Publish, then delete: the order that makes delivery at least once.
	if strings.Index(src, "r.publish(ctx, row.Topic, row.Payload)") > strings.Index(src, "q.DeleteLedgerOutbox(") {
		t.Error("the relay must publish before it deletes")
	}
	if strings.Contains(src, "SaveNote") {
		t.Error("only the entity named in (outbox ...) gets a transactional save")
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "outbox_gen.go", src, 0); err != nil {
		t.Errorf("does not parse: %v", err)
	}
	if tasks := read(t, out, "tilegen.tasks.json"); strings.Contains(tasks, "Outbox") {
		t.Error("which rows share a transaction is not a judgment call: the outbox has no holes")
	}
}

func TestOutboxErrors(t *testing.T) {
	for name, c := range map[string]struct{ store, events, want string }{
		"no such entity": {"get save", "(outbox Invoice)", "Invoice has no store in this package"},
		"cannot save":    {"get list", "(outbox Payment)", "PaymentStore needs save"},
		"twice":          {"get save", "(outbox Payment) (outbox Payment)", "duplicate (outbox Payment)"},
		"malformed":      {"get save", "(outbox)", "names the entity saved together with its events"},
	} {
		sp, _ := writeSpec(t, t.TempDir(), outboxSpec(c.store, c.events), "")
		if err := run(Options{Spec: sp, Out: t.TempDir()}, io.Discard); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want an error containing %q, got: %v", name, c.want, err)
		}
	}
}

func TestNoOutboxNoRequirement(t *testing.T) {
	// Without (outbox ...) nothing changes: the store need is as it was.
	sp, _ := writeSpec(t, t.TempDir(), outboxSpec("get save", ""), "")
	out := t.TempDir()
	if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, out, "tilegen.lock"), "(tile ledger.PaymentStore memory)") {
		t.Error("a store that asks for nothing stays on memory")
	}
}

func inboxSpec(entities, events string) string {
	return `(project ledger (module example.com/ledger) (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0))
  (package ledger
    ` + entities + `
    (events (event PaymentTaken (field ID uuid.UUID) (field Cents int64)) ` + events + `)))`
}

func TestInboxHandlesAnEventOnce(t *testing.T) {
	// The handler's writes and the record that it ran are one transaction,
	// so every store in the package has to be able to join it: (inbox ...)
	// alone moves both stores to Postgres.
	out := t.TempDir()
	two := "(entity Payment (field ID uuid.UUID) (store get save)) (entity Balance (field ID string) (field Cents int64) (store get save))"
	sp, _ := writeSpec(t, t.TempDir(), inboxSpec(two, "(inbox PaymentTaken ID)"), "")
	if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	lock := read(t, out, "tilegen.lock")
	for _, want := range []string{"(tile ledger.PaymentStore postgres-sqlc)", "(tile ledger.BalanceStore postgres-sqlc)"} {
		if !strings.Contains(lock, want) {
			t.Errorf("lock missing %s:\n%s", want, lock)
		}
	}
	if schema := read(t, out, "db/schema.sql"); !strings.Contains(schema, "CREATE TABLE ledger_inbox (\n  consumer TEXT NOT NULL,\n  key TEXT NOT NULL,") || !strings.Contains(schema, "PRIMARY KEY (consumer, key)") {
		t.Errorf("schema has no inbox table:\n%s", schema)
	}
	if q := read(t, out, "db/query.sql"); !strings.Contains(q, "-- name: MarkLedgerInbox :execrows\nINSERT INTO ledger_inbox (consumer, key) VALUES ($1, $2) ON CONFLICT DO NOTHING;") {
		t.Errorf("the mark must be an insert that a repeat cannot make:\n%s", q)
	}
	src := read(t, out, "ledger/inbox_gen.go")
	for _, want := range []string{
		"func (in *Inbox) OncePaymentTaken(consumer string, h func(ctx context.Context, q *db.Queries, e PaymentTaken) error) func(context.Context, PaymentTaken) error {",
		`"PaymentTaken:"+fmt.Sprint(e.ID)`,
		"if first == 0 {\n\t\treturn nil // handled before: a repeat is not an error",
		"func (in *Inbox) Prune(ctx context.Context, olderThan time.Duration) (int64, error) {",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("inbox_gen.go missing %q", want)
		}
	}
	// Mark, then handle, then commit: a failed handler leaves no mark.
	mark, handle, commit := strings.Index(src, "q.MarkLedgerInbox("), strings.Index(src, "handle(q)"), strings.Index(src, "return tx.Commit(ctx)")
	if !(mark < handle && handle < commit) {
		t.Error("the record, the handler and the commit must come in that order, in one transaction")
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "inbox_gen.go", src, 0); err != nil {
		t.Errorf("does not parse: %v", err)
	}
	if tasks := read(t, out, "tilegen.tasks.json"); strings.Contains(tasks, "Inbox") {
		t.Error("the inbox has no holes")
	}
	for name, c := range map[string]struct{ entities, events, want string }{
		"no such event":    {two, "(inbox PaymentTakn ID)", "these events have no PaymentTakn"},
		"no such field":    {two, "(inbox PaymentTaken Id)", "PaymentTaken has no field Id"},
		"twice":            {two, "(inbox PaymentTaken ID) (inbox PaymentTaken Cents)", "an event has one identity"},
		"no field":         {two, "(inbox PaymentTaken)", "(inbox ?event ?field)"},
		"nothing to write": {"", "(inbox PaymentTaken ID)", "needs an entity with a (store ...) for the handler to write"},
	} {
		sp, _ := writeSpec(t, t.TempDir(), inboxSpec(c.entities, c.events), "")
		if err := run(Options{Spec: sp, Out: t.TempDir()}, io.Discard); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want an error containing %q, got: %v", name, c.want, err)
		}
	}
}
