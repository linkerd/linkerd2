#!/usr/bin/env bash
# Read-only GitHub REST access for the AI reviewer. GETs one path under
# repos/OWNER/REPO/ from github.com, for a repository listed in
# .review/upstream-repos. Nothing else reaches gh, so the reviewer cannot set
# the method, host, fields, headers, or an input file.
#
# Usage: gh-read.sh 'repos/OWNER/REPO[/PATH][?QUERY]'
# Env: GH_TOKEN.

set -euo pipefail

if [ $# -ne 1 ]; then
  echo "usage: gh-read.sh 'repos/OWNER/REPO[/PATH][?QUERY]'" >&2
  exit 2
fi

path=$1
re='^repos/([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)(/[A-Za-z0-9_.~^/-]*)?(\?[A-Za-z0-9_.=&%-]*)?$'
if ! [[ $path =~ $re ]] || [[ "/${path%%\?*}/" == */../* ]]; then
  echo "gh-read.sh: refusing path: $path" >&2
  exit 2
fi
repo=${BASH_REMATCH[1]}

# Resolved from this script's location, so the caller cannot point it at
# another list.
list="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/.review/upstream-repos"
if ! grep -qixF -- "$repo" "$list" 2>/dev/null; then
  echo "gh-read.sh: $repo is not an upstream repository for this PR" >&2
  exit 2
fi

# Raw media type returns file contents as text instead of base64.
accept=application/vnd.github+json
[[ $path =~ ^repos/[^/]+/[^/]+/contents/ ]] && accept=application/vnd.github.raw+json

exec gh api --hostname github.com --method GET -H "Accept: $accept" "$path"
