#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# ROOT_PACKAGE :: the package (relative to $GOPATH/src) that is the target for code generation
ROOT_PACKAGE="github.com/linkerd/linkerd2"
# CUSTOM_RESOURCE_NAME :: the name of the custom resource that we're generating client code for
CUSTOM_RESOURCE_NAME="serviceprofile"
# CUSTOM_RESOURCE_VERSION :: the version of the resource
CUSTOM_RESOURCE_VERSION="v1alpha1"

HACK_DIR=$(dirname "${BASH_SOURCE}")
REPO_ROOT=${HACK_DIR}/..

# run the code-generator entrypoint script

${REPO_ROOT}/vendor/k8s.io/code-generator/generate-groups.sh all "$ROOT_PACKAGE/controller/gen/client" "$ROOT_PACKAGE/controller/gen/apis" "$CUSTOM_RESOURCE_NAME:$CUSTOM_RESOURCE_VERSION"
