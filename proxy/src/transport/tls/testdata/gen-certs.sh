#!/bin/bash
#
# Requires:
# go get -u github.com/cloudflare/cfssl/cmd/cfssl
# go get -u github.com/cloudflare/cfssl/cmd/cfssljson
#
set -euox pipefail

ca() {
  filename=$1
  name=$2
  echo '{"names":[{"CN": "${name}","OU":"None"}]}' \
    | cfssl genkey -initca - \
    | cfssljson -bare ${name}

  rm ${name}.csr
}

ee() {
  ca_name=$1
  ee_deployment=$2
  ee_namespace=$3
  controller_namespace=$4

  ee_name=${ee_deployment}-${ee_namespace}
  hostname=${ee_deployment}.deployment.${ee_namespace}.conduit-managed.${controller_namespace}.svc.cluster.local

  echo '{}' \
    | cfssl gencert -ca ${ca_name}.pem -ca-key ${ca_name}-key.pem -hostname=${hostname} - \
    | cfssljson -bare ${ee_name}
  openssl pkcs8 -topk8 -nocrypt -inform pem -outform der \
    -in ${ee_name}-key.pem \
    -out ${ee_name}-${ca_name}.p8
  openssl x509 -inform pem -outform der \
    -in ${ee_name}.pem \
    -out ${ee_name}-${ca_name}.crt
  rm \
    ${ee_name}.pem \
    ${ee_name}-key.pem \
    ${ee_name}.csr

  crt=`base64 -w0 ${ee_name}-${ca_name}.crt`
  p8=`base64 -w0 ${ee_name}-${ca_name}.p8`

  secret="${ee_name}-${ca_name}-secret.yml"
  echo "apiVersion: v1" > ${secret}
  echo "kind: Secret" >> ${secret}
  echo "metadata:" >> ${secret}
  echo "  name: ${ee_deployment}-deployment-tls-conduit-io" >> ${secret}
  echo "data:" >> ${secret}
  echo "  certificate.crt: ${crt}" >> "${ee_name}-${ca_name}-secret.yml"
  echo "  private-key.p8: ${p8}" >> "${ee_name}-${ca_name}-secret.yml"
}

ca "Cluster-local CA 1" ca1
ca "Cluster-local CA 1" ca2 # Same name, different key pair.

# The controller itself.
ee ca1 controller conduit conduit

ee ca1 foo ns1 conduit
ee ca2 foo ns1 conduit # Same, but different CA
ee ca1 bar ns1 conduit # Different service.
