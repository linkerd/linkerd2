#!/usr/bin/env bash
# Writes the reviewer's inputs to .review/: the PR's metadata and diff at the
# reviewed commit, and the upstream repositories gh-read.sh may read.
#
# Env: GH_TOKEN, GITHUB_REPOSITORY, PR, SHA, UPSTREAM_REPOS (space-separated
# OWNER/REPO).

set -euo pipefail

dir=.review
mkdir -p "$dir"

gh pr view "$PR" --repo "$GITHUB_REPOSITORY" \
  --json headRefOid,title,body,commits,files >"$dir/pr.json"
head=$(jq -r .headRefOid "$dir/pr.json")
if [ "$head" != "$SHA" ]; then
  echo "::error::PR head moved from $SHA to $head; the new commit gets its own review."
  exit 1
fi
gh pr diff "$PR" --repo "$GITHUB_REPOSITORY" >"$dir/pr.diff"

tr ' ' '\n' <<<"$UPSTREAM_REPOS" | sed '/^$/d' >"$dir/upstream-repos"
