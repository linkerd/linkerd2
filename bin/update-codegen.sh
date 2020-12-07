#!/usr/bin/env sh

set -eu

bindir=$( cd "${0%/*}" && pwd )
rootdir=$( cd "$bindir"/.. && pwd )
gen_ver=$( awk '/k8s.io\/code-generator/ { print $2 }' "$rootdir/go.mod" )
codegen_pkg=${GOPATH}/pkg/mod/k8s.io/code-generator@${gen_ver}

# ROOT_PACKAGE :: the package that is the target for code generation
ROOT_PACKAGE=github.com/linkerd/linkerd2
# CUSTOM_RESOURCE_NAME :: the name of the custom resource that we're generating client code for
CUSTOM_RESOURCE_NAME=serviceprofile
# CUSTOM_RESOURCE_VERSION :: the version of the resource
CUSTOM_RESOURCE_VERSION=v1alpha2

rm -f "${rootdir}/controller/gen/apis/${CUSTOM_RESOURCE_NAME}/${CUSTOM_RESOURCE_VERSION}/zz_generated.deepcopy.go"
rm -rf "${rootdir}/controller/gen/client"
rm -rf "${GOPATH}/src/${ROOT_PACKAGE}/controller/gen"

chmod +x "${codegen_pkg}/generate-groups.sh"

# run the code-generator entrypoint script
GO111MODULE='on' "${codegen_pkg}/generate-groups.sh" \
  'deepcopy,client,informer,lister' \
  "${ROOT_PACKAGE}/controller/gen/client" \
  "${ROOT_PACKAGE}/controller/gen/apis" \
  "${CUSTOM_RESOURCE_NAME}:${CUSTOM_RESOURCE_VERSION}" \
  --go-header-file "${codegen_pkg}"/hack/boilerplate.go.txt

# copy generated code out of GOPATH
cp -R "${GOPATH}/src/${ROOT_PACKAGE}/controller/gen" 'controller/'
