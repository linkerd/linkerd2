#!/bin/sh
#
# docker
#

set -eu

. bin/_log.sh

# TODO this should be set to the canonical public docker regsitry; we can override this
# docker regsistry in, for instance, CI.
export DOCKER_REGISTRY="${DOCKER_REGISTRY:-gcr.io/runconduit}"

# When set, causes docker's build output to be emitted to stderr.
export DOCKER_TRACE="${DOCKER_TRACE:-}"

docker_repo() {
    repo="$1"

    name="$repo"
    if [ -n "${DOCKER_REGISTRY:-}" ]; then
        name="$DOCKER_REGISTRY/$name"
    fi

    echo "$name"
}

docker_tags() {
    image="$1"
    docker image ls "${image}" | sed 1d | awk '{print $2}'
}

docker_build() {
    dir="$1"
    shift

    repo="$1"
    shift

    tag="$1"
    shift

    file="$1"
    shift

    extra="$@"

    output="/dev/null"
    if [ -n "$DOCKER_TRACE" ]; then
        output="/dev/stderr"
    fi

    # Even when we haven't built an image locally, we can try to use a known prior version
    # of the image to prevent rebuilding layers.
    if [ -n "${DOCKER_BUILD_CACHE_FROM_TAG:-}" ]; then
        if [ -n "$extra" ]; then
            extra="$extra "
        fi
        extra="${extra}--cache-from='$repo:${DOCKER_BUILD_CACHE_FROM_TAG}'"
    fi

    log_debug "  :; docker build $dir -t $repo:$tag -f $file $extra"
    docker build "$dir" \
        -t "$repo:$tag" \
        -f "$file" \
        $extra \
        > "$output"

    echo "$repo:$tag"
}

# Builds a docker image if it doesn't exist and/or can't be found.
#
# If the `tag` is 'latest', an image will always be built.
docker_maybe_build() {
    dir="$1"
    shift

    repo="$1"
    shift

    tag="$1"
    shift

    file="$1"
    shift

    extra="$@"

    if [ -z "${DOCKER_FORCE_BUILD:-}" ]; then
        docker pull "${repo}:${tag}" >/dev/null 2>&1 || true

        for t in $(docker_tags "${repo}:${tag}") ; do
            if [ "$t" = "$tag" ]; then
                docker tag "${repo}:${tag}" "${repo}:latest" >/dev/null

                echo "${repo}:${tag}"
                return 0
            fi
        done
    fi

    docker_build "$dir" "$repo" "$tag" "$file" $extra
}

docker_pull() {
    repo=$(docker_repo "$1")
    tag="$2"
    log_debug "  :; docker pull $repo:$tag"
    docker pull "$repo:$tag"
}

docker_push() {
    repo=$(docker_repo "$1")
    tag="$2"
    log_debug "  :; docker push $repo:$tag"
    docker push "$repo:$tag"
}

docker_retag() {
    repo=$(docker_repo "$1")
    from="$2"
    to="$3"
    log_debug "  :; docker tag $repo:$from $repo:$to"
    docker tag "$repo:$from" "$repo:$to"
    echo "$repo:$to"
}
