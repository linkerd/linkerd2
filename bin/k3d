#!/usr/bin/env sh

set -eu

bindir=$( cd "${0%/*}" && pwd )
targetbin=$( cd "$bindir"/.. && pwd )/target/bin
k3dbin=$targetbin/.k3d

if [ ! -f "$k3dbin" ]; then
  if [ "$(uname -s)" = Darwin ]; then
    os=darwin
    arch=amd64
  elif [ "$(uname -o)" = Msys ]; then
    os=windows
    arch=amd64
  else
    os=linux
    case $(uname -m) in
      x86_64) arch=amd64 ;;
      arm) arch=arm64 ;;
    esac
  fi

  mkdir -p "$targetbin"
  "$bindir"/scurl -o "$k3dbin" https://github.com/rancher/k3d/releases/latest/download/k3d-$os-$arch
  chmod +x "$k3dbin"
fi

"$k3dbin" "$@"

