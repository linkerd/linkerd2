set -eu

install_gcloud() {
    dir="$1"

    export CLOUDSDK_CORE_DISABLE_PROMPTS=1
    if [ -d "$dir/bin" ]; then
        . "$dir/path.bash.inc"
        gcloud components update
    else
        rm -rf "$dir"
        curl https://sdk.cloud.google.com | bash
        . "$dir/path.bash.inc"
    fi
}

set_gcloud_config() {
    project="$1"
    zone="$2"
    cluster="$3"

    gcloud auth activate-service-account --key-file .gcp.json
    gcloud config set core/project "$project"
    gcloud config set compute/zone "$zone"
    gcloud config set container/cluster "$cluster"
}

get_k8s_ctx() {
    project="$1"
    zone="$2"
    cluster="$3"

    for c in $(kubectl config get-clusters |sed 1d) ; do
        if [ "$c" = "gke_${project}_${zone}_${cluster}" ]; then
            return 0
        fi
    done

    gcloud container clusters get-credentials "$cluster"
}