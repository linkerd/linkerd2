#!/bin/sh

set -eu

# Checks if target/helm exists and it installs it if it doesn't.
# Use `install_helm true` to also install Tiller.
install_helm() {
    bindir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
    rootdir="$( cd $bindir/.. && pwd )"

    if [ ! -f $rootdir/target/helm ]; then
        if [ "$(uname -s)" = "Darwin" ]; then
            os=darwin
        else
            os=linux
        fi
        helmcurl="https://get.helm.sh/helm-v2.14.3-${os}-amd64.tar.gz"
        targetdir="${os}-amd64"
        tmp=$(mktemp -d -t helm.XXX)
        mkdir -p target
        (
            cd "$tmp"
            curl -Lsf -o "./helm.tar.gz" "$helmcurl"
            tar zf "./helm.tar.gz" -x "$targetdir"
            chmod +x "$targetdir/helm"
        )
        mv "$tmp/$targetdir/helm" $rootdir/target
        rm -rf "$tmp"
    fi

    if [ $1 = true ]; then
        $rootdir/target/helm init
    fi
}