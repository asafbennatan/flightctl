#!/usr/bin/env bash
#
# Build a container image from a Containerfile
#
# Usage: build_image.sh <containerfile> <image_name:tag> [registry_address]
#
# Arguments:
#   containerfile      - Path to the Containerfile
#   image_name:tag     - Local image name and tag (e.g. flightctl-device:golden)
#   registry_address   - Optional registry to tag and push to (e.g. 192.168.1.1:5000)
#
# Environment variables:
#   BUILD_ARGS         - Additional build arguments (e.g. --build-arg=FOO=bar)
#   SOURCE_GIT_TAG     - Git tag for versioning
#   SOURCE_GIT_TREE_STATE - Git tree state (clean/dirty)
#   SOURCE_GIT_COMMIT  - Git commit hash
#
set -ex

CONTAINERFILE="$1"
IMAGE_NAME="$2"
REGISTRY_ADDRESS="${3:-}"

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"

if [[ -z "$CONTAINERFILE" ]] || [[ -z "$IMAGE_NAME" ]]; then
    echo "Usage: $0 <containerfile> <image_name:tag> [registry_address]"
    exit 1
fi

if [[ ! -f "$CONTAINERFILE" ]]; then
    echo "Error: Containerfile not found: $CONTAINERFILE"
    exit 1
fi

# Build version arguments
args="${BUILD_ARGS:-}"
args="${args:+${args} }--build-arg=SOURCE_GIT_TAG=${SOURCE_GIT_TAG:-$("${SCRIPT_DIR}/../../../hack/current-version")}"
args="${args:+${args} }--build-arg=SOURCE_GIT_TREE_STATE=${SOURCE_GIT_TREE_STATE:-$( ( ( [ ! -d ".git/" ] || git diff --quiet ) && echo 'clean' ) || echo 'dirty' )}"
args="${args:+${args} }--build-arg=SOURCE_GIT_COMMIT=${SOURCE_GIT_COMMIT:-$(git rev-parse --short "HEAD^{commit}" 2>/dev/null || echo "unknown")}"

echo -e "\033[32mBuilding image ${IMAGE_NAME}\033[m"
podman build ${args:+${args}} -f "${CONTAINERFILE}" -t "${IMAGE_NAME}" .

# Tag and push to registry if provided
if [[ -n "$REGISTRY_ADDRESS" ]]; then
    REMOTE_REF="${REGISTRY_ADDRESS}/${IMAGE_NAME}"
    echo -e "\033[32mTagging and pushing to ${REMOTE_REF}\033[m"
    podman tag "${IMAGE_NAME}" "${REMOTE_REF}"
    podman push "${REMOTE_REF}"
fi

