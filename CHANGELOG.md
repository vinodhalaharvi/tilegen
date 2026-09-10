# Changelog

## Unreleased

- CI runs the postgres example through real sqlc (v1.31.1) and requires the
  result to build and vet. `make demo-postgres` explains how to install sqlc
  when it is missing, and honors `SQLC=/path/to/sqlc`.

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
