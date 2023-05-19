#!/usr/bin/env bash
# shellcheck disable=SC2120
# (disabling SC2120 so that we can use functions with optional args
# see https://github.com/koalaman/shellcheck/wiki/SC2120#exceptions )

set -eu

extract_release_notes() {
  bindir=$( cd "${BASH_SOURCE[0]%/*}" && pwd )
  rootdir=$( cd "$bindir"/.. && pwd )

  if [ $# -eq 0 ]
  then
    # Make temporary file to save the release commit message into.
    tmp=$(mktemp -t release-commit-message.XXX.txt)
  else
    tmp="$rootdir/$1"
  fi

  # Save commit message into temporary file.
  #
  # Match each occurrence of the regex and increment `n` by 1. While n == 1
  # (which is true only for the first section) print that line of `CHANGES.md`.
  # This ends up being the first section of release changes.
  awk '/^## (edge|stable)-[0-9]+\.[0-9]+\.[0-9]+/{n++} n==1' "$rootdir"/CHANGES.md > "$tmp"

  echo "$tmp"
}
