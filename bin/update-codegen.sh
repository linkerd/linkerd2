#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(dirname "${BASH_SOURCE[0]}")"
SCRIPT_ROOT="$(dirname "${SCRIPT_DIR}")"
GEN_VER=$( awk '/k8s.io\/code-generator/ { print $2 }' "${SCRIPT_ROOT}/go.mod" )
KUBE_OPEN_API_VER=$( awk '/k8s.io\/kube-openapi/ { print $2 }' "${SCRIPT_ROOT}/go.mod" )
CODEGEN_PKG=$(mktemp -d -t "code-generator-${GEN_VER}.XXX")/code-generator

git clone --depth 1 --branch "$GEN_VER" https://github.com/kubernetes/code-generator "$CODEGEN_PKG"
(
    cd "$CODEGEN_PKG"
    go get k8s.io/kube-openapi/pkg/common@"$KUBE_OPEN_API_VER"
)

# Remove previously generated code
rm -rf "${SCRIPT_ROOT}/controller/gen/client/clientset/*"
rm -rf "${SCRIPT_ROOT}/controller/gen/client/listeners/*"
rm -rf "${SCRIPT_ROOT}/controller/gen/client/informers/*"
crds=(serviceprofile server serverauthorization link policy policy externalworkload)
for crd in "${crds[@]}"
do
  rm -f "${SCRIPT_ROOT}"/controller/gen/apis/"${crd}"/*/zz_generated.deepcopy.go
done

# shellcheck disable=SC1091
source "${CODEGEN_PKG}/kube_codegen.sh"

# Create a symlink so that the root of the repo is inside github.com/linkerd/linkerd2.
# This is required because the codegen scripts expect it.
mkdir -p "${SCRIPT_ROOT}/github.com/linkerd"
ln -s "$(realpath "${SCRIPT_ROOT}")" "${SCRIPT_ROOT}/github.com/linkerd/linkerd2"

kube::codegen::gen_helpers \
    --boilerplate "${SCRIPT_ROOT}/controller/gen/boilerplate.go.txt" \
    github.com/linkerd/linkerd2/controller/gen/apis

if [[ -n "${API_KNOWN_VIOLATIONS_DIR:-}" ]]; then
    report_filename="${API_KNOWN_VIOLATIONS_DIR}/codegen_violation_exceptions.list"
    if [[ "${UPDATE_API_KNOWN_VIOLATIONS:-}" == "true" ]]; then
        update_report="--update-report"
    fi
fi

kube::codegen::gen_openapi \
    --output-pkg github.com/linkerd/linkerd2/controller/gen \
    --output-dir "${SCRIPT_ROOT}/controller/gen/client" \
    --report-filename "${report_filename:-"/dev/null"}" \
    ${update_report:+"${update_report}"} \
    --boilerplate "${SCRIPT_ROOT}/controller/gen/boilerplate.go.txt" \
    github.com/linkerd/linkerd2/controller/gen/apis

kube::codegen::gen_client \
    --with-watch \
    --output-pkg github.com/linkerd/linkerd2/controller/gen/client \
    --output-dir "${SCRIPT_ROOT}/controller/gen/client" \
    --boilerplate "${SCRIPT_ROOT}/controller/gen/boilerplate.go.txt" \
    github.com/linkerd/linkerd2/controller/gen/apis

# Once the code has been generated, we can remove the symlink.
rm -rf "${SCRIPT_ROOT}/github.com"
