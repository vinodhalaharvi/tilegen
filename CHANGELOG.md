# Changelog

## Unreleased

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
