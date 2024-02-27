#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(dirname "${BASH_SOURCE[0]}")"
SCRIPT_ROOT="$(dirname "${SCRIPT_DIR}")"
GEN_VER=$( awk '/k8s.io\/code-generator/ { print $2 }' "${SCRIPT_ROOT}/go.mod" )
CODEGEN_PKG=target/code-generator-${GEN_VER}

if [[ ! -d "$CODEGEN_PKG" ]]; then
    mkdir -p "$CODEGEN_PKG"
    git clone --depth 1 --branch "$GEN_VER" https://github.com/kubernetes/code-generator "$CODEGEN_PKG"
fi

# Remove previously generated code
rm -rf "${SCRIPT_ROOT}/controller/gen/client/clientset/*"
rm -rf "${SCRIPT_ROOT}/controller/gen/client/listeners/*"
rm -rf "${SCRIPT_ROOT}/controller/gen/client/informers/*"
crds=(serviceprofile:v1alpha2 server:v1beta1 serverauthorization:v1beta1 link:v1alpha1 policy:v1alpha1 policy:v1beta3 externalworkload:v1beta1)
for crd in "${crds[@]}"
do
  crd_path=$(tr : / <<< "$crd")
  rm -f "${SCRIPT_ROOT}/controller/gen/apis/${crd_path}/zz_generated.deepcopy.go"
done

# shellcheck disable=SC1091
source "${CODEGEN_PKG}/kube_codegen.sh"

# Create a symlink so that the root of the repo is inside github.com/linkerd/linkerd2.
# This is required because the codegen scripts expect it.
mkdir -p "${SCRIPT_ROOT}/github.com/linkerd"
ln -s "$(realpath "${SCRIPT_ROOT}")" "${SCRIPT_ROOT}/github.com/linkerd/linkerd2"

kube::codegen::gen_helpers \
    --input-pkg-root github.com/linkerd/linkerd2/controller/gen/apis \
    --output-base "${SCRIPT_ROOT}" \
    --boilerplate "${SCRIPT_ROOT}/controller/gen/boilerplate.go.txt"

if [[ -n "${API_KNOWN_VIOLATIONS_DIR:-}" ]]; then
    report_filename="${API_KNOWN_VIOLATIONS_DIR}/codegen_violation_exceptions.list"
    if [[ "${UPDATE_API_KNOWN_VIOLATIONS:-}" == "true" ]]; then
        update_report="--update-report"
    fi
fi

kube::codegen::gen_openapi \
    --input-pkg-root github.com/linkerd/linkerd2/controller/gen/apis \
    --output-pkg-root github.com/linkerd/linkerd2/controller/gen\
    --output-base "${SCRIPT_ROOT}" \
    --report-filename "${report_filename:-"/dev/null"}" \
    ${update_report:+"${update_report}"} \
    --boilerplate "${SCRIPT_ROOT}/controller/gen/boilerplate.go.txt"

kube::codegen::gen_client \
    --with-watch \
    --input-pkg-root github.com/linkerd/linkerd2/controller/gen/apis \
    --output-pkg-root github.com/linkerd/linkerd2/controller/gen/client \
    --output-base "${SCRIPT_ROOT}" \
    --boilerplate "${SCRIPT_ROOT}/controller/gen/boilerplate.go.txt"

# Once the code has been generated, we can remove the symlink.
rm -rf "${SCRIPT_ROOT}/github.com"
