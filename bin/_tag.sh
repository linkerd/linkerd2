#!/bin/sh

set -eu

git_sha_head() {
    git rev-parse --short=8 HEAD
}

clean_head() {
    git diff-index --quiet HEAD --
}

named_tag() {
    echo "$(git name-rev --tags --name-only $(git_sha_head))"
}

head_root_tag() {
    if clean_head ; then
        clean_head_root_tag
    else
        echo "dev-$(git_sha_head)-$USER"
    fi
}

clean_head_root_tag() {
    if clean_head ; then
        if [ "$(named_tag)" != "undefined" ]; then
            echo "$(named_tag)"
        else
            echo "git-$(git_sha_head)"
        fi
    else
        echo "Commit unstaged changes." >&2
        exit 3
    fi
}
