#!/bin/bash

set -eu

git_sha_head() {
    git rev-parse --short=8 HEAD
}

go_deps_sha() {
    bindir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
    rootdir="$( cd $bindir/.. && pwd )"
    cat $rootdir/go.mod $rootdir/Dockerfile-go-deps | shasum - | awk '{print $1}' |cut -c 1-8
}

clean_head() {
    [ -n "${CI_FORCE_CLEAN:-}" ] || git diff-index --quiet HEAD --
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

validate_tag() {
    file="$1"
    shift

    image="$1"
    shift

    sha="$1"
    shift

    dockerfile_tag=$(grep -oe $image':[^ ]*' $file) || true
    deps_tag="$image:$sha"
    if [ "$dockerfile_tag" != "" ] && [ "$dockerfile_tag" != "$deps_tag" ]; then
        echo "Tag in "$file" does not match source tree:"
        echo $dockerfile_tag" ("$file")"
        echo $deps_tag" (source)"
        return 3
    fi
}

# This function should be called by any docker-build-* script that relies on Go
# dependencies. To confirm the set of scripts that should call this function,
# run:
# $ grep -ER 'docker-build-go-deps' .

validate_go_deps_tag() {
    file="$1"
    validate_tag "$file" "gcr.io/linkerd-io/go-deps" "$(go_deps_sha)"
}
