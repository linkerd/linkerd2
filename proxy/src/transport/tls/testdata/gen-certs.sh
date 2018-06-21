#!/bin/bash
set -euox pipefail

ca() {
  filename=$1
  name=$2
  echo '{"names":[{"CN": "${name}","OU":"None"}]}' \
    | cfssl genkey -initca - \
    | cfssljson -bare ${name}
}

ee() {
  ca_name=$1
  ee_name=$2
  hostname=$3
  echo '{}' \
    | cfssl gencert -ca ${ca_name}.pem -ca-key ${ca_name}-key.pem -hostname=${hostname} - \
    | cfssljson -bare ${ee_name}
  openssl pkcs8 -topk8 -nocrypt -inform pem -outform der \
    -in ${ee_name}-key.pem \
    -out ${ee_name}-${ca_name}.p8
  openssl x509 -inform pem -outform der \
    -in ${ee_name}.pem \
    -out ${ee_name}-${ca_name}.crt
  rm ${ee_name}.pem
}

ca "Cluster-local CA 1" ca1
ca "Cluster-local CA 1" ca2 # Same name, different key pair.
ee ca1 foo-ns1 foo.ns1.conduit-managed-pods.conduit.svc.cluster.local
ee ca2 foo-ns1 foo.ns1.conduit-managed-pods.conduit.svc.cluster.local # Same, but different CA
ee ca1 bar-ns1 bar.ns1.conduit-managed-pods.conduit.svc.cluster.local # Different service.

rm *-key.pem *.csr

