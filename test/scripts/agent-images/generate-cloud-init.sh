#!/bin/bash
set -euo pipefail

# Script to generate cloud-init seed ISO for e2e tests
# Usage: generate-cloud-init.sh <output-iso-path> <user-data-file> <registry-address> <rpm-folder> <certs-folder> <flightctl-config-file>

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUTPUT_ISO="${1}"
USER_DATA_TEMPLATE="${2}"
REGISTRY_ADDRESS="${3}"
RPM_FOLDER="${4:-}"
CERTS_FOLDER="${5:-}"
FLIGHTCTL_CONFIG_FILE="${6:-}"

# Validate required parameters
if [ -z "${OUTPUT_ISO}" ] || [ -z "${USER_DATA_TEMPLATE}" ] || [ -z "${REGISTRY_ADDRESS}" ]; then
    echo "Error: Missing required parameters"
    echo "Usage: generate-cloud-init.sh <output-iso-path> <user-data-template> <registry-address> [rpm-folder] [certs-folder] [flightctl-config-file]"
    exit 1
fi

# Check if cloud-init user-data template exists
if [ ! -f "${USER_DATA_TEMPLATE}" ]; then
    echo "Error: User-data template not found: ${USER_DATA_TEMPLATE}"
    exit 1
fi

# Default values
CLOUD_USER_NAME="${CLOUD_USER_NAME:-user}"
CLOUD_USER_PASSWORD="${CLOUD_USER_PASSWORD:-user}"

# Resolve RPM folder path
RPM_FOLDER_ABS=""
if [ -n "${RPM_FOLDER}" ]; then
    # If it's an absolute path, use it as-is
    if [[ "${RPM_FOLDER}" =~ ^/ ]]; then
        RPM_FOLDER_ABS="${RPM_FOLDER}"
    else
        # Otherwise, make it relative to project root (assuming we're in test/scripts/agent-images)
        PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
        RPM_FOLDER_ABS="${PROJECT_ROOT}/${RPM_FOLDER}"
    fi
    # Validate that the directory exists
    if [ ! -d "${RPM_FOLDER_ABS}" ]; then
        echo "Warning: RPM folder not found: ${RPM_FOLDER_ABS}"
        echo "Continuing without RPMs in seed CDROM..."
        RPM_FOLDER_ABS=""
    elif [ -z "$(find "${RPM_FOLDER_ABS}" -maxdepth 1 -name '*.rpm' 2>/dev/null)" ]; then
        echo "Warning: No RPM files found in folder: ${RPM_FOLDER_ABS}"
        echo "Continuing without RPMs in seed CDROM..."
        RPM_FOLDER_ABS=""
    fi
fi

# Resolve certs folder path
CERTS_FOLDER_ABS=""
if [ -n "${CERTS_FOLDER}" ]; then
    # If it's an absolute path, use it as-is
    if [[ "${CERTS_FOLDER}" =~ ^/ ]]; then
        CERTS_FOLDER_ABS="${CERTS_FOLDER}"
    else
        # Otherwise, make it relative to project root
        PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
        CERTS_FOLDER_ABS="${PROJECT_ROOT}/${CERTS_FOLDER}"
    fi
    # Validate that the directory exists
    if [ ! -d "${CERTS_FOLDER_ABS}" ]; then
        echo "Warning: Certs folder not found: ${CERTS_FOLDER_ABS}"
        echo "Continuing without certs in seed CDROM..."
        CERTS_FOLDER_ABS=""
    elif [ -z "$(find "${CERTS_FOLDER_ABS}" -type f 2>/dev/null)" ]; then
        echo "Warning: No files found in certs folder: ${CERTS_FOLDER_ABS}"
        echo "Continuing without certs in seed CDROM..."
        CERTS_FOLDER_ABS=""
    fi
fi

# Resolve flightctl config file path
FLIGHTCTL_CONFIG_CONTENT=""
if [ -n "${FLIGHTCTL_CONFIG_FILE}" ]; then
    # If it's an absolute path, use it as-is
    if [[ "${FLIGHTCTL_CONFIG_FILE}" =~ ^/ ]]; then
        FLIGHTCTL_CONFIG_FILE_ABS="${FLIGHTCTL_CONFIG_FILE}"
    else
        # Otherwise, make it relative to project root
        PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
        FLIGHTCTL_CONFIG_FILE_ABS="${PROJECT_ROOT}/${FLIGHTCTL_CONFIG_FILE}"
    fi
    # Validate that the file exists
    if [ ! -f "${FLIGHTCTL_CONFIG_FILE_ABS}" ]; then
        echo "Error: Flightctl config file not found: ${FLIGHTCTL_CONFIG_FILE_ABS}"
        exit 1
    fi
    echo "Reading flightctl config from ${FLIGHTCTL_CONFIG_FILE_ABS}..."
    # Read the config file and indent it for YAML embedding (each line needs 6 spaces for proper indentation)
    FLIGHTCTL_CONFIG_CONTENT=$(sed 's/^/      /' "${FLIGHTCTL_CONFIG_FILE_ABS}")
else
    echo "Warning: No flightctl config file specified"
    FLIGHTCTL_CONFIG_CONTENT="      # No flightctl config provided"
fi

# Generate cloud-init user-data from template
echo "Generating cloud-init user-data..."
TEMP_USER_DATA=$(mktemp)

# Create a temporary file with the flightctl config content for sed replacement
TEMP_FLIGHTCTL_CONFIG=$(mktemp)
echo "${FLIGHTCTL_CONFIG_CONTENT}" > "${TEMP_FLIGHTCTL_CONFIG}"

# Use sed to replace the placeholders with actual values
sed "s|\${REGISTRY_ADDRESS}|${REGISTRY_ADDRESS}|g" "${USER_DATA_TEMPLATE}" | \
    sed "s|\${CLOUD_USER_NAME}|${CLOUD_USER_NAME}|g" | \
    sed "s|\${CLOUD_USER_PASSWORD}|${CLOUD_USER_PASSWORD}|g" | \
    sed "/\${FLIGHTCTL_CONFIG_CONTENT}/r ${TEMP_FLIGHTCTL_CONFIG}" | \
    sed "/\${FLIGHTCTL_CONFIG_CONTENT}/d" > "${TEMP_USER_DATA}"

rm -f "${TEMP_FLIGHTCTL_CONFIG}"

# Create cloud-init seed ISO
echo "Creating cloud-init seed ISO..."

# Check if genisoimage is available (required)
if ! command -v genisoimage &> /dev/null; then
    echo "Error: genisoimage not found."
    echo "Please install genisoimage: sudo dnf install -y genisoimage"
    rm -f "${TEMP_USER_DATA}"
    exit 1
fi

# Create meta-data
TEMP_META_DATA=$(mktemp)
cat > "${TEMP_META_DATA}" <<EOF
instance-id: $(uuidgen)
local-hostname: flightctl-e2e-vm
EOF

# Create temporary directory for ISO contents
ISO_TEMP_DIR=$(mktemp -d)
trap "rm -rf ${ISO_TEMP_DIR} ${TEMP_META_DATA} ${TEMP_USER_DATA}" EXIT

# Create ISO root directory
ISO_ROOT="${ISO_TEMP_DIR}/iso-root"
mkdir -p "${ISO_ROOT}"

# Copy user-data and meta-data to ISO root
cp "${TEMP_USER_DATA}" "${ISO_ROOT}/user-data"
cp "${TEMP_META_DATA}" "${ISO_ROOT}/meta-data"

# Copy only required RPM files to ISO (flightctl-agent and flightctl-selinux, excluding src.rpm)
if [ -n "${RPM_FOLDER_ABS}" ]; then
    RPM_ISO_DIR="${ISO_ROOT}/rpms"
    mkdir -p "${RPM_ISO_DIR}"
    RPM_COUNT=0
    for pattern in "flightctl-agent-*.rpm" "flightctl-selinux-*.rpm"; do
        for rpm in "${RPM_FOLDER_ABS}"/${pattern}; do
            # Skip source RPMs
            if [[ "${rpm}" == *.src.rpm ]]; then
                continue
            fi
            if [ -f "${rpm}" ]; then
                cp "${rpm}" "${RPM_ISO_DIR}/"
                RPM_COUNT=$((RPM_COUNT + 1))
            fi
        done
    done
    if [ "${RPM_COUNT}" -gt 0 ]; then
        echo "Including ${RPM_COUNT} RPM file(s) (flightctl-agent, flightctl-selinux)"
    fi
fi

# Copy certs to ISO if provided
if [ -n "${CERTS_FOLDER_ABS}" ]; then
    CERTS_COUNT=$(find "${CERTS_FOLDER_ABS}" -type f 2>/dev/null | wc -l)
    if [ "${CERTS_COUNT}" -gt 0 ]; then
        echo "Including ${CERTS_COUNT} cert file(s) from folder: ${CERTS_FOLDER_ABS}"
        CERTS_ISO_DIR="${ISO_ROOT}/certs"
        mkdir -p "${CERTS_ISO_DIR}"
        cp -r "${CERTS_FOLDER_ABS}"/* "${CERTS_ISO_DIR}/" 2>&1 || {
            echo "Warning: Failed to copy some cert files, continuing..."
        }
    fi
fi

# Create ISO using genisoimage
genisoimage \
    -output "${OUTPUT_ISO}" \
    -volid cidata \
    -joliet \
    -rock \
    "${ISO_ROOT}"

# Set proper SELinux context for the ISO if running as root
if [ "$EUID" -eq 0 ]; then
    restorecon -v "${OUTPUT_ISO}" || true
fi

echo "Cloud-init seed ISO created successfully: ${OUTPUT_ISO}"

