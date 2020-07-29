#!/usr/bin/env bash

set -eu

bindir=$( cd "${BASH_SOURCE[0]%/*}" && pwd )

# shellcheck source=_log.sh
. "$bindir"/_log.sh

# TODO this should be set to the canonical public docker registry; we can override this
# docker registry in, for instance, CI.
export DOCKER_REGISTRY=${DOCKER_REGISTRY:-gcr.io/linkerd-io}

# When set, causes docker's build output to be emitted to stderr.
export DOCKER_TRACE=${DOCKER_TRACE:-}

# When set, use `docker buildx` and use the github actions cache to store/retrieve images
export DOCKER_BUILDKIT=${DOCKER_BUILDKIT:-}

# buildx cache directory. Needed if DOCKER_BUILDKIT is used
export DOCKER_BUILDKIT_CACHE=${DOCKER_BUILDKIT_CACHE:-}

# When set together with DOCKER_BUILDKIT, it will build and push the multi architecture images to the registry
export DOCKER_MULTIARCH=${DOCKER_MULTIARCH:-}

# Default supported docker image architectures
export SUPPORTED_ARCHS=linux/amd64,linux/arm64,linux/arm/v7

docker_repo() {
    repo=$1

    name=$repo
    if [ -n "${DOCKER_REGISTRY:-}" ]; then
        name="$DOCKER_REGISTRY/$name"
    fi

    echo "$name"
}

docker_build() {
    repo=$(docker_repo "$1")
    shift

    tag=$1
    shift

    file=$1
    shift

    output=/dev/null
    if [ -n "$DOCKER_TRACE" ]; then
        output=/dev/stderr
    fi

    rootdir=$( cd "$bindir"/.. && pwd )

    if [ -n "$DOCKER_BUILDKIT" ]; then
      cache_params=""
      if [ -n "$DOCKER_BUILDKIT_CACHE" ]; then
        cache_params="--cache-from type=local,src=${DOCKER_BUILDKIT_CACHE} --cache-to type=local,dest=${DOCKER_BUILDKIT_CACHE},mode=max"
      fi

      output_params="--load"
      if [ -n "$DOCKER_MULTIARCH" ]; then
        output_params="--push --platform $SUPPORTED_ARCHS"
      fi

      log_debug "  :; docker buildx $rootdir $cache_params $output_params -t $repo:$tag -f $file $*"
      # shellcheck disable=SC2086
      docker buildx build "$rootdir" $cache_params \
          $output_params \
          -t "$repo:$tag" \
          -f "$file" \
          "$@" \
          > "$output"
    else
      log_debug "  :; docker build $rootdir -t $repo:$tag -f $file $*"
      docker build "$rootdir" \
          -t "$repo:$tag" \
          -f "$file" \
          "$@" \
          > "$output"
    fi

    echo "$repo:$tag"
}

docker_pull() {
    repo=$(docker_repo "$1")
    tag=$2
    log_debug "  :; docker pull $repo:$tag"
    docker pull "$repo:$tag"
}

docker_push() {
    repo=$(docker_repo "$1")
    tag=$2
    log_debug "  :; docker push $repo:$tag"
    docker push "$repo:$tag"
}

docker_retag() {
    repo=$(docker_repo "$1")
    from=$2
    to=$3
    log_debug "  :; docker tag $repo:$from $repo:$to"
    docker tag "$repo:$from" "$repo:$to"
    echo "$repo:$to"
}
