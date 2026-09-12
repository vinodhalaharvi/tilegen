# tilegen

[![ci](https://github.com/vinodhalaharvi/tilegen/actions/workflows/ci.yml/badge.svg)](https://github.com/vinodhalaharvi/tilegen/actions/workflows/ci.yml)

Compile a small S-expression spec into Go scaffolding by successive lowering
passes and **tree tiling** - the instruction-selection technique compilers
use - then hand the remaining, precisely described holes to an LLM.

Deterministic structure (module, types, interfaces, signatures, wiring, SQL)
is compiled. Only method bodies are left open, each with a contract, an
intent, and constraints.

```mermaid
flowchart LR
  S[spec.sexp] --> V[parse + validate]
  V --> E[expand]
  E --> C["concretize<br/>(config.sexp)"]
  C --> T["select<br/>(tiling)"]
  T --> M[emit]
  M --> A[go.mod]
  M --> B[*.go]
  M --> Q["db/*.sql + sqlc.yaml"]
  M --> L[tilegen.tasks.json]
```

Every pass maps S-expressions to S-expressions. Run with `-dump` to see each
stage in `<out>/.tilegen/` - the same idea as `GOSSAFUNC` in the Go compiler.

## Quick start

```sh
go install github.com/vinodhalaharvi/tilegen@latest
tilegen -config examples/shop/config.sexp -out out/shop -dump examples/shop/spec.sexp
cd out/shop && go mod tidy && go build ./...   # compiles; bodies panic until filled
```

Or from a clone: `make demo`, `make dump`, `make help`.

## The spec

```lisp
(project shop
  (module github.com/acme/shop)
  (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0))
  (package orders
    (entity Order
      (field ID uuid.UUID)
      (field PlacedAt time.Time)
      (store get list save delete
        (constraint "Save must be idempotent for the same ID.")))
    (interface Pricer
      (method Quote (params (o *Order)) (returns int64 error)))
    (llm "Write a Pricer that applies a 10% discount to orders over 100.00.")))
```

| Form | Meaning |
|---|---|
| `(module path)` `(go 1.22)` | go.mod basics, validated by `x/mod` |
| `(require (alias module version [importpath]))` | dependency + the qualifier types use |
| `(package name ...)` | a Go package |
| `(entity Name fields... (store ops...))` | struct + `NameStore` interface + implementation stub |
| `(struct Name (field N T (tag "...") (doc "..."))...)` | plain struct |
| `(interface Name (method N (params (n T)...) (returns T...)) (embed T))` | an interface |
| `(implement Iface (as Name) (field n T)...)` | an implementation of any interface in the package |
| `(enum Status pending paid shipped)` | a string type with constants, `StatusValues`, `Valid()`, `ParseStatus()` |
| `(events (event Name (field ...))...)` | event structs, a typed `Bus` interface, and an in-process `LocalBus` |
| `(llm "intent")` | explicit hole: pure intent, no structure yet |
| `(doc "...")` | doc comment on package, type, field, or method |

Types are Go syntax, parsed by `go/parser`. Quote them if they contain spaces
or parentheses: `(field F "func(int) error")`. Standard-library qualifiers
(`time`, `context`) need no declaration; anything else must come from a
`require` or a project package, or tilegen reports the exact line.
Every problem is reported at once, and likely typos come with the fix:
`(entiy Note ...)` gives `unknown form ... (did you mean entity?)`. A typo of
a known form is an error; only a genuinely unknown form goes to the LLM. Types from
another project package are written qualified (`orders.Order`); tilegen adds
the import and rejects import cycles between packages.

### Store operations

`(store ...)` inside an entity lists the methods of its `NameStore`
interface. Query methods are derived from fields, so they stay
deterministic down to the SQL: with `(storage postgres)` each one gets a
sqlc query, and the LLM only maps rows to domain types.

| Op | Method on `ShareStore` | sqlc query |
|---|---|---|
| `get` `list` `save` `delete` | `Get(id)`, `List()`, `Save(share)`, `Delete(id)` | `GetShare`, `ListShares`, ... |
| `count` | `Count() (int64, error)` | `CountShares` |
| `(list-by NoteID)` | `ListByNoteID(noteID) ([]*Share, error)` | `ListSharesByNoteID` |
| `(get-by Email)` | `GetByEmail(email) (*Share, error)` | `GetShareByEmail` |
| `(count-by NoteID)` | `CountByNoteID(noteID) (int64, error)` | `CountSharesByNoteID` |
| `(exists-by Email)` | `ExistsByEmail(email) (bool, error)` | `ExistsShareByEmail` |
| `(delete-by NoteID)` | `DeleteByNoteID(noteID) error` | `DeleteSharesByNoteID` |
| `(method Name (params ...) (returns ...) (doc ...))` | custom, as written | none: a hole for the LLM |

Add or remove ops at any time: reconciliation appends new stubs to your
implementation and never touches the methods you wrote.

### Events

```lisp
(events
  (doc "Bus carries sharing events between the parts of the app.")
  (event NoteShared (field NoteID int64) (field Email string))
  (event NoteUnshared (field NoteID int64)))
```

generates, in `<package>/events_gen.go` and with no holes, one struct per
event, a typed `Bus` interface (`PublishNoteShared(ctx, e) error`,
`OnNoteShared(h) func()` returning an unsubscribe func), and `LocalBus`, an
in-process implementation. `LocalBus` delivers synchronously: `Publish`
calls every handler in subscription order, in the publisher's goroutine, and
returns their errors joined, so there are no goroutines to leak and errors
reach the publisher. It is tested for ordering, unsubscribing, error
joining, subscribing from a handler, and concurrency (under `-race`).
Asynchronous or networked buses are future event-bus tiles.

### Implementing any interface

```lisp
(interface Mailer
  (method Send (params (to string) (subject string) (body string)) (returns error)))

(interface AuditLog
  (embed io.Writer)                ; embedded interfaces: project or standard library
  (method Flush (returns error)))

(implement Sharer (as EmailSharer)
  (doc "EmailSharer shares notes by email.")
  (field store ShareStore)         ; dependencies, injected by the constructor
  (field mailer Mailer)
  (constraint "Never share a note twice with the same email."))

(implement AuditLog (as FileAuditLog) (field path string))
```

`implement` works for any interface in the package, including generated
store interfaces. It scaffolds a struct, a `New...` constructor taking the
dependencies, one stub per method, and a `var _ Iface = (*Impl)(nil)`
check. It also writes tasks that list the dependencies and constraints.
Methods of embedded interfaces are included: project interfaces directly,
and standard-library ones like `io.Writer` from Go's own type checker, so no
method lists are hardcoded. Variadic last parameters are written
`(args "...any")`. Like every scaffolded file, it is reconciled as the
interface changes.

## Starting from an existing project

Most projects already exist. `tilegen import` lifts one into a spec:

```sh
tilegen import ~/go-projects/myapp
  wrote  spec/00-project.sexp
  wrote  spec/10-catalog.sexp
  wrote  spec/NOTES.md
imported 5 of 8 exported type(s) in 1 package(s); 3 skipped (see spec/NOTES.md)
next: tilegen check spec   # what the spec would regenerate, next to what you have
```

Everything it writes comes from the **type checker**
(`x/tools/go/packages`, `go/types`), never from names or source text:
structs and their field types and tags, interfaces and their method sets
(embedded and variadic included), defined string types with typed constants
(enums), which concrete type implements which interface
(`types.Implements`), and the module and its requires. If the project does
not type-check, import refuses and prints the errors, because a spec lifted
from types the compiler rejects would be quietly wrong; `-force` imports
the packages that do check.

What it cannot prove, it does not guess. Generic types, aliases, unexported
fields and anything else are listed in `NOTES.md` with the reason. Store
ops, needs and events are **not** inferred from method names, because a rule
like "a method called `GetByX` means `(get-by X)`" works on one codebase and
not the next. An interface that is really a store imports as a plain
`(interface ...)`, which is always correct; turn it into
`(entity ... (store get list save (durable)))` yourself when you want
tilegen to own it.

It is a starting point, not a round trip. The spec covers what it covers,
the rest stays ordinary Go, and `tilegen check` shows the difference.

## Specs across files

`SPEC` can be a directory. tilegen reads every `.sexp` file in it in name
order, and the **merge** pass links them into one project, like a linker:
one `(project ...)` header, plus any number of top-level `(package ...)`,
`(require ...)`, `(repo ...)` and `(config ...)` forms. Packages with the
same name merge, identical requires dedupe, and conflicts are reported with
both file positions. See `examples/shopdir`, which compiles to exactly the
same Go as `examples/shop/spec.sexp`.

## Git and GitHub

```lisp
(repo
  (github acme/shop)       ; or just shop: the owner is your `gh` login
  (visibility private)     ; private (default) | public | internal
  (description "...")
  (topics orders billing)  ; added to derived ones: go, golang, tilegen, ...
  (license mit))           ; mit (default) | none
```

The `(repo ...)` tile writes starter files, once and then yours:
`README.md`, `LICENSE`, `Makefile` and `.gitignore`. Without
`(module ...)`, the module path is derived from the repository.

```sh
tilegen -git -name myshop examples/shopdir          # repo and module follow -name
tilegen -git -dry-run -name myshop examples/shopdir # print the plan, write nothing
```

With `-git`, tilegen uses `<out>` as is if it is already a git repository.
If the GitHub repository exists, it clones it and generates into it, leaving
the changes for you to review and commit. If it does not, it runs
`git init`, generates, runs `go mod tidy` (and `sqlc generate` for postgres),
makes the first commit, creates the repository with `gh repo create`, and
adds topics. Without `(github ...)`, `-git` makes a local repository only.

## Workstation: worktrees and tmux

A `(workspace ...)` form says how tilegen runs on your machine. It never
changes generated code. Put the spec inside the repository it generates, and
one folder holds everything:

```lisp
(workspace
  (name myshop)                   ; default for -name
  (out ..)                        ; default for -out: this spec lives at spec/
  (worktrees                      ; checkouts at ../myshop.wt/<name>
    (worktree billing (branch feat/billing))
    (worktree pricing (branch feat/pricer)))
  (tmux
    (session myshop
      (window code    (dir .)            (run "$EDITOR ."))
      (window check   (dir .)            (run "make tidy check"))
      (window billing (worktree billing) (run "claude"))
      (window pricing (worktree pricing) (run "claude")))))
```

```sh
tilegen -git spec/        # generate into ..; create or clone the GitHub repo
tilegen up spec/          # create missing worktrees, open or attach the session
tilegen status spec/      # per checkout: changes, open holes, branch
tilegen down spec/        # close the session; -prune also removes clean worktrees
```

Each worktree is a full checkout on its own branch, so separate agents can
fill different packages' holes in parallel without touching each other's
files. `up` is safe to re-run: it reuses existing worktrees, recreates
missing ones from their branches, and adds windows you added to the spec.
Inside tmux it switches your client; outside, it attaches. `down -prune`
uses `git worktree remove`, which refuses to delete uncommitted work, and
branches are always kept. Plain generation never runs commands: worktrees,
tmux and `run` only happen when you type `up`.

## The config

Same spec, different config, different (equally valid) Go:

```lisp
(config
  (json-tags snake)     ; snake | camel | none
  (context-first yes)   ; prepend ctx context.Context to interface methods
  (storage auto)        ; auto (cheapest legal per store) | memory | postgres | pgx
  (layout flat))        ; flat | internal
```

## The passes

**expand** removes sugar and knows nothing about the config:

```lisp
(entity Order (field ID uuid.UUID) (store get))
=>
(struct Order (field ID uuid.UUID))
(interface OrderStore (method Get (params (id uuid.UUID)) (returns *Order error)))
(impl OrderStore (for Order) (ops get))
```

**concretize** makes every config decision explicit in the tree:

```lisp
(field PlacedAt time.Time)  =>  (field PlacedAt time.Time (tag "json:\"placed_at\""))
(params (id uuid.UUID))     =>  (params (ctx context.Context) (id uuid.UUID))
(impl OrderStore ...)       =>  (impl OrderStore ... (backend memory))
```

**select** is instruction selection proper. It covers the tree with tiles
whose outputs are target forms (`go/file`, `gomod`, `sql/query`, `llm/task`).
Tiles are tried biggest-first (maximal munch). The `impl` tile is the big
one: through the package symbol table it covers the interface and struct
too, and emits an implementation file, a `var _ I = (*T)(nil)` assertion, SQL,
and one task per method. The last tile matches anything: forms nothing else
covers become LLM tasks (or errors under `-strict`), just as a compiler
guarantees coverage with a one-node tile per operator.

## What is reused instead of written

| Job | Tool |
|---|---|
| go.mod create/merge | `golang.org/x/mod/modfile` |
| module path / version checks | `x/mod/module`, `x/mod/semver` |
| Go type grammar | `go/parser.ParseExpr` |
| Go AST, imports | `go/parser`, `x/tools/go/ast/astutil` |
| formatting, stdlib imports | `go/format`, `x/tools/imports` (the goimports engine gopls uses) |
| which stdlib packages exist | `go list std` |
| database access code | [sqlc](https://sqlc.dev), fed by the postgres tile |
| type checking the result | `go build`, `go vet` |

Everything tilegen itself implements is the reader, the matcher, the tiles,
and a thin text renderer: about 1,750 lines of code, not counting comments.

## Handing off to an LLM

```sh
tilegen prompt spec/                                   # list the open tasks
tilegen prompt spec/ sharing.EmailSharer.ShareNote     # one self-contained prompt
tilegen prompt spec/ -all                              # every prompt, separated by ---
```

To let an LLM fill the holes itself, tell tilegen which command to run, in
`workspace.sexp` (it is specific to your machine):

```lisp
(workspace
  (fill
    (command "claude -p")     ; any CLI that reads the prompt on stdin and prints the answer
    (retries 1)               ; re-ask once, with the compiler's errors
    (timeout "5m")))
```

```sh
tilegen fill spec/                  # every open hole
tilegen fill spec/ 'billing.*'      # one package; patterns use path.Match
tilegen fill -dry-run spec/         # what would be filled; calls nothing
```

For each hole, fill sends the prompt, takes the method from the reply,
checks it is exactly the method asked for with the same signature, splices
it in with go/ast, fixes imports, and builds. If the build fails, it puts
the hole back and asks again with the compiler's errors and the previous
answer. A hole it cannot fill stays a hole, so the project never ends up
broken. Drifted methods are fixed first; fill refuses to start if the
project has any other build error. Pair it with worktrees to fill packages
in parallel:

```lisp
(window billing (worktree billing) (run "tilegen fill spec/ 'billing.*'"))
```

A prompt holds everything an LLM needs and nothing it has to go looking for:
what to write (the exact signature, the hole's file and line), the intent and
constraints, the rules for the reply, and the full text of every file
involved, including sqlc's generated code for postgres stores. It is built
from the same plan as generation, so it reflects the current spec even
before you regenerate, and it writes nothing. Pipe it into any LLM CLI.

It also carries the **API surface** of the packages that task touches: the
exported declarations of each one, with doc comments, signatures only,
printed from `go/types`. So a postgres store task gets pgx's real
`ErrNoRows` and `Rows` rather than the model's memory of them. Scope is
narrow on purpose: only packages the task's files import, plus any named in
its intent (`map pgx.ErrNoRows to ErrNotFound`) and resolved through
go.mod, never the standard library, at most four per prompt and trimmed to
a budget. Results are cached under `<out>/.tilegen/api`. See what a package
contributes with:

```sh
tilegen api github.com/jackc/pgx/v5
```

`tilegen.tasks.json` lists every open hole with `file`, `line`, `symbol`,
`contract`, `intent`, `constraints`, and `context_files`, plus instructions.
Give it to your LLM together with the listed files. Then:

1. The LLM replaces `panic("tilegen:hole <id>")` bodies.
2. `go build ./... && go vet ./...` - the compile-time assertions reject any
   drift from the interfaces.
3. Re-run tilegen at any time. Holes are found by parsing the real files, so
   filled ones drop off the list and line numbers stay current.

## For agents: -json

The read-only commands print structured output with `-json`, so a program
does not have to parse tables meant for people:

```sh
tilegen explain -json spec/   # every need: chosen tile, score, candidates, rejections
tilegen plan -json spec/      # the levels, with each node's dependencies
tilegen tiles -json           # the registry: capabilities, offers, costs, legality
tilegen check -json spec/     # what would change, and whether that is a failure
```

```json
{
  "need": "orders.OrderStore",
  "capability": "store",
  "requirements": ["durable"],
  "chosen": "postgres-sqlc",
  "score": 29,
  "chosen_by": "auto",
  "candidates": [
    {"tile": "postgres-sqlc", "score": 29, "own": 22,
     "chain": [{"rule": "row-mapper", "score": 7, "detail": "1 entity"}]},
    {"tile": "postgres-pgx", "score": 45, "own": 45}
  ],
  "illegal": [{"tile": "memory", "reason": "an in-memory map loses its data on restart"}],
  "position": "spec/10-orders.sexp:12:7"
}
```

An agent can then ask the compiler what it would do, and why, rather than
deciding architecture itself or guessing at reasons. Both renderings come
from one computation, so they cannot disagree; a test checks that they
report the same problems. `check -json` still exits non-zero on failure, so
it works in CI either way.

## The plan graph

`tilegen plan` shows what a run makes and in what order, inferred from the
plan tilegen already builds. Nothing is declared and nothing is written:

```
$ tilegen plan spec/
level 1
  file   db/schema.sql   (postgres-sqlc)
  need   sessions.SessionStore   (memory: store, chosen by auto (score 12))
  task   sessions.MemorySessionStore.Get   Look up id in s.m while holding s.mu. ...
level 2
  tool   sqlc generate   (postgres-sqlc: run after tilegen, before building)
level 3
  file   internal/db/   (postgres-sqlc: written by sqlc generate)
level 4
  task   links.PostgresLinkStore.Get   Call the sqlc-generated s.q.GetLink ...
```

The levels carry real information: a postgres store's holes cannot be
filled until sqlc has produced the types they use, while a memory store's
can be filled straight away. Edges come from what the plan already knows,
which tile staged which file, what each task was given as context, and what
each covering delegated to, so there is nothing for a tile to declare and
nothing to get out of step.

```sh
tilegen plan spec/          # levels
tilegen plan -dot spec/ | dot -Tsvg -o plan.svg
```

A cycle is reported with the nodes involved rather than silently dropped.
Later work (filling independent holes in parallel, invalidating only what a
spec change touches, caching expensive fills) builds on this graph; today it
is a projection, so you can see how the compiler arrived.

## Checking in CI

```sh
tilegen check spec/                # fail on out-of-date files, drift, or open holes
tilegen check -allow-holes spec/   # while the LLM work is still in progress
```

`check` runs the same plan as generation and compares it with the disk,
writing nothing. It exits 1 if regenerating would change a file (someone
edited generated code, or the spec changed without regenerating), if a
method you implemented has drifted from the spec, or, unless
`-allow-holes` is set, while holes remain:

```
  stale    orders/orders_gen.go: differs from what the spec generates
  stub     Count to billing/memory_invoice_store.go: new in the spec
  holes    10 open (listed in tilegen.tasks.json)
tilegen: check failed: 2 file(s) out of date. Run `tilegen spec` to update, then fill the holes
```

Generation itself is plan-then-apply, like `terraform plan` and `apply`,
so the two can never disagree.

## Reconciliation: the spec changes, the code follows

Spec forms come and go, and every run brings the code in line. One rule:
**tilegen's own files follow the spec; anything you wrote is never changed.**

| The spec changes | tilegen does |
|---|---|
| a method is added | appends a stub with a hole to your file |
| a method's signature changes, stub untouched | updates the stub |
| a method's signature changes, you implemented it | reports **drift**; it becomes an LLM task |
| a method is removed, stub untouched | removes the stub |
| a method is removed, you implemented it | reports an **orphan**; your code stays |
| a package or storage backend is removed | deletes its generated files, and scaffolded files that are still pure scaffolding; orphans with your code are reported |

An untouched stub is a method whose body is still just the hole, so it holds
nothing of yours. There is no state file: generated files carry a
`Code generated by tilegen. DO NOT EDIT.` header and scaffolded files a
`Scaffolded by tilegen.` one, so each run finds its files on disk and
compares them with the spec. Nested modules (folders with their own
`go.mod`) are never touched. Drift and orphans also appear in
`tilegen.tasks.json`, so an LLM can finish the job.

`go.mod` is merged, never rewritten: versions are only raised, so
`go mod tidy` results survive regeneration.

## Choosing tiles

A node says what it **needs**; tiles say what they **offer**. Where more
than one tile offers a capability they compete, and the cheapest legal one
wins. Storage and event buses work the same way, through the same solver:

```lisp
(entity Order
  (store get list save
    (durable)))              ; this data must survive a restart

(events
  (cross-process)            ; handlers live in other processes
  (event JobStarted (field JobID int64)))
```

Requirements are one shared vocabulary: `(durable)` means the same for a
store and for a bus. Each tile declares when it cannot serve one:

```lisp
(tile local-bus
  (offers event-bus)
  (illegal-when (durable) "an in-process bus loses undelivered events on restart")
  (illegal-when (cross-process) "an in-process bus only reaches handlers in this program")
  (cost (llm-work 0) (maintenance 0) (dependency 0) (runtime 1)))
```

So the same spec gives you a `LocalBus` for a package whose events stay in
the process and a `NatsBus` for one whose do not:

```
$ tilegen explain spec.sexp
fleet.Bus   (event-bus)   needs: cross-process   chosen by: auto
  chosen  nats-bus        score 35   llm 5·4 + maint 3·3 + dep 4·1 + run 2·1
  illegal local-bus       an in-process bus only reaches handlers in this program

local.Bus   (event-bus)   needs: none   chosen by: auto
  chosen  local-bus       score 1    llm 0·4 + maint 0·3 + dep 0·1 + run 1·1
          nats-bus        score 35   llm 5·4 + maint 3·3 + dep 4·1 + run 2·1
```

Storage has three tiles: `memory`, `postgres-sqlc` (tilegen writes SQL, sqlc
writes the Go) and `postgres-pgx` (tilegen writes the schema, the LLM writes
the SQL). A config may name one for a capability, `(storage postgres)` or
`(events nats)`, and then it decides, but naming one that is illegal for
some node is an error at that node.

### The policy: what your team values

Tiles declare what they cost; a policy prices them and says what people
want. It lives beside the spec (any `.sexp` file in the folder) or in
`-policy FILE`:

```lisp
(policy
  (weights (llm-work 4) (maintenance 3) (dependency 1) (runtime 1) (uncertainty 5))

  (prefer postgres-gorm
    (strength required)                      ; required | strong | weak
    (source client "approved-library list, contract §4"))

  (margin 8)                                 ; how far behind a weak preference may be
  (avoid pgx "we standardised on sqlc"))     ; never choose it
```

Weights price every tile and chain rule, so the same spec gives different
teams different architectures with no tile edited.

Costs say what a tile is worth technically. Preferences say what people
want, and they are not the same thing: a tile that loses on every
engineering measure can still be right because a client requires it or a
team knows it. So preferences live in the policy, with **who holds them**
and **how strongly**, while tiles declare only technical costs, since a tile
cannot know your team.

| Strength | What it does |
|---|---|
| `required` | a constraint, not a cost: every other tile becomes illegal, with the source as the reason |
| `strong` | wins over any legal candidate, however much cheaper it is |
| `weak` | wins ties and anything within `(margin N)`; what a bare `(prefer X)` means |

`explain` then reads like an architecture decision record: the technical
ranking, what overturned it, who asked for that, and where each number came
from.

```
        postgres-sqlc   score 29   llm 3·4 + maint 2·3 + dep 3·1 + run 1·1 = 22
                                  + row-mapper 7 (1 entity)
chosen  postgres-pgx    score 45   llm 7·4 + maint 5·3 + dep 1·1 + run 1·1
  cost    postgres-pgx llm-work: 7 (derived: the LLM writes every query and scan by hand)
  policy: strongly preferred; cost alone would have picked postgres-sqlc (29 vs 45)
          [source: team, 9 of 11 engineers know pgx; nobody has used sqlc]
```

Cost numbers carry provenance, so an author's estimate is not mistaken for a
measurement: `(source derived "...")`, `measured`, `user`, or nothing for a
built-in default. A tile is never pruned from the registry, only for one
run: another team's policy can make it the winner again.

### How the covering is found

The cheapest covering is computed bottom-up, the way a compiler's
instruction selector does it: the cost of covering a node is the tile's own
cost, plus the cost of covering the children it delegates, plus any chain
that converts its form to the one its parent wants. Doing it greedily would
be locally right and globally wrong as soon as a tile's cost depends on its
children's.

Four properties hold, and each has a test:

| Property | What it means |
|---|---|
| Totality | every need is covered, or reported with every rejection and its reason |
| Determinism | the same spec, policy and lock always give the same covering; ties break by tile name |
| Legality before cost | an illegal tile is never scored, however cheap; legality depends only on the spec |
| Optimality | among legal coverings, the chosen one is cheapest under the policy's weights, checked against brute-force enumeration |

### Chain rules

A backend's output has a *form*. memory and pgx hand back your domain types
directly; sqlc hands back its own row types (`db.Order`), so something must
convert them. Converters are chain rules: tiles that turn one form into
another, at a cost computed from the entity:

```lisp
(tile row-mapper
  (converts db-rows domain)
  (cost-rule "1 per entity, +1 per enum field (ParseX), +1 per nullable field (pgtype), +2 per JSONB field"))
```

Selection adds the cheapest chain to each backend's own cost. A plain entity
still picks sqlc; one heavy in enums and nullable fields tips to pgx:

```
profiles.ProfileStore   needs: durable   chosen by: auto
  chosen  postgres-pgx    score 45   llm 7·4 + maint 5·3 + dep 1·1 + run 1·1
          postgres-sqlc   score 51   llm 3·4 + maint 2·3 + dep 3·1 + run 1·1 = 22
                                    + row-mapper 29 (1 entity, 1 enum field, 3 nullable fields)
  illegal memory          an in-memory map loses its data on restart
```

`examples/auto` uses all three storage tiles in one project. A tile whose
form no chain converts to domain is illegal.

## The tile registry

Every tile declares what it covers, what capability it produces, what it
needs and what it costs. `tilegen tiles` lists them all; `-sexp` prints the
registry as S-expressions, the shape tiles will have once they can be
written as data:

```lisp
(tile postgres-sqlc
  (pass select)
  (doc "postgres via sqlc: tilegen writes SQL, sqlc writes the Go, the LLM maps rows")
  (covers (impl ?iface ?parts...) (backend postgres))
  (produces store)
  (requires (tool sqlc))
  (cost (llm-work 3) (maintenance 2) (dependency 3) (runtime 1)))

(tile catch-all
  (pass select)
  (doc "covers anything no other tile covers: the LLM as the tile of last resort")
  (covers _)
  (produces llm/task)
  (cost (llm-work 10) (uncertainty 10)))
```

Costs are declared but not yet used: selection is still maximal munch plus
the config. Choosing the cheapest legal tile is next on the roadmap.

## Writing a tile

A pass tile is a pattern plus a function, with registry metadata:

```go
{Name: "enum", Pattern: Pat("(enum ?name ?items...)"), Then: selectEnum,
	Produces: "go/type", Doc: "a string type with constants, Valid() and Parse()",
	Cost: Cost{{"llm-work", 0}, {"maintenance", 0}}},
```

`Then` returns target forms and calls `m.Sub(...)` on any leaves it wants
other tiles to cover. Put more specific patterns before general ones.

A storage backend is a registered tile in its own file, like a database/sql
driver. Config, validation, the store tile, GitHub topics, starter files and
`-git` all ask the registry, so a new backend changes nothing else:

```go
func init() {
	RegisterBackend(&Backend{
		Name: "memory", Tile: "memory", Doc: "in-memory store: a map guarded by a mutex",
		Cost: Cost{{"llm-work", 2}, {"maintenance", 1}, {"dependency", 0}, {"runtime", 1}},
		Implement: func(in StoreInput) (StoreParts, error) { ... },
	})
}
```

A new package-level form is a registered tile in its own file too: its rule
joins a pass just before the catch-all, and it validates its own form. The
event bus (`events.go`) is built this way; nothing in the core names it:

```go
func init() {
	RegisterPackageTile(&PackageTile{
		Form: "events", Pass: Select,
		Rule: Rule{Name: "events", Pattern: Pat("(events ?items...)"), Then: selectEvents,
			Produces: "event-bus", Doc: "event structs, a typed Bus interface, and an in-process LocalBus"},
		Validate: validateEvents,
	})
}
```

If you keep giving an LLM the same instructions, that convention is a tile
waiting to be written.

## Background

- Andrew Appel, *Modern Compiler Implementation*, ch. 9: tree tiling, maximal
  munch, and "optimal" versus "optimum" tilings.
- Fraser, Hanson and Proebsting, *Engineering a Simple, Efficient
  Code-Generator Generator* (iburg, 1992).
- Go's own compiler rewrites SSA with S-expression rules in
  `src/cmd/compile/internal/ssa/_gen/*.rules`, compiled to Go by rulegen.

## Status

v0.3.0. Next up is v0.4, *Measured*: running `tilegen fill` for real, logging
every attempt, and replacing declared LLM costs with measured ones. See
[ROADMAP.md](ROADMAP.md).

## License

MIT
