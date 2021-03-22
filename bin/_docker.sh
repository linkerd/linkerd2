#!/usr/bin/env bash

set -eu

docker buildx &> /dev/null || { echo 'Please install docker buildx before proceeding'; exit 1; }

bindir=$( cd "${BASH_SOURCE[0]%/*}" && pwd )

# shellcheck source=_log.sh
. "$bindir"/_log.sh

# TODO this should be set to the canonical public docker registry; we can override this
# docker registry in, for instance, CI.
export DOCKER_REGISTRY=${DOCKER_REGISTRY:-cr.l5d.io/linkerd}

# buildx cache directory
export DOCKER_BUILDKIT_CACHE=${DOCKER_BUILDKIT_CACHE:-}

# build the multi-arch images. Currently DOCKER_PUSH is also required
export DOCKER_MULTIARCH=${DOCKER_MULTIARCH:-}

# When set together with DOCKER_MULTIARCH, it will push the multi-arch images to the registry
export DOCKER_PUSH=${DOCKER_PUSH:-}

# Default supported docker image architectures
export SUPPORTED_ARCHS=${SUPPORTED_ARCHS:-linux/amd64,linux/arm64,linux/arm/v7}

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

    rootdir=$( cd "$bindir"/.. && pwd )
    cache_params=""

    if [ -n "$DOCKER_BUILDKIT_CACHE" ]; then
      cache_params="--cache-from type=local,src=${DOCKER_BUILDKIT_CACHE} --cache-to type=local,dest=${DOCKER_BUILDKIT_CACHE},mode=max"
    fi

    output_params="--load"
    if [ -n "$DOCKER_MULTIARCH" ]; then
      output_params="--platform $SUPPORTED_ARCHS"
      if [ -n "$DOCKER_PUSH" ]; then
        output_params+=" --push"
      else
        echo "Error: env DOCKER_PUSH=1 is missing"
        echo "When building the multi-arch images it is required to push the images to the registry"
        echo "See https://github.com/docker/buildx/issues/59 for more details"
        exit 1
      fi
    fi

    log_debug "  :; docker buildx $rootdir $cache_params $output_params -t $repo:$tag -f $file $*"
    # shellcheck disable=SC2086
    docker buildx build "$rootdir" $cache_params \
        $output_params \
        -t "$repo:$tag" \
        -f "$file" \
        "$@"

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
