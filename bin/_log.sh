#!/usr/bin/env sh
set -eu

# build debug logging is disabled by default; enable with BUILD_DEBUG=1
# shell trace logging is disabled by default; enable with TRACE=1

export TRACE="${TRACE:-}"
if [ -n "$TRACE" ]; then
    set -x
fi

log_debug() {
    if [ -z "$TRACE" ] && [ -n "${BUILD_DEBUG:-}" ]; then
        echo "$@" >&2
    fi
}
