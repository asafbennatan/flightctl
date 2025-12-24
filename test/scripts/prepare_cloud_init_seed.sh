#!/usr/bin/env bash
set -e -x -o pipefail

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"
source "${SCRIPT_DIR}"/functions

PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"

# Default paths
AGENT_DIR="${AGENT_DIR:-bin/agent/etc/flightctl}"
# Try brew-rpm first (if BREW_BUILD_URL was used), then fall back to bin/rpm
if [ -d "bin/brew-rpm" ] && [ -n "$(find bin/brew-rpm -maxdepth 1 -name '*.rpm' 2>/dev/null)" ]; then
    RPM_FOLDER="${RPM_FOLDER:-bin/brew-rpm}"
else
    RPM_FOLDER="${RPM_FOLDER:-bin/rpm}"
fi
CERTS_FOLDER="${CERTS_FOLDER:-${AGENT_DIR}/certs}"
CLOUD_INIT_ISO="${CLOUD_INIT_ISO:-bin/output/cloud-init-seed.iso}"
USER_DATA_TEMPLATE="${USER_DATA_TEMPLATE:-test/scripts/agent-images/cloud-init-user-data.yaml}"

# Ensure agent config exists
if [ ! -f "${AGENT_DIR}/config.yaml" ]; then
    echo "Error: Agent config not found at ${AGENT_DIR}/config.yaml"
    echo "Please run prepare_agent_config.sh first"
    exit 1
fi

# Get registry address
REGISTRY_ADDRESS=$(registry_address)
echo "Using registry address: ${REGISTRY_ADDRESS}"

# Create output directory
mkdir -p "$(dirname "${CLOUD_INIT_ISO}")"

# Generate cloud-init seed ISO
echo "Generating cloud-init seed ISO..."
"${SCRIPT_DIR}/agent-images/generate-cloud-init.sh" \
    "${CLOUD_INIT_ISO}" \
    "${USER_DATA_TEMPLATE}" \
    "${REGISTRY_ADDRESS}" \
    "${RPM_FOLDER}" \
    "${CERTS_FOLDER}" \
    "${AGENT_DIR}/config.yaml"

echo "Cloud-init seed ISO created at: ${CLOUD_INIT_ISO}"

