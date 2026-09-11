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
| `(interface Name (method N (params (n T)...) (returns T...)))` | plain interface |
| `(llm "intent")` | explicit hole: pure intent, no structure yet |
| `(doc "...")` | doc comment on package, type, field, or method |

Types are Go syntax, parsed by `go/parser`. Quote them if they contain spaces
or parentheses: `(field F "func(int) error")`. Standard-library qualifiers
(`time`, `context`) need no declaration; anything else must come from a
`require` or a project package, or tilegen reports the exact line. Types from
another project package are written qualified (`orders.Order`); tilegen adds
the import and rejects import cycles between packages.

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
  (storage memory)      ; memory | postgres (emits sqlc inputs)
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

`tilegen.tasks.json` lists every open hole with `file`, `line`, `symbol`,
`contract`, `intent`, `constraints`, and `context_files`, plus instructions.
Give it to your LLM together with the listed files. Then:

1. The LLM replaces `panic("tilegen:hole <id>")` bodies.
2. `go build ./... && go vet ./...` - the compile-time assertions reject any
   drift from the interfaces.
3. Re-run tilegen at any time. Holes are found by parsing the real files, so
   filled ones drop off the list and line numbers stay current.

Rules of the road:

- `// Code generated ... DO NOT EDIT.` files (`*_gen.go`, `db/*.sql`,
  `sqlc.yaml`) are rewritten every run. Change the spec, not these.
- Implementation files are scaffolded once and are yours; tilegen never
  overwrites them.
- `go.mod` is merged, so `go mod tidy` results survive regeneration.

## Writing a tile

A tile is a pattern plus a function. For example, an enum tile for the
select pass:

```go
{Name: "enum", Pattern: Pat("(enum ?name ?values...)"),
	Then: func(m *Munch, b Bindings, n *Node) ([]*Node, error) {
		// return (go/...) target forms; call m.Sub(...) on any leaves
		// you want other tiles to cover
	}},
```

Put more specific patterns before general ones. If you keep giving an LLM
the same instructions, that convention is a tile waiting to be written.

## Background

- Andrew Appel, *Modern Compiler Implementation*, ch. 9: tree tiling, maximal
  munch, and "optimal" versus "optimum" tilings.
- Fraser, Hanson and Proebsting, *Engineering a Simple, Efficient
  Code-Generator Generator* (iburg, 1992).
- Go's own compiler rewrites SSA with S-expression rules in
  `src/cmd/compile/internal/ssa/_gen/*.rules`, compiled to Go by rulegen.

## Status

v0.1.0. Next ideas: an `enum` tile, numeric tile costs for competing tiles,
`tilegen check` to fail CI while holes remain, multi-package examples.

## License

MIT
