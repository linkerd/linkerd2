#!/usr/bin/env bash
# Copyright (c) 2018 Tigera, Inc. All rights reserved.
# Copyright 2018 Istio Authors
# Modifications copyright (c) Linkerd authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This file was inspired by:
# 1) https://github.com/projectcalico/cni-plugin/blob/c1175467c227c1656577c80bfc0ee7795da2e2bc/k8s-install/scripts/install-cni.sh
# 2) https://github.com/istio/cni/blob/c63a509539b5ed165a6617548c31b686f13c2133/deployments/kubernetes/install/scripts/install-cni.sh

# Script to install Linkerd CNI on a Kubernetes host.
# - Expects the host CNI binary path to be mounted at /host/opt/cni/bin.
# - Expects the host CNI network config path to be mounted at /host/etc/cni/net.d.
# - Expects the desired CNI config in the CNI_NETWORK_CONFIG env variable.

# Ensure all variables are defined, and that the script fails when an error is hit.
set -u -e

# Helper function for raising errors
# Usage:
# some_command || exit_with_error "some_command_failed: maybe try..."
exit_with_error() {
  echo "${1}"
  exit 1
}

# The directory on the host where existing CNI plugin configs are installed
# and where this script will write out its configuration through the container
# mount point. Defaults to /etc/cni/net.d, but can be overridden by setting
# DEST_CNI_NET_DIR.
DEST_CNI_NET_DIR=${DEST_CNI_NET_DIR:-/etc/cni/net.d}
# The directory on the host where existing CNI binaries are installed. Defaults to
# /opt/cni/bin, but can be overridden by setting DEST_CNI_BIN_DIR. The linkerd-cni
# binary will end up in this directory from the host's point of view.
DEST_CNI_BIN_DIR=${DEST_CNI_BIN_DIR:-/opt/cni/bin}
# The mount prefix of the host machine from the container's point of view.
# Defaults to /host, but can be overridden by setting CONTAINER_MOUNT_PREFIX.
CONTAINER_MOUNT_PREFIX=${CONTAINER_MOUNT_PREFIX:-/host}
# The location in the container where the linkerd-cni binary resides. Can be
# overridden by setting CONTAINER_CNI_BIN_DIR. The binary in this directory
# will be copied over to the host DEST_CNI_BIN_DIR through the mount point.
CONTAINER_CNI_BIN_DIR=${CONTAINER_CNI_BIN_DIR:-/opt/cni/bin}
# Directory path where CNI configuration should live on the host
HOST_CNI_NET="${CONTAINER_MOUNT_PREFIX}${DEST_CNI_NET_DIR}"
# Default path for when linkerd runs as a standalone CNI plugin
DEFAULT_CNI_CONF_PATH="${HOST_CNI_NET}/01-linkerd-cni.conf"
KUBECONFIG_FILE_NAME=${KUBECONFIG_FILE_NAME:-ZZZ-linkerd-cni-kubeconfig}

############################
### Function definitions ###
############################

# Cleanup will remove any installed configuration from the host If there are any
# *conflist files, then linkerd-cni configuration parameters will be removed
# from them; otherwise, if linkerd-cni is the only plugin, the configuration
# file will be removed.
cleanup() {
  # First, kill 'inotifywait' so we don't process any DELETE/CREATE events
  if [ "$(pgrep inotifywait)" ]; then
    echo 'Sending SIGKILL to inotifywait'
    kill -s KILL "$(pgrep inotifywait)"
  fi

  echo 'Removing linkerd-cni artifacts.'

  # Find all conflist files and print them out using a NULL separator instead of
  # writing each file in a new line. We will subsequently read each string and
  # attempt to rm linkerd config from it using jq helper.
  local cni_data=''
  find "${HOST_CNI_NET}" -maxdepth 1 -type f \( -iname '*conflist' \) -print0 |
    while read -r -d $'\0' file; do
      echo "Removing linkerd-cni config from $file"
      cni_data=$(jq 'del( .plugins[]? | select( .type == "linkerd-cni" ))' "$file")
      # TODO (matei): we should write this out to a temp file and then do a `mv`
      # to be atomic. 
      echo "$cni_data" > "$file"
    done

  # Check whether configuration file has been created by our own cni plugin
  # and if so, rm it.
  if [ -e "${DEFAULT_CNI_CONF_PATH}" ]; then
    echo "Cleaning up ${DEFAULT_CNI_CONF_PATH}"
    rm -f "${DEFAULT_CNI_CONF_PATH}"
  fi

  # Remove binary and kubeconfig file
  if [ -e "${HOST_CNI_NET}/${KUBECONFIG_FILE_NAME}" ]; then
    echo "Removing linkerd-cni kubeconfig: ${HOST_CNI_NET}/${KUBECONFIG_FILE_NAME}"
    rm -f "${HOST_CNI_NET}/${KUBECONFIG_FILE_NAME}"
  fi
  if [ -e "${CONTAINER_MOUNT_PREFIX}${DEST_CNI_BIN_DIR}"/linkerd-cni ]; then
    echo "Removing linkerd-cni binary: ${CONTAINER_MOUNT_PREFIX}${DEST_CNI_BIN_DIR}/linkerd-cni"
    rm -f "${CONTAINER_MOUNT_PREFIX}${DEST_CNI_BIN_DIR}/linkerd-cni"
  fi

  echo 'Exiting.'
  exit 0
}

# Capture the usual signals and exit from the script
trap 'echo "SIGINT received, simply exiting..."; cleanup' INT
trap 'echo "SIGTERM received, simply exiting..."; cleanup' TERM
trap 'echo "SIGHUP received, simply exiting..."; cleanup' HUP

# Install CNI bin will copy the linkerd-cni binary on the host's filesystem
install_cni_bin() {
  # Place the new binaries if the mounted directory is writeable.
  dir="${CONTAINER_MOUNT_PREFIX}${DEST_CNI_BIN_DIR}"
  if [ ! -w "${dir}" ]; then
    exit_with_error "${dir} is non-writeable, failure"
  fi
  for path in "${CONTAINER_CNI_BIN_DIR}"/*; do
    cp "${path}" "${dir}"/ || exit_with_error "Failed to copy ${path} to ${dir}."
  done

  echo "Wrote linkerd CNI binaries to ${dir}"
}

create_cni_conf() {
  # Create temp configuration and kubeconfig files
  #
  TMP_CONF='/tmp/linkerd-cni.conf.default'
  # If specified, overwrite the network configuration file.
  CNI_NETWORK_CONFIG_FILE="${CNI_NETWORK_CONFIG_FILE:-}"
  CNI_NETWORK_CONFIG="${CNI_NETWORK_CONFIG:-}"

  # If the CNI Network Config has been overwritten, then use template from file
  if [ -e "${CNI_NETWORK_CONFIG_FILE}" ]; then
    echo "Using CNI config template from ${CNI_NETWORK_CONFIG_FILE}."
    cp "${CNI_NETWORK_CONFIG_FILE}" "${TMP_CONF}"
  elif [ "${CNI_NETWORK_CONFIG}" ]; then
    echo 'Using CNI config template from CNI_NETWORK_CONFIG environment variable.'
    cat >"${TMP_CONF}" <<EOF
${CNI_NETWORK_CONFIG}
EOF
  fi

  SERVICE_ACCOUNT_PATH=/var/run/secrets/kubernetes.io/serviceaccount
  KUBE_CA_FILE=${KUBE_CA_FILE:-${SERVICE_ACCOUNT_PATH}/ca.crt}
  SKIP_TLS_VERIFY=${SKIP_TLS_VERIFY:-false}
  # Pull out service account token.
  SERVICEACCOUNT_TOKEN=$(cat ${SERVICE_ACCOUNT_PATH}/token)

  # Check if we're running as a k8s pod.
  # The check will assert whether token exists and is a regular file
  if [ -f "${SERVICE_ACCOUNT_PATH}/token" ]; then
    # We're running as a k8d pod - expect some variables.
    # If the variables are null, exit
    if [ -z "${KUBERNETES_SERVICE_HOST}" ]; then
      echo 'KUBERNETES_SERVICE_HOST not set'; exit 1;
    fi
    if [ -z "${KUBERNETES_SERVICE_PORT}" ]; then
      echo 'KUBERNETES_SERVICE_PORT not set'; exit 1;
    fi

    if [ "${SKIP_TLS_VERIFY}" = 'true' ]; then
      TLS_CFG='insecure-skip-tls-verify: true'
    elif [ -f "${KUBE_CA_FILE}" ]; then
      TLS_CFG="certificate-authority-data: $(base64 "${KUBE_CA_FILE}" | tr -d '\n')"
    fi

    # Write a kubeconfig file for the CNI plugin. Do this
    # to skip TLS verification for now. We should eventually support
    # writing more complete kubeconfig files. This is only used
    # if the provided CNI network config references it.
    touch "${CONTAINER_MOUNT_PREFIX}${DEST_CNI_NET_DIR}/${KUBECONFIG_FILE_NAME}"
    chmod "${KUBECONFIG_MODE:-600}" "${CONTAINER_MOUNT_PREFIX}${DEST_CNI_NET_DIR}/${KUBECONFIG_FILE_NAME}"
    cat > "${CONTAINER_MOUNT_PREFIX}${DEST_CNI_NET_DIR}/${KUBECONFIG_FILE_NAME}" <<EOF
# Kubeconfig file for linkerd CNI plugin.
apiVersion: v1
kind: Config
clusters:
- name: local
  cluster:
    server: ${KUBERNETES_SERVICE_PROTOCOL:-https}://[${KUBERNETES_SERVICE_HOST}]:${KUBERNETES_SERVICE_PORT}
    ${TLS_CFG}
users:
- name: linkerd-cni
  user:
    token: ${SERVICEACCOUNT_TOKEN}
contexts:
- name: linkerd-cni-context
  context:
    cluster: local
    user: linkerd-cni
current-context: linkerd-cni-context
EOF

  fi

  # Insert any of the supported "auto" parameters.
  grep '__KUBERNETES_SERVICE_HOST__' ${TMP_CONF} && sed -i s/__KUBERNETES_SERVICE_HOST__/"${KUBERNETES_SERVICE_HOST}"/g ${TMP_CONF}
  grep '__KUBERNETES_SERVICE_PORT__' ${TMP_CONF} && sed -i s/__KUBERNETES_SERVICE_PORT__/"${KUBERNETES_SERVICE_PORT}"/g ${TMP_CONF}
  # Check in container
  sed -i s/__KUBERNETES_NODE_NAME__/"${KUBERNETES_NODE_NAME:-$(hostname)}"/g ${TMP_CONF}
  sed -i s/__KUBECONFIG_FILENAME__/"${KUBECONFIG_FILE_NAME}"/g ${TMP_CONF}
  sed -i s/__CNI_MTU__/"${CNI_MTU:-1500}"/g ${TMP_CONF}

  # Use alternative command character "~", since these include a "/".
  sed -i s~__KUBECONFIG_FILEPATH__~"${DEST_CNI_NET_DIR}/${KUBECONFIG_FILE_NAME}"~g ${TMP_CONF}


  # Log the config file before inserting service account token.
  # This way auth token is not visible in the logs.
  echo "CNI config: $(cat ${TMP_CONF})"

  sed -i s/__SERVICEACCOUNT_TOKEN__/"${SERVICEACCOUNT_TOKEN:-}"/g ${TMP_CONF}
}

install_cni_conf() {
  local cni_conf_path=$1
 
  create_cni_conf
  local tmp_data=''
  local conf_data=''
  if [ -e "${cni_conf_path}" ]; then
   # Add the linkerd-cni plugin to the existing list
   tmp_data=$(cat "${TMP_CONF}")
   conf_data=$(jq --argjson CNI_TMP_CONF_DATA "${tmp_data}" -f /linkerd/filter.jq "${cni_conf_path}")
   echo "${conf_data}" > ${TMP_CONF}
  fi

  # If the old config filename ends with .conf, rename it to .conflist, because it has changed to be a list
  filename=${cni_conf_path##*/}
  extension=${filename##*.}
  # When this variable has a file, we must delete it later.
  old_file_path=
  if [ "${filename}" != '01-linkerd-cni.conf' ] && [ "${extension}" = 'conf' ]; then
   old_file_path=${cni_conf_path}
   echo "Renaming ${cni_conf_path} extension to .conflist"
   cni_conf_path="${cni_conf_path}list"
  fi

  if [ -e "${DEFAULT_CNI_CONF_PATH}" ] && [ "$cni_conf_path" != "${DEFAULT_CNI_CONF_PATH}" ]; then
   echo "Removing Linkerd's configuration file: ${DEFAULT_CNI_CONF_PATH}"
   rm -f "${DEFAULT_CNI_CONF_PATH}"
  fi

  # Move the temporary CNI config into place.
  mv "${TMP_CONF}" "${cni_conf_path}" || exit_with_error 'Failed to mv files.'
  [ -n "$old_file_path" ] && rm -f "${old_file_path}" && echo "Removing unwanted .conf file"

  echo "Created CNI config ${cni_conf_path}"
}

# Sync() is responsible for reacting to file system changes. It is used in
# conjunction with inotify events; sync() is called with the name of the file that
# has changed, the event type (which can be either 'CREATE' or 'DELETE'), and
# the previously observed SHA of the configuration file.
#
# Based on the changed file and event type, sync() might re-install the CNI
# plugin's configuration file.
sync() {
  local filename=$1
  local ev=$2
  local filepath="${HOST_CNI_NET}/$filename"

  local prev_sha=$3

  local config_file_count
  local new_sha
  if [ "$ev" = 'DELETE' ]; then
    # When the event type is 'DELETE', we check to see if there are any `*conf` or `*conflist`
    # files on the host's filesystem. If none are present, we install in
    # 'interface' mode, using our own CNI config file.
    config_file_count=$(find "${HOST_CNI_NET}" -maxdepth 1 -type f \( -iname '*conflist' -o -iname '*conf' \) | sort | wc -l)
    if [ "$config_file_count" -eq 0 ]; then
      echo "No active CNI configuration file found after $ev event; re-installing in \"interface\" mode"
      install_cni_conf "${DEFAULT_CNI_CONF_PATH}"
    fi
  elif [ "$ev" = 'CREATE' ]; then
    # When the event type is 'CREATE', we check the previously observed SHA (updated
    # with each file watch) and compare it against the new file's SHA. If they
    # differ, it means something has changed.
    new_sha=$(sha256sum "${filepath}" | while read -r s _; do echo "$s"; done)
    if [ "$new_sha" != "$prev_sha" ]; then
      # Create but don't rm old one since we don't know if this will be configured
      # to run as _the_ cni plugin.
      echo "New file [$filename] detected; re-installing in \"chained\" mode"
      install_cni_conf "$filepath"
    else
      # If the SHA hasn't changed or we get an unrecognised event, ignore it.
      # When the SHA is the same, we can get into infinite loops whereby a file has
      # been created and after re-install the watch keeps triggering CREATE events
      # that never end.
      echo "Ignoring event: $ev $filepath; no real changes detected"
    fi
  fi
}

# Monitor will start a watch on host's CNI config directory. Although files are
# mostly `mv'd`, because they are moved from the container's filesystem, the
# events logged will typically be a DELETED followed by a CREATE. When we are on
# the same system partition, `mv` simply renames, however, that won't be the
# case so we don't watch any "moved_to" or "moved_from" events.
monitor() {
  inotifywait -m "${HOST_CNI_NET}" -e create,delete |
    while read -r directory action filename; do
      if [[ "$filename" =~ .*.(conflist|conf)$ ]]; then 
        echo "Detected change in $directory: $action $filename"
        sync "$filename" "$action" "$cni_conf_sha"
        # When file exists (i.e we didn't deal with a DELETE ev)
        # then calculate its sha to be used the next turn.
        if [[ -e "$directory/$filename" && "$action" != 'DELETE' ]]; then
          cni_conf_sha="$(sha256sum "$directory/$filename" | while read -r s _; do echo "$s"; done)"
        fi
      fi
    done
}

################################
### CNI Plugin Install Logic ###
################################

install_cni_bin

# Install CNI configuration. If we have an existing CNI configuration file (*.conflist or *.conf) that is not linkerd's,
# then append our configuration to that file. Otherwise, if no CNI config files
# are present, install our stand-alone config file.
config_file_count=$(find "${HOST_CNI_NET}" -maxdepth 1 -type f \( -iname '*conflist' -o -iname '*conf' \) | grep -v linkerd | sort | wc -l)
if [ "$config_file_count" -eq 0 ]; then
  echo "No active CNI configuration files found; installing in \"interface\" mode in ${DEFAULT_CNI_CONF_PATH}"
  install_cni_conf "${DEFAULT_CNI_CONF_PATH}"
else
  find "${HOST_CNI_NET}" -maxdepth 1 -type f \( -iname '*conflist' -o -iname '*conf' \) -print0 |
    while read -r -d $'\0' file; do
      echo "Installing CNI configuration in \"chained\" mode for $file"
      install_cni_conf "$file"
    done
fi

# Compute SHA for first config file found; this will be updated after every iteration.
# First config file is likely to be chosen as the de facto CNI config by the
# host.
cni_conf_sha="$(sha256sum "$(find "${HOST_CNI_NET}" -maxdepth 1 -type f \( -iname '*conflist' -o -iname '*conf' \) | sort | head -n 1)" | while read -r s _; do echo "$s"; done)"

# Watch in bg so we can receive interrupt signals through 'trap'. From 'man
# bash': 
# "If  bash  is  waiting  for a command to complete and receives a signal
# for which a trap has been set, the trap will not be executed until the command
# completes. When bash is waiting for an asynchronous command via the wait
# builtin, the reception of a signal for which a trap has been set will cause
# the wait builtin to return immediately with an exit status greater than 128,
# immediately after which the trap is executed."
monitor &
while true; do
  # sleep so script never finishes
  # we start sleep in bg so we can trap signals
  sleep infinity &
  # block
  wait $!
done
