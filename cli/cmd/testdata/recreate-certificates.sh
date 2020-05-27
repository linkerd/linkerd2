#!/bin/bash -e

#
# recreate certificates for tests.
# 
# as the certificates have a limited validity period, they need to be recreated ocasionally (usually every 1-3 years)
# In order to avoid having to find out how to exactly do that every time, here are the commands to created them
#
# The script will only (re-)create the certificates needed. It will not update the golden files or test source code!

# create a RSA key pair and a certificate with a validity of 500 days
# usage: create_cert <file basename> <subject> <CA cert file> <CA key file>
create_cert() {
    basename=$1
    subject=$2
    cacert=$3
    cakey=$4

    openssl genrsa -out ${basename}-key.pem 2048 2>/dev/null
    openssl req -new -sha256 -key ${basename}-key.pem -subj "${subject}" -out ${basename}.csr 
    openssl x509 -req -in ${basename}.csr -CA ${cacert} -CAkey ${cakey} -CAcreateserial -out ${basename}.pem -days 500 -sha256 2>/dev/null

    echo -n "${basename}.pem: "
    cat "${basename}.pem" | base64 | tr -d '\n' ; printf "\n\n"

    echo -n "${basename}-key.pem: "
    cat "${basename}-key.pem" | base64 | tr -d '\n' ; printf "\n\n"

}

#
# two level webhook secrets.
#
# golden file: upgrade_two_level_webhook_cert.golden
#
tmp_dir=$(mktemp -d  cert_setup-XXXXXXXXXX)

echo "# temporary directory: $tmp_dir"

pushd $tmp_dir >/dev/null

openssl genrsa -out CA-key.pem 2048 2>/dev/null
openssl req -new -key CA-key.pem -x509 -days 1000 -out CA-cert.pem -subj "/CN=Linkerd Webhook CA" 

echo
echo -n "CA-cert.pem: "
cat "CA-cert.pem" | base64 | tr -d '\n' ; printf "\n\n"


create_cert smi-metrics-webhook "/CN=linkerd-smi-metrics.linkerd.svc" CA-cert.pem CA-key.pem
create_cert tap-webhook "/CN=linkerd-tap.linkerd.svc" CA-cert.pem CA-key.pem
create_cert sp-validator-webhook "/CN=linkerd-sp-validator.linkerd.svc" CA-cert.pem CA-key.pem
create_cert proxy-injector-webhook "/CN=linkerd-proxy-injector.linkerd.svc" CA-cert.pem CA-key.pem


popd >/dev/null
