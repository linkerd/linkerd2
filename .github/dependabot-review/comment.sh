#!/usr/bin/env bash
# Appends stdin to the PR's review comment, creating it if missing. Existing
# text is never changed.
#
# Env: GH_TOKEN, GITHUB_REPOSITORY, PR.

set -euo pipefail

marker='<!-- dependabot-ai-review -->'
add=$(cat)

id=$(gh api --paginate "repos/$GITHUB_REPOSITORY/issues/$PR/comments" \
  --jq ".[] | select(.user.login == \"github-actions[bot]\"
                     and (.body | startswith(\"$marker\"))) | .id" | awk 'NR == 1')

if [ -z "$id" ]; then
  body="$marker
## 🤖 Automated dependency review

_Written by an AI reviewer (Claude) in the Dependabot review workflow. This is not a human review._

$add"
  gh api "repos/$GITHUB_REPOSITORY/issues/$PR/comments" -f body="$body" >/dev/null
else
  old=$(gh api "repos/$GITHUB_REPOSITORY/issues/comments/$id" --jq .body)
  gh api -X PATCH "repos/$GITHUB_REPOSITORY/issues/comments/$id" \
    -f body="$old
$add" >/dev/null
fi
