#!/usr/bin/env bash
# Personalizes tilegen, verifies it, creates the GitHub repo, adds topics,
# and publishes v0.1.0.
#
# Prereqs: go, git, make, perl, gh (run `gh auth login` first).
# Usage:   ./bootstrap.sh              # public repo named tilegen
#          REPO=mytool VISIBILITY=private ./bootstrap.sh
set -euo pipefail

REPO="${REPO:-tilegen}"
VISIBILITY="${VISIBILITY:-public}" # public | private | internal
VERSION="v0.1.0"

command -v gh >/dev/null || { echo "Install the GitHub CLI: https://cli.github.com" >&2; exit 1; }
gh auth status >/dev/null 2>&1 || { echo "Run: gh auth login" >&2; exit 1; }

OWNER="$(gh api user --jq .login)"
AUTHOR="$(git config user.name || true)"
AUTHOR="${AUTHOR:-$OWNER}"
MODULE="github.com/$OWNER/$REPO"
echo "==> $MODULE (author: $AUTHOR)"

# 1. Personalize module path, license holder, README links.
go mod edit -module "$MODULE"
AUTHOR="$AUTHOR" perl -pi -e 's/YOUR NAME/$ENV{AUTHOR}/g' LICENSE
MODULE="$MODULE" perl -pi -e 's#github\.com/OWNER/tilegen#$ENV{MODULE}#g' README.md

# 2. Newest x/tools and x/mod (newer goimports stdlib index), then verify
#    before anything leaves your machine.
go get golang.org/x/tools@latest golang.org/x/mod@latest
go mod tidy
make check
make demo

# 3. Commit and create the GitHub repo in one step.
[ -d .git ] || git init -q -b main
git add -A
git commit -q -m "tilegen $VERSION: S-expression lowering + tree tiling to Go scaffolding"
gh repo create "$REPO" --"$VISIBILITY" --source=. --remote=origin --push \
  --description "Compile S-expression specs into Go scaffolding via lowering passes and tree tiling, leaving typed holes for an LLM."

# 4. Topics (lowercase, hyphens, max 20).
gh repo edit "$OWNER/$REPO" --add-topic \
  go,golang,code-generation,codegen,s-expressions,dsl,compiler,instruction-selection,tree-tiling,scaffolding,llm,sqlc,go-ast,goimports

# 5. Tag and release.
git tag -a "$VERSION" -m "tilegen $VERSION"
git push origin "$VERSION"
gh release create "$VERSION" --verify-tag --title "$VERSION" --notes-file CHANGELOG.md

echo
echo "==> https://github.com/$OWNER/$REPO"
echo "==> go install $MODULE@$VERSION"
