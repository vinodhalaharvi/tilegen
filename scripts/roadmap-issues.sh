#!/usr/bin/env bash
# Creates GitHub milestones and issues from ROADMAP.md.
# Every "## heading" with unchecked items becomes a milestone, and every
# "- [ ] **Title**: body" line becomes an issue in it. Safe to re-run:
# milestones and issues that already exist (matched by title) are skipped.
# Run from anywhere inside the repo, after `gh auth login`.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
command -v gh >/dev/null || { echo "Install the GitHub CLI: https://cli.github.com" >&2; exit 1; }

gh label create roadmap --color 5319e7 --description "Planned in ROADMAP.md" --force >/dev/null

milestones="$(gh api 'repos/{owner}/{repo}/milestones?state=all&per_page=100' --jq '.[].title')"
section=""
head_re='^## (.+)$'
item_re='^- \[ \] \*\*([^*]+)\*\*:? *(.*)$'

while IFS= read -r line || [[ -n $line ]]; do
  if [[ $line =~ $head_re ]]; then
    section="${BASH_REMATCH[1]}"
    continue
  fi
  [[ -n $section && $line =~ $item_re ]] || continue
  title="${BASH_REMATCH[1]}"
  body="${BASH_REMATCH[2]}"

  if ! grep -qxF "$section" <<<"$milestones"; then
    gh api 'repos/{owner}/{repo}/milestones' -f title="$section" >/dev/null
    milestones+=$'\n'"$section"
    echo "milestone  $section"
  fi

  exists="$(gh issue list --state all --limit 200 --json title \
    --jq "map(select(.title == \"$title\")) | length")"
  if [[ $exists == 0 ]]; then
    gh issue create --title "$title" --milestone "$section" --label roadmap \
      --body "$body"$'\n\n'"Tracked in ROADMAP.md, milestone \"$section\"." >/dev/null
    echo "  issue    $title"
  else
    echo "  exists   $title"
  fi
done < ROADMAP.md
