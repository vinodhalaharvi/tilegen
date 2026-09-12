package main

// The documentation the service serves: three walkthroughs.
// Every spec shown is complete and carries a policy, and every one is
// shown twice - the same spec with one decision changed - beside the real
// output of both. Nothing here is a fragment, and nothing is invented:
// each sample was generated and pasted.

const docsHTML = `
<div class="lede">
  <h1>Describe what your service is made of.<br>Get the project.</h1>
  <p>You write a spec: your types, what is stored, what is served, what must survive
  a restart. tilegen writes the Go — structs, interfaces, SQL schema and queries,
  HTTP routing, event buses, the wiring — and leaves you the decisions that are
  yours, each as a method with a contract and a note.</p>
  <p>The last few lines of every spec below are a <b>policy</b>: a decision that did
  not come from the code. A client's approved list, your team's one database, an
  appliance with no server to run. Change those lines and the architecture changes
  under you, while your types, your operations and your API stay exactly where they
  were. Each walkthrough shows the same spec twice, so you can see what moves.</p>
</div>

<h2 id="one"><span class="n">One</span>A list of tasks</h2>

<p>The smallest thing worth generating: one type, four operations, and a decision
about where it lives.</p>

<div class="steps">

<div class="step"><h4>The spec, in full</h4>
<div class="pair">
  <figure>
    <figcaption>todo.sexp — every line of it</figcaption>
    <pre><code data-lang="sexp">(project todo
  (module example.com/todo)
  (go 1.22))

(package tasks
  (entity Task
    (field ID int64)
    (field Title string)
    (field Done bool)
    (store get list save delete
      (durable))))

(policy
  (prefer sqlite-sqlc
    (strength required)
    (source ops "one binary, one file, no database server")))</code></pre>
  </figure>
  <figure>
    <figcaption>What would it do?</figcaption>
    <pre><code data-lang="sh">tasks.TaskStore
  you asked for   durable
  kept in         sqlite-sqlc
  ruled out       memory         — an in-memory map loses its data
                                   on restart
  ruled out       postgres-sqlc  — the policy requires sqlite-sqlc
                                   (source: ops, one binary, one file,
                                   no database server)
  ruled out       postgres-pgx   — the policy requires sqlite-sqlc
                                   (source: ops, one binary, one file,
                                   no database server)</code></pre>
    <figcaption style="margin-top:10px">what you get</figcaption>
    <pre><code data-lang="sh">go.mod
tasks/tasks_gen.go
tasks/sqlite_task_store.go
db/schema.sql
db/query.sql
sqlc.yaml
tilegen.lock
tilegen.tasks.json</code></pre>
  </figure>
</div>
<button class="try">Open this in the editor</button>
</div>

<div class="step"><h4>What it wrote</h4>
<div class="pair">
  <figure>
    <figcaption>tasks/tasks_gen.go — regenerated every run</figcaption>
    <pre><code data-lang="go">type Task struct {
	ID    int64  ` + "`" + `json:"id"` + "`" + `
	Title string ` + "`" + `json:"title"` + "`" + `
	Done  bool   ` + "`" + `json:"done"` + "`" + `
}

// TaskStore persists Task values.
type TaskStore interface {
	// Get returns the Task with the given ID.
	Get(ctx context.Context, id int64) (*Task, error)
	// List returns all Task values ordered by ID.
	List(ctx context.Context) ([]*Task, error)
	// Save inserts or replaces task by ID.
	Save(ctx context.Context, task *Task) error
	// Delete removes the Task with the given ID.
	Delete(ctx context.Context, id int64) error
}

var _ TaskStore = (*SqliteTaskStore)(nil)
var ErrNotFound = errors.New("tasks: not found")</code></pre>
  </figure>
  <figure>
    <figcaption>db/schema.sql and db/query.sql</figcaption>
    <pre><code data-lang="sql">CREATE TABLE tasks (
  id INTEGER PRIMARY KEY,
  title TEXT NOT NULL,
  done BOOLEAN NOT NULL
);

-- name: SaveTask :exec
INSERT INTO tasks (id, title, done)
VALUES (?1, ?2, ?3)
ON CONFLICT (id) DO UPDATE SET
  title = EXCLUDED.title, done = EXCLUDED.done;</code></pre>
    <figcaption style="margin-top:10px">tasks/sqlite_task_store.go — <b>yours</b></figcaption>
    <pre><code data-lang="go">type SqliteTaskStore struct {
	q *db.Queries
}

func (s *SqliteTaskStore) Get(ctx context.Context, id int64) (*Task, error) {
	panic("tilegen:hole tasks.SqliteTaskStore.Get")
}</code></pre>
  </figure>
</div>
<p>Four methods like that one, each listed in <code>tilegen.tasks.json</code> with its
signature and what it should do. The project compiles as it stands.</p>
</div>

<div class="step"><h4>The same spec, one policy different</h4>
<p>Ops changed their mind: this runs on the cluster now, next to the postgres
everything else already uses.</p>
<div class="pair">
  <figure>
    <figcaption>todo.sexp — every line of it, again</figcaption>
    <pre><code data-lang="sexp">(project todo
  (module example.com/todo)
  (go 1.22))

(package tasks
  (entity Task
    (field ID int64)
    (field Title string)
    (field Done bool)
    (store get list save delete
      (durable))))

(policy
  (prefer postgres-sqlc
    (strength required)
    (source team "we run one postgres for everything")))</code></pre>
  </figure>
  <figure>
    <figcaption>What would it do?</figcaption>
    <pre><code data-lang="sh">tasks.TaskStore
  you asked for   durable
  kept in         postgres-sqlc
  ruled out       memory         — an in-memory map loses its data
                                   on restart
  ruled out       sqlite-sqlc    — the policy requires postgres-sqlc
                                   (source: team, we run one postgres
                                   for everything)
  ruled out       postgres-pgx   — the policy requires postgres-sqlc
                                   (source: team, we run one postgres
                                   for everything)</code></pre>
    <figcaption style="margin-top:10px">db/schema.sql and db/query.sql</figcaption>
    <pre><code data-lang="sql">CREATE TABLE tasks (
  id BIGINT PRIMARY KEY,
  title TEXT NOT NULL,
  done BOOLEAN NOT NULL
);

-- name: SaveTask :exec
INSERT INTO tasks (id, title, done)
VALUES ($1, $2, $3)
ON CONFLICT (id) DO UPDATE SET
  title = EXCLUDED.title, done = EXCLUDED.done;</code></pre>
  </figure>
</div>
<button class="try">Open this in the editor</button>
<p><code>INTEGER</code> became <code>BIGINT</code>, <code>?1</code> became
<code>$1</code>, <code>sqlite_task_store.go</code> became
<code>postgres_task_store.go</code>, and <code>sqlc.yaml</code> now names a different
engine and driver. Your entity, your four operations and <code>TaskStore</code> are
character for character identical.</p>
</div>

</div>

<h2 id="two"><span class="n">Two</span>An orders API</h2>

<p>A real service: an enum the database enforces, a store with queries, a method
only you can write, a JSON API, and events. One spec, and then the same spec with
one policy line changed.</p>

<div class="steps">

<div class="step"><h4>The spec, in full</h4>
<div class="pair">
  <figure>
    <figcaption>orders.sexp — every line of it</figcaption>
    <pre><code data-lang="sexp">(project orders
  (module example.com/orders)
  (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0)))

(package orders
  (enum Status pending paid shipped cancelled)

  (entity Order
    (field ID uuid.UUID)
    (field CustomerEmail string)
    (field Status Status)
    (field TotalCents int64)
    (field PlacedAt time.Time)
    (store get list save delete count
      (list-by Status)
      (get-by CustomerEmail)
      (durable)
      (method Refund
        (doc "Refund reverses a paid order and records why.")
        (params (id uuid.UUID) (reason string))
        (returns error))
      (constraint "Never write CustomerEmail to logs.")))

  (http
    (route GET    "/orders"      (list Order))
    (route GET    "/orders/{id}" (get Order))
    (route POST   "/orders"      (save Order))
    (route DELETE "/orders/{id}" (delete Order)))

  (events
    (event OrderPaid (field OrderID uuid.UUID) (field TotalCents int64))
    (event OrderShipped (field OrderID uuid.UUID))))

(config
  (json-tags snake)
  (context-first yes))

(policy
  (prefer postgres-sqlc
    (strength required)
    (source team "one postgres, and we want generated queries")))</code></pre>
    <button class="try">Open this in the editor</button>
  </figure>
  <figure>
    <figcaption>What would it do?</figcaption>
    <pre><code data-lang="sh">orders.OrderStore
  you asked for   durable
  kept in         postgres-sqlc
  ruled out       memory        — an in-memory map loses its data
                                  on restart
  ruled out       sqlite-sqlc   — the policy requires postgres-sqlc
                                  (source: team, one postgres, and we
                                  want generated queries)
  ruled out       postgres-pgx  — the policy requires postgres-sqlc
                                  (source: team, one postgres, and we
                                  want generated queries)

orders.Bus
  kept in         local-bus</code></pre>
    <figcaption style="margin-top:10px">what you get — 10 methods are yours</figcaption>
    <pre><code data-lang="sh">go.mod
orders/orders_gen.go          types, enum, interfaces
orders/postgres_order_store.go   yours
orders/http_gen.go            routing and handlers
orders/http.go                yours
orders/events_gen.go          events and the bus, finished
db/schema.sql
db/query.sql
sqlc.yaml
tilegen.lock
tilegen.tasks.json</code></pre>
  </figure>
</div>
</div>

<div class="step"><h4>The enum becomes a rule the database keeps</h4>
<div class="pair">
  <figure>
    <figcaption>orders/orders_gen.go</figcaption>
    <pre><code data-lang="go">type Status string

const (
	StatusPending   Status = "pending"
	StatusPaid      Status = "paid"
	StatusShipped   Status = "shipped"
	StatusCancelled Status = "cancelled"
)

// StatusValues lists every Status, in declaration order.
var StatusValues = []Status{StatusPending, StatusPaid,
	StatusShipped, StatusCancelled}

func (s Status) Valid() bool { ... }
func ParseStatus(s string) (Status, error) { ... }</code></pre>
  </figure>
  <figure>
    <figcaption>db/schema.sql and db/query.sql</figcaption>
    <pre><code data-lang="sql">CREATE TABLE orders (
  id UUID PRIMARY KEY,
  customer_email TEXT NOT NULL,
  status TEXT CHECK (status IN ('pending', 'paid',
    'shipped', 'cancelled')) NOT NULL,
  total_cents BIGINT NOT NULL,
  placed_at TIMESTAMPTZ NOT NULL
);

-- name: ListOrdersByStatus :many
SELECT * FROM orders WHERE status = $1 ORDER BY id;

-- name: GetOrderByCustomerEmail :one
SELECT * FROM orders WHERE customer_email = $1
ORDER BY id LIMIT 1;

-- name: CountOrders :one
SELECT count(*) FROM orders;</code></pre>
  </figure>
</div>
<p>You wrote <code>(list-by Status)</code> and <code>(get-by CustomerEmail)</code>;
those are the queries. You never write the Go that runs them either —
<code>sqlc generate</code> does that from these two files.</p>
</div>

<div class="step"><h4>The API, and the two decisions it leaves you</h4>
<div class="pair">
  <figure>
    <figcaption>orders/http_gen.go</figcaption>
    <pre><code data-lang="go">func (h *Handler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /orders/{id}", h.DeleteOrder)
	mux.HandleFunc("GET /orders/{id}", h.GetOrder)
	mux.HandleFunc("GET /orders", h.ListOrders)
	mux.HandleFunc("POST /orders", h.SaveOrder)
	return mux
}

func (h *Handler) SaveOrder(w http.ResponseWriter, r *http.Request) {
	var v Order
	if err := readJSON(r, &v); err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	if err := h.validateOrder(&v); err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	if err := h.orders.Save(r.Context(), &v); err != nil {
		httpError(w, h.statusForOrder(err), err)
		return
	}
	writeJSON(w, http.StatusOK, &v)
}</code></pre>
  </figure>
  <figure>
    <figcaption>orders/http.go — <b>yours</b></figcaption>
    <pre><code data-lang="go">func (h *Handler) validateOrder(order *Order) error {
	panic("tilegen:hole orders.Handler.validateOrder")
}

func (h *Handler) statusForOrder(err error) int {
	panic("tilegen:hole orders.Handler.statusForOrder")
}</code></pre>
    <figcaption style="margin-top:10px">tilegen.tasks.json</figcaption>
    <pre><code data-lang="json">{
  "id": "orders.OrderStore.Refund",
  "contract": "Refund(ctx context.Context, id uuid.UUID,
                reason string) error",
  "intent": "Refund reverses a paid order and records why.",
  "constraints": ["Never write CustomerEmail to logs."]
}</code></pre>
    <p class="dim" style="margin-top:10px">Two decisions per entity, not two per
    route. Unknown paths answer 404 and wrong methods 405 without anyone writing
    that. <code>Refund</code> is yours because only you know what refunding means —
    and your constraint travels with it.</p>
  </figure>
</div>
</div>

<div class="step"><h4>The same spec, one policy different</h4>
<p>A new rule arrives: no build-time code generators in CI. Nothing about the orders
domain changed, so nothing in the spec above it changes either.</p>
<div class="pair">
  <figure>
    <figcaption>orders.sexp — every line of it, again</figcaption>
    <pre><code data-lang="sexp">(project orders
  (module example.com/orders)
  (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0)))

(package orders
  (enum Status pending paid shipped cancelled)

  (entity Order
    (field ID uuid.UUID)
    (field CustomerEmail string)
    (field Status Status)
    (field TotalCents int64)
    (field PlacedAt time.Time)
    (store get list save delete count
      (list-by Status)
      (get-by CustomerEmail)
      (durable)
      (method Refund
        (doc "Refund reverses a paid order and records why.")
        (params (id uuid.UUID) (reason string))
        (returns error))
      (constraint "Never write CustomerEmail to logs.")))

  (http
    (route GET    "/orders"      (list Order))
    (route GET    "/orders/{id}" (get Order))
    (route POST   "/orders"      (save Order))
    (route DELETE "/orders/{id}" (delete Order)))

  (events
    (event OrderPaid (field OrderID uuid.UUID) (field TotalCents int64))
    (event OrderShipped (field OrderID uuid.UUID))))

(config
  (json-tags snake)
  (context-first yes))

(policy
  (prefer postgres-pgx
    (strength required)
    (source team "no build-time code generators in CI")))</code></pre>
    <button class="try">Open this in the editor</button>
  </figure>
  <figure>
    <figcaption>What would it do?</figcaption>
    <pre><code data-lang="sh">orders.OrderStore
  you asked for   durable
  kept in         postgres-pgx
  ruled out       memory         — an in-memory map loses its data
                                   on restart
  ruled out       postgres-sqlc  — the policy requires postgres-pgx
                                   (source: team, no build-time code
                                   generators in CI)
  ruled out       sqlite-sqlc    — the policy requires postgres-pgx
                                   (source: team, no build-time code
                                   generators in CI)</code></pre>
    <figcaption style="margin-top:10px">what you get now</figcaption>
    <pre><code data-lang="sh">go.mod
orders/orders_gen.go
orders/pgx_order_store.go     yours
orders/http_gen.go
orders/http.go                yours
orders/events_gen.go
db/schema.sql
tilegen.lock                  no db/query.sql
tilegen.tasks.json            no sqlc.yaml</code></pre>
    <figcaption style="margin-top:10px">orders/pgx_order_store.go — <b>yours</b></figcaption>
    <pre><code data-lang="go">type PgxOrderStore struct {
	pool *pgxpool.Pool
}

func (s *PgxOrderStore) Get(ctx context.Context, id uuid.UUID) (*Order, error) {
	panic("tilegen:hole orders.PgxOrderStore.Get")
}</code></pre>
    <figcaption style="margin-top:10px">tilegen.tasks.json — the same method, a bigger job</figcaption>
    <pre><code data-lang="json">{
  "id": "orders.PgxOrderStore.Get",
  "intent": "Write the SQL and run it with s.pool (pgx v5),
             against the table in db/schema.sql. A query that
             fits: SELECT * FROM orders WHERE id = $1;
             Map pgx.ErrNoRows to ErrNotFound."
}</code></pre>
  </figure>
</div>
<p>The generated queries and the sqlc config are gone, because this way there is no
generator. The schema stays, and the note on each method now carries the query it
would have generated, so whoever writes the body is not starting from nothing. Your
types, your enum, your routes and your handlers did not move.</p>
</div>

</div>

<h2 id="three"><span class="n">Three</span>A support desk, shipped and then hosted</h2>

<p>Two packages, an interface you implement, events, and a store that deliberately
is not durable. Then the business model changes, and two lines of policy turn a
shipped appliance into a hosted service.</p>

<div class="steps">

<div class="step"><h4>The spec, in full — the shipped version</h4>
<div class="pair">
  <figure>
    <figcaption>desk.sexp — every line of it</figcaption>
    <pre><code data-lang="sexp">(project desk
  (module example.com/desk)
  (go 1.22)
  (require
    (uuid github.com/google/uuid v1.6.0)
    (nats github.com/nats-io/nats.go v1.37.0)))

(package tickets
  (enum State open pending solved closed)

  (entity Ticket
    (field ID uuid.UUID)
    (field Subject string)
    (field Requester string)
    (field State State)
    (field OpenedAt time.Time)
    (field ClosedAt *time.Time)
    (store get list save
      (list-by State)
      (count-by State)
      (durable)
      (constraint "A closed ticket must not change state again.")))

  (http
    (route GET  "/tickets"      (list Ticket))
    (route GET  "/tickets/{id}" (get Ticket))
    (route POST "/tickets"      (save Ticket)))

  (events
    (event TicketOpened (field TicketID uuid.UUID))
    (event TicketSolved (field TicketID uuid.UUID))))

(package agents
  (entity Agent
    (field ID uuid.UUID)
    (field Email string)
    (store get list save (get-by Email)))

  (interface Assigner
    (doc "Assigner chooses who should take a ticket.")
    (method Assign
      (params (ticket *tickets.Ticket))
      (returns *Agent error)))

  (implement Assigner (as RoundRobinAssigner)
    (field agents AgentStore)
    (constraint "Never assign to an agent with no email.")))

(policy
  (prefer sqlite-sqlc
    (strength required)
    (source ops "the desk ships as one binary to each customer")))</code></pre>
    <button class="try">Open this in the editor</button>
  </figure>
  <figure>
    <figcaption>What would it do?</figcaption>
    <pre><code data-lang="sh">tickets.TicketStore
  you asked for   durable
  kept in         sqlite-sqlc
  ruled out       memory         — an in-memory map loses its data
                                   on restart
  ruled out       postgres-sqlc  — the policy requires sqlite-sqlc
                                   (source: ops, the desk ships as
                                   one binary to each customer)

tickets.Bus
  kept in         local-bus

agents.AgentStore
  kept in         sqlite-sqlc</code></pre>
    <figcaption style="margin-top:10px">what you get — 12 methods are yours</figcaption>
    <pre><code data-lang="sh">tickets/tickets_gen.go
tickets/sqlite_ticket_store.go     yours
tickets/http_gen.go
tickets/http.go                    yours
tickets/events_gen.go              finished
agents/agents_gen.go
agents/sqlite_agent_store.go       yours
agents/round_robin_assigner.go     yours
db/schema.sql, db/query.sql, sqlc.yaml</code></pre>
  </figure>
</div>
</div>

<div class="step"><h4>Events arrive finished; your abstraction arrives scaffolded</h4>
<div class="pair">
  <figure>
    <figcaption>tickets/events_gen.go — nothing left to write</figcaption>
    <pre><code data-lang="go">type Bus interface {
	// PublishTicketOpened delivers e to every TicketOpened handler.
	PublishTicketOpened(ctx context.Context, e TicketOpened) error
	// OnTicketOpened subscribes h and returns a func that
	// unsubscribes it.
	OnTicketOpened(h func(context.Context, TicketOpened) error) func()

	PublishTicketSolved(ctx context.Context, e TicketSolved) error
	OnTicketSolved(h func(context.Context, TicketSolved) error) func()
}

// LocalBus is an in-process Bus. Publish calls every handler in
// subscription order and returns their errors joined.
type LocalBus struct { ... }

var _ Bus = (*LocalBus)(nil)</code></pre>
  </figure>
  <figure>
    <figcaption>agents/round_robin_assigner.go — <b>yours</b></figcaption>
    <pre><code data-lang="go">type RoundRobinAssigner struct {
	agents AgentStore
}

func NewRoundRobinAssigner(agents AgentStore) *RoundRobinAssigner {
	return &RoundRobinAssigner{agents: agents}
}

// Assign chooses who should take a ticket.
//
// Never assign to an agent with no email.
func (s *RoundRobinAssigner) Assign(ctx context.Context,
	ticket *tickets.Ticket) (*Agent, error) {
	panic("tilegen:hole agents.RoundRobinAssigner.Assign")
}

var _ Assigner = (*RoundRobinAssigner)(nil)</code></pre>
    <p class="dim" style="margin-top:10px">The constraint you wrote sits on the
    method, where whoever writes it will read it. The struct, the constructor and
    the interface check are done.</p>
  </figure>
</div>
</div>

<div class="step"><h4>The same spec, hosted instead of shipped</h4>
<p>You stop shipping binaries and start hosting. One postgres, many tenants — and
now the reporting service needs to hear about solved tickets, so the events must
leave the process. Two lines change.</p>
<div class="pair">
  <figure>
    <figcaption>desk.sexp — every line of it, again</figcaption>
    <pre><code data-lang="sexp">(project desk
  (module example.com/desk)
  (go 1.22)
  (require
    (uuid github.com/google/uuid v1.6.0)
    (nats github.com/nats-io/nats.go v1.37.0)))

(package tickets
  (enum State open pending solved closed)

  (entity Ticket
    (field ID uuid.UUID)
    (field Subject string)
    (field Requester string)
    (field State State)
    (field OpenedAt time.Time)
    (field ClosedAt *time.Time)
    (store get list save
      (list-by State)
      (count-by State)
      (durable)
      (constraint "A closed ticket must not change state again.")))

  (http
    (route GET  "/tickets"      (list Ticket))
    (route GET  "/tickets/{id}" (get Ticket))
    (route POST "/tickets"      (save Ticket)))

  (events
    (cross-process)
    (event TicketOpened (field TicketID uuid.UUID))
    (event TicketSolved (field TicketID uuid.UUID))))

(package agents
  (entity Agent
    (field ID uuid.UUID)
    (field Email string)
    (store get list save (get-by Email)))

  (interface Assigner
    (doc "Assigner chooses who should take a ticket.")
    (method Assign
      (params (ticket *tickets.Ticket))
      (returns *Agent error)))

  (implement Assigner (as RoundRobinAssigner)
    (field agents AgentStore)
    (constraint "Never assign to an agent with no email.")))

(policy
  (prefer postgres-sqlc
    (strength required)
    (source ops "we host it now: one postgres, many tenants")))</code></pre>
    <button class="try">Open this in the editor</button>
  </figure>
  <figure>
    <figcaption>What would it do?</figcaption>
    <pre><code data-lang="sh">tickets.Bus
  you asked for   cross-process
  kept in         nats-bus
  ruled out       local-bus  — an in-process bus only reaches
                               handlers in this program

tickets.TicketStore
  you asked for   durable
  kept in         postgres-sqlc
  ruled out       sqlite-sqlc  — the policy requires postgres-sqlc
                                 (source: ops, we host it now: one
                                 postgres, many tenants)</code></pre>
    <figcaption style="margin-top:10px">tickets/nats_bus.go — <b>yours now</b></figcaption>
    <pre><code data-lang="go">type NatsBus struct {
	conn    *nats.Conn
	subject string
}

func NewNatsBus(conn *nats.Conn, subject string) *NatsBus {
	return &NatsBus{conn: conn, subject: subject}
}

func (h *NatsBus) PublishTicketOpened(ctx context.Context,
	e TicketOpened) error {
	panic("tilegen:hole tickets.NatsBus.PublishTicketOpened")
}</code></pre>
    <figcaption style="margin-top:10px">tilegen.tasks.json</figcaption>
    <pre><code data-lang="json">{
  "id": "tickets.NatsBus.PublishTicketOpened",
  "intent": "Encode e as JSON and publish it on
             s.subject+\".TicketOpened\" with s.conn.
             Return the publish error."
}</code></pre>
  </figure>
</div>
<p>Both stores moved to postgres, <code>UUID</code> and <code>TIMESTAMPTZ</code>
columns came back, and the in-process bus became a NATS one. The
<code>Bus</code> interface and the event types are unchanged: only the way they
travel is different, so only the implementation moved. It went from 12 methods
yours to 16, and the four new ones are the publish and subscribe pairs, each with
its subject and encoding spelled out.</p>
<div class="aside">Regenerating does not delete the sqlite store you had already
filled in. tilegen reports it as an orphan and leaves it alone, so you can port the
code across or keep it as a test fake.</div>
</div>

</div>

<h2 id="reference">Everything a spec can say</h2>

<p>Every form above appears in one of the three specs. This is the whole vocabulary.</p>

<h3>Top level</h3>
<table>
<tr><td><code>(project name ...)</code></td><td>module, Go version, dependencies. One per spec.</td></tr>
<tr><td><code>(package name ...)</code></td><td>a Go package. As many as you like; they may refer to each other.</td></tr>
<tr><td><code>(config ...)</code></td><td>house style: <code>(json-tags snake|camel|none)</code>, <code>(context-first yes|no)</code>, <code>(layout flat|internal)</code>.</td></tr>
<tr><td><code>(policy ...)</code></td><td>decisions that did not come from the code.</td></tr>
</table>

<h3>Inside a package</h3>
<table>
<tr><td><code>(entity T ...)</code></td><td>a type that is stored: fields, and a store.</td></tr>
<tr><td><code>(struct T ...)</code></td><td>a type with no store: a value object, a request body.</td></tr>
<tr><td><code>(enum T a b c)</code></td><td>a string type, constants, <code>Valid()</code>, <code>ParseT()</code>, and a database constraint.</td></tr>
<tr><td><code>(interface T ...)</code></td><td>an interface; <code>(embed io.Writer)</code> borrows another's methods.</td></tr>
<tr><td><code>(implement T (as N) ...)</code></td><td>a struct that satisfies it, with its dependencies as fields.</td></tr>
<tr><td><code>(http (route ...) ...)</code></td><td>a JSON API over this package's stores.</td></tr>
<tr><td><code>(events (event ...) ...)</code></td><td>typed publish and subscribe; add <code>(cross-process)</code> to leave the program.</td></tr>
<tr><td><code>(llm "...")</code></td><td>work you can describe but not type out.</td></tr>
</table>

<h3>Inside a store</h3>
<table>
<tr><th>You write</th><th>You get</th></tr>
<tr><td><code>get</code></td><td><code>Get(ctx, id) (*T, error)</code></td></tr>
<tr><td><code>list</code></td><td><code>List(ctx) ([]*T, error)</code></td></tr>
<tr><td><code>save</code></td><td><code>Save(ctx, *T) error</code> — insert or update</td></tr>
<tr><td><code>delete</code></td><td><code>Delete(ctx, id) error</code></td></tr>
<tr><td><code>count</code></td><td><code>Count(ctx) (int64, error)</code></td></tr>
<tr><td><code>(list-by F)</code></td><td><code>ListByF(ctx, v) ([]*T, error)</code></td></tr>
<tr><td><code>(get-by F)</code></td><td><code>GetByF(ctx, v) (*T, error)</code></td></tr>
<tr><td><code>(count-by F)</code></td><td><code>CountByF(ctx, v) (int64, error)</code></td></tr>
<tr><td><code>(exists-by F)</code></td><td><code>ExistsByF(ctx, v) (bool, error)</code></td></tr>
<tr><td><code>(delete-by F)</code></td><td><code>DeleteByF(ctx, v) error</code></td></tr>
<tr><td><code>(durable)</code></td><td>must survive a restart, so a real database</td></tr>
<tr><td><code>(method N ...)</code></td><td>anything else; always left for you, with your doc attached</td></tr>
<tr><td><code>(constraint "...")</code></td><td>a rule, carried to every method of the store</td></tr>
</table>

<h3>Policy</h3>
<table>
<tr><td><code>(prefer T (strength required))</code></td><td>nothing else may be used</td></tr>
<tr><td><code>(prefer T (strength strong))</code></td><td>use it unless it cannot do the job</td></tr>
<tr><td><code>(prefer T (strength weak))</code></td><td>use it when the choice is close</td></tr>
<tr><td><code>(source who "why")</code></td><td>who decided and why — shown wherever it applies</td></tr>
<tr><td><code>(avoid T "why")</code></td><td>never use it, and say so</td></tr>
</table>

<p>What can be preferred or avoided: <code>memory</code>, <code>sqlite-sqlc</code>,
<code>postgres-sqlc</code>, <code>postgres-pgx</code> for stores;
<code>local-bus</code> and <code>nats-bus</code> for events.</p>

<h2 id="next">What to do with the zip</h2>

<div class="single">
<pre><code data-lang="sh">unzip project.zip &amp;&amp; cd project
sqlc generate     # only if the project has a db/ folder
go mod tidy
go build ./...</code></pre>
</div>

<p>Files ending <code>_gen.go</code> are rewritten every time you generate; do not
edit them. Everything else is yours and is never overwritten. The methods waiting
for you are listed with their signatures and notes in
<code>tilegen.tasks.json</code>.</p>

<p>For a project you will come back to, install the command line tool. Edit the spec,
run it again, and it updates in place: new methods appear, methods you have written
are untouched, anything the spec no longer describes is reported rather than
deleted.</p>

<div class="single">
<pre><code data-lang="sh">go install github.com/vinodhalaharvi/tilegen@latest

tilegen spec/              # generate, or update after a change
tilegen explain spec/      # how each store will be kept, and why
tilegen check spec/        # fail if out of date or methods missing — for CI
tilegen prompt spec/       # a ready-to-paste brief for one method
tilegen import ./existing  # lift a project you already have into a spec</code></pre>
</div>

<h2 id="errors">When a spec is wrong</h2>

<p>Mistakes are caught before anything is generated, with the line and usually the
word you meant:</p>

<div class="single">
<pre><code data-lang="sh">spec.sexp:7:5: entity Order needs an ID field
spec.sexp:12:3: unknown form (entiy Customer ...) (did you mean entity?)
spec.sexp:19:7: (get Order) needs {id} in the path, since it acts on one Order
spec.sexp:24:3: duplicate type "Order"
spec.sexp:31:9: packages orders and billing import each other</code></pre>
</div>

<div class="aside">This service generates files and returns them. It runs nothing: no
build, no database, no calls out. What you download is yours to read before you run
it.</div>
`
