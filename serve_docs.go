package main

// The documentation the service serves. It is for people writing specs:
// what each form means, and what it gives them. Examples are paired with
// the code they actually produce, taken from real output.

const docsHTML = `
<div class="lede">
  <h1>Describe what your service is made of.<br>Get the project.</h1>
  <p>You write a short spec. tilegen writes the types, the interfaces, the database
  schema and queries, the HTTP routing and the wiring. Where a decision is yours —
  what makes an order valid, how to price a cart — it leaves a method with a clear
  contract and a note about what it should do.</p>
</div>

<h2 id="start">The smallest spec that works</h2>

<div class="pair">
  <figure>
    <figcaption>spec</figcaption>
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
    <figcaption>tasks/tasks_gen.go</figcaption>
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

var ErrNotFound = errors.New("tasks: not found")</code></pre>
  </figure>
</div>
<button class="try">Open this in the editor</button>

<p>You also get an implementation with the four methods waiting for you, and a
list of them in <code>tilegen.tasks.json</code>. The project compiles as it is,
so you can fill them in any order.</p>

<div class="single">
  <figcaption>tasks/memory_task_store.go — yours, never overwritten</figcaption>
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

<div class="aside">A spec is made of parentheses, names and strings: everything is
<code>(head arguments…)</code>, comments start with <code>;</code>, and order does
not matter. Misspell a form and you get the line number and, usually, the word you
meant.</div>

<h2 id="contents">What a spec can say</h2>

<ul class="toc">
  <li><a href="#project">project <span>— module, Go version, dependencies</span></a></li>
  <li><a href="#package">package <span>— a Go package</span></a></li>
  <li><a href="#entity">entity <span>— a type that is stored</span></a></li>
  <li><a href="#store">store <span>— the operations you need</span></a></li>
  <li><a href="#durable">durable <span>— data that must survive a restart</span></a></li>
  <li><a href="#enum">enum <span>— a fixed set of values</span></a></li>
  <li><a href="#http">http <span>— a JSON API</span></a></li>
  <li><a href="#events">events <span>— publish and subscribe</span></a></li>
  <li><a href="#interface">interface, implement <span>— your own abstractions</span></a></li>
  <li><a href="#struct">struct, llm <span>— everything else</span></a></li>
  <li><a href="#config">config <span>— house style</span></a></li>
  <li><a href="#policy">policy <span>— decisions that are not tilegen's to make</span></a></li>
</ul>

<h2 id="project">project</h2>

<p>One per spec. It names the module, the Go version, and any dependency you refer
to elsewhere by a short name.</p>

<div class="single">
  <pre><code data-lang="sexp">(project shop
  (doc "An online shop.")
  (module github.com/acme/shop)
  (go 1.22)
  (require
    (uuid github.com/google/uuid v1.6.0)
    (decimal github.com/shopspring/decimal v1.4.0)))</code></pre>
</div>

<p>Declaring <code>uuid</code> is what lets you write <code>(field ID uuid.UUID)</code>
below. Standard library packages need no <code>require</code>: write
<code>time.Time</code> or <code>json.RawMessage</code> and they are understood.</p>

<h2 id="package">package</h2>

<p>One Go package. Use as many as you like, and refer across them by name.</p>

<div class="single">
  <pre><code data-lang="sexp">(project billing (module example.com/billing) (go 1.22))

(package orders
  (entity Order (field ID int64) (field Total int64) (store get save)))

(package invoices
  (entity Invoice
    (field ID int64)
    (field Order orders.Order)      ; a type from the other package
    (store get save)))</code></pre>
</div>
<button class="try">Open this in the editor</button>

<p>If two packages end up needing each other, you are told which two, while you are
still writing the spec.</p>

<h2 id="entity">entity</h2>

<p>A type that is stored somewhere. Fields become struct fields with JSON tags, and
a pointer type is optional — in a database, a nullable column.</p>

<div class="single">
  <pre><code data-lang="sexp">(entity Customer
  (doc "Customer is someone who has ordered at least once.")
  (field ID uuid.UUID)
  (field Email string)
  (field Name string)
  (field Company *string)                     ; optional
  (field SignedUpAt time.Time)
  (field TotalCents int64 (doc "Minor units: 12.34 is 1234."))
  (store get list save))</code></pre>
</div>

<p>Every entity needs an <code>ID</code> field; it is what <code>get</code> and
<code>delete</code> take. <code>int64</code>, <code>string</code> and
<code>uuid.UUID</code> all work.</p>

<h2 id="store">store</h2>

<p>The operations you actually need. You get an interface with exactly these methods
and nothing else.</p>

<table>
<tr><th>You write</th><th>You get</th></tr>
<tr><td><code>get</code></td><td><code>Get(ctx, id) (*T, error)</code></td></tr>
<tr><td><code>list</code></td><td><code>List(ctx) ([]*T, error)</code></td></tr>
<tr><td><code>save</code></td><td><code>Save(ctx, *T) error</code> — insert or update</td></tr>
<tr><td><code>delete</code></td><td><code>Delete(ctx, id) error</code></td></tr>
<tr><td><code>count</code></td><td><code>Count(ctx) (int64, error)</code></td></tr>
<tr><td><code>(list-by Field)</code></td><td><code>ListByField(ctx, v) ([]*T, error)</code></td></tr>
<tr><td><code>(get-by Field)</code></td><td><code>GetByField(ctx, v) (*T, error)</code></td></tr>
<tr><td><code>(count-by Field)</code></td><td><code>CountByField(ctx, v) (int64, error)</code></td></tr>
<tr><td><code>(exists-by Field)</code></td><td><code>ExistsByField(ctx, v) (bool, error)</code></td></tr>
<tr><td><code>(delete-by Field)</code></td><td><code>DeleteByField(ctx, v) error</code></td></tr>
</table>

<div class="pair">
  <figure>
    <figcaption>spec</figcaption>
    <pre><code data-lang="sexp">(project orders
  (module example.com/orders)
  (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0)))

(package orders
  (entity Order
    (field ID uuid.UUID)
    (field CustomerEmail string)
    (field PlacedAt time.Time)
    (store get list save delete count
      (list-by CustomerEmail)
      (exists-by CustomerEmail)
      (durable)
      (constraint "Never write CustomerEmail to logs."))))</code></pre>
  </figure>
  <figure>
    <figcaption>db/query.sql</figcaption>
    <pre><code data-lang="sql">-- name: GetOrder :one
SELECT * FROM orders WHERE id = $1;

-- name: ListOrders :many
SELECT * FROM orders ORDER BY id;

-- name: SaveOrder :exec
INSERT INTO orders (id, customer_email, placed_at)
VALUES ($1, $2, $3)
ON CONFLICT (id) DO UPDATE SET
  customer_email = EXCLUDED.customer_email,
  placed_at = EXCLUDED.placed_at;

-- name: ListOrdersByCustomerEmail :many
SELECT * FROM orders WHERE customer_email = $1 ORDER BY id;

-- name: ExistsOrderByCustomerEmail :one
SELECT EXISTS (SELECT 1 FROM orders WHERE customer_email = $1);</code></pre>
  </figure>
</div>
<button class="try">Open this in the editor</button>

<h3>A method of your own</h3>

<p>When you need something the table above does not cover, describe it. You get the
method on the interface and a stub with your description attached.</p>

<div class="single">
  <pre><code data-lang="sexp">(store get list save
  (method MarkShipped
    (doc "MarkShipped records that an order left the warehouse.")
    (params (id uuid.UUID) (at time.Time))
    (returns error)))</code></pre>
</div>

<h3>Rules to remember</h3>

<p>A <code>constraint</code> travels with every method of that store, so whoever
writes them sees the rule. They are notes, not checked code.</p>

<div class="single">
  <pre><code data-lang="sexp">(store get save
  (constraint "Saving the same email twice must not create two customers.")
  (constraint "Never write Email to logs."))</code></pre>
</div>

<h2 id="durable">durable</h2>

<p>A store is kept in memory unless you say otherwise, which suits sessions, caches
and tests. Say <code>(durable)</code> when the data must survive a restart, and you
get a real database instead: schema, queries and an implementation to match.</p>

<div class="single">
  <pre><code data-lang="sexp">(project shop (module example.com/shop) (go 1.22))

(package shop
  (entity Order                     ; must not be lost
    (field ID int64)
    (field Total int64)
    (store get list save (durable)))

  (entity Draft                     ; scratch: memory is fine
    (field ID int64)
    (field Body string)
    (store get save delete)))</code></pre>
</div>
<button class="try">Open this in the editor</button>

<p>Each store is decided on its own, so one project can keep orders in a database
and drafts in memory. Press <b>What would it do?</b> on that spec and you will see
what was chosen for each, and what was ruled out.</p>

<h2 id="enum">enum</h2>

<p>A fixed set of values, and a database that refuses anything else.</p>

<div class="pair">
  <figure>
    <figcaption>spec</figcaption>
    <pre><code data-lang="sexp">(project orders (module example.com/orders) (go 1.22))

(package orders
  (enum Status
    (doc "Status is where an order is in its life.")
    pending paid shipped cancelled)

  (entity Order
    (field ID int64)
    (field Status Status)
    (field Total int64)
    (store get list save (durable) (list-by Status))))</code></pre>
  </figure>
  <figure>
    <figcaption>orders/orders_gen.go and db/schema.sql</figcaption>
    <pre><code data-lang="go">type Status string

const (
	StatusPending   Status = "pending"
	StatusPaid      Status = "paid"
	StatusShipped   Status = "shipped"
	StatusCancelled Status = "cancelled"
)

// Valid reports whether s is one of the declared values.
func (s Status) Valid() bool { … }

// ParseStatus returns the Status for s.
func ParseStatus(s string) (Status, error) { … }</code></pre>
    <pre style="margin-top:10px"><code data-lang="sql">CREATE TABLE orders (
  id INTEGER PRIMARY KEY,
  status TEXT CHECK (status IN ('pending', 'paid', 'shipped', 'cancelled')) NOT NULL,
  total INTEGER NOT NULL
);</code></pre>
  </figure>
</div>
<button class="try">Open this in the editor</button>

<h2 id="http">http</h2>

<p>A JSON API over the package's stores. Each route names an operation and an entity;
<code>{id}</code> in a path is the record's ID.</p>

<div class="pair">
  <figure>
    <figcaption>spec</figcaption>
    <pre><code data-lang="sexp">(project api (module example.com/api) (go 1.22))

(package orders
  (entity Order
    (field ID int64)
    (field Total int64)
    (store get list save delete (durable)))

  (http
    (route GET    "/orders"      (list Order))
    (route GET    "/orders/{id}" (get Order))
    (route POST   "/orders"      (save Order))
    (route DELETE "/orders/{id}" (delete Order))))</code></pre>
  </figure>
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
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
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
</div>
<button class="try">Open this in the editor</button>

<p>Unknown paths answer 404 and wrong methods answer 405 without you writing
anything. Two decisions are yours, one pair per entity:</p>

<div class="single">
  <figcaption>orders/http.go — yours</figcaption>
  <pre><code data-lang="go">// validateOrder reports what makes a request unacceptable.
func (h *Handler) validateOrder(order *Order) error {
	panic("tilegen:hole orders.Handler.validateOrder")
}

// statusForOrder maps a store error to an HTTP status.
func (h *Handler) statusForOrder(err error) int {
	panic("tilegen:hole orders.Handler.statusForOrder")
}</code></pre>
</div>

<p><code>get</code> and <code>delete</code> act on one record and need
<code>{id}</code>; <code>list</code> and <code>save</code> act on the collection and
must not have it. Getting that the wrong way round is an error that says so.</p>

<h2 id="events">events</h2>

<p>Typed publish and subscribe inside a package. This one arrives finished — there
is nothing left to write.</p>

<div class="pair">
  <figure>
    <figcaption>spec</figcaption>
    <pre><code data-lang="sexp">(project shipping (module example.com/shipping) (go 1.22))

(package shipping
  (entity Shipment
    (field ID int64)
    (field OrderID int64)
    (store get save))

  (events
    (doc "Shipping events, for anything that wants to react.")
    (event ShipmentCreated
      (field ShipmentID int64)
      (field OrderID int64))
    (event ShipmentDelivered
      (field ShipmentID int64))))</code></pre>
  </figure>
  <figure>
    <figcaption>shipping/events_gen.go</figcaption>
    <pre><code data-lang="go">type Bus interface {
	// PublishShipmentCreated delivers e to every handler.
	PublishShipmentCreated(ctx context.Context, e ShipmentCreated) error
	// OnShipmentCreated subscribes h and returns a func that
	// unsubscribes it.
	OnShipmentCreated(h func(context.Context, ShipmentCreated) error) func()

	PublishShipmentDelivered(ctx context.Context, e ShipmentDelivered) error
	OnShipmentDelivered(h func(context.Context, ShipmentDelivered) error) func()
}

// LocalBus delivers in the publisher's goroutine, in subscription
// order, and returns the handlers' errors joined.
type LocalBus struct { … }

var _ Bus = (*LocalBus)(nil)</code></pre>
  </figure>
</div>
<button class="try">Open this in the editor</button>

<p>If the events have to reach another program, or survive a restart, say so and you
get an implementation that can:</p>

<div class="single">
  <pre><code data-lang="sexp">(events
  (cross-process)                   ; handlers live in other programs
  (event JobStarted (field JobID int64))
  (event JobFinished (field JobID int64)))</code></pre>
</div>

<h2 id="interface">interface and implement</h2>

<p>Describe an abstraction, and ask for something that satisfies it.</p>

<div class="pair">
  <figure>
    <figcaption>spec</figcaption>
    <pre><code data-lang="sexp">(project pricing (module example.com/pricing) (go 1.22))

(package pricing
  (entity Cart
    (field ID int64)
    (field TotalCents int64)
    (store get save))

  (interface Pricer
    (doc "Pricer works out what a cart costs.")
    (method Price
      (doc "Price returns the total after discounts.")
      (params (cart *Cart))
      (returns int64 error)))

  (implement Pricer (as SeasonalPricer)
    (doc "SeasonalPricer applies the running campaign.")
    (field carts CartStore)
    (constraint "A discount must never take a total below zero.")))</code></pre>
  </figure>
  <figure>
    <figcaption>pricing/seasonal_pricer.go — yours</figcaption>
    <pre><code data-lang="go">// SeasonalPricer applies the running campaign.
type SeasonalPricer struct {
	carts CartStore
}

func NewSeasonalPricer(carts CartStore) *SeasonalPricer {
	return &SeasonalPricer{carts: carts}
}

// Price returns the total after discounts.
//
// A discount must never take a total below zero.
func (s *SeasonalPricer) Price(cart *Cart) (int64, error) {
	panic("tilegen:hole pricing.SeasonalPricer.Price")
}</code></pre>
  </figure>
</div>
<button class="try">Open this in the editor</button>

<p>An interface can borrow methods from another, including from the standard
library:</p>

<div class="single">
  <pre><code data-lang="sexp">(interface AuditLog
  (embed io.Writer)                 ; adds Write([]byte) (int, error)
  (method Flush (returns error)))</code></pre>
</div>

<h2 id="struct">struct and llm</h2>

<p>A <code>struct</code> is a type with no store: a value object, a request body, a
piece of configuration.</p>

<div class="single">
  <pre><code data-lang="sexp">(struct Address
  (field Line1 string)
  (field Line2 *string)
  (field City string)
  (field Postcode string))</code></pre>
</div>

<p>An <code>llm</code> form is for work you can describe but not type out. Nothing is
generated; it becomes a task with your words as the brief.</p>

<div class="single">
  <pre><code data-lang="sexp">(llm "A middleware that rejects requests without a valid API key,
      reading the keys from the environment.")</code></pre>
</div>

<h2 id="config">config</h2>

<p>House style: it changes how the code reads, never what it does. Put it anywhere in
the spec. These are the defaults.</p>

<div class="single">
  <pre><code data-lang="sexp">(config
  (json-tags snake)       ; snake | camel | none
  (context-first yes)     ; ctx context.Context first in every method
  (layout flat))          ; flat | internal (packages under internal/)</code></pre>
</div>

<h2 id="policy">policy</h2>

<p>Some decisions are not tilegen's to make: a client's approved-library list, a
team that only runs one database, an appliance that ships to a single box. Say so,
and say who decided, so the reason is still there in six months.</p>

<p>Here is the same spec, twice. The only difference is the five lines at the end.</p>

<div class="pair">
  <figure>
    <figcaption>without a policy → db/schema.sql</figcaption>
    <pre><code data-lang="sql">CREATE TABLE orders (
  id UUID PRIMARY KEY,
  placed_at TIMESTAMPTZ NOT NULL,
  total BIGINT NOT NULL
);</code></pre>
    <pre style="margin-top:10px"><code data-lang="sh">orders/orders_gen.go
orders/postgres_order_store.go
sqlc.yaml        engine: "postgresql"</code></pre>
  </figure>
  <figure>
    <figcaption>with the policy → db/schema.sql</figcaption>
    <pre><code data-lang="sql">CREATE TABLE orders (
  id TEXT PRIMARY KEY,
  placed_at TEXT NOT NULL,
  total INTEGER NOT NULL
);</code></pre>
    <pre style="margin-top:10px"><code data-lang="sh">orders/orders_gen.go
orders/sqlite_order_store.go
sqlc.yaml        engine: "sqlite"</code></pre>
  </figure>
</div>

<div class="single">
  <figcaption>the spec, with the policy at the end</figcaption>
  <pre><code data-lang="sexp">(project shop
  (module example.com/shop)
  (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0)))

(package orders
  (entity Order
    (field ID uuid.UUID)
    (field PlacedAt time.Time)
    (field Total int64)
    (store get list save (durable))))

(policy
  (prefer sqlite-sqlc
    (strength required)
    (source ops "ships to a single box; no database server")))</code></pre>
</div>
<button class="try">Open this in the editor</button>

<p>The schema, the queries and the implementation all change. Nothing else in the
spec moved.</p>

<table>
<tr><th>strength</th><th>meaning</th></tr>
<tr><td><code>required</code></td><td>nothing else may be used, even if it costs more</td></tr>
<tr><td><code>strong</code></td><td>use it unless it cannot do the job</td></tr>
<tr><td><code>weak</code></td><td>use it when the choice is close</td></tr>
</table>

<p>The other direction works too, and the reason is shown wherever that option comes
up:</p>

<div class="single">
  <pre><code data-lang="sexp">(policy
  (avoid postgres-pgx "we standardised on generated queries"))</code></pre>
</div>

<h2 id="whatyouget">What arrives in the zip</h2>

<table>
<tr><td><code>go.mod</code></td><td>your module and its dependencies</td></tr>
<tr><td><code>pkg/pkg_gen.go</code></td><td>types, interfaces, enums</td></tr>
<tr><td><code>pkg/…_store.go</code></td><td>the store, with the methods left for you</td></tr>
<tr><td><code>pkg/http_gen.go</code></td><td>routing and handlers</td></tr>
<tr><td><code>pkg/http.go</code></td><td>validation and error mapping, left for you</td></tr>
<tr><td><code>pkg/events_gen.go</code></td><td>events and the bus</td></tr>
<tr><td><code>db/schema.sql</code>, <code>db/query.sql</code></td><td>for durable stores</td></tr>
<tr><td><code>tilegen.tasks.json</code></td><td>every method left to write, with its contract</td></tr>
<tr><td><code>GENERATED.md</code></td><td>what to run next</td></tr>
</table>

<div class="single">
  <pre><code data-lang="sh">unzip project.zip && cd project
sqlc generate     # only if the project has a db/ folder
go mod tidy
go build ./...</code></pre>
</div>

<p>Files ending <code>_gen.go</code> are rewritten whenever you generate again, so do
not edit them. Everything else is yours and is never overwritten.</p>

<h2 id="holes">The methods left for you</h2>

<p>Each one is listed with its exact signature, what it should do, and any rule you
wrote:</p>

<div class="single">
  <figcaption>tilegen.tasks.json</figcaption>
  <pre><code data-lang="json">{
  "id": "orders.PostgresOrderStore.Get",
  "file": "orders/postgres_order_store.go",
  "contract": "Get(ctx context.Context, id uuid.UUID) (*Order, error)",
  "intent": "Call the sqlc-generated s.q.GetOrder and convert the db.Order to *Order.",
  "constraints": ["Never write CustomerEmail to logs."]
}</code></pre>
</div>

<p>Write them yourself, or hand the file to a coding assistant. With the command line
tool, <code>tilegen prompt</code> writes a complete brief for any one of them.</p>

<h2 id="cli">Keeping a project</h2>

<p>This page is the quick way in. For a project you will come back to, install it:</p>

<div class="single">
  <pre><code data-lang="sh">go install github.com/vinodhalaharvi/tilegen@latest</code></pre>
</div>

<table>
<tr><td><code>tilegen spec/</code></td><td>generate, or update after a change</td></tr>
<tr><td><code>tilegen explain spec/</code></td><td>how each store will be kept, and why</td></tr>
<tr><td><code>tilegen check spec/</code></td><td>fail if code is out of date or methods are missing — for CI</td></tr>
<tr><td><code>tilegen prompt spec/</code></td><td>a ready-to-paste brief for a method</td></tr>
<tr><td><code>tilegen import ./existing</code></td><td>lift a project you already have into a spec</td></tr>
</table>

<p>The difference that matters: run it again after editing the spec and it updates in
place. New methods are added, methods you have written are left alone, and anything
the spec no longer describes is reported rather than deleted.</p>

<h2 id="api">From a script</h2>

<div class="single">
  <pre><code data-lang="sh">curl -X POST https://your-service/generate \
  --data-binary @spec.sexp -o project.zip

curl -X POST https://your-service/explain \
  --data-binary @spec.sexp | jq .</code></pre>
</div>

<p>Send the spec as the body, or as JSON with <code>spec</code>, <code>config</code>,
<code>policy</code> and <code>name</code> fields. Errors come back as JSON with a
message that points at the line.</p>

<div class="aside">This service generates files and returns them. It does not run
anything: no build, no database, no calls out. What you download is yours to read
before you run it.</div>

<h2 id="examples">Two larger examples</h2>

<h3>A support desk</h3>

<div class="single">
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
    (field Body string)
    (field Requester string)
    (field Priority Priority)
    (field State State)
    (field OpenedAt time.Time)
    (field ClosedAt *time.Time)
    (store get list save
      (list-by State)
      (list-by Priority)
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
    (field Name string)
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

<h3>An inventory service that ships to one box</h3>

<div class="single">
  <pre><code data-lang="sexp">(project inventory
  (module example.com/inventory)
  (go 1.22))

(package stock
  (enum Movement received sold damaged returned)

  (entity Item
    (field ID string)                ; the SKU
    (field Name string)
    (field OnHand int64)
    (field ReorderAt int64)
    (store get list save
      (durable)
      (constraint "OnHand must never go below zero.")))

  (entity Entry
    (field ID int64)
    (field SKU string)
    (field Kind Movement)
    (field Quantity int64)
    (field At time.Time)
    (store list save (list-by SKU) (count-by Kind) (durable)))

  (http
    (route GET  "/items"      (list Item))
    (route GET  "/items/{id}" (get Item))
    (route POST "/items"      (save Item))
    (route POST "/entries"    (save Entry)))

  (events
    (event StockLow (field SKU string) (field OnHand int64))))

(config
  (json-tags snake)
  (layout internal))

(policy
  (prefer sqlite-sqlc
    (strength required)
    (source ops "one binary, one file, no database server")))</code></pre>
</div>
<button class="try">Open this in the editor</button>

<h2 id="errors">When a spec is wrong</h2>

<p>Mistakes are caught before anything is generated, and the message says where, and
usually what you meant:</p>

<div class="single">
  <pre><code data-lang="sh">spec.sexp:7:5: entity Order needs an ID field
spec.sexp:12:3: unknown form (entiy Customer ...) (did you mean entity?)
spec.sexp:19:7: (get Order) needs {id} in the path, since it acts on one Order
spec.sexp:24:3: duplicate type "Order"
spec.sexp:31:9: packages orders and billing import each other</code></pre>
</div>

<p>A form tilegen does not know, and that is not a near-miss for one it does, becomes
a task rather than an error — so you can sketch in your own words and decide later.</p>
`
