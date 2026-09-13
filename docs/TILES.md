# Tiles

A tile is one rule of the compiler: a pattern it matches, a thing it emits,
and a declared cost. Covering the whole spec tree with tiles is the
`select` pass, and it is the same technique an instruction selector uses to
cover an expression tree with machine instructions.

This is the reference: the full registry as it stands, what each field of a
tile means, worked selections with real numbers, and how to write one.

- [The two questions a tile answers](#the-two-questions-a-tile-answers)
- [The registry](#the-registry)
- [Anatomy of a tile](#anatomy-of-a-tile)
- [Capabilities and requirements](#capabilities-and-requirements)
- [The tile catalogue](#the-tile-catalogue)
- [Worked example 1: the config decides](#worked-example-1-the-config-decides)
- [Worked example 2: cost decides, per store](#worked-example-2-cost-decides-per-store)
- [Worked example 3: legality decides](#worked-example-3-legality-decides)
- [Worked example 4: the policy decides](#worked-example-4-the-policy-decides)
- [The cost model](#the-cost-model)
- [Chain rules](#chain-rules)
- [Writing a tile](#writing-a-tile)
- [Debugging selection](#debugging-selection)

## The two questions a tile answers

Structural tiles answer **"what Go does this form become?"**. `(enum Status
pending paid)` becomes a string type with constants, `Valid()` and
`ParseStatus()`. There is no competition: one form, one tile, one answer.

Capability tiles answer **"which implementation should this need get?"**. A
store has to be *some* store; several tiles offer to be one, and they
compete. The spec says what a need requires, tiles say what they cost and
when they are illegal, and the cheapest legal tile wins.

Both kinds live in one registry and are listed by one command, because the
distinction is about whether anything else offers the same capability, not
about the machinery.

## The registry

```sh
tilegen tiles
```

```
PASS        TILE            PRODUCES       COST                          NEEDS           WHAT IT DOES
expand      entity          struct, store                                                splits an entity into a struct, a store interface and an impl
concretize  package-layout  package                                                      places a package per (layout ...)
concretize  json-tag        field                                                        adds json tags per (json-tags ...)
concretize  context-first   method                                                       prepends ctx context.Context per (context-first ...)
concretize  backend         impl                                                         records the storage backend chosen by (storage ...)
concretize  implement       implement                                                    keeps implement dependencies free of json tags
select      project         gomod                                                        writes go.mod and resolves imports
select      package         go/file                                                      collects a package into its generated file
select      impl            store                                                        implements a store through the registered backend
select      struct          go/struct      llm 0                                         a struct, as written
select      interface       go/interface   llm 0                                         an interface, as written
select      implement       go/file        llm 5, maint 2                                implements any interface: struct, constructor, stubs, check
select      enum            go/type        llm 0, maint 0                                a string type with constants, Valid() and Parse()
select      llm             llm/task       llm 8, unc 6                                  an explicit hole: pure intent for the LLM
select      events          event-bus      llm 0, maint 0, dep 0                         event structs, a typed Bus interface, and an in-process LocalBus
select      http            go/file        llm 2, maint 1, dep 0                         a Handler and ServeMux over the package's stores
select      catch-all       llm/task       llm 10, unc 10                                covers anything no other tile covers: the LLM as the tile of last resort
select      local-bus       event-bus      llm 0, maint 0, dep 0, run 1                  in-process bus: synchronous delivery, ordered, errors joined
select      nats-bus        event-bus      llm 5, maint 3, dep 4, run 2                  NATS bus: events cross processes and survive a restart (JetStream)
select      memory          store          llm 2, maint 1, dep 0, run 1                  in-memory store: a map guarded by a mutex
select      postgres-pgx    store          llm 7, maint 5, dep 1, run 1                  postgres via pgx: tilegen writes the schema, the LLM writes the SQL
select      postgres-sqlc   store          llm 3, maint 2, dep 3, run 1  sqlc (missing)  postgres via sqlc: tilegen writes SQL, sqlc writes the Go, the LLM maps rows
select      sqlite-sqlc     store          llm 3, maint 2, dep 1, run 1  sqlc (missing)  sqlite via sqlc: one file, no server; uuid and time are TEXT
select      row-mapper      domain                                                       converts sqlc's row types to domain types, by hand

24 tile(s).
capability event-bus  offered by local-bus, nats-bus
capability store      offered by memory, postgres-pgx, postgres-sqlc, sqlite-sqlc
Each need gets the cheapest legal tile; see tilegen explain.
```

`NEEDS` is checked against your machine as it prints: `sqlc (missing)` here
means the two sqlc-backed tiles are still legal and still scored, but you
cannot complete a project that chooses one until you install it.

A bare argument filters by name, pass or capability, which is how you ask a
narrower question:

```sh
tilegen tiles store       # only the storage tiles
tilegen tiles select      # only the select pass
tilegen tiles -json       # the same, for programs
```

## Anatomy of a tile

`-sexp` prints the registry as S-expressions — the shape tiles will have
once they can be written as data rather than Go:

```sh
tilegen tiles -sexp postgres-sqlc
```

```lisp
(tile postgres-sqlc
  (pass select)
  (doc "postgres via sqlc: tilegen writes SQL, sqlc writes the Go, the LLM maps rows")
  (covers (alias postgres))
  (offers store)
  (form db-rows)
  (requires (tool sqlc))
  (cost
    (llm-work 3
      (source derived "the LLM writes only row-to-domain mapping; sqlc writes the queries"))
    (maintenance 2)
    (dependency 3 (source derived "pgx, plus sqlc as a build-time tool"))
    (runtime 1)))
```

| Field | Meaning |
|---|---|
| `pass` | which pass it runs in: `expand`, `concretize` or `select` |
| `covers` | the pattern it matches. `(alias postgres)` is the name a config may use; `?x` binds one node, `?x...` binds the rest, `_` matches anything |
| `produces` | the target form it emits: `go/file`, `gomod`, `sql/query`, `llm/task` |
| `offers` | the capability it competes for, if any. A tile with `offers` is scored against its rivals |
| `form` | the shape of what it hands back. `db-rows` is sqlc's own row types, which something must convert |
| `requires` | external tools it needs, checked on your `PATH` |
| `illegal-when` | requirements it cannot serve, each with the reason a user will read |
| `cost` | what it costs on each dimension, optionally with provenance |
| `cost-rule` | for chains, whose cost depends on the entity rather than being fixed |

Two more, from other tiles. `memory` declares when it must not be chosen:

```lisp
(tile memory
  (pass select)
  (doc "in-memory store: a map guarded by a mutex")
  (covers (alias memory))
  (offers store)
  (illegal-when (durable) "an in-memory map loses its data on restart")
  (cost (llm-work 2) (maintenance 1) (dependency 0) (runtime 1)))
```

And `catch-all` is the guarantee that selection always terminates, the way a
compiler guarantees coverage with a one-node tile per operator:

```lisp
(tile catch-all
  (pass select)
  (doc "covers anything no other tile covers: the LLM as the tile of last resort")
  (covers _)
  (produces llm/task)
  (cost (llm-work 10) (uncertainty 10)))
```

It is deliberately the most expensive thing in the registry. A spec form
nothing else understands does not fail; it becomes a described hole, and its
cost says plainly that this is the worst way to get one. `-strict` turns it
into an error instead, which is what you want in CI.

## Capabilities and requirements

A capability is a job several tiles can do. There are two:

| Capability | What it does | Requirements it understands |
|---|---|---|
| `store` | persists an entity's values | `durable` |
| `event-bus` | delivers a package's events to handlers | `durable`, `cross-process` |

Requirements are **one shared vocabulary**. `(durable)` means the same
thing to a store and to a bus, and each tile says for itself which ones it
cannot serve. You write them as bare forms inside the node that has the
need:

```lisp
(entity Order
  (store get list save
    (durable)))            ; this data must survive a restart

(events
  (cross-process)          ; handlers live in other processes
  (event JobStarted (field JobID int64)))
```

Needs get stable IDs — `orders.OrderStore`, `orders.Bus` — and those IDs are
the keys in `tilegen.lock`.

## The tile catalogue

### Structural tiles

| Tile | Pass | Covers | Produces |
|---|---|---|---|
| `entity` | expand | `(entity ?name ?items...)` | a struct, a `NameStore` interface, and an `impl` |
| `package-layout` | concretize | a package | placement per `(layout ...)` |
| `json-tag` | concretize | a field | the tag per `(json-tags ...)` |
| `context-first` | concretize | a method | a leading `ctx context.Context` per `(context-first ...)` |
| `backend` | concretize | an `impl` | the backend recorded per `(storage ...)` |
| `project` | select | the project | `go.mod`, with imports resolved |
| `package` | select | a package | its generated file |
| `struct` | select | `(struct ...)` | the struct, as written |
| `interface` | select | `(interface ...)` | the interface, as written |
| `enum` | select | `(enum ...)` | a string type, constants, `Values`, `Valid()`, `ParseX()` |
| `implement` | select | `(implement ...)` | struct, constructor, stubs, and the compile-time check |
| `impl` | select | an expanded entity's `impl` | the store, through whichever backend won |
| `http` | select | `(http ...)` | a `Handler` and `ServeMux` over the package's stores |
| `events` | select | `(events ...)` | event structs and a typed `Bus` interface |
| `llm` | select | `(llm "...")` | an explicit hole |
| `catch-all` | select | anything | an LLM task |

Note `struct`, `interface` and `enum` cost `llm 0`: they are complete. No
hole, no prompt, nothing for a model to get wrong. The costs on `implement`
(`llm 5`) and `llm` (`llm 8, unc 6`) are the honest admission that those
forms end in someone writing code.

### Store tiles

| Tile | Config alias | Cost | Illegal when | Needs |
|---|---|---|---|---|
| `memory` | `memory` | llm 2, maint 1, dep 0, run 1 | `durable` — a map loses its data on restart | |
| `postgres-sqlc` | `postgres` | llm 3, maint 2, dep 3, run 1 | | sqlc |
| `postgres-pgx` | `pgx` | llm 7, maint 5, dep 1, run 1 | | |
| `sqlite-sqlc` | `sqlite` | llm 3, maint 2, dep 1, run 1 | `cross-process` — a sqlite file serves one process | sqlc |

The split between the two postgres tiles is the clearest illustration of
what the cost model is for. `postgres-sqlc` writes the SQL and lets sqlc
generate the Go, so the LLM only maps rows: cheap in `llm-work`, expensive
in `dependency` because it adds a build-time tool. `postgres-pgx` writes
only the schema and leaves every query to the LLM: the reverse trade.
Neither is better in the abstract, which is why the choice is priced rather
than hardcoded.

### Event bus tiles

| Tile | Config alias | Cost | Illegal when |
|---|---|---|---|
| `local-bus` | `local` | llm 0, maint 0, dep 0, run 1 | `cross-process`, `durable` |
| `nats-bus` | `nats` | llm 5, maint 3, dep 4, run 2 | |

```lisp
(tile local-bus
  (pass select)
  (doc "in-process bus: synchronous delivery, ordered, errors joined")
  (covers (alias local))
  (offers event-bus)
  (illegal-when (cross-process) "an in-process bus only reaches handlers in this program")
  (illegal-when (durable) "an in-process bus loses undelivered events on restart")
  (cost (llm-work 0) (maintenance 0) (dependency 0) (runtime 1)))
```

`local-bus` scores 1, the cheapest thing in the registry, and is chosen for
every package whose events stay in the process. It has exactly two ways to
be wrong, and it declares both.

### Chain tiles

| Tile | Converts | Cost rule |
|---|---|---|
| `row-mapper` | `db-rows` → `domain` | 1 per entity, +1 per field the dialect stores as text, +1 per nullable field, +2 per JSON field |

## Worked example 1: the config decides

`examples/shop/config.sexp` says `(storage memory)`, so there is nothing to
choose. `explain` still shows the ranking, so you can see what you turned
down:

```sh
tilegen explain -config examples/shop/config.sexp -out out/shop examples/shop/spec.sexp
```

```
orders.OrderStore   (store)   needs: none   chosen by: config
  chosen  memory          score 12   llm 2·4 + maint 1·3 + dep 0·1 + run 1·1
          postgres-sqlc   score 40   llm 3·4 + maint 2·3 + dep 3·1 + run 1·1 = 22
                                    + row-mapper 18 (1 entity, 1 parsed field, 1 nullable field)
          postgres-pgx    score 45   llm 7·4 + maint 5·3 + dep 1·1 + run 1·1
          sqlite-sqlc     score 53   llm 3·4 + maint 2·3 + dep 1·1 + run 1·1 = 20
                                    + row-mapper 33 (1 entity, 4 parsed fields, 1 nullable field)

orders.Bus   (event-bus)   needs: none   chosen by: lock
  chosen  local-bus       score 1    llm 0·4 + maint 0·3 + dep 0·1 + run 1·1
          nats-bus        score 35   llm 5·4 + maint 3·3 + dep 4·1 + run 2·1
```

`chosen by:` is the field to read first. It is one of:

| Value | Meaning |
|---|---|
| `config` | `(storage ...)` or `(events ...)` named this tile |
| `lock` | a previous run pinned it in `tilegen.lock`; `-reselect` ignores that |
| `auto` | the cost model chose it |
| `policy` | a preference overturned the cheapest |

Naming a tile that is illegal for some node is an error at that node, not a
silent fallback:

```
tilegen: spec.sexp:2:41: the config chose memory, which is illegal for b.XStore:
an in-memory map loses its data on restart;
legal: sqlite-sqlc (score 27), postgres-sqlc (score 29), postgres-pgx (score 45), or use auto
```

## Worked example 2: cost decides, per store

`examples/auto` is three stores with different needs in one project, under
`(storage auto)`:

```lisp
(package orders
  (entity Order
    (field ID uuid.UUID) (field TotalCents int64) (field PlacedAt time.Time)
    (store get list save (durable))))          ; must survive a restart

(package sessions
  (entity Session
    (field ID string) (field UserEmail string) (field ExpiresAt time.Time)
    (store get save delete)))                  ; scratch data: may live in memory

(package profiles
  (enum Plan free pro team)
  (entity Profile
    (field ID uuid.UUID) (field Email string)
    (field Plan Plan)                          ; enum: sqlc hands back a string to parse
    (field Nickname *string)                   ; nullable: sqlc hands back pgtype.Text
    (field AvatarURL *string)
    (field DeletedAt *time.Time)
    (store get save (durable))))
```

```sh
tilegen explain examples/auto
```

```
orders.OrderStore   (store)   needs: durable   chosen by: auto
  chosen  postgres-sqlc   score 29   llm 3·4 + maint 2·3 + dep 3·1 + run 1·1 = 22
                                    + row-mapper 7 (1 entity)
          sqlite-sqlc     score 38   llm 3·4 + maint 2·3 + dep 1·1 + run 1·1 = 20
                                    + row-mapper 18 (1 entity, 2 parsed fields)
          postgres-pgx    score 45   llm 7·4 + maint 5·3 + dep 1·1 + run 1·1
  illegal memory          an in-memory map loses its data on restart

profiles.ProfileStore   (store)   needs: durable   chosen by: auto
  chosen  postgres-pgx    score 45   llm 7·4 + maint 5·3 + dep 1·1 + run 1·1
          postgres-sqlc   score 51   llm 3·4 + maint 2·3 + dep 3·1 + run 1·1 = 22
                                    + row-mapper 29 (1 entity, 1 parsed field, 3 nullable fields)
          sqlite-sqlc     score 60   llm 3·4 + maint 2·3 + dep 1·1 + run 1·1 = 20
                                    + row-mapper 40 (1 entity, 3 parsed fields, 3 nullable fields)
  illegal memory          an in-memory map loses its data on restart

sessions.SessionStore   (store)   needs: none   chosen by: auto
  chosen  memory          score 12   llm 2·4 + maint 1·3 + dep 0·1 + run 1·1
          ...
```

Three stores, three different backends, one spec and no config change:

- **sessions** needs nothing, so the cheapest thing in the registry wins.
- **orders** needs durability, which makes `memory` illegal before anything
  is scored. Its three fields map cleanly onto postgres types, so the mapper
  is trivial (`row-mapper 7`) and sqlc wins.
- **profiles** needs durability too, but it has an enum and three nullable
  fields. sqlc hands those back as strings and `pgtype` wrappers, so the
  mapper costs 29 and drags sqlc's total to 51 — past pgx's flat 45. The
  same tile that won for `orders` loses here, for a reason you can read.

That last flip is the whole argument for costing chains rather than
backends. Greedy choice would be locally right and globally wrong.

## Worked example 3: legality decides

Legality is checked before cost, always, and depends only on the spec — never
on a policy or a price. A bus whose handlers live in other processes:

```lisp
(package fleet
  (events
    (cross-process)
    (event JobStarted (field JobID int64))))
(package local
  (events
    (event CacheWarmed (field Key string))))
```

```
fleet.Bus   (event-bus)   needs: cross-process   chosen by: auto
  chosen  nats-bus        score 35   llm 5·4 + maint 3·3 + dep 4·1 + run 2·1
  illegal local-bus       an in-process bus only reaches handlers in this program

local.Bus   (event-bus)   needs: none   chosen by: auto
  chosen  local-bus       score 1    llm 0·4 + maint 0·3 + dep 0·1 + run 1·1
          nats-bus        score 35   llm 5·4 + maint 3·3 + dep 4·1 + run 2·1
```

`local-bus` is 35× cheaper and still loses, because it is not scored at all.
The reason printed is the string the tile itself declared, which is why
those strings are written for the person reading them.

## Worked example 4: the policy decides

Costs say what a tile is worth technically. A policy says what your team
wants, and the two are not the same thing: a tile that loses on every
engineering measure can still be right because a client requires it or your
team knows it. So preferences live in a policy file, with who holds them and
how strongly, and tiles declare only technical costs.

```lisp
(policy
  (prefer sqlite-sqlc
    (strength required)
    (source client "one file, no server")))
```

```sh
tilegen explain -policy sqlite-policy.sexp -reselect examples/auto
```

```
weights: llm-work 4, maintenance 3, dependency 1, runtime 1, uncertainty 5
prefer: sqlite-sqlc (required; source: client, one file, no server)

orders.OrderStore   (store)   needs: durable   chosen by: auto
  chosen  sqlite-sqlc     score 38   llm 3·4 + maint 2·3 + dep 1·1 + run 1·1 = 20
                                    + row-mapper 18 (1 entity, 2 parsed fields)
  illegal memory          the policy requires sqlite-sqlc (source: client, one file, no server)
  illegal postgres-pgx    the policy requires sqlite-sqlc (source: client, one file, no server)
  illegal postgres-sqlc   the policy requires sqlite-sqlc (source: client, one file, no server)
  policy: cheapest, and preferred [source: client, one file, no server]
```

`required` is a constraint rather than a discount: every other tile becomes
illegal, carrying your stated source as its reason.

| Strength | Effect |
|---|---|
| `required` | every other tile becomes illegal, with the source as the reason |
| `strong` | wins over any legal candidate, however much cheaper |
| `weak` | wins ties and anything within `(margin N)`; what a bare `(prefer X)` means |

`(avoid pgx "we standardised on sqlc")` is the mirror image: never chosen.
A tile is never removed from the registry, only ruled out for one run, so
another team's policy can make it the winner again.

Read the output of `explain` under a policy as an architecture decision
record: the technical ranking, what overturned it, who asked for that, and
where each number came from.

## The cost model

Every tile declares what it costs on up to five dimensions. A policy prices
them with weights; the score is the dot product.

| Dimension | What it measures |
|---|---|
| `llm-work` | how much a model or a person still has to write |
| `maintenance` | how much there is to keep working afterwards |
| `dependency` | what it drags into the project |
| `runtime` | what it costs when the program runs |
| `uncertainty` | how unsure we are that it will work at all |

The default weights, which `examples/auto/policy.sexp` also states
explicitly:

```lisp
(weights (llm-work 4) (maintenance 3) (dependency 1) (runtime 1) (uncertainty 5))
```

So `memory`'s score is arithmetic you can check by hand:

```
llm 2·4 + maint 1·3 + dep 0·1 + run 1·1  =  8 + 3 + 0 + 1  =  12
```

Weights are the knob that matters. A team with plenty of engineers and no
appetite for dependencies raises `dependency`; a team leaning hard on an LLM
raises `llm-work`. The same spec then yields a different architecture with
no tile edited and no spec change.

Cost numbers carry provenance so an author's guess is not mistaken for a
measurement:

```
cost postgres-pgx llm-work: 7 (derived: the LLM writes every query and scan by hand)
```

`(source derived "...")`, `measured`, `user`, or nothing at all for a
built-in default. Replacing declared LLM costs with measured ones — by
running `fill` for real and logging every attempt — is what v0.4 is for.

Four properties hold of the search, and each has a test:

| Property | What it means |
|---|---|
| Totality | every need is covered, or reported with every rejection and its reason |
| Determinism | same spec, policy and lock ⇒ same covering; ties break by tile name |
| Legality before cost | an illegal tile is never scored, however cheap |
| Optimality | the chosen covering is cheapest under the weights, checked against brute-force enumeration |

## Chain rules

A backend's output has a *form*. `memory` and `postgres-pgx` hand back your
domain types directly. sqlc hands back its own row types (`db.Order`), so
something must convert them — and that conversion is code someone writes,
so it has to be priced. Converters are tiles too:

```lisp
(tile row-mapper
  (pass select)
  (doc "converts sqlc's row types to domain types, by hand")
  (covers (convert db-rows domain))
  (produces domain)
  (converts db-rows domain)
  (cost-rule "1 per entity, +1 per field the dialect stores as text (an enum always; uuid and time under sqlite), +1 per nullable field, +2 per JSON field"))
```

Selection adds the cheapest chain from a tile's form to `domain` onto that
tile's own cost. The rule counts *units*, then prices them as
`llm-work = units` and `maintenance = ⌈units/2⌉`:

| Entity | Units | llm·4 + maint·3 | Shown as |
|---|---|---|---|
| 1 plain entity | 1 | 4 + 3 | `row-mapper 7 (1 entity)` |
| + 2 text-stored fields | 3 | 12 + 6 | `row-mapper 18 (1 entity, 2 parsed fields)` |
| + 1 enum, 3 nullable | 5 | 20 + 9 | `row-mapper 29 (1 entity, 1 parsed field, 3 nullable fields)` |
| + 3 text-stored, 3 nullable | 7 | 28 + 12 | `row-mapper 40 (1 entity, 3 parsed fields, 3 nullable fields)` |

The cost follows the **dialect**, which is what makes the comparison honest.
Postgres has a `uuid` type and a `timestamptz`; sqlite stores both as TEXT.
So the identical entity costs sqlite's mapper more, because every such field
is parsed by hand. That is why `orders` shows `row-mapper 7` under postgres
and `row-mapper 18` under sqlite.

A tile whose form no chain converts to `domain` is illegal — which is how a
backend that produces something nothing can consume is caught at selection
rather than at compile time.

## Writing a tile

### A pass tile

A pattern, a function, and registry metadata:

```go
{Name: "enum", Pattern: Pat("(enum ?name ?items...)"), Then: selectEnum,
	Produces: "go/type", Doc: "a string type with constants, Valid() and Parse()",
	Cost: Cost{{"llm-work", 0}, {"maintenance", 0}}},
```

`Then` returns target forms and calls `m.Sub(...)` on any leaf it wants
other tiles to cover. Put more specific patterns before general ones: tiles
are tried biggest-first, which is maximal munch.

### A storage backend

A registered tile in its own file, like a `database/sql` driver. Config,
validation, the store tile, GitHub topics, starter files and `-git` all ask
the registry, so adding one changes nothing else:

```go
func init() {
	registerStoreBackend(&Backend{
		Name:        "memory",
		Tile:        "memory",
		Doc:         "in-memory store: a map guarded by a mutex",
		Cost:        Cost{{Dim: "llm-work", Value: 2}, {Dim: "maintenance", Value: 1},
		                  {Dim: "dependency", Value: 0}, {Dim: "runtime", Value: 1}},
		IllegalWhen: map[string]string{"durable": "an in-memory map loses its data on restart"},
		Implement: func(in StoreInput) (StoreParts, error) { ... },
	})
}
```

Write the `IllegalWhen` strings for the person who will read them in
`explain`. They are the whole explanation of a rejection, and they end up in
architecture discussions.

### A SQL backend

A SQL backend is a `Dialect` plus a registered tile. The query shapes are
shared, so a database only says how it spells things:

```go
var SQLite = &Dialect{
	Name: "sqlite", SQLPackage: "database/sql",
	Types: map[string]string{"int64": "INTEGER", "uuid.UUID": "TEXT", "time.Time": "TEXT", ...},
	Placeholder: func(i int) string { return fmt.Sprintf("?%d", i+1) },
	Upsert: ..., Enum: ..., Overrides: ...,
}
```

`sqlite-sqlc` is exactly that: the same shape as `postgres-sqlc`, writing
`INTEGER PRIMARY KEY` and `?1` placeholders, with `engine: "sqlite"` in
`sqlc.yaml`. Because the mapper's cost reads the dialect, declaring the type
map is also what makes its cost honest.

### A package-level form

A new top-level form is a registered tile in its own file too. Its rule
joins a pass just before the catch-all, and it validates its own form. The
event bus is built this way, and nothing in the core names it:

```go
func init() {
	RegisterPackageTile(&PackageTile{
		Form: "events", Pass: Select,
		Rule: Rule{Name: "events", Pattern: Pat("(events ?items...)"), Then: selectEvents,
			Produces: "event-bus",
			Doc: "event structs, a typed Bus interface, and an in-process LocalBus"},
		Validate: validateEvents,
	})
}
```

`make check` runs a registry consistency check before anything else, so a
tile that claims a capability nobody registered, or collides with an
existing name, fails immediately rather than at the first spec that hits it.

> If you keep giving an LLM the same instructions, that convention is a tile
> waiting to be written.

## Debugging selection

```sh
tilegen tiles                       # is my tile registered at all?
tilegen tiles mytile -sexp          # did it register the pattern and cost I meant?
tilegen explain SPEC                # why did this need get that tile?
tilegen explain -reselect SPEC      # ignore tilegen.lock and choose again
tilegen explain -json SPEC | jq .   # the same reasoning, structured
tilegen -dump SPEC                  # then read <out>/.tilegen/04-select.sexp
```

Common surprises, in the order they usually happen:

| Symptom | Cause |
|---|---|
| `chosen by: lock` and the tile is not what you expect | an earlier run pinned it; use `-reselect` |
| your tile never appears as a candidate | it does not `offer` the capability, or its pattern does not match — check `-sexp` |
| your tile is listed under `illegal` | an `illegal-when` matched a requirement in the spec, or a policy `required`/`avoid` ruled it out |
| a form became an LLM task you did not expect | nothing covered it and `catch-all` took it; run with `-strict` to make that an error |
| the score is not what you computed | check the weights line at the top of `explain`, and whether a chain was added to the tile's own cost |

Both the table and the JSON come from one computation, so they cannot
disagree; a test asserts they report the same problems.
