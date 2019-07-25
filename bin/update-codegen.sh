#!/bin/bash

set -eu

GO111MODULE=on go mod vendor
git clone https://github.com/kubernetes/code-generator.git vendor/k8s.io/code-generator
# check out code-generator at the revision Gopkg.lock had previously referenced
git -C vendor/k8s.io/code-generator checkout aae79feb89bdded3679da91fd8c19b6dfcbdb79a

# ROOT_PACKAGE :: the package that is the target for code generation
ROOT_PACKAGE="github.com/linkerd/linkerd2"
# CUSTOM_RESOURCE_NAME :: the name of the custom resource that we're generating client code for
CUSTOM_RESOURCE_NAME="serviceprofile"
# CUSTOM_RESOURCE_VERSION :: the version of the resource
CUSTOM_RESOURCE_VERSION="v1alpha2"

bindir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
rootdir="$( cd $bindir/.. && pwd )"

# run the code-generator entrypoint script
${rootdir}/vendor/k8s.io/code-generator/generate-groups.sh all "$ROOT_PACKAGE/controller/gen/client" "$ROOT_PACKAGE/controller/gen/apis" "$CUSTOM_RESOURCE_NAME:$CUSTOM_RESOURCE_VERSION"
