package main

// The documentation the service serves. Three walkthroughs, each carried
// all the way through: the spec, what it decides, the code it writes, then
// one change to the spec and what that changes. Every sample is real
// output, generated and pasted.

const docsHTML = `
<div class="lede">
  <h1>Describe what your service is made of.<br>Get the project.</h1>
  <p>You write a short spec: your types, what is stored, what is served, what must
  survive a restart. tilegen writes the Go — the structs, the interfaces, the SQL
  schema and queries, the HTTP routing, the wiring — and leaves you the decisions
  that are yours to make, each as a method with a contract and a note.</p>
  <p class="dim">Three walkthroughs follow. Each one ends by changing the spec, so
  you can see what moves.</p>
</div>

<h2 id="one"><span class="n">One</span>A list of tasks</h2>

<p>The smallest spec that produces something.</p>

<div class="steps">

<div class="step"><h4>Write the spec</h4>
<div class="pair">
  <figure>
    <figcaption>todo.sexp</figcaption>
    <pre><code data-lang="sexp">(project todo
  (module example.com/todo)
  (go 1.22))

(package tasks
  (entity Task
    (field ID int64)
    (field Title string)
    (field Done bool)
    (store get list save delete)))</code></pre>
  </figure>
  <figure>
    <figcaption>what you get</figcaption>
    <pre><code data-lang="sh">go.mod
tasks/tasks_gen.go
tasks/memory_task_store.go
tilegen.lock
tilegen.tasks.json</code></pre>
    <p class="dim" style="margin-top:10px">No database: nothing here
    has to survive a restart yet, so the store is a map behind a mutex.</p>
  </figure>
</div>
<button class="try">Open this in the editor</button>
</div>

<div class="step"><h4>The types and the interface</h4>
<div class="single">
  <figcaption>tasks/tasks_gen.go — regenerated every run, do not edit</figcaption>
  <pre><code data-lang="go">type Task struct {
	ID    int64  ` + "`json:\"id\"`" + `
	Title string ` + "`json:\"title\"`" + `
	Done  bool   ` + "`json:\"done\"`" + `
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

// Compile-time check that MemoryTaskStore satisfies TaskStore.
var _ TaskStore = (*MemoryTaskStore)(nil)

// ErrNotFound is returned when a requested value does not exist.
var ErrNotFound = errors.New("tasks: not found")</code></pre>
</div>
</div>

<div class="step"><h4>The part that is yours</h4>
<div class="single">
  <figcaption>tasks/memory_task_store.go — <b>yours: tilegen never overwrites this file</b></figcaption>
  <pre><code data-lang="go">// MemoryTaskStore implements TaskStore using memory storage.
type MemoryTaskStore struct {
	mu sync.Mutex
	m  map[int64]*Task
}

func NewMemoryTaskStore() *MemoryTaskStore {
	return &MemoryTaskStore{m: make(map[int64]*Task)}
}

func (s *MemoryTaskStore) Get(ctx context.Context, id int64) (*Task, error) {
	panic("tilegen:hole tasks.MemoryTaskStore.Get")
}</code></pre>
</div>
<p>Four methods like that one. The project compiles as it stands, so you can fill
them in any order — by hand, or by handing the file to a coding assistant.</p>
</div>

<div class="step"><h4>Change one thing: this must survive a restart</h4>
<p>Add <code>(durable)</code> to the store.</p>
<div class="pair">
  <figure>
    <figcaption>the only edit</figcaption>
    <pre><code data-lang="sexp">    (store get list save delete
      (durable))</code></pre>
  </figure>
  <figure>
    <figcaption>what you get now</figcaption>
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
<div class="pair">
  <figure>
    <figcaption>db/schema.sql</figcaption>
    <pre><code data-lang="sql">CREATE TABLE tasks (
  id INTEGER PRIMARY KEY,
  title TEXT NOT NULL,
  done BOOLEAN NOT NULL
);</code></pre>
  </figure>
  <figure>
    <figcaption>tilegen.tasks.json — the same method, a different job</figcaption>
    <pre><code data-lang="json">{
  "id": "tasks.SqliteTaskStore.Get",
  "contract": "Get(ctx context.Context, id int64) (*Task, error)",
  "intent": "Call the sqlc-generated s.q.GetTask and convert the
             db.Task row to *Task. Map sql.ErrNoRows to ErrNotFound."
}</code></pre>
  </figure>
</div>
<p>One word changed the storage, the schema, the queries and what each unwritten
method is being asked to do.</p>
</div>

<div class="step"><h4>Change one more thing: the team already runs postgres</h4>
<p>Left alone, tilegen kept this in sqlite: one file, nothing to run. But your team
has a postgres everything else already uses, and that is not a fact about this
service — it is a fact about your team. It goes in a policy, with who decided it.</p>
<div class="single">
  <figcaption>added to the end of the spec</figcaption>
  <pre><code data-lang="sexp">(policy
  (prefer postgres-sqlc
    (strength required)
    (source team "we run one postgres for everything")))</code></pre>
</div>
<div class="pair">
  <figure>
    <figcaption>before: db/schema.sql and db/query.sql</figcaption>
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
  </figure>
  <figure>
    <figcaption>after</figcaption>
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
<div class="single">
  <figcaption>What would it do?</figcaption>
  <pre><code data-lang="sh">tasks.TaskStore
  you asked for   durable
  kept in         postgres-sqlc
  ruled out       memory         — an in-memory map loses its data on restart
  ruled out       sqlite-sqlc    — the policy requires postgres-sqlc
                                   (source: team, we run one postgres for everything)
  ruled out       postgres-pgx   — the policy requires postgres-sqlc
                                   (source: team, we run one postgres for everything)</code></pre>
</div>
<p><code>sqlite_task_store.go</code> becomes <code>postgres_task_store.go</code>, the
column type and the placeholders change, and the reason is recorded where anyone
will find it. Your entity, your store operations and the interface did not move.</p>
</div>

</div>

<h2 id="two"><span class="n">Two</span>An orders API, and a client with an opinion</h2>

<p>The same shape as a real service: an enum, a durable store, a JSON API. Then a
policy arrives from outside the code.</p>

<div class="steps">

<div class="step"><h4>Write the spec</h4>
<div class="single">
  <figcaption>orders.sexp</figcaption>
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
    (store get list save delete
      (list-by Status)
      (durable)
      (constraint "Never write CustomerEmail to logs.")))

  (http
    (route GET    "/orders"      (list Order))
    (route GET    "/orders/{id}" (get Order))
    (route POST   "/orders"      (save Order))
    (route DELETE "/orders/{id}" (delete Order))))</code></pre>
</div>
<button class="try">Open this in the editor</button>
</div>

<div class="step"><h4>See what it decides, before generating anything</h4>
<div class="single">
  <figcaption>What would it do?</figcaption>
  <pre><code data-lang="sh">orders.OrderStore
  you asked for   durable
  kept in         postgres-sqlc
  also possible   sqlite-sqlc
  also possible   postgres-pgx
  ruled out       memory  — an in-memory map loses its data on restart</code></pre>
</div>
<p>Nothing is written yet. Storing a <code>uuid.UUID</code> and a
<code>time.Time</code> is something postgres does with its own column types, so
that is what it reaches for.</p>
</div>

<div class="step"><h4>The schema and the queries</h4>
<div class="pair">
  <figure>
    <figcaption>db/schema.sql</figcaption>
    <pre><code data-lang="sql">CREATE TABLE orders (
  id UUID PRIMARY KEY,
  customer_email TEXT NOT NULL,
  status TEXT CHECK (status IN ('pending', 'paid', 'shipped', 'cancelled')) NOT NULL,
  total_cents BIGINT NOT NULL,
  placed_at TIMESTAMPTZ NOT NULL
);</code></pre>
  </figure>
  <figure>
    <figcaption>db/query.sql</figcaption>
    <pre><code data-lang="sql">-- name: GetOrder :one
SELECT * FROM orders WHERE id = $1;

-- name: ListOrdersByStatus :many
SELECT * FROM orders WHERE status = $1 ORDER BY id;

-- name: SaveOrder :exec
INSERT INTO orders (id, customer_email, status, total_cents, placed_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO UPDATE SET
  customer_email = EXCLUDED.customer_email,
  status = EXCLUDED.status;</code></pre>
  </figure>
</div>
<p>The enum became a constraint the database enforces. You never write the SQL, and
you never write the Go that runs it either — <code>sqlc generate</code> does that
from these two files.</p>
</div>

<div class="step"><h4>The API</h4>
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

func (h *Handler) GetOrder(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	v, err := h.orders.Get(r.Context(), id)
	if err != nil {
		httpError(w, h.statusForOrder(err), err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}</code></pre>
  </figure>
  <figure>
    <figcaption>orders/http.go — <b>yours</b></figcaption>
    <pre><code data-lang="go">// validateOrder reports what makes a request
// unacceptable, before it reaches the store.
func (h *Handler) validateOrder(order *Order) error {
	panic("tilegen:hole orders.Handler.validateOrder")
}

// statusForOrder maps a store error to an HTTP
// status: not found to 404, a conflict to 409,
// anything else to 500.
func (h *Handler) statusForOrder(err error) int {
	panic("tilegen:hole orders.Handler.statusForOrder")
}</code></pre>
    <p class="dim" style="margin-top:10px">Two decisions per entity,
    not two per route. Unknown paths answer 404 and wrong methods answer 405 without
    anyone writing that.</p>
  </figure>
</div>
</div>

<div class="step"><h4>Change one thing: it ships as one binary</h4>
<p>The client runs this on an appliance. No database server. That is not a fact
about your code, so it does not go in your code — it goes in a policy, with the
person who decided it.</p>
<div class="single">
  <figcaption>added to the end of the spec</figcaption>
  <pre><code data-lang="sexp">(policy
  (prefer sqlite-sqlc
    (strength required)
    (source ops "ships as one binary; no database server")))</code></pre>
</div>
<div class="single">
  <figcaption>What would it do?</figcaption>
  <pre><code data-lang="sh">orders.OrderStore
  you asked for   durable
  kept in         sqlite-sqlc
  ruled out       memory         — an in-memory map loses its data on restart
  ruled out       postgres-sqlc  — the policy requires sqlite-sqlc
                                   (source: ops, ships as one binary; no database server)
  ruled out       postgres-pgx   — the policy requires sqlite-sqlc
                                   (source: ops, ships as one binary; no database server)</code></pre>
</div>
<div class="pair">
  <figure>
    <figcaption>db/schema.sql, before</figcaption>
    <pre><code data-lang="sql">CREATE TABLE orders (
  id UUID PRIMARY KEY,
  customer_email TEXT NOT NULL,
  status TEXT CHECK (...) NOT NULL,
  total_cents BIGINT NOT NULL,
  placed_at TIMESTAMPTZ NOT NULL
);

-- name: SaveOrder :exec
INSERT INTO orders (...)
VALUES ($1, $2, $3, $4, $5)</code></pre>
  </figure>
  <figure>
    <figcaption>db/schema.sql, after</figcaption>
    <pre><code data-lang="sql">CREATE TABLE orders (
  id TEXT PRIMARY KEY,
  customer_email TEXT NOT NULL,
  status TEXT CHECK (...) NOT NULL,
  total_cents INTEGER NOT NULL,
  placed_at TEXT NOT NULL
);

-- name: SaveOrder :exec
INSERT INTO orders (...)
VALUES (?1, ?2, ?3, ?4, ?5)</code></pre>
  </figure>
</div>
<p>Column types, placeholders, the driver in <code>sqlc.yaml</code>, and
<code>postgres_order_store.go</code> becoming <code>sqlite_order_store.go</code>. The
handlers, the types and the enum did not move: they never depended on where the
data was kept.</p>
<div class="aside">Anything the spec no longer describes is <em>reported</em>, not
deleted. If you had already written the postgres store, tilegen tells you it is now
an orphan and leaves it alone.</div>
</div>

</div>

<h2 id="three"><span class="n">Three</span>Two packages, events and an abstraction</h2>

<p>Everything above, plus the parts that make it a service rather than a table:
events other code can react to, and an interface you will implement yourself.</p>

<div class="steps">

<div class="step"><h4>Write the spec</h4>
<div class="single">
  <figcaption>desk.sexp</figcaption>
  <pre><code data-lang="sexp">(project desk
  (module example.com/desk)
  (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0)))

(package tickets
  (enum Priority low normal high urgent)
  (enum State open pending solved closed)

  (entity Ticket
    (field ID uuid.UUID)
    (field Subject string)
    (field Requester string)
    (field Priority Priority)
    (field State State)
    (field OpenedAt time.Time)
    (field ClosedAt *time.Time)                ; optional: a nullable column
    (store get list save
      (list-by State)
      (count-by State)
      (durable)
      (method Escalate
        (doc "Escalate raises a ticket's priority and records why.")
        (params (id uuid.UUID) (reason string))
        (returns error))
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
    (store get list save (get-by Email) (durable)))

  (interface Assigner
    (doc "Assigner chooses who should take a ticket.")
    (method Assign
      (params (ticket *tickets.Ticket))
      (returns *Agent error)))

  (implement Assigner (as RoundRobinAssigner)
    (field agents AgentStore)
    (constraint "Never assign to an agent with no email.")))</code></pre>
</div>
<button class="try">Open this in the editor</button>
</div>

<div class="step"><h4>Events, finished</h4>
<div class="pair">
  <figure>
    <figcaption>what you wrote</figcaption>
    <pre><code data-lang="sexp">(events
  (event TicketOpened
    (field TicketID uuid.UUID))
  (event TicketSolved
    (field TicketID uuid.UUID)))</code></pre>
  </figure>
  <figure>
    <figcaption>tickets/events_gen.go — no methods left for you</figcaption>
    <pre><code data-lang="go">type Bus interface {
	PublishTicketOpened(ctx context.Context, e TicketOpened) error
	OnTicketOpened(h func(context.Context, TicketOpened) error) func()
	PublishTicketSolved(ctx context.Context, e TicketSolved) error
	OnTicketSolved(h func(context.Context, TicketSolved) error) func()
}

// LocalBus is an in-process Bus. Publish calls every handler in
// subscription order and returns their errors joined.
type LocalBus struct{ ... }

var _ Bus = (*LocalBus)(nil)</code></pre>
  </figure>
</div>
<p><code>OnTicketOpened</code> hands back a function that unsubscribes. If the events
have to reach another program, add <code>(cross-process)</code> and you get an
implementation that can.</p>
</div>

<div class="step"><h4>Your abstraction, scaffolded</h4>
<div class="single">
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
func (s *RoundRobinAssigner) Assign(ticket *tickets.Ticket) (*Agent, error) {
	panic("tilegen:hole agents.RoundRobinAssigner.Assign")
}

// Compile-time check that RoundRobinAssigner satisfies Assigner.
var _ Assigner = (*RoundRobinAssigner)(nil)</code></pre>
</div>
<p>The constraint you wrote is on the method, where whoever writes it will read it.
<code>Escalate</code> on the ticket store is the same idea: tilegen cannot know what
escalating means, so it carries your words and leaves the body.</p>
</div>

<div class="step"><h4>Change one thing: these events must leave the process</h4>
<p>Tickets are solved in this service, but the reporting service needs to hear about
it. That is a fact about your architecture, so it goes in the spec: add
<code>(cross-process)</code> to the events.</p>
<div class="pair">
  <figure>
    <figcaption>the only edit</figcaption>
    <pre><code data-lang="sexp">  (events
    (cross-process)
    (event TicketOpened
      (field TicketID uuid.UUID))
    (event TicketSolved
      (field TicketID uuid.UUID)))</code></pre>
  </figure>
  <figure>
    <figcaption>What would it do?</figcaption>
    <pre><code data-lang="sh">tickets.Bus
  you asked for   cross-process
  kept in         nats-bus
  ruled out       local-bus  — an in-process bus only reaches
                               handlers in this program</code></pre>
  </figure>
</div>
<div class="pair">
  <figure>
    <figcaption>tickets/nats_bus.go — <b>yours</b></figcaption>
    <pre><code data-lang="go">type NatsBus struct {
	conn    *nats.Conn
	subject string
}

func NewNatsBus(conn *nats.Conn, subject string) *NatsBus {
	return &NatsBus{conn: conn, subject: subject}
}

func (h *NatsBus) PublishTicketOpened(ctx context.Context, e TicketOpened) error {
	panic("tilegen:hole tickets.NatsBus.PublishTicketOpened")
}</code></pre>
  </figure>
  <figure>
    <figcaption>tilegen.tasks.json</figcaption>
    <pre><code data-lang="json">{
  "id": "tickets.NatsBus.PublishTicketOpened",
  "contract": "PublishTicketOpened(ctx context.Context,
                e TicketOpened) error",
  "intent": "Encode e as JSON and publish it on
             s.subject+\".TicketOpened\" with s.conn.
             Return the publish error."
}</code></pre>
    <p class="dim" style="margin-top:10px">The event types and the <code>Bus</code>
    interface are unchanged. Only the way they travel is different, so only the
    implementation moved.</p>
  </figure>
</div>
<p>The in-process bus arrived finished; this one cannot, because how you name
subjects and encode payloads is yours to decide. So tilegen writes the type, the
constructor and the interface check, and leaves one method per event with the job
spelled out.</p>
</div>

<div class="step"><h4>And one that is not in the spec at all</h4>
<p>Agents come from your identity provider at boot, so that store need not be
durable — remove <code>(durable)</code> and it is kept in memory. Two stores in one
project, kept two different ways, decided one at a time.</p>
<div class="single">
  <figcaption>What would it do?</figcaption>
  <pre><code data-lang="sh">agents.AgentStore
  kept in         memory
  also possible   sqlite-sqlc
  also possible   postgres-sqlc

tickets.TicketStore
  you asked for   durable
  kept in         postgres-sqlc
  ruled out       memory  — an in-memory map loses its data on restart</code></pre>
</div>
<p>The database schema now has one table instead of two, and
<code>postgres_agent_store.go</code> becomes <code>memory_agent_store.go</code>.</p>
</div>

</div>

<h2 id="reference">Everything a spec can say</h2>

<h3>Top level</h3>
<table>
<tr><td><code>(project name ...)</code></td><td>module, Go version, dependencies. One per spec.</td></tr>
<tr><td><code>(package name ...)</code></td><td>a Go package. As many as you like.</td></tr>
<tr><td><code>(config ...)</code></td><td>house style: JSON tag case, context-first, folder layout.</td></tr>
<tr><td><code>(policy ...)</code></td><td>decisions that are not tilegen's to make.</td></tr>
</table>

<h3>Inside a package</h3>
<table>
<tr><td><code>(entity T ...)</code></td><td>a type that is stored: fields, and a store.</td></tr>
<tr><td><code>(struct T ...)</code></td><td>a type with no store: a value object, a request body.</td></tr>
<tr><td><code>(enum T a b c)</code></td><td>a string type, its constants, a parser, and a database constraint.</td></tr>
<tr><td><code>(interface T ...)</code></td><td>an interface; <code>(embed io.Writer)</code> borrows methods.</td></tr>
<tr><td><code>(implement T (as N) ...)</code></td><td>a struct that satisfies it, with its dependencies.</td></tr>
<tr><td><code>(http (route ...) ...)</code></td><td>a JSON API over this package's stores.</td></tr>
<tr><td><code>(events (event ...) ...)</code></td><td>typed publish and subscribe.</td></tr>
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
<tr><td><code>(method N ...)</code></td><td>anything else; always left for you to write</td></tr>
<tr><td><code>(constraint "...")</code></td><td>a rule, carried to every method of the store</td></tr>
</table>

<h3>Policy</h3>
<table>
<tr><td><code>(prefer T (strength required))</code></td><td>nothing else may be used</td></tr>
<tr><td><code>(prefer T (strength strong))</code></td><td>use it unless it cannot do the job</td></tr>
<tr><td><code>(prefer T (strength weak))</code></td><td>use it when the choice is close</td></tr>
<tr><td><code>(source who "why")</code></td><td>who decided, and why — shown wherever it applies</td></tr>
<tr><td><code>(avoid T "why")</code></td><td>never use it, and say so</td></tr>
</table>

<h2 id="next">What to do with the zip</h2>

<div class="single">
  <pre><code data-lang="sh">unzip project.zip && cd project
sqlc generate     # only if the project has a db/ folder
go mod tidy
go build ./...</code></pre>
</div>

<p>Files ending <code>_gen.go</code> are rewritten every time you generate, so do not
edit them. Everything else is yours and is never overwritten. The methods waiting for
you are listed with their signatures and notes in <code>tilegen.tasks.json</code>.</p>

<p>For a project you will come back to, install the command line tool. Editing the
spec and running it again updates in place: new methods appear, methods you have
written are untouched, and anything the spec no longer describes is reported.</p>

<div class="single">
  <pre><code data-lang="sh">go install github.com/vinodhalaharvi/tilegen@latest

tilegen spec/              # generate, or update after a change
tilegen explain spec/      # how each store will be kept, and why
tilegen check spec/        # fail if out of date or methods are missing — for CI
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
