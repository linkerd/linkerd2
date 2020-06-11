#!/bin/bash

set -eu

set -x

handle_input() {
  export images=""
  export images_host=""

  while :
  do
    case $1 in
      -h|--help)
        echo "Load into KinD the images for Linkerd's proxy, controller, web, grafana, cli-bin, debug and cni-plugin."
        echo ""
        echo "Usage:"
        echo "    bin/kind-load [--images] [--images-host ssh://linkerd-docker]"
        echo ""
        echo "Examples:"
        echo ""
        echo "    # Load images from the local docker instance"
        echo "    bin/kind-load"
        echo ""
        echo "    # Load images from tar files located under the 'image-archives' directory"
        echo "    bin/kind-load --images"
        echo ""
        echo "    # Retrieve images from a remote docker instance and then load them into KinD"
        echo "    bin/kind-load --images --images-host ssh://linkerd-docker"
        echo ""
        echo "Available Commands:"
        echo "    --images: use 'kind load image-archive' to load the images from local .tar files in the current directory."
        echo "    --images-host: the argument to this option is used as the remote docker instance from which images are first retrieved"
        echo "                   (using 'docker save') to be then loaded into KinD. This command requires --images."
        exit 0
        ;;
      --images)
        images=1
        ;;
      --images-host)
        images_host=$2
        if [ -z "$images_host" ]; then
          echo "Error: the argument for --images-host was not specified"
          exit 1
        fi
        shift
        ;;
      *)
        break
    esac
    shift
  done

  if [ "$images_host" ] && [ -z "$images" ]; then
    echo "Error: --images-host needs to be used with --images" >&2
    exit 1
  fi

  export input_arg=${1:-''}
}
