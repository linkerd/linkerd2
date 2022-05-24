#!/usr/bin/env bash

set -eu

docker buildx &> /dev/null || { echo 'Please install docker buildx before proceeding'; exit 1; }

bindir=$( cd "${BASH_SOURCE[0]%/*}" && pwd )

# shellcheck source=_log.sh
. "$bindir"/_log.sh
# shellcheck source=_os.sh
. "$bindir"/_os.sh

# TODO this should be set to the canonical public docker registry; we can override this
# docker registry in, for instance, CI.
export DOCKER_REGISTRY=${DOCKER_REGISTRY:-cr.l5d.io/linkerd}

# buildx cache directory
export DOCKER_BUILDKIT_CACHE=${DOCKER_BUILDKIT_CACHE:-}

export DOCKER_TARGET=${DOCKER_TARGET:-$(os)}

# When set together with DOCKER_TARGET=multi-arch, it will push the multi-arch images to the registry
export DOCKER_PUSH=${DOCKER_PUSH:-}

# Default supported docker image architectures
export SUPPORTED_ARCHS=${SUPPORTED_ARCHS:-linux/amd64,linux/arm64,linux/arm/v7}

# Splitting of DOCKER_IMAGES variable is desired.
# shellcheck disable=SC2206
export DOCKER_IMAGES=(${DOCKER_IMAGES:-
    cli-bin
    cni-plugin
    controller
    policy-controller
    metrics-api
    debug
    proxy
    web
    jaeger-webhook
    tap
})

docker_repo() {
    repo=$1

    name=$repo
    if [ "${DOCKER_REGISTRY:-}" ]; then
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

    rootdir=${ROOTDIR:-$( cd "$bindir"/.. && pwd )}
    cache_params=""

    if [ "$DOCKER_BUILDKIT_CACHE" ]; then
      cache_params="--cache-from type=local,src=${DOCKER_BUILDKIT_CACHE} --cache-to type=local,dest=${DOCKER_BUILDKIT_CACHE},mode=max"
    fi

    output_params="--load"
    if [ "$DOCKER_TARGET" = 'multi-arch' ]; then
      output_params="--platform $SUPPORTED_ARCHS"
      if [ "$DOCKER_PUSH" ]; then
        output_params+=" --push"
      else
        echo 'Error: env DOCKER_PUSH=1 is missing
When building the multi-arch images it is required to push the images to the registry
See https://github.com/docker/buildx/issues/59 for more details'
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

docker_rename_registry() {
  tag=$1
  from=$2
  to=$3
  for img in "${DOCKER_IMAGES[@]}" ; do
    docker tag "$from/$img:$tag" "$to/$img:$tag"
  done
}
