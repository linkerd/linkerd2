#!/bin/sh
#
# gcp -- mostly for CI
#

set -eu

. bin/_log.sh

get_k8s_ctx() {
    project="$1"
    zone="$2"
    cluster="$3"

    for c in $(kubectl config get-clusters |sed 1d) ; do
        if [ "$c" = "gke_${project}_${zone}_${cluster}" ]; then
            return 0
        fi
    done

    log_debug "  :; gcloud container clusters get-credentials $cluster"
    gcloud container clusters get-credentials "$cluster"
}
