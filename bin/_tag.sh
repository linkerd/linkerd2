#!/usr/bin/env bash

set -eu

git_sha_head() {
    git rev-parse --short=8 HEAD
}

clean_head() {
    [ -n "${CI_FORCE_CLEAN:-}" ] || git diff-index --quiet HEAD --
}

named_tag() {
    tag="$(git name-rev --tags --name-only "$(git_sha_head)")"
    tag=${tag%"^0"}
    echo "${tag}"
}

head_root_tag() {
    if clean_head ; then
        clean_head_root_tag
    else
        name=${USER//[^[:alnum:].-]/}
        echo "dev-$(git_sha_head)-$name"
    fi
}

clean_head_root_tag() {
    if clean_head ; then
        if [ "$(named_tag)" != undefined ]; then
            named_tag
        else
            echo "git-$(git_sha_head)"
        fi
    else
        echo 'Commit unstaged changes.' >&2
        exit 3
    fi
}
