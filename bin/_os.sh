#!/usr/bin/env bash

set -eu

export OS_ARCH_ALL="linux-amd64 linux-arm64 linux-arm darwin darwin-arm64 windows"

architecture() {
  arch=$(uname -m)
  case $arch in
    x86_64)
      arch=amd64
      ;;
    armv8*)
      arch=arm64
      ;;
    aarch64*)
      arch=arm64
      ;;
    armv*)
      arch=arm
      ;;
    amd64|arm64)
      arch=$arch
      ;;
    *)
      echo "unsupported architecture: $arch" >&2
      exit 1
      ;;
  esac
  echo "$arch"
}

os() {
  os=$(uname -s)
  arch=""
  case $os in
    CYGWIN* | MINGW64*)
      os=windows
      ;;
    Darwin)
      os=darwin
      ;;
    Linux)
      os=linux
      arch=$(architecture)
      ;;
    *)
      echo "unsupported os: $os" >&2
      exit 1
      ;;
  esac

  if [ -n "$arch" ]; then
    echo "$os-$arch"
  else
    echo "$os"
  fi
}
