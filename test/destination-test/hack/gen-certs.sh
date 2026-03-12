#!/usr/bin/env bash
#
# Generate shared trust root and issuer certificates for Linkerd multicluster
# using step-cli. This creates a single CA that both clusters will share,
# enabling cross-cluster mTLS.
#
# Usage:
#   ./gen-certs.sh [output-dir]
#
# Output:
#   LINKERD_CA_DIR/ca.crt       - Trust anchor (root CA)
#   LINKERD_CA_DIR/issuer.crt   - Issuer certificate (intermediate)
#   LINKERD_CA_DIR/issuer.key   - Issuer private key
#
# These files are read by helmfile.yaml during cluster setup.

set -euo pipefail

# Default CA directory
CA_DIR="${1:-${LINKERD_CA_DIR:-/tmp/linkerd-ca}}"

# Certificate validity periods
CA_VALIDITY="87600h"      # 10 years
ISSUER_VALIDITY="8760h"    # 1 year

echo "==> Generating Linkerd certificates in $CA_DIR"

# Create output directory
mkdir -p "$CA_DIR"

# Generate root CA (trust anchor)
echo "==> Generating root CA (trust anchor)"
step certificate create \
  "root.linkerd.cluster.local" \
  "$CA_DIR/ca.crt" \
  "$CA_DIR/ca.key" \
  --profile root-ca \
  --no-password \
  --insecure \
  --not-after="$CA_VALIDITY" \
  --kty=EC \
  --crv=P-256

echo "==> Root CA fingerprint:"
step certificate fingerprint "$CA_DIR/ca.crt"

# Generate issuer certificate (intermediate CA)
echo "==> Generating issuer certificate (intermediate CA)"
step certificate create \
  "identity.linkerd.cluster.local" \
  "$CA_DIR/issuer.crt" \
  "$CA_DIR/issuer.key" \
  --profile intermediate-ca \
  --ca "$CA_DIR/ca.crt" \
  --ca-key "$CA_DIR/ca.key" \
  --no-password \
  --insecure \
  --not-after="$ISSUER_VALIDITY" \
  --kty=EC \
  --crv=P-256

echo "==> Issuer certificate fingerprint:"
step certificate fingerprint "$CA_DIR/issuer.crt"

# Verify issuer is signed by CA
echo "==> Verifying certificate chain"
step certificate verify \
  "$CA_DIR/issuer.crt" \
  --roots "$CA_DIR/ca.crt"

echo ""
echo "✓ Certificate generation complete!"
echo ""
echo "Files created in $CA_DIR:"
ls -lh "$CA_DIR"
echo ""
echo "Set environment variable:"
echo "  export LINKERD_CA_DIR=$CA_DIR"
echo ""
echo "Or pass to helmfile:"
echo "  LINKERD_CA_DIR=$CA_DIR helmfile sync"
