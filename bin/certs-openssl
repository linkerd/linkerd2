#!/usr/bin/env sh
#
set -eu

# Creates the root and issuer (intermediary) self-signed certificates for the control plane using openssl.
#
# For instructions on doing this with step-cli, check https://linkerd.io/2/tasks/generate-certificates

# Generate CA config
cat > ca.cnf << EOF
[ req ]
distinguished_name=dn
prompt = no
[ ext ]
basicConstraints = CA:TRUE
keyUsage = digitalSignature, keyCertSign, cRLSign
[ dn ]
CN = identity.linkerd.cluster.local
EOF

# Generate CA key
openssl ecparam -out ca.key -name prime256v1 -genkey -noout

# Generate CA cert
openssl req -key ca.key -new -x509 -days 7300 -sha256 -out ca.crt -config ca.cnf -extensions ext

# Generate the intermediate issuer key
openssl ecparam -out issuer.key -name prime256v1 -genkey -noout

# Generate the intermediate issuer csr and cert
openssl req -new -sha256 -key issuer.key -out issuer.csr  -config ca.cnf
openssl x509 -sha256 -req -in issuer.csr -out issuer.crt -CA ca.crt -CAkey ca.key -days 7300 -extfile ca.cnf -extensions ext -CAcreateserial
