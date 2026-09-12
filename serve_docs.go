package main

// The documentation the service serves. It is for people writing specs,
// so it says what a spec can contain and what comes back, with examples
// meant to be pasted into the editor and run.

const docsHTML = `
<h2 id="start">Getting started</h2>

<p>You describe what your program is made of. tilegen writes the Go: the types,
the interfaces, the database schema and queries, the HTTP routing, the wiring.
Where a decision is yours to make — what makes an order valid, how to price a
cart — it leaves a method with a clear contract and a note about what it should
do, for you or an assistant to fill in.</p>

<p>The smallest spec that works:</p>

<pre><code>(project todo
  (module example.com/todo)
  (go 1.22))

(package tasks
  (entity Task
    (field ID int64)
    (field Title string)
    (field Done bool)
    (store get list save delete)))
</code></pre>
<p><button class="try">try this ↑</button></p>

<p>That gives you a <code>Task</code> struct with JSON tags, a <code>TaskStore</code>
interface, an in-memory implementation, and a list of the four methods left
to write. Press <strong>download project</strong> and you have a Go module.</p>

<div class="note">A spec is S-expressions: parentheses, names, and strings.
Everything is <code>(head arguments...)</code>. Comments start with <code>;</code>.
Order does not matter. Nothing is magic — if a form is misspelled you get an
error pointing at the line, usually with a suggestion.</div>

<h2 id="contents">What a spec contains</h2>

<div class="toc">
  <a href="#project">project — name, module, Go version, dependencies</a>
  <a href="#package">package — a Go package</a>
  <a href="#entity">entity — a type and its store</a>
  <a href="#store">store — the operations you need</a>
  <a href="#durable">durable — data that must survive a restart</a>
  <a href="#enum">enum — a fixed set of values</a>
  <a href="#http">http — a JSON API</a>
  <a href="#events">events — publish and subscribe</a>
  <a href="#interface">interface and implement</a>
  <a href="#struct">struct — a plain type</a>
  <a href="#llm">llm — anything else</a>
  <a href="#config">config — house style</a>
  <a href="#policy">policy — your preferences</a>
</div>

<h2 id="project">project</h2>

<p>Every spec has exactly one. It names the module, the Go version, and any
dependency you refer to by name elsewhere.</p>

<pre><code>(project shop
  (doc "An online shop.")
  (module github.com/acme/shop)
  (go 1.22)
  (require
    (uuid github.com/google/uuid v1.6.0)
    (decimal github.com/shopspring/decimal v1.4.0)))
</code></pre>

<p>The short name in each <code>require</code> is how you write the package in a
field type: declaring <code>uuid</code> lets you write
<code>(field ID uuid.UUID)</code>. Standard library packages need no
<code>require</code>; write <code>time.Time</code> or <code>json.RawMessage</code>
and they are understood.</p>

<h2 id="package">package</h2>

<p>One Go package. Put as many as you like in a spec, and refer across them
with the package name:</p>

<pre><code>(project demo (module example.com/demo) (go 1.22))

(package orders
  (entity Order (field ID int64) (store get save)))

(package billing
  (entity Invoice
    (field ID int64)
    (field Order orders.Order)      ; a type from another package
    (store get save)))
</code></pre>
<p><button class="try">try this ↑</button></p>

<p>Import cycles are refused, with both sides named, so you find out while
writing the spec rather than after generating.</p>

<h2 id="entity">entity</h2>

<p>A type that is stored somewhere. Fields become struct fields with JSON tags;
a pointer type is optional, and becomes a nullable column.</p>

<pre><code>(entity Customer
  (doc "Customer is someone who has ordered at least once.")
  (field ID uuid.UUID)
  (field Email string)
  (field Name string)
  (field Company *string)           ; optional
  (field SignedUpAt time.Time)
  (field LastSeen *time.Time)       ; optional
  (store get list save))
</code></pre>

<p>Every entity needs an <code>ID</code> field; it is what <code>get</code> and
<code>delete</code> take. <code>int64</code>, <code>string</code> and
<code>uuid.UUID</code> all work.</p>

<h3>Field documentation</h3>

<pre><code>(field TotalCents int64 (doc "Minor units, so 12.34 is 1234."))
</code></pre>

<p>A field's doc shows up as a comment on the struct field, and in the notes
for anyone implementing methods that touch it.</p>

<h2 id="store">store</h2>

<p>The operations your program actually needs. You get an interface with exactly
these methods and nothing else.</p>

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

<pre><code>(project demo (module example.com/demo) (go 1.22) (require (uuid github.com/google/uuid v1.6.0)))

(package orders
  (entity Order
    (field ID uuid.UUID)
    (field CustomerEmail string)
    (field Status string)
    (field PlacedAt time.Time)
    (store get list save delete count
      (list-by CustomerEmail)
      (get-by CustomerEmail)
      (count-by Status)
      (exists-by CustomerEmail)
      (delete-by CustomerEmail))))
</code></pre>
<p><button class="try">try this ↑</button></p>

<h3>A method of your own</h3>

<p>When you need something the list above does not cover, describe it:</p>

<pre><code>(store get list save
  (method MarkShipped
    (doc "MarkShipped records that an order left the warehouse.")
    (params (id uuid.UUID) (at time.Time))
    (returns error)))
</code></pre>

<p>You get the method on the interface and a stub with your description
attached. Custom methods are always left for you to write, since only you
know what they mean.</p>

<h3>Rules to remember</h3>

<pre><code>(store get save
  (constraint "Saving the same email twice must not create two customers.")
  (constraint "Never write Email to logs."))
</code></pre>

<p>Constraints travel with every method of that store, so whoever implements
them sees the rules. They are notes for a human or an assistant, not checked
code.</p>

<h2 id="durable">durable</h2>

<p>By default a store is kept in memory, which is fine for sessions, caches and
tests. Say <code>(durable)</code> when the data must survive a restart and you
get a real database instead — schema, queries and all.</p>

<pre><code>(project shop (module example.com/shop) (go 1.22))

(package shop
  (entity Order                    ; must not be lost
    (field ID int64)
    (field Total int64)
    (store get list save (durable)))

  (entity Draft                    ; scratch: memory is fine
    (field ID int64)
    (field Body string)
    (store get save delete)))
</code></pre>
<p><button class="try">try this ↑</button></p>

<p>Press <strong>what would it do?</strong> on that one: each store is decided
separately, so one project can keep orders in a database and drafts in memory.
tilegen tells you what it picked and why it ruled the others out.</p>

<h2 id="enum">enum</h2>

<p>A fixed set of values. You get a string type, a constant for each value, the
full list, a validity check and a parser — and, in a database, a constraint
that rejects anything else.</p>

<pre><code>(project demo (module example.com/demo) (go 1.22))

(package orders
  (enum Status
    (doc "Status is where an order is in its life.")
    pending paid shipped cancelled)

  (entity Order
    (field ID int64)
    (field Status Status)
    (store get list save (durable) (list-by Status))))
</code></pre>
<p><button class="try">try this ↑</button></p>

<p>That gives you <code>StatusPending</code>, <code>StatusPaid</code> and so on,
plus <code>StatusValues</code>, <code>Status.Valid()</code> and
<code>ParseStatus(s)</code>.</p>

<h2 id="http">http</h2>

<p>A JSON API over the package's stores. Each route names a method and an
entity, and <code>{id}</code> in a path is the record's ID.</p>

<pre><code>(http
  (doc "The orders API.")
  (route GET    "/orders"      (list Order))
  (route GET    "/orders/{id}" (get Order))
  (route POST   "/orders"      (save Order))
  (route DELETE "/orders/{id}" (delete Order)))
</code></pre>

<p>You get a <code>Handler</code>, a <code>Routes()</code> method returning a
standard <code>*http.ServeMux</code>, and a handler per route that parses the
id, decodes the body, calls the store and writes JSON with the right status
code. Unknown paths give 404 and wrong methods give 405, without you doing
anything.</p>

<p>Two things are yours, one pair per entity:</p>

<pre><code>func (h *Handler) validateOrder(order *Order) error  // what makes a request unacceptable
func (h *Handler) statusForOrder(err error) int      // which status a store error deserves
</code></pre>

<p>A full example, ready to run:</p>

<pre><code>(project api
  (module example.com/api)
  (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0)))

(package catalog
  (enum Availability in-stock backordered discontinued)

  (entity Product
    (field ID uuid.UUID)
    (field SKU string)
    (field Name string)
    (field PriceCents int64)
    (field Availability Availability)
    (store get list save delete
      (get-by SKU)
      (list-by Availability)
      (durable)
      (constraint "Two products must never share a SKU.")))

  (http
    (route GET    "/products"      (list Product))
    (route GET    "/products/{id}" (get Product))
    (route POST   "/products"      (save Product))
    (route DELETE "/products/{id}" (delete Product))))
</code></pre>
<p><button class="try">try this ↑</button></p>

<p><code>GET</code> with <code>{id}</code> and <code>DELETE</code> with
<code>{id}</code> act on one record; <code>list</code> and <code>save</code> act
on the collection and take no <code>{id}</code>. Getting that wrong is an error
with an explanation.</p>

<h2 id="events">events</h2>

<p>Typed publish and subscribe within a package. No holes: this one is complete
when you get it.</p>

<pre><code>(project demo (module example.com/demo) (go 1.22))

(package shipping
  (entity Shipment (field ID int64) (field OrderID int64) (store get save))

  (events
    (doc "Shipping events, for anything that wants to react.")
    (event ShipmentCreated (field ShipmentID int64) (field OrderID int64))
    (event ShipmentDelivered (field ShipmentID int64))))
</code></pre>
<p><button class="try">try this ↑</button></p>

<p>You get a struct per event and a <code>Bus</code> interface with a
<code>PublishShipmentCreated</code> and an <code>OnShipmentCreated</code> for
each, where subscribing returns a function that unsubscribes. The default
implementation delivers in the same process, in order, and hands you back any
error a handler returned.</p>

<p>If the events have to reach another process, or survive a restart, say so
and you get an implementation that can:</p>

<pre><code>(events
  (cross-process)                  ; handlers live in other programs
  (event JobStarted (field JobID int64))
  (event JobFinished (field JobID int64)))
</code></pre>

<h2 id="interface">interface and implement</h2>

<p>Describe an interface, and optionally ask for something that implements it:</p>

<pre><code>(project demo (module example.com/demo) (go 1.22))

(package pricing
  (entity Cart (field ID int64) (field TotalCents int64) (store get save))

  (interface Pricer
    (doc "Pricer works out what a cart costs.")
    (method Price
      (doc "Price returns the total in minor units, after discounts.")
      (params (cart *Cart))
      (returns int64 error)))

  (implement Pricer (as SeasonalPricer)
    (doc "SeasonalPricer applies whatever campaign is running.")
    (field carts CartStore)
    (constraint "A discount must never take the total below zero.")))
</code></pre>
<p><button class="try">try this ↑</button></p>

<p>You get the interface, a <code>SeasonalPricer</code> struct with the
dependencies you listed, a constructor, a compile-time check that it satisfies
<code>Pricer</code>, and a stub per method with your notes attached.</p>

<p>An interface can also borrow methods from another:</p>

<pre><code>(interface AuditLog
  (embed io.Writer)                ; adds Write([]byte) (int, error)
  (method Flush (returns error)))
</code></pre>

<h2 id="struct">struct</h2>

<p>A type with no store: a value object, a request body, a configuration.</p>

<pre><code>(struct Address
  (field Line1 string)
  (field Line2 *string)
  (field City string)
  (field Postcode string))
</code></pre>

<h2 id="llm">llm</h2>

<p>Something you want written but cannot describe as a type:</p>

<pre><code>(llm "A middleware that rejects requests without a valid API key,
      reading keys from the environment.")
</code></pre>

<p>It becomes a task in the list with your words as the brief. Nothing is
generated for it, so this is for work you intend to hand to an assistant.</p>

<h2 id="config">config</h2>

<p>House style. It changes how the code looks, never what it does.</p>

<pre><code>(config
  (json-tags snake)       ; snake | camel | none
  (context-first yes)     ; put ctx context.Context first in every method
  (layout flat))          ; flat | internal (packages under internal/)
</code></pre>

<p>Put it anywhere in the spec. The defaults are shown above.</p>

<h2 id="policy">policy</h2>

<p>Your preferences about which implementation to use. Left alone, tilegen
picks for you and tells you why. When the choice is not yours to make — a
client's approved-library list, a team that only knows one database — say so:</p>

<pre><code>(policy
  (prefer sqlite-sqlc
    (strength required)
    (source client "single file, no database server")))
</code></pre>

<p><code>strength</code> is <code>required</code> (nothing else may be used),
<code>strong</code> (use it unless it is impossible), or <code>weak</code> (use
it when it is a close call). <code>source</code> is who wants it and why; it
shows up in the explanation, so a decision made months ago still has its reason
attached.</p>

<p>The other direction works too:</p>

<pre><code>(policy
  (avoid postgres-pgx "we standardised on generated queries"))
</code></pre>

<h2 id="whatyouget">What you get</h2>

<p>A zip with a complete Go module:</p>

<table>
<tr><td><code>go.mod</code></td><td>your module and its dependencies</td></tr>
<tr><td><code>&lt;package&gt;/&lt;package&gt;_gen.go</code></td><td>types, interfaces, enums</td></tr>
<tr><td><code>&lt;package&gt;/&lt;impl&gt;_store.go</code></td><td>the store implementation, with the methods left for you</td></tr>
<tr><td><code>&lt;package&gt;/http_gen.go</code></td><td>routing and handlers</td></tr>
<tr><td><code>&lt;package&gt;/http.go</code></td><td>validation and error mapping, left for you</td></tr>
<tr><td><code>&lt;package&gt;/events_gen.go</code></td><td>events and the bus</td></tr>
<tr><td><code>db/schema.sql</code>, <code>db/query.sql</code></td><td>for durable stores</td></tr>
<tr><td><code>tilegen.tasks.json</code></td><td>every method left to write, with its contract and notes</td></tr>
<tr><td><code>GENERATED.md</code></td><td>what to run next</td></tr>
</table>

<p>Then:</p>

<pre><code>unzip project.zip &amp;&amp; cd project
sqlc generate     # only if the project has a db/ folder
go mod tidy
go build ./...
</code></pre>

<p>Files ending <code>_gen.go</code> are rewritten whenever you regenerate, so do
not edit them. Everything else is yours and is never overwritten.</p>

<h2 id="holes">The methods left for you</h2>

<p>An unwritten method looks like this:</p>

<pre><code>// Get returns the Order with the given ID.
func (s *PostgresOrderStore) Get(ctx context.Context, id uuid.UUID) (*Order, error) {
	panic("tilegen:hole orders.PostgresOrderStore.Get")
}
</code></pre>

<p>The project compiles with these in place, so you can fill them in any order.
<code>tilegen.tasks.json</code> lists each one with its exact signature, what it
should do, and any constraints you wrote:</p>

<pre><code>{
  "id": "orders.PostgresOrderStore.Get",
  "file": "orders/postgres_order_store.go",
  "contract": "Get(ctx context.Context, id uuid.UUID) (*Order, error)",
  "intent": "Call the sqlc-generated s.q.GetOrder and convert the db.Order to *Order.",
  "constraints": ["Saving the same email twice must not create two customers."]
}
</code></pre>

<p>Write them yourself, or hand the file to a coding assistant. If you install
the command line tool, <code>tilegen prompt</code> writes a complete, ready-to-paste
brief for any one of them.</p>

<h2 id="cli">The command line</h2>

<p>This page is the quick way in. For a project you keep, install it:</p>

<pre><code>go install github.com/vinodhalaharvi/tilegen@latest
</code></pre>

<table>
<tr><td><code>tilegen spec/</code></td><td>generate, or update after a spec change</td></tr>
<tr><td><code>tilegen explain spec/</code></td><td>what it picked for each store, and why</td></tr>
<tr><td><code>tilegen check spec/</code></td><td>fail if the code is out of date or methods are missing — for CI</td></tr>
<tr><td><code>tilegen prompt spec/</code></td><td>a ready-to-paste brief for a method</td></tr>
<tr><td><code>tilegen import ./existing</code></td><td>lift a project you already have into a spec</td></tr>
</table>

<p>The important difference: run it again after editing the spec and it updates
in place. New methods are appended, methods you have written are never touched,
and anything the spec no longer describes is reported rather than deleted.</p>

<h2 id="api">Using it from a script</h2>

<pre><code>curl -X POST https://your-service/generate \
  --data-binary @spec.sexp -o project.zip

curl -X POST https://your-service/explain \
  --data-binary @spec.sexp | jq .
</code></pre>

<p>Send the spec as the body, or as JSON with <code>spec</code>,
<code>config</code>, <code>policy</code> and <code>name</code> fields. Errors
come back as JSON with a message that points at the line.</p>

<div class="note">This service generates and returns files. It does not run
anything: no build, no database, no network calls out. What you download is
yours to inspect before you run it.</div>

<h2 id="examples">Three complete examples</h2>

<h3>A link shortener</h3>
<pre><code>(project shortener
  (module example.com/shortener)
  (go 1.22))

(package links
  (entity Link
    (field ID string)                ; the short code
    (field Target string)
    (field CreatedAt time.Time)
    (field Hits int64)
    (store get save delete count
      (exists-by Target)
      (durable)
      (constraint "A code must never be reused for a different target.")))

  (http
    (route GET    "/links"      (list Link))
    (route POST   "/links"      (save Link))
    (route GET    "/links/{id}" (get Link))
    (route DELETE "/links/{id}" (delete Link)))

  (events
    (event LinkFollowed (field Code string) (field At time.Time))))
</code></pre>
<p><button class="try">try this ↑</button></p>

<h3>A support desk</h3>
<pre><code>(project desk
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
    (constraint "Never assign to an agent with no email.")))
</code></pre>
<p><button class="try">try this ↑</button></p>

<h3>An inventory service, with preferences</h3>
<pre><code>(project inventory
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
    (store list save
      (list-by SKU)
      (count-by Kind)
      (durable)))

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
    (source ops "it ships to a single box; no database server")))
</code></pre>
<p><button class="try">try this ↑</button></p>

<h2 id="errors">When something is wrong</h2>

<p>Mistakes are caught before anything is generated, and the message says where
and usually what you meant:</p>

<pre><code>spec.sexp:7:5: entity Order needs an ID field
spec.sexp:12:3: unknown form (entiy Customer ...) (did you mean entity?)
spec.sexp:19:7: (get Order) needs {id} in the path, since it acts on one Order
spec.sexp:24:3: duplicate type "Order"
spec.sexp:31:9: packages orders and billing import each other
</code></pre>

<p>If a form you wrote is not one tilegen knows, and is not a near-miss for one
that is, it becomes a task in the list rather than an error — so you can sketch
in your own words and decide later.</p>
`
