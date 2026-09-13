# Running tilegen locally

Everything here was run against a clean checkout. Commands are shown with
their real output, so you can tell at a glance whether your run matched.

- [Prerequisites](#prerequisites)
- [Getting the Go toolchain right](#getting-the-go-toolchain-right)
- [Getting tilegen](#getting-tilegen)
- [Build and verify](#build-and-verify)
- [Make targets](#make-targets)
- [Your first generated project](#your-first-generated-project)
- [Reading what it produced](#reading-what-it-produced)
- [Watching the passes](#watching-the-passes)
- [The read-only commands](#the-read-only-commands)
- [The postgres and sqlite paths](#the-postgres-and-sqlite-paths)
- [Running it as a service](#running-it-as-a-service)
- [Starting your own project](#starting-your-own-project)
- [Troubleshooting](#troubleshooting)

## Prerequisites

| Tool | Needed for | Notes |
|---|---|---|
| Go | everything | see the next section; any Go 1.21+ will work |
| git | cloning; `-git` | |
| make | the `make` targets | the targets are one-liners you can run by hand |
| sqlc | `make demo-postgres`, `make demo-auto` | only for SQL backends: `brew install sqlc`, or [install docs](https://docs.sqlc.dev/en/latest/overview/install.html) |
| gh | `-git` creating a GitHub repo | `gh auth login` first |
| tmux | `tilegen up/status/down` | only for the workspace commands |
| graphviz | `tilegen plan -dot \| dot -Tsvg` | only for rendering the plan graph |
| an LLM CLI | `tilegen fill` | anything that reads a prompt on stdin and prints the answer |

Only Go and git are needed to build tilegen and generate a project with the
default memory backend. Everything else is opt-in, and tilegen tells you
when a tile wants a tool you do not have — `tilegen tiles` prints
`sqlc (missing)` next to the tiles that need it.

## Getting the Go toolchain right

`go.mod` declares:

```
go 1.26.0
```

That is a real floor, not a formality: the pinned `golang.org/x/mod`,
`golang.org/x/term` and `golang.org/x/tools` also require 1.26, so an older
toolchain cannot build this module even with the directive relaxed.

**You almost certainly do not need to install 1.26 by hand.** Since Go 1.21,
the toolchain downloads the one a module asks for, automatically, the first
time you build. So if you already have any Go 1.21 or newer:

```sh
go version          # whatever you have, e.g. go1.22.2
cd tilegen
go build ./...      # "go: downloading go1.26.0 (linux/amd64)", then builds
```

This is controlled by `GOTOOLCHAIN`, which is `auto` by default:

| Value | Behaviour |
|---|---|
| `auto` (default) | download and use the toolchain `go.mod` asks for |
| `local` | never download; fail if the local toolchain is too old |
| `go1.26.0` | use exactly this one, downloading if needed |

If you have no Go at all, or you would rather pin it yourself, install 1.26
from [go.dev/dl](https://go.dev/dl/). Distro packages are usually well
behind — Ubuntu 24.04 ships 1.22 — so prefer the official tarball:

```sh
curl -LO https://go.dev/dl/go1.26.0.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.26.0.linux-amd64.tar.gz
export PATH=/usr/local/go/bin:$PATH        # add to your shell profile
go version                                 # go version go1.26.0 linux/amd64
```

On macOS, `brew install go` tracks the current release closely, and the
official `.pkg` from go.dev is equivalent.

If your network blocks Go's download hosts, see
[the toolchain notes in Troubleshooting](#go-cannot-download-the-126-toolchain).

## Getting tilegen

Either install the binary:

```sh
go install github.com/vinodhalaharvi/tilegen@latest
tilegen -version        # tilegen v0.3.0
```

`go install` puts it in `$(go env GOPATH)/bin`, which needs to be on your
`PATH`:

```sh
export PATH=$(go env GOPATH)/bin:$PATH
```

Or clone, which is what you want if you intend to read the passes, run the
demos, or write a tile:

```sh
git clone https://github.com/vinodhalaharvi/tilegen.git
cd tilegen
```

The rest of this guide assumes a clone.

## Build and verify

```sh
make build          # go build -trimpath -o bin/tilegen .
./bin/tilegen -version
```

```
tilegen v0.3.0
```

Then run what CI runs, which is a gofmt check, `go vet` and the test suite:

```sh
make check
```

```
ok  	github.com/vinodhalaharvi/tilegen	9.554s
```

The suite is a single package and takes about ten seconds on a modest
machine. It covers the passes, the tile registry's internal consistency,
the selection properties (totality, determinism, legality-before-cost,
optimality against brute force), and golden files under `testdata/`.

If a deliberate change to the passes makes the golden files disagree, rewrite
them with `make golden` and read the diff before committing it.

## Make targets

`make help` lists them; this is the same list with what each one is for.

| Target | What it does | Needs |
|---|---|---|
| `build` | `bin/tilegen` | |
| `install` | `go install .` into `GOBIN` | |
| `test` | the suite, uncached | |
| `cover` | the suite with a coverage summary | |
| `golden` | rewrite golden files after an intended pass change | |
| `vet` / `fmt` / `fmt-check` | the usual | |
| `check` | `fmt-check` + `vet` + `test`; everything CI runs | |
| `demo` | generate `examples/shop` into `out/shop`, then prove it compiles | |
| `demo-dir` | the same project from a directory of `.sexp` files | |
| `demo-postgres` | the postgres variant, through sqlc | sqlc |
| `demo-auto` | per-store backend selection in one project | sqlc |
| `dump` | print the S-expression after every pass | |
| `tidy` | `go mod tidy` | |
| `clean` | remove `bin/`, `out/`, `coverage.out` | |

The `SPEC`, `CONFIG` and `OUT` variables are overridable, so the targets work
on your own spec too:

```sh
make demo SPEC=spec/ CONFIG= OUT=../myshop
```

## Your first generated project

`make demo` is three commands. Run them by hand once, so the pieces are not
hidden:

```sh
./bin/tilegen -config examples/shop/config.sexp -out out/shop -dump examples/shop/spec.sexp
```

```
  wrote  go.mod
  wrote  orders/orders_gen.go
  wrote  orders/memory_order_store.go
  wrote  orders/http_gen.go
  wrote  orders/http.go
  wrote  orders/events_gen.go
  wrote  billing/billing_gen.go
  wrote  billing/memory_invoice_store.go
  wrote  tilegen.lock
  wrote  tilegen.tasks.json
11 open LLM task(s), 0 hole(s) already filled -> out/shop/tilegen.tasks.json
```

Nothing was compiled and nothing was run: tilegen wrote files and stopped.
Now prove the result is real Go:

```sh
cd out/shop && go mod tidy && go build ./... && go vet ./...
```

It compiles. The eleven method bodies that are still holes are
`panic("tilegen:hole ...")`, which type-checks fine — that is the point.
Finally, check that generation is idempotent and that tilegen agrees with
what is on disk:

```sh
cd ../.. && ./bin/tilegen check -config examples/shop/config.sexp -out out/shop -allow-holes examples/shop/spec.sexp
```

```
  holes    11 open (listed in tilegen.tasks.json)
ok: 7 generated file(s) match the spec; 11 open hole(s) allowed
```

Drop `-allow-holes` and it exits 1 while holes remain, which is what you
want in CI once the LLM work is meant to be finished.

## Reading what it produced

```
out/shop/
├── go.mod                             module, Go version, requires
├── billing/
│   ├── billing_gen.go                 Invoice struct, InvoiceStore, Invoicer
│   └── memory_invoice_store.go        the memory implementation: stubs with holes
├── orders/
│   ├── orders_gen.go                  Order, Status enum, OrderStore, Pricer
│   ├── memory_order_store.go          the memory implementation: stubs with holes
│   ├── events_gen.go                  event structs, Bus interface, LocalBus
│   ├── http_gen.go                    Handler and ServeMux over the store
│   └── http.go                        validation and error mapping: holes
├── tilegen.lock                       which tile each need got, pinned
└── tilegen.tasks.json                 every open hole, with contract and intent
```

Two headers tell the two kinds of file apart, and they decide what
regeneration is allowed to touch:

| Header | Meaning |
|---|---|
| `Code generated by tilegen. DO NOT EDIT.` | tilegen owns it; rewritten every run |
| `Scaffolded by tilegen.` | written once, then yours; stubs are appended or updated, your code never is |

`*_gen.go` is the first kind. `memory_order_store.go` and `http.go` are the
second: that is where the holes live and where an LLM or you will write.

`tilegen.lock` pins the tile chosen for each need, so a later run does not
silently re-architect the project when costs or tiles change. `-reselect`
ignores it deliberately.

## Watching the passes

`-dump` writes the S-expression after every pass to `<out>/.tilegen/`:

```sh
ls out/shop/.tilegen/
```

```
00-parse.sexp  01-merge.sexp  02-expand.sexp  03-concretize.sexp  04-select.sexp
```

`make dump` prints them all in order. Reading `02-expand.sexp` next to
`03-concretize.sexp` is the fastest way to understand the compiler: expand
removes sugar and knows nothing about your config, concretize writes every
config decision into the tree, and select covers the result with tiles.

## The read-only commands

None of these write anything, and each takes `-json` for programs.

```sh
./bin/tilegen tiles                    # the registry
./bin/tilegen explain examples/auto    # why each need got its tile
./bin/tilegen plan   -out out/shop -config examples/shop/config.sexp examples/shop/spec.sexp
./bin/tilegen prompt -out out/shop -config examples/shop/config.sexp examples/shop/spec.sexp
```

`prompt` with no task ID lists the open holes and where they are:

```
11 open task(s). Print one with: tilegen prompt examples/shop/spec.sexp ID

  orders.MemoryOrderStore.Get   orders/memory_order_store.go:24   Look up id in s.m while holding s.mu. Return ErrNotFound ...
  orders.MemoryOrderStore.List  orders/memory_order_store.go:28   Collect every value in s.m while holding s.mu and sort th...
```

`api` is the odd one out: it resolves an import path through **the current
module**, so run it inside the project that depends on the package, not in
the tilegen checkout.

```sh
cd out/shop && tilegen api github.com/google/uuid
```

`plan` shows what a run makes, in dependency order — useful for seeing why
a postgres store's holes cannot be filled until sqlc has run:

```
level 0
  file   billing/billing_gen.go
  file   go.mod
  ...
  tile   local-bus
  tile   memory
level 1
  file   orders/memory_order_store.go   (memory)
  need   orders.OrderStore   (memory: store, chosen by config (score 12))
  need   orders.Bus   (local-bus: event-bus, chosen by lock (score 1))
  task   orders.Handler.validateOrder   Report what makes this Order unacceptable, ...
level 2
  task   orders.MemoryOrderStore.Get   Look up id in s.m while holding s.mu. ...
```

`tiles` and `explain` are the subject of their own guide:
**[TILES.md](TILES.md)**.

## The postgres and sqlite paths

These need sqlc on your `PATH`. Check first:

```sh
./bin/tilegen tiles store
```

The `NEEDS` column reads `sqlc (found)` or `sqlc (missing)` for the
SQL-backed tiles. If it is missing, install sqlc and re-run. Then:

```sh
make demo-postgres
```

which generates with `examples/shop/config.postgres.sexp`, runs
`sqlc generate` to turn `db/*.sql` into `internal/db`, and then builds and
vets. The ordering matters and is not incidental: tilegen writes the SQL,
sqlc writes the Go, and only then do the row-mapping holes have types to
refer to.

`make demo-auto` is the more interesting one: `examples/auto` has three
stores with different needs in one project, and tilegen picks a different
backend for each. Run `./bin/tilegen explain examples/auto` before and after
to see the reasoning.

You do not need a running Postgres for any of this. sqlc is a compiler, not
a client; it reads your SQL and emits Go. A database is only needed when you
actually run the generated program.

## Running it as a service

```sh
./bin/tilegen serve                       # or PORT=8080
curl -X POST localhost:8080/generate --data-binary @examples/shop/spec.sexp -o project.zip
curl -X POST localhost:8080/explain  --data-binary @examples/auto/spec.sexp | jq .
```

`GET /` is a page with a spec on the left and the tile choices on the right.
The server executes nothing: no git, no sqlc, no build. It writes the
project into an in-memory plan and zips that, so the container needs no
toolchain, which is why the Dockerfile can use a distroless base.

```sh
docker build -t tilegen .
docker run --rm -p 8080:8080 tilegen
```

## Starting your own project

From an existing Go module:

```sh
tilegen import ~/go-projects/myapp      # writes spec/*.sexp and spec/NOTES.md
tilegen check spec                      # what the spec would regenerate, next to what you have
```

From nothing, the shortest useful spec is:

```lisp
(project notes
  (module github.com/you/notes)
  (go 1.22)
  (package notes
    (entity Note
      (field ID int64)
      (field Body string)
      (store get list save (durable)))))
```

Ask what that would build before building it:

```sh
tilegen explain spec.sexp
```

```
notes.NoteStore   (store)   needs: durable   chosen by: auto
  chosen  sqlite-sqlc     score 27   llm 3·4 + maint 2·3 + dep 1·1 + run 1·1 = 20
                                    + row-mapper 7 (1 entity)
          postgres-sqlc   score 29   ...
          postgres-pgx    score 45   ...
  illegal memory          an in-memory map loses its data on restart
```

`(durable)` made `memory` illegal, so a SQL backend won — which means this
project needs sqlc to finish:

```sh
tilegen -out ../notes -dump spec.sexp
cd ../notes && sqlc generate && go mod tidy && go build ./...
```

Drop `(durable)` and `memory` wins instead, at which point there is nothing
to install and `go build` works straight away. Toggling that one form and
re-running `explain` is the fastest way to get a feel for selection; see
[TILES.md](TILES.md) for the rest.

## Troubleshooting

### `go: go.mod requires go >= 1.26.0`

Your toolchain is older and `GOTOOLCHAIN` is pinned to `local`. Either unset
it (`export GOTOOLCHAIN=auto`) or install Go 1.26 as above.

### Go cannot download the 1.26 toolchain

On a restricted network, `GOTOOLCHAIN=auto` fails because toolchain module
zips are served from `storage.googleapis.com`, and the tarballs on
`go.dev/dl` redirect there too. Options, cheapest first:

1. Allow `go.dev` and `storage.googleapis.com` through your proxy.
2. Install the tarball on a machine that can reach go.dev and copy
   `/usr/local/go` across; it is self-contained and relocatable if you set
   `GOROOT`.
3. Build the toolchain from source, which only needs `github.com`. Go 1.26
   requires a Go 1.24.6+ bootstrap, and Go 1.24 requires 1.22.6+, so from a
   distro 1.22 the chain is three builds:

   ```sh
   for v in go1.22.12 go1.24.6 go1.26.0; do
     curl -sLO https://codeload.github.com/golang/go/tar.gz/refs/tags/$v
     tar xzf $v && mv go-$v $v
   done
   (cd go1.22.12/src && GOROOT_BOOTSTRAP=$(go env GOROOT) ./make.bash)
   (cd go1.24.6/src  && GOROOT_BOOTSTRAP=$PWD/../../go1.22.12 ./make.bash)
   (cd go1.26.0/src  && GOROOT_BOOTSTRAP=$PWD/../../go1.24.6  ./make.bash)
   export PATH=$PWD/go1.26.0/bin:$PATH
   ```

   Each stage is a full toolchain build: budget 10–20 minutes per stage on
   one core. The bootstrap floors are exact — 1.24.0 will *not* build 1.26,
   it reports `does not meet the minimum bootstrap requirement of go1.24.6`.

### `sqlc not found`

`make demo-postgres` and `make demo-auto` stop early on purpose rather than
generating something that cannot be completed. Install sqlc, or use `make
demo`, which needs no tools at all.

### `tilegen: check failed: N file(s) out of date`

Expected, and informative. Either the spec changed and you have not
regenerated, or someone edited a `DO NOT EDIT` file. The lines above it say
which and why: `stale` is a generated file that differs, `stub` is a new
method the spec added, `drift` is a method you implemented whose signature
no longer matches the spec.

### The generated project will not build

Run `go mod tidy` in the output directory first; tilegen writes `go.mod` but
never runs commands. If it still fails and the errors are in `internal/db`,
you are on a SQL backend and have not run `sqlc generate` yet — `tilegen
plan` shows exactly which level that step belongs to.

### `unknown form ... (did you mean entity?)`

A typo in the spec. tilegen reports every problem in one pass with file,
line and column, and suggests a fix for near-misses of known forms. A
genuinely unknown form is not an error — it becomes an LLM task, unless you
pass `-strict`, which is worth doing in CI.

### A tile you expected was not chosen

`tilegen explain SPEC` prints every candidate with its score, every rejected
tile with the reason, and whether the choice came from the config, the lock
or the cost model. If it says `chosen by: lock`, an earlier run pinned it;
`-reselect` chooses again. See [TILES.md](TILES.md).
