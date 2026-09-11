# Changelog

## Unreleased

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
