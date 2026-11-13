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
