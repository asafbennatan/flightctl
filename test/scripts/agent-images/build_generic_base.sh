#!/usr/bin/env bash
# Build generic base image for QCOW2 caching
# This script builds a generic template without run-specific data

set -e

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"
source "${SCRIPT_DIR}"/../functions

GENERIC_IMAGE_NAME="localhost:5000/flightctl-device:base-generic"
LOCAL_IMAGE_REF="flightctl-device:base-generic"
FINAL_REF="${GENERIC_IMAGE_NAME}"

log_info() {
    echo -e "\033[32m[GENERIC-BASE] $1\033[m"
}

log_warn() {
    echo -e "\033[33m[GENERIC-BASE] $1\033[m"
}

log_error() {
    echo -e "\033[31m[GENERIC-BASE] $1\033[m"
}

build_generic_base() {
    log_info "Building generic base image for QCOW2 caching..."
    
    # Add standard build arguments for caching and versioning
    local build_args=""
    build_args="${build_args:+${build_args} }--build-arg=SOURCE_GIT_TAG=${SOURCE_GIT_TAG:-$(git describe --tags --exclude latest 2>/dev/null || echo "latest")}"
    build_args="${build_args:+${build_args} }--build-arg=SOURCE_GIT_TREE_STATE=${SOURCE_GIT_TREE_STATE:-$( ( ( [ ! -d ".git/" ] || git diff --quiet ) && echo 'clean' ) || echo 'dirty' )}"
    build_args="${build_args:+${build_args} }--build-arg=SOURCE_GIT_COMMIT=${SOURCE_GIT_COMMIT:-$(git rev-parse --short "HEAD^{commit}" 2>/dev/null || echo "unknown")}"

    # Use GitHub Actions cache when GITHUB_ACTIONS=true, otherwise no caching
    local cache_flags=()
    if [ "${GITHUB_ACTIONS:-false}" = "true" ]; then
        local registry="${REGISTRY:-localhost}"
        local registry_owner_tests="${REGISTRY_OWNER_TESTS:-flightctl-tests}"
        cache_flags=("--cache-from=${registry}/${registry_owner_tests}/flightctl-device-base-generic")
    fi

    log_info "Building generic base image: ${GENERIC_IMAGE_NAME}"
    podman build ${build_args:+${build_args}} "${cache_flags[@]}" \
        -f "${SCRIPT_DIR}/Containerfile-e2e-base-generic.local" \
        -t "${GENERIC_IMAGE_NAME}" \
        .
    
    log_info "Generic base image built successfully"
}

build_generic_qcow2() {
    log_info "Building QCOW2 from generic base image..."
    
    mkdir -p bin/output

    # Transfer image to rootful podman storage for bootc-image-builder
    log_info "Transferring image to rootful podman storage"
    podman save "${FINAL_REF}" | sudo podman load
    
    # Use the full image reference that was loaded
    log_info "Producing QCOW2 image for ${FINAL_REF}"

    # Create cache directories if they don't exist
    mkdir -p "$(pwd)/bin/dnf-cache"
    mkdir -p "$(pwd)/bin/osbuild-cache"

    sudo podman run --rm \
                    -it \
                    --privileged \
                    --pull=newer \
                    --security-opt label=type:unconfined_t \
                    -v "$(pwd)"/bin/output:/output \
                    -v "$(pwd)"/bin/dnf-cache:/var/cache/dnf:Z \
                    -v "$(pwd)"/bin/osbuild-cache:/var/cache/osbuild:Z \
                    -v /var/lib/containers/storage:/var/lib/containers/storage \
                    quay.io/centos-bootc/bootc-image-builder:latest \
                    build \
                    --type qcow2 \
                    --local "${FINAL_REF}"
    
    # Reset the owner to the user running make
    sudo chown -R "${USER}:$(id -gn ${USER})" "$(pwd)"/bin/output
    
    log_info "Generic QCOW2 built successfully"
}

# Main execution
case "${1:-all}" in
    "container")
        build_generic_base
        ;;
    "qcow2")
        build_generic_qcow2
        ;;
    "all")
        build_generic_base
        build_generic_qcow2
        ;;
    *)
        echo "Usage: $0 {container|qcow2|all}"
        echo ""
        echo "Commands:"
        echo "  container  - Build only the generic base container image"
        echo "  qcow2      - Build only the QCOW2 from existing generic base"
        echo "  all        - Build both container and QCOW2 (default)"
        exit 1
        ;;
esac
