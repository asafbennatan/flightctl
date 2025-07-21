#!/usr/bin/env bash

# This script extracts the CA certificate from the flightctl-ca-secret
# for use in agent images when cert-manager is enabled

set -x -euo pipefail
SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"

source "${SCRIPT_DIR}"/functions

# Create the directory structure
CERT_DIR="bin/e2e-certs/pki/CA"
mkdir -p "${CERT_DIR}"

# Extract CA certificate from flightctl-ca-secret
echo "Extracting CA certificate from flightctl-ca-secret..."

# Wait for the CA certificate to be issued
echo "Waiting for CA certificate to be issued..."
kubectl wait --for=condition=Ready certificate/flightctl-ca -n flightctl-external --timeout=300s

# Get the CA certificate from the secret
kubectl get secret flightctl-ca-secret -n flightctl-external -o jsonpath='{.data.tls\.crt}' | base64 -d > "${CERT_DIR}/ca.crt"

# Convert to PEM format for compatibility
cp "${CERT_DIR}/ca.crt" "${CERT_DIR}/ca.pem"

echo "CA certificate extracted to ${CERT_DIR}/ca.pem" 