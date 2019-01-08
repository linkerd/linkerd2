#!/bin/sh

# Script to install Linkerd2 CNI on a Kubernetes host.
# - Expects the host CNI binary path to be mounted at /host/opt/cni/bin.
# - Expects the host CNI network config path to be mounted at /host/etc/cni/net.d.
# - Expects the desired CNI config in the CNI_NETWORK_CONFIG env variable.

# Ensure all variables are defined, and that the script fails when an error is hit.
set -u -e

# Capture the usual signals and exit from the script
trap 'echo "SIGINT received, simply exiting..."; exit 0' INT
trap 'echo "SIGTERM received, simply exiting..."; exit 0' TERM
trap 'echo "SIGHUP received, simply exiting..."; exit 0' HUP

# Helper function for raising errors
# Usage:
# some_command || exit_with_error "some_command_failed: maybe try..."
exit_with_error(){
  echo "$1"
  exit 1
}

# The directory on the host where CNI networks are installed. Defaults to
# /etc/cni/net.d, but can be overridden by setting CNI_NET_DIR. This is used
# for populating absolute paths in the CNI network config to assets
# which are installed in the CNI network config directory.
HOST_CNI_NET_DIR=${CNI_NET_DIR:-/etc/cni/net.d}

# Clean up any existing binaries / config / assets.
rm -f /host/opt/cni/bin/linkerd2-cni

# Place the new binaries if the directory is writeable.
dir=/host/opt/cni/bin
if [ ! -w "$dir" ]; then
  echo "$dir is non-writeable, skipping"
fi
for path in /opt/cni/bin/*;
do
  cp "$path" "$dir"/ || exit_with_error "Failed to copy $path to $dir."
done

echo "Wrote linkerd2 CNI binaries to $dir"

TMP_CONF='/linkerd2-cni.conf.tmp'
# If specified, overwrite the network configuration file.
: "${CNI_NETWORK_CONFIG_FILE:=}"
: "${CNI_NETWORK_CONFIG:=}"
if [ -e "${CNI_NETWORK_CONFIG_FILE}" ]; then
  echo "Using CNI config template from ${CNI_NETWORK_CONFIG_FILE}."
  cp "${CNI_NETWORK_CONFIG_FILE}" "${TMP_CONF}"
elif [ "${CNI_NETWORK_CONFIG}" != "" ]; then
  echo "Using CNI config template from CNI_NETWORK_CONFIG environment variable."
  cat >$TMP_CONF <<EOF
${CNI_NETWORK_CONFIG}
EOF
fi

SERVICE_ACCOUNT_PATH=/var/run/secrets/kubernetes.io/serviceaccount
KUBE_CA_FILE=${KUBE_CA_FILE:-$SERVICE_ACCOUNT_PATH/ca.crt}
SKIP_TLS_VERIFY=${SKIP_TLS_VERIFY:-false}
# Pull out service account token.
SERVICEACCOUNT_TOKEN=$(cat $SERVICE_ACCOUNT_PATH/token)

# Check if we're running as a k8s pod.
if [ -f "$SERVICE_ACCOUNT_PATH/token" ]; then
  # We're running as a k8d pod - expect some variables.
  if [ -z "${KUBERNETES_SERVICE_HOST}" ]; then
    echo "KUBERNETES_SERVICE_HOST not set"; exit 1;
  fi
  if [ -z "${KUBERNETES_SERVICE_PORT}" ]; then
    echo "KUBERNETES_SERVICE_PORT not set"; exit 1;
  fi

  if [ "$SKIP_TLS_VERIFY" = "true" ]; then
    TLS_CFG="insecure-skip-tls-verify: true"
  elif [ -f "$KUBE_CA_FILE" ]; then
    TLS_CFG="certificate-authority-data: $(cat "$KUBE_CA_FILE" | base64 | tr -d '\n')"
  fi

  # Write a kubeconfig file for the CNI plugin. Do this
  # to skip TLS verification for now. We should eventually support
  # writing more complete kubeconfig files. This is only used
  # if the provided CNI network config references it.
  touch /host/etc/cni/net.d/linkerd2-cni-kubeconfig
  chmod "${KUBECONFIG_MODE:-600}" /host/etc/cni/net.d/linkerd2-cni-kubeconfig
  cat > /host/etc/cni/net.d/linkerd2-cni-kubeconfig <<EOF
# Kubeconfig file for linkerd2 CNI plugin.
apiVersion: v1
kind: Config
clusters:
- name: local
  cluster:
    server: ${KUBERNETES_SERVICE_PROTOCOL:-https}://[${KUBERNETES_SERVICE_HOST}]:${KUBERNETES_SERVICE_PORT}
    $TLS_CFG
users:
- name: linkerd2-cni
  user:
    token: "${SERVICEACCOUNT_TOKEN}"
contexts:
- name: linkerd2-cni-context
  context:
    cluster: local
    user: linkerd2-cni
current-context: linkerd2-cni-context
EOF

fi

# Insert any of the supported "auto" parameters.
grep "__KUBERNETES_SERVICE_HOST__" $TMP_CONF && sed -i s/__KUBERNETES_SERVICE_HOST__/"${KUBERNETES_SERVICE_HOST}"/g $TMP_CONF
grep "__KUBERNETES_SERVICE_PORT__" $TMP_CONF && sed -i s/__KUBERNETES_SERVICE_PORT__/"${KUBERNETES_SERVICE_PORT}"/g $TMP_CONF
sed -i s/__KUBERNETES_NODE_NAME__/"${KUBERNETES_NODE_NAME:-$(hostname)}"/g $TMP_CONF
sed -i s/__KUBECONFIG_FILENAME__/linkerd2-cni-kubeconfig/g $TMP_CONF
sed -i s/__CNI_MTU__/"${CNI_MTU:-1500}"/g $TMP_CONF

# Use alternative command character "~", since these include a "/".
sed -i s~__KUBECONFIG_FILEPATH__~"${HOST_CNI_NET_DIR}"/linkerd2-cni-kubeconfig~g $TMP_CONF
sed -i s~__LOG_LEVEL__~"${LOG_LEVEL:-warn}"~g $TMP_CONF
sed -i s~__INCOMING_PROXY_PORT__~"${INCOMING_PROXY_PORT:=-1}"~g $TMP_CONF
sed -i s~__OUTGOING_PROXY_PORT__~"${OUTGOING_PROXY_PORT:=-1}"~g $TMP_CONF
sed -i s~__PROXY_UID__~"${PROXY_UID:=-1}"~g $TMP_CONF
sed -i s~__PORTS_TO_REDIRECT__~"${PORTS_TO_REDIRECT:=[]}"~g $TMP_CONF
sed -i s~__INBOUND_PORTS_TO_IGNORE__~"${INBOUND_PORTS_TO_IGNORE:=[]}"~g $TMP_CONF
sed -i s~__OUTBOUND_PORTS_TO_IGNORE__~"${OUTBOUND_PORTS_TO_IGNORE:=[]}"~g $TMP_CONF
sed -i s~__SIMULATE__~"${SIMULATE:=false}"~g $TMP_CONF

CNI_CONF_NAME=${CNI_CONF_NAME:-10-linkerd2-cni.conf}
CNI_OLD_CONF_NAME=${CNI_OLD_CONF_NAME:-10-linkerd2-cni.conf}

# Log the config file before inserting service account token.
# This way auth token is not visible in the logs.
echo "CNI config: $(cat ${TMP_CONF})"

sed -i s/__SERVICEACCOUNT_TOKEN__/"${SERVICEACCOUNT_TOKEN:-}"/g $TMP_CONF

# Delete old CNI config files for upgrades.
if [ "${CNI_CONF_NAME}" != "${CNI_OLD_CONF_NAME}" ]; then
    rm -f "/host/etc/cni/net.d/${CNI_OLD_CONF_NAME}"
fi
# Move the temporary CNI config into place.
mv $TMP_CONF /host/etc/cni/net.d/"${CNI_CONF_NAME}" || \
  exit_with_error "Failed to mv files."

echo "Created CNI config ${CNI_CONF_NAME}"

# Unless told otherwise, sleep forever.
# This prevents Kubernetes from restarting the pod repeatedly.
should_sleep=${SLEEP:-"true"}
echo "Done configuring CNI. Sleep=$should_sleep"
while [ "$should_sleep" = "true"  ]; do
  sleep 10
done