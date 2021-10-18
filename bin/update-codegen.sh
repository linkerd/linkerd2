#!/usr/bin/env bash

set -eu

bindir=$( cd "${0%/*}" && pwd )
rootdir=$( cd "$bindir"/.. && pwd )
gen_ver=$( awk '/k8s.io\/code-generator/ { print $2 }' "$rootdir/go.mod" )
codegen_pkg=${GOPATH}/pkg/mod/k8s.io/code-generator@${gen_ver}

# ROOT_PACKAGE :: the package that is the target for code generation
ROOT_PACKAGE=github.com/linkerd/linkerd2

crds=(serviceprofile:v1alpha2 server:v1beta1 serverauthorization:v1beta1)

# remove previously generated code
rm -rf "${rootdir}/controller/gen/client"
rm -rf "${GOPATH}/src/${ROOT_PACKAGE}/controller/gen"
for crd in ${crds[@]}
do
  crd_path=$(tr : / <<< "$crd")
  rm -f "${rootdir}/controller/gen/apis/${crd_path}/zz_generated.deepcopy.go"
done


chmod +x "${codegen_pkg}/generate-groups.sh"

# run the code-generator entrypoint script
GO111MODULE='on' "${codegen_pkg}/generate-groups.sh" \
  'deepcopy,client,informer,lister' \
  "${ROOT_PACKAGE}/controller/gen/client" \
  "${ROOT_PACKAGE}/controller/gen/apis" \
  "${crds[*]}" \
  --go-header-file "${codegen_pkg}"/hack/boilerplate.go.txt

# copy generated code out of GOPATH
cp -R "${GOPATH}/src/${ROOT_PACKAGE}/controller/gen" 'controller/'
