#!/bin/sh

set -eu

git_sha() {
    git rev-parse "$1" | cut -c 1-8
}

proxy_deps_sha() {
    cat Cargo.lock proxy/Dockerfile-deps | shasum - | awk '{print $1}' |cut -c 1-8
}

go_deps_sha() {
    cat Gopkg.lock Dockerfile-go-deps | shasum - | awk '{print $1}' |cut -c 1-8
}

dir_tag() {
    dir="$1"
    echo "git-$(git log -n 1 --format="%h" "$dir")"
}

clean_head_root_tag() {
    if git diff-index --quiet HEAD -- ; then
        echo "git-$(git_sha HEAD)"
    else
        echo "Commit unstaged changes or set an explicit build tag." >&2
        exit 3
    fi
}

master_root_tag() {
    echo "git-$(git_sha master)"
}
