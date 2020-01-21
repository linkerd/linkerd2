#!/usr/bin/env bash

set -eu

# ROOT_PACKAGE :: the package that is the target for code generation
ROOT_PACKAGE=github.com/linkerd/linkerd2
# CUSTOM_RESOURCE_NAME :: the name of the custom resource that we're generating client code for
CUSTOM_RESOURCE_NAME=serviceprofile
# CUSTOM_RESOURCE_VERSION :: the version of the resource
CUSTOM_RESOURCE_VERSION=v1alpha2

SCRIPT_ROOT=$(git rev-parse --show-toplevel)

# Grab code-generator version from go.sum.
CODEGEN_VERSION=$(grep 'k8s.io/code-generator' go.sum | awk '{print $2}' | head -1)
CODEGEN_PKG=$(echo `go env GOPATH`"/pkg/mod/k8s.io/code-generator@${CODEGEN_VERSION}")

# code-generator does work with go.mod but makes assumptions about
# the project living in `$GOPATH/src`. To work around this and support
# any location; create a temporary directory, use this as an output
# base, and copy everything back once generated.
TEMP_DIR=$(mktemp -d)
cleanup() {
    echo ">> Removing ${TEMP_DIR}"
    rm -rf ${TEMP_DIR}
}
trap "cleanup" EXIT SIGINT

echo ">> Temporary output directory ${TEMP_DIR}"

# Ensure we can execute.
chmod +x ${CODEGEN_PKG}/generate-groups.sh

${CODEGEN_PKG}/generate-groups.sh all \
    "$ROOT_PACKAGE/controller/gen/client" "$ROOT_PACKAGE/controller/gen/apis" \
    "$CUSTOM_RESOURCE_NAME:$CUSTOM_RESOURCE_VERSION" \
    --output-base "${TEMP_DIR}" \
    --go-header-file ${SCRIPT_ROOT}/bin/boilerplate.go.txt

# Copy everything back.
cp -r "${TEMP_DIR}/${ROOT_PACKAGE}/." "${SCRIPT_ROOT}/"
