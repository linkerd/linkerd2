#!/usr/bin/env bash

set -eu

bindir=$( cd "${0%/*}" && pwd )
rootdir=$( cd "$bindir"/.. && pwd )
gen_ver=$( awk '/k8s.io\/code-generator/ { print $2 }' "$rootdir/go.mod" )
codegen_pkg=${GOPATH}/pkg/mod/k8s.io/code-generator@${gen_ver}

# ROOT_PACKAGE :: the package that is the target for code generation
ROOT_PACKAGE=github.com/linkerd/linkerd2

crds=(serviceprofile:v1alpha2 server:v1beta1 serverauthorization:v1beta1 link:v1alpha1 policy:v1alpha1)

# remove previously generated code
rm -rf "${rootdir}/controller/gen/client"
rm -rf "${GOPATH}/src/${ROOT_PACKAGE}/controller/gen"
for crd in "${crds[@]}"
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

# Temporary fix for https://github.com/kubernetes/code-generator/issues/135
sed -i 's/Group: \"server\"/Group: \"policy.linkerd.io\"/g' "${rootdir}/controller/gen/client/clientset/versioned/typed/server/v1beta1/fake/fake_server.go"
sed -i 's/Group: \"serverauthorization\"/Group: \"policy.linkerd.io\"/g' "${rootdir}/controller/gen/client/clientset/versioned/typed/serverauthorization/v1beta1/fake/fake_serverauthorization.go"
sed -i 's/Group: \"link\"/Group: \"multicluster.linkerd.io\"/g' "${rootdir}/controller/gen/client/clientset/versioned/typed/link/v1alpha1/fake/fake_link.go"
sed -i 's/Group: \"policy\"/Group: \"policy.linkerd.io\"/g' "${rootdir}/controller/gen/client/clientset/versioned/typed/policy/v1alpha1/fake/fake_authorizationpolicy.go"
sed -i 's/Group: \"policy\"/Group: \"policy.linkerd.io\"/g' "${rootdir}/controller/gen/client/clientset/versioned/typed/policy/v1alpha1/fake/fake_httproute.go"
sed -i 's/Group: \"policy\"/Group: \"policy.linkerd.io\"/g' "${rootdir}/controller/gen/client/clientset/versioned/typed/policy/v1alpha1/fake/fake_meshtlsauthentication.go"
sed -i 's/Group: \"policy\"/Group: \"policy.linkerd.io\"/g' "${rootdir}/controller/gen/client/clientset/versioned/typed/policy/v1alpha1/fake/fake_networkauthentication.go"
