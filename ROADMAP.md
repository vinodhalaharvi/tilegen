# Roadmap

One milestone at a time, in order. A milestone ships as a tagged release when
every item's "done when" holds. The order is deliberate: **prove it, then
measure it, then optimize it.** The cost model (v0.4) needs real costs, and
real costs come from the measurements in v0.3.

## How we work

- Every item is one GitHub issue and one PR, with a test and a CHANGELOG line.
- Golden-file changes are reviewed on purpose (`make golden`), never waved through.
- `make check` and `make demo` stay green on `main`.
- This file is a plan, not a contract: update it when we learn something.

## v0.1.0 - Released

- [x] **S-expression reader, matcher, tiling engine** with positions carried through every pass.
- [x] **Passes: expand, concretize, select** with `-dump` after each.
- [x] **Emitters reusing modfile, go/parser, astutil, go/format, goimports, sqlc.**
- [x] **Safe regeneration**: go.mod merged, implementation files kept, holes tracked by AST.

## v0.2.0 - Released

Make what exists trustworthy before adding anything clever.

- [x] **Run demo-postgres with real sqlc in CI**: Done when CI runs `sqlc generate` on the postgres example and the result builds and vets. (v0.1 generated sqlc inputs without executing sqlc.)
- [x] **Multi-package example**: Done when an `examples/` spec has `billing` using `orders.Order`, the cross-package import resolves, and `make demo` builds it.
- [x] **Enum tile**: Done when `(enum Status pending paid shipped)` emits a string type, constants, and a `Valid()` method, covered by a golden test.
- [x] **`tilegen check` command**: Done when it exits non-zero if holes remain or generated files are stale, so CI can gate on it.
- [x] **Did-you-mean for uncovered forms**: Done when `(entiy ...)` warns `did you mean entity?` instead of silently becoming an LLM task.
- [x] **Module path without the network**: Done when a spec with `(github name)` and no `(module ...)` takes its module path from the project's go.mod, so plain generation never calls `gh` and every machine gets the same module; `gh` is only asked on the first `-git` run.

Shipped in this cycle beyond the plan, found by using tilegen on a real test project:

- [x] **Specs across files**: a folder of `.sexp` files, linked by a merge pass; `(config ...)` can live in the spec.
- [x] **Git and GitHub setup**: `(repo ...)` with starter files; `-git` clones or creates the repository, commits and adds topics; `-name`, `-dry-run`.
- [x] **Workstation**: `(workspace ...)` with git worktrees and a tmux session; `tilegen up`, `status`, `down`.
- [x] **Reconciliation**: the code follows the spec as forms come and go; your code is only added to, never changed.
- [x] **Store query ops**: `count`, `(list-by F)`, `(get-by F)`, `(count-by F)`, `(exists-by F)`, `(delete-by F)`, custom methods; sqlc queries for postgres.
- [x] **Implement any interface**: `(implement ...)`, embedded interfaces (standard library via Go's type checker), variadic parameters.
- [x] **go.mod never lowers versions** that `go mod tidy` raised.

## v0.3.0 - Handoff

Make the LLM step one command, and measure everything it does.

- [x] **`tilegen prompt <id>`**: Done when it prints one self-contained prompt per hole: instructions, contract, intent, constraints, and context files inlined.
- [x] **`tilegen fill`**: Done when `tilegen fill -llm "claude -p"` (any CLI that reads a prompt on stdin) fills each hole, splices the body in with go/ast, runs `go build`, retries once with the compiler error, and leaves failures as open holes.
- [ ] **Metrics log** (deferred until fill runs for real; build it with Experiment 1): Done when every fill attempt appends to `.tilegen/metrics.jsonl`: hole id, prompt and response size, token counts when the CLI reports them, attempts, and first-try build success.
- [ ] **Experiment 1: agent-only vs tilegen**: Done when `docs/experiment-1.md` compares building the same feature both ways on tokens, cost, time, and first-try build success.

## v0.4.0 - Costs

The compiler chooses between tiles, explains why, and stays stable.

- [x] **Tile registry**: Done when the storage backends are registered tiles that declare what they cover, produce and cost, and `tilegen tiles` lists the registry.
- [ ] **Event bus tile**: Done when `(events (event Name (field ...))...)` generates event structs, a typed bus interface and an in-process implementation, added through the registry without changing the core.
- [ ] **Legality guards on tiles**: Done when a tile can declare conditions (for example "no dynamic queries") and illegal tiles are skipped before costing.
- [ ] **Forms and chain rules**: Done when tiles declare the form they produce (`db-rows`, `domain`) and converters between forms are costed tiles, so a mapper's cost counts.
- [ ] **Bottom-up cost selection**: Done when select finds the cheapest covering by dynamic programming (iburg-style), with maximal munch kept as a fallback, and the sqlc-vs-pgx example picks pgx when the mapper is expensive.
- [ ] **Explain choices in the dump**: Done when the select dump shows, per node, the winning tile, its cost, and the runners-up.
- [ ] **`tilegen.lock`**: Done when choices are pinned across runs and only re-selected when a pinned tile becomes illegal or `-reselect` is passed.
- [ ] **Measured costs for LLM tiles**: Done when LLM tile costs come from v0.3 metrics instead of guesses.

## v0.5.0 - Intent

Describe what you want; tiling chooses how.

- [ ] **Tiles as data**: Done when tiles can be written in `.sexp` tile files and loaded as packs, not only in Go.
- [ ] **Policy file**: Done when `(policy (prefer ...) (avoid ...) (weights ...))` changes which tiles win without editing any tile.
- [ ] **Abstract storage**: Done when `(persistent relational)` plus project constraints resolves to a concrete backend through costed rewrites, visible in the dump.
- [ ] **Whole-stack choices**: Done when non-additive costs such as "one dependency, used everywhere" are handled by comparing candidate stacks, each costed per node.

## Talk track

Runs alongside v0.3. Submit once experiment 1 has numbers.

- [ ] **Talk abstract**: Done when submitted to a meetup.
- [ ] **Slides**: Done when the deck covers the hook, tiling in five minutes, the catch-all-tile reframe, the demo, and honest limits.
- [ ] **Recorded demo**: Done when there's a backup video of spec, dump, build, tasks, fill, and regenerate.

## Known gaps

- Generic interfaces (`Repo[T any]`) and named results are not supported.
- `implement` covers interfaces from the same package only.
- Tasks from `(llm ...)` and uncovered forms have no file, so they never close on their own.

## Not doing (for now)

Other target languages, a web UI, editor plugins, prebuilt release binaries
(`go install` is enough), and fuzzy tile matching. Adaptations stay as
explicit, costed rewrites so every choice can be explained.
