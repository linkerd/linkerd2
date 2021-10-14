#!/usr/bin/env sh

set -eu

bindir=$( cd "${0%/*}" && pwd )
rootdir=$( cd "$bindir"/.. && pwd )
gen_ver=$( awk '/k8s.io\/code-generator/ { print $2 }' "$rootdir/go.mod" )
codegen_pkg=${GOPATH}/pkg/mod/k8s.io/code-generator@${gen_ver}

# ROOT_PACKAGE :: the package that is the target for code generation
ROOT_PACKAGE=github.com/linkerd/linkerd2

# The name and version of the service profile CRD that we generate client code
# for.
service_profile_name=serviceprofile
service_profile_versions=v1alpha2

# The name and version of the server CRD that we generate client code for.
server_name=server
server_version=v1beta1

# The name and version of the server authorization CRD that we generate client
# code for
server_authorization_name=serverauthorization
server_authorization_version=v1beta1

rm -f "${rootdir}/controller/gen/apis/$service_profile_name/$service_profile_versions/zz_generated.deepcopy.go"
rm -f "${rootdir}/controller/gen/apis/$server_name/$server_version/zz_generated.deepcopy.go"
rm -f "${rootdir}/controller/gen/apis/$server_authorization_name/$server_authorization_version/zz_generated.deepcopy.go"
rm -rf "${rootdir}/controller/gen/client"
rm -rf "${GOPATH}/src/${ROOT_PACKAGE}/controller/gen"

chmod +x "${codegen_pkg}/generate-groups.sh"

# run the code-generator entrypoint script
GO111MODULE='on' "${codegen_pkg}/generate-groups.sh" \
  'deepcopy,client,informer,lister' \
  "${ROOT_PACKAGE}/controller/gen/client" \
  "${ROOT_PACKAGE}/controller/gen/apis" \
  "$service_profile_name:$service_profile_versions $server_name:$server_version $server_authorization_name:$server_authorization_version" \
  --go-header-file "${codegen_pkg}"/hack/boilerplate.go.txt

# copy generated code out of GOPATH
cp -R "${GOPATH}/src/${ROOT_PACKAGE}/controller/gen" 'controller/'
