# Where things live

tilegen is one command and eight packages. The command is at the root of
the module, so `go install github.com/vinodhalaharvi/tilegen@latest`
works; everything else is under `internal/`.

| Package | What it holds |
|---|---|
| `main` (the root) | the command line: `main.go` and the `explain`, `plan` and `tiles` commands. It also embeds the studio's files and the README, and hands them to the packages that serve them |
| `internal/sexp` | S-expressions: `Node`, `Parse`, the printers, and pattern matching |
| `internal/compile` | the compiler: capabilities, offers, costs and the registries; the solver, policy and the lock file; validation, the three passes, plan and emit; the workspace; and the forms for events, API, auth, secrets and transport. It declares what can be asked for and holds no tile of its own |
| `internal/tiles` | every built-in tile, one file each: stores, event buses, API styles, auth schemes and secret sources; and the `tile-spec` form. Importing it registers them |
| `internal/fill` | `tilegen fill`, `tilegen prompt`, and the API surface a prompt carries |
| `internal/symbols` | what a library exports, read by the type checker; `symbolstest` beside it is the small module its tests read |
| `internal/importer` | reading what a team already has: go.mod coverage, `tilegen import`, tile-spec skeletons, and drafting a tile with a model |
| `internal/serve` | the HTTP service, the studio page, the docs pages, and the key |
| `internal/mcpserver` | the MCP server |

They depend on one another in one direction. Each package imports only
what is to its left:

```
sexp <- compile <- tiles   <-.
                <- symbols <- importer <- serve <- mcpserver
                <- fill
```

and `main` imports them all.

`compile` is the largest because its parts call one another: the context
names the rules, the rules name the solver, and the solver names the
context.

## Adding a tile

A tile is one file in `internal/tiles`, named for what it offers and what
it is: `store_bolt.go`, `bus_kafka.go`, `api_grpc.go`, `auth_oidc.go`,
`secrets_vault.go`. The file registers
the tile and holds the code that emits its Go, and a test refuses a file
that holds two tiles or is named for another.

For a storage tile, `tilegen import -tiles` writes a skeleton from a
go.mod, and `tilegen -out . SPEC` generates the file with its two holes;
[TILES.md](TILES.md) has the rest.

## What keeps a change honest

- `testdata/catalogue.{txt,json}` and `testdata/decisions/*.json` are
  golden files: the catalogue, and every example's decisions with their
  reasons. A change that alters either fails until it is run with
  `go test -update` and the diff is read.
- A test generates a tile and builds tilegen with it in place, and checks
  that every function and file a tile task names exists.
- Each package has tests of its own beside it. The tests at the root are
  the ones that run the whole compiler.
