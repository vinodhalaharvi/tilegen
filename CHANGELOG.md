# Changelog

## Unreleased

## v0.3.4 - 2026-11-13

- Studio: `Clear all` unticks every requirement and keeps the service,
  its fields and the weights. The brief box says that it ticks on top of
  what is already ticked.
- Studio: "at once" in a brief no longer ticks sign-in.

## v0.3.3 - 2026-11-13

- Drafts: a `(tile NAME (offers CAPABILITY) ...)` form given with a
  compile declares a tool tilegen has no tile for. It takes part in every
  decision and explanation and generates nothing: when a draft wins,
  tilegen reports the choice and stops. A draft wins only what it
  claims, and pays uncertainty for never having run.
- `tilegen serve` can ask Claude to draft a tile for each go.mod
  requirement no tile speaks (`POST /draft`). Every draft passes the
  same checks as one written by hand, and is sent back once with the
  reason if it fails. It is off unless a key is set and the server
  listens on this machine only.
- `tilegen key set | remove | status` keeps an Anthropic API key in the
  operating system's keychain. `ANTHROPIC_API_KEY` is used first when
  set. The key is never sent to the page.
- `(running postgres nats)` in a policy names the engines a team already
  operates. A tile on one of them costs nothing more to operate, which
  `explain` shows as `ops 0` with the reason.
- `POST /gomods` reads go.mod files and says which engines their direct
  requirements show are in use, which requirements a tile speaks, and
  which none does. The studio's "What you already run" card uses it to
  tick engines from the files you give it.
- `tilegen serve` opens on the studio: tick what the system must do, see
  each need go to a tile with the cost of every alternative and the
  reason each other tile was ruled out, read every generated file, and
  download the project. The page, its libraries and its typefaces are in
  the binary, so it works with no network. The earlier walkthroughs are
  at `/docs`.
- `POST /compile` returns the choices, the plan and every file from one
  compile.
- tilegen builds for `js/wasm`, where it is one function from a spec to
  that same answer, so the compiler can run in a browser page.
- Files are finished the same way where the `go` command is absent (a
  browser, an image without Go) as where it is present: unused imports
  dropped, standard-library ones added, grouped as goimports groups
  them.

## v0.3.2 - 2026-11-12

- `(app)` generates the program: `cmd/<name>/wire_gen.go` with `Deps`,
  `App`, `build` and `Serve`, and a `main.go` scaffolded once. The wiring
  is read back from the constructors the run produces, so it needs no
  per-tile knowledge and follows constructors edited by hand. What
  nothing in the project supplies becomes a field of `Deps`.
- `(inbox Event Field)` inside `(events ...)` makes a handler take effect
  once per event, however many times it is delivered: the record that it
  ran is committed with the handler's own writes. It puts an `inbox`
  requirement on every store in the package, which `postgres-sqlc`
  satisfies. With `(outbox ...)`, delivery is at least once and the
  effect exactly once. No holes.
- `(watch Event)` in `(api ...)` is a server-streaming RPC fed by the
  package's bus, for the `connect` and `grpc` tiles. It needs a stream,
  so it rules out `http-json`. `NewAPIServer` takes the `Bus` when
  something is watched. A slow client is disconnected rather than
  slowing the publisher; `allow<Event>` is the one hole.
- `(outbox Order)` inside `(events ...)` saves an `Order` and records
  its events in one transaction, and generates a relay that publishes
  them to the bus at least once. It puts an `outbox` requirement on the
  entity's store, which `postgres-sqlc` satisfies, so the store is
  chosen by it. No holes.
- Generating ends with the commands that make the tree build, tools
  first: `next: cd out && sqlc generate && go mod tidy && go build ./...`.
  Running `go mod tidy` before sqlc or buf made Go look for the missing
  package on the network and report a repository that does not exist.
- `(secrets (secret DatabaseURL)... )` generates a typed `Secrets`
  interface and its implementation, with no holes. Three tiles offer it:
  `env-secrets`, the default; `file-secrets`, for `(rotating)`; and
  `vault`, for `(audited)` or when `(config (secrets vault))` says so.
  Every constructor fails at startup and lists all the missing secrets
  together. `(key ...)` names a secret outside the program.
- `(api (get Order) (save Order) ... (typed-clients))` serves a package's
  entities over a transport chosen by what its clients need, the way a
  store's backend is chosen. Three tiles offer it: `http-json` (the
  server `(http ...)` generates), `connect`, and `grpc`. The requirements
  are `(typed-clients)`, `(browser-clients)` and `(grpc-native)`.
- The `connect` and `grpc` tiles write the `.proto` from the entities,
  with `buf.yaml` and `buf.gen.yaml`, and a complete server over the
  package's stores. `buf generate` is the tool step, as `sqlc generate`
  is for SQL. Validation and error codes are the only holes.
- Field and enum numbers are pinned in `tilegen.lock`. Reordering never
  renumbers, a new field takes a number nobody has had, and a removed
  one is reserved for good.
- `(config (api grpc))` chooses the tile outright.

## v0.3.1 - 2026-11-12

- Installable with Homebrew: `brew install vinodhalaharvi/tap/tilegen`.
- Each tile declares the engine it runs on, and tiles that name the same
  one share its server in the whole-project view: both postgres tiles,
  both NATS tiles, and Kafka as a bus and as an inbound edge. A
  `(tile-spec ...)` says it with `(engine NAME)`.
- `tilegen explain -project` compares the choice made need by need with
  the cheapest for the project as a whole, where a server is paid once per
  engine, a library once per tile, and each extra engine in a capability
  costs the policy's new `(extra-engine N)`. It is a report: what tilegen
  generates does not change.
- Selection is now written as a dynamic program on the
  [dyna](https://github.com/vinodhalaharvi/dyna) library: the alternatives
  for a need, an algebra that prices one, and dyna to fold them. Nothing
  it chooses changes; `explain` prints the same text for every example. A
  new test prices every covering one at a time and checks that the
  dynamic program finds the same minimum.
- Every spec shown anywhere on the site is a complete spec with a policy
  in it, including the editor's starting spec. Each of the three
  walkthroughs prints its whole spec twice — the same file with one
  decision changed — beside what tilegen decided and the real output of
  both. No fragments, no policy floating apart from the spec it belongs
  to. Tests enforce all of it.
- Every walkthrough now ends by changing a decision that comes from
  outside the code, and shows what that changes: a team policy moving a
  task list from sqlite to postgres, a client policy moving an orders API
  the other way, and events that must leave the process. A test fails if
  a walkthrough stops short of showing generated code.
- One type family and four sizes across the page, so nothing jumps.
- The documentation is three walkthroughs, each carried from the spec to
  the generated code and then through one change to the spec: a task list
  gaining `(durable)`, an orders API meeting a client policy, and a
  two-package service with events and an interface. Every sample is real
  generated output.
- Fixed the syntax highlighting, which chained regexes over text it had
  already marked up and printed its own span tags into the page. Each
  language now has a single-pass tokeniser.
- A sqlite store's hints named `pgx.ErrNoRows`; they now name
  `sql.ErrNoRows`, which is what that driver returns.
- The service has a Docs tab: what a spec can contain, form by form, each
  shown beside the code it produces, with eleven complete examples that
  load into the editor and run. Written for people using tilegen, not
  people reading its source; a test generates every documented example,
  and another keeps the language user-facing. The pages were redesigned:
  syntax colouring for specs, Go and SQL, a palette and type scale meant
  for reading, and a fix for an inherited rule that stretched every code
  block to half the viewport.
- tilegen runs without a Go toolchain. The standard library is baked in
  (`stdlist.go`, regenerated by `go generate`) instead of shelling out to
  `go list std`, and standard imports are stated in the target forms
  rather than left to goimports, which needs the `go` command. Generated
  code is byte-identical; the service in a distroless image previously
  reported every standard package as unknown.
- `tilegen serve`: the compiler over HTTP. `POST /generate` returns the
  project as a zip, `POST /explain` and `POST /plan` answer as JSON,
  `GET /tiles` lists the registry, and `GET /` is a page with a spec
  editor beside the tile choices. It executes nothing and writes nothing:
  generation stays a plan in memory, which is zipped directly. Requests are
  bounded by body size, timeout and spec size.
- A `Dockerfile` (distroless, non-root, static) and `deploy/cloudrun.sh`.
- A SQL backend declares a `Dialect`: column types, placeholders, upsert,
  enum constraint and sqlc overrides. `sql.go` no longer assumes postgres,
  and sqlc.yaml takes its engine and driver from the dialect.
- `sqlite-sqlc`: sqlite through sqlc, durable but illegal for
  `(cross-process)`. Cheaper on dependencies, so it wins for a simple
  entity; its mapper parses `uuid.UUID` and `time.Time` from TEXT, so
  postgres wins for entities that use them. Every generated statement is
  tested against a real sqlite database.
- The row-mapper's cost depends on the candidate's dialect, so the same
  entity is priced differently per backend.
- Prompts name the project's direct modules with versions and say to import
  nothing else, so a filler works from what the spec declares rather than
  from a remembered import path.
- A tile can ask for a package's API by path: the http tile's `statusFor`
  hole gets the error package of the backend behind that resource
  (`pgconn` for postgres, nothing for memory), since deciding what deserves
  a 409 is a question about the driver.
- A test pins the boundary: generation is byte-identical whether or not any
  hole has been filled.
- HTTP routing tile: `(http (route GET "/links/{id}" (get Link)) ...)`
  generates a `Handler` over the package's stores, `Routes()` returning an
  `*http.ServeMux`, and a handler per route: typed path values, JSON
  decoding that rejects unknown fields, the store call, and the right
  status. No dependency, since Go 1.22's router matches methods and
  wildcards and answers 404 and 405. Only validation and error mapping are
  holes, one pair per resource. The shop example now serves an API.
- `(uses T)` on a `go/func`: types a body names but no signature does, so
  imports resolve. `stubsAndTasksAs` lets a tile choose its receiver name.
- A tmux window can be split into panes: `(window demo (split horizontal)
  (pane (run "...")) (pane (run "...")))`, with `vertical` for stacked.
  Panes take the same `(dir ...)`, `(worktree ...)` and `(run ...)` as
  windows. Good for showing a project; windows remain better for working
  in one. Panes are addressed by tmux's own pane ids, so a config with
  `pane-base-index 1` works.
- `up`, `status` and `down` need only a `(workspace ...)` form, so a spec
  folder with no project yet can open its session and watch the project
  appear. Opening a session no longer needs a git repository either; only
  worktrees do.
- Generation says nothing is there to generate, and points at `tilegen up`
  when the workspace declares a session; after generating it names the
  session to open.
- `-json` on the read-only commands: `explain` (coverings, scores,
  candidates, rejections, chains, positions), `plan` (levels with each
  node's dependencies), `tiles` (capabilities, offers, costs, legality) and
  `check` (what would change, and the problems). Text and JSON come from
  one computation, so they cannot disagree, and `check -json` still exits
  non-zero on failure.
- `tilegen plan [-dot] [SPEC]` prints the plan as a dependency graph in
  topological levels, or as graphviz DOT. Every edge is inferred from the
  plan tilegen already builds: which tile staged which file, a tool step's
  inputs and outputs, each task's context files, and each covering's
  children. So a postgres store's holes sit after `sqlc generate` and its
  output, while a memory store's do not. Cycles are reported with the nodes
  involved. It writes nothing.
- A `tilegen.lock` written by an older tilegen is a warning and is
  rewritten, rather than an error that blocks the project.
- Selection is one general mechanism. Nodes declare needs, tiles register
  offers of a capability, and one bottom-up solver finds the cheapest legal
  covering: a tile's own cost, plus its children's coverings, plus any
  chain. Storage backends and event-bus transports are both offers now, and
  a new capability costs no new machinery. Four properties are tested:
  totality, determinism, legality before cost, and optimality against
  brute-force enumeration.
- Requirements are a shared vocabulary: `(durable)` means the same on a
  store and on an events form, and a capability declares which it accepts.
- Event buses compete. `local-bus` (in-process, complete code, no holes) is
  illegal for `(durable)` or `(cross-process)`; `nats-bus` serves those,
  scaffolding an implementation with one hole per method and NATS-specific
  hints. `(events nats)` in the config names one explicitly.
- `tilegen.lock` is uniform: `(tile NEED TILE)` for every capability.
- `tilegen tiles` lists capabilities and who offers them; `-sexp` shows
  `(offers ...)`, `(alias ...)` and `(illegal-when (durable) "...")`.
- Prompts carry the API surface of the packages a task touches: exported
  declarations with doc comments, signatures only, from `go/types`. Scoped
  to the packages the task's files import plus any named in its intent
  (resolved through go.mod), never the standard library, capped and
  budgeted, cached under `<out>/.tilegen/api`. `tilegen api IMPORT-PATH`
  shows what a package contributes. So an LLM writes against pgx's real
  API instead of its memory of it.
- `tilegen import [DIR]` lifts an existing Go module into a spec: module,
  requires, structs with field types and tags, interfaces with their full
  method sets, enums (defined string types with typed constants), and
  implementations found with `types.Implements`. Everything comes from the
  type checker, so the project must type-check first (`-force` imports the
  packages that do). What cannot be proved is listed in `NOTES.md` with a
  reason; store ops, needs and events are never guessed from names.
- Policy file: `(policy (weights ...) (prefer ...) (margin N) (avoid TILE
  "why"))`, beside the spec or in `-policy FILE`, sets what a team values.
  Weights price every tile and chain rule, `prefer` with `margin` breaks
  near-ties, and `avoid` rejects a tile with its reason shown in `explain`.
  The same spec can then give different teams different architectures with
  no tile edited. Unknown dimensions or tile names are errors with
  suggestions.
## v0.3.0 - 2026-09-11

v0.3 hands the holes to an LLM and makes tilegen choose. `tilegen prompt`
and `tilegen fill` turn tasks into self-contained prompts and fill them with
any LLM CLI, building and retrying with the compiler's errors. The tile
registry makes every tile declare what it covers, produces and costs; new
tiles register themselves (the event bus is the first). Stores declare
needs, backends declare legality, and `(storage auto)` picks the cheapest
legal backend per store, counting chain rules like sqlc's row mapper, with
`tilegen explain` showing why and `tilegen.lock` keeping choices stable.

- `tilegen.lock` pins each store's backend in the generated project. A
  pinned choice sticks while it stays legal, so cost or spec changes never
  silently move a store; `explain` shows when auto would now choose
  differently. Illegal or unregistered pins are re-selected with a warning;
  `-reselect` (also on `explain`) chooses again; an explicit
  `(storage NAME)` wins. Generation writes the lock; `check` verifies it.
- Chain rules. Backends declare the form their code yields (sqlc: db-rows;
  memory and pgx: domain), and registered converters (`RegisterChain`)
  turn one form into another at a cost computed from the entity. The
  row-mapper costs 1 per entity, +1 per enum or nullable field, +2 per
  JSONB field. Selection adds the cheapest conversion path (Dijkstra over
  forms) to each backend's cost, so an entity heavy in enums and nullable
  fields picks pgx over sqlc. `explain`, `-dump` and `tilegen tiles` show
  the chain; a backend with no path to domain is illegal. `examples/auto`
  now uses memory, postgres-sqlc and postgres-pgx in one project.
- Selection: stores declare needs (`(durable)`), backends declare when
  they are illegal (`IllegalWhen`), and `(storage auto)`, now the default,
  gives every store the cheapest legal backend by declared cost times
  built-in weights. Stores in one project can use different backends. An
  explicit backend that is illegal for a store is an error at the spec.
  Legality depends only on the spec. Existing specs without needs choose
  exactly as before.
- `tilegen explain [SPEC] [STORE...]` shows each store's needs, the legal
  backends with their score arithmetic, and the illegal ones with reasons;
  the choice is also recorded in `-dump`.
- A third backend, `postgres-pgx`: tilegen writes the schema, the LLM writes
  the SQL against a `pgxpool.Pool`, hinted with the query sqlc would have
  compiled. Backends can now declare imports and project-level forms
  (sqlc.yaml comes from the sqlc backend).
- `examples/auto` and `make demo-auto` (in CI): memory and postgres-sqlc in
  one project.
- Event bus tile: `(events (event NoteShared (field ...))...)` generates
  event structs, a typed `Bus` interface and `LocalBus`, a synchronous
  in-process implementation (ordered delivery, joined errors, safe
  unsubscribe and concurrency), with no holes. It follows `context-first`
  and needs Go 1.20+. The shop example publishes OrderPlaced and
  OrderShipped.
- Package tiles register themselves: `RegisterPackageTile` adds a rule to a
  pass just before the catch-all, with its own validation and did-you-mean
  names. The event bus is the first; nothing in the core names it.
- `tilegen tiles` fits the terminal width (`-v` for full rows) and shows
  costs compactly.
- Tile registry. Every pass tile declares the capability it produces, a
  doc line and a declared cost; storage backends are registered tiles in
  their own files (`RegisterBackend`, like database/sql drivers) that also
  declare their tools, GitHub topics, reserved packages and generate step.
  Config, validation, the store tile, starter files and `-git` all ask the
  registry, so the core no longer names any backend. `tilegen tiles` lists
  the registry, and `-sexp` prints it as S-expressions. Output is unchanged.
- `tilegen fill [SPEC] [ID or pattern...]` fills holes with any LLM CLI
  set in `(workspace (fill (command "claude -p") (retries 1) (timeout
  "5m")))` or `-llm`. Per hole: prompt, take the method from the reply,
  check its receiver, name and signature, splice it in with go/ast, run
  goimports, build; on failure restore the hole and re-ask with the
  compiler's errors and the previous answer. Unfillable holes stay holes.
  Drifted methods are filled first, and their expected compile errors are
  tolerated; any other baseline build error stops fill. `-dry-run` lists
  what would be filled.
- `tilegen prompt [SPEC] [ID...]` prints a self-contained Markdown prompt
  per task: what to write, contract, intent, constraints, reply rules, and
  the full text of every file involved (for postgres stores, sqlc's
  generated Go too). No ID lists the open tasks; `-all` prints them all. It
  uses the generation plan, so it is never stale, and writes nothing.
- Subcommands accept flags after the spec: `tilegen check spec/ -allow-holes`.
## v0.2.0 - 2026-09-11

v0.2 made tilegen trustworthy on real projects: specs across files, git and
GitHub setup, a workstation with worktrees and tmux, reconciliation as the
spec changes, store query ops, implementing any interface, enums,
did-you-mean, and `tilegen check` for CI.

- Fix: regenerating a postgres project could delete sqlc's `query.sql.go`.
  sqlc copied the `db/query.sql` header comment into its Go, and the sweep
  looked for tilegen's marker in a file's first 256 bytes. Markers now
  count only on a file's first line (Go's convention), and the header ends
  with an empty SQL statement so it no longer leaks into sqlc's output.
- `tilegen check [SPEC]` fails (exit 1) if generating would change any
  file, if a method you wrote has drifted from the spec, or, without
  `-allow-holes`, if holes remain. It writes nothing. Generation is now
  plan-then-apply, so check and generation share one plan; "wrote" lists
  only files that actually changed. Every demo target ends with a check,
  so CI verifies generation is idempotent.
- `(enum Status pending paid shipped)` generates a string type, typed
  constants, `StatusValues`, `Valid()` and `ParseStatus()`; values like
  `in-transit` become `StatusInTransit`. With postgres, enum fields are
  `TEXT` columns with a `CHECK` constraint, and store tasks say to convert
  them with `ParseStatus`, never a bare cast.
- Did you mean: every unknown name gets a suggestion when it is a likely
  typo, across the spec, config, repo and workspace forms, store ops and
  field references. A typo of a known package form, like `(entiy ...)`, is
  now an error instead of quietly becoming an LLM task; a genuinely
  unknown form still goes to the LLM. Config errors are reported all at
  once, like spec errors.
- A spec with `(github name)` and no `(module ...)` takes its module path
  and owner from the project's go.mod (or the spec's module), so plain
  generation never calls `gh`, and every machine gets the same module.
  `gh` is only asked on the first `-git` run, before go.mod exists.
- `(implement Iface (as Name) (field n T)...)` implements any interface in
  the package, generated store interfaces included: struct, constructor
  with injected dependencies, stubs, compile-time check, tasks, and
  reconciliation.
- Interfaces can embed others with `(embed T)`. Implementations include
  embedded methods: project interfaces directly, standard-library ones
  (like `io.Writer`) via Go's type checker. Unresolvable embeds warn.
- Variadic last parameters: `(args "...any")`.
- Store query ops derived from fields: `count`, `(list-by F)`, `(get-by F)`,
  `(count-by F)`, `(exists-by F)`, `(delete-by F)`, plus custom
  `(method ...)`. With postgres each derived op gets a sqlc query; custom
  methods are holes. Field parameters keep Go initialisms (`noteID`).
- Reconciliation: the code follows the spec as forms come and go. Missing
  methods are appended to your files as stubs; untouched stubs take new
  signatures or are removed; methods you implemented are reported as drift
  (an LLM task) or orphans, never changed. Stale generated files, and
  scaffolded files that are still pure scaffolding, are deleted. No state
  file: the headers in the files on disk are the state. This fixes methods
  added to a store after scaffolding being silently counted as done.
- go.mod versions are only ever raised: regenerating keeps a `go` line or
  a require that `go mod tidy` raised, instead of lowering it back to the
  spec's version and breaking the build until the next tidy.
- CI runs the postgres example through real sqlc (v1.31.1) and requires the
  result to build and vet. `make demo-postgres` explains how to install sqlc
  when it is missing, and honors `SQLC=/path/to/sqlc`.
- The shop example has a second package, `billing`, whose types use
  `orders.Order`, so both demos and CI exercise cross-package imports.
- Import cycles between project packages are rejected at the spec, pointing
  at the type that closes the loop, instead of failing later in `go build`.
- Writing `orders.Order` inside package `orders` gets a clear error that
  suggests the unqualified type.
- LLM tasks list the generated files of other project packages whose types
  the store uses, as extra context.
- Specs can be split across a directory of `.sexp` files. A new merge pass
  links them into one project: same-named packages merge, identical requires
  dedupe, conflicts report both positions. `examples/shopdir` compiles to
  the same Go as `examples/shop/spec.sexp`, checked by a test.
- A `(config ...)` form can live in the spec; `-config` still wins.
- New `(repo ...)` form: GitHub coordinates, visibility, description, topics
  and license. Its tile writes starter files (README.md, LICENSE, Makefile,
  .gitignore), and `(module ...)` becomes optional when it can be derived.
- `-git` clones the GitHub repository if it exists, or runs git init,
  commits, creates it with `gh`, and adds topics. `-name` picks the project,
  repository and module name. `-dry-run` prints the plan and writes nothing.
- New `(workspace ...)` form for the local workstation: `(name ...)` and
  `(out ...)` default -name and -out, git worktrees live at `<out>.wt/`,
  and a tmux session has one window per checkout with an optional command.
- `tilegen up` creates missing worktrees and opens or attaches the session;
  re-running reuses everything and adds new windows. `tilegen status` shows
  each checkout's changes, open holes and branch. `tilegen down` closes the
  session; `-prune` removes clean worktrees and keeps branches.

## v0.1.0

First release: the smallest compiler that is still a compiler.

- S-expression reader with line:col positions carried through every pass.
- Tiling engine: tree patterns (`?x`, `?xs...`, `_`) plus rules; maximal munch.
- Passes: `expand` (sugar removal), `concretize` (config-driven), `select` (tiling onto target forms).
- Catch-all tile: forms nothing covers become LLM tasks (or errors with `-strict`).
- Emitters reuse existing tools: `x/mod/modfile`, `go/parser`, `astutil`, `go/format`, `x/tools/imports`, sqlc.
- `entity` tile: struct + store interface + implementation stub + compile-time assertion.
- Storage backends: `memory`, `postgres` (emits schema, queries, and `sqlc.yaml`).
- Regeneration is safe: go.mod is merged, implementation files are never overwritten, filled holes drop off `tilegen.tasks.json`.
- `-dump` writes the S-expression after every pass.
