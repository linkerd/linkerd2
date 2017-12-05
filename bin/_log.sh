#!/bin/sh

set -eu

# debug logging is enabled by default and may be disabled with BUILD_DEBUG=
#export BUILD_DEBUG="${BUILD_DEBUG:-}"

export TRACE="${TRACE:-}"
if [ -n "$TRACE" ]; then
    set -x
fi

log_debug() {
    if [ -z "$TRACE" ] && [ -n "${BUILD_DEBUG:-}" ]; then
        echo "$@" >&2
    fi
}
