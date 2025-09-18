#!/usr/bin/env bash
# QCOW2 Cache Manager for E2E Testing
# Handles caching of generic QCOW2 templates in container registries

set -e

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"
source "${SCRIPT_DIR}"/../functions

# Configuration
REGISTRY_OWNER_TESTS="${REGISTRY_OWNER_TESTS:-flightctl-tests}"
QUAY_TESTS_ORG="quay.io/${REGISTRY_OWNER_TESTS}"
QCOW2_CACHE_IMAGE="${QUAY_TESTS_ORG}/flightctl-device-qcow2"
QCOW2_CACHE_TAG="${QCOW2_CACHE_TAG:-base-qcow2}"
QCOW2_OUTPUT_DIR="bin/output/qcow2"
QCOW2_FILE="${QCOW2_OUTPUT_DIR}/disk.qcow2"

# Get git information for tagging
GIT_COMMIT=$(git rev-parse --short "HEAD^{commit}" 2>/dev/null || echo "unknown")
GIT_TAG=$(git describe --tags --exclude latest 2>/dev/null || echo "latest")
GIT_TREE_STATE=$(( ( [ ! -d ".git/" ] || git diff --quiet ) && echo 'clean' ) || echo 'dirty' )

# Create versioned tags
QCOW2_CACHE_TAG_COMMIT="${QCOW2_CACHE_TAG}-${GIT_COMMIT}"
QCOW2_CACHE_TAG_LATEST="${QCOW2_CACHE_TAG}-latest"

log_info() {
    echo -e "\033[32m[QCOW2-CACHE] $1\033[m"
}

log_warn() {
    echo -e "\033[33m[QCOW2-CACHE] $1\033[m"
}

log_error() {
    echo -e "\033[31m[QCOW2-CACHE] $1\033[m"
}

# Check if we should use registry caching
should_use_registry_cache() {
    # Always attempt registry caching - it's safe to try and fall back to local build
    return 0
}

# Try to pull cached QCOW2 from registry
pull_cached_qcow2() {
    local cache_tag="$1"
    local output_file="$2"
    
    log_info "Attempting to pull cached QCOW2: ${QCOW2_CACHE_IMAGE}:${cache_tag}"
    
    if podman pull "${QCOW2_CACHE_IMAGE}:${cache_tag}" 2>/dev/null; then
        log_info "Successfully pulled cached QCOW2 image"
        
        # Extract QCOW2 from container
        mkdir -p "$(dirname "$output_file")"
        container_id=$(podman create "${QCOW2_CACHE_IMAGE}:${cache_tag}")
        podman cp "${container_id}:/qcow2/disk.qcow2" "$output_file"
        podman rm "${container_id}"
        
        if [ -f "$output_file" ]; then
            log_info "QCOW2 extracted successfully to: $output_file"
            return 0
        else
            log_error "Failed to extract QCOW2 from cached image"
            return 1
        fi
    else
        log_warn "Failed to pull cached QCOW2: ${QCOW2_CACHE_IMAGE}:${cache_tag}"
        return 1
    fi
}

# Build and push QCOW2 to registry cache (manual operation)
push_qcow2_cache() {
    local qcow2_file="$1"
    local cache_tag="$2"
    
    if [ ! -f "$qcow2_file" ]; then
        log_error "QCOW2 file not found: $qcow2_file"
        return 1
    fi
    
    log_info "Manually pushing QCOW2 to registry cache: ${QCOW2_CACHE_IMAGE}:${cache_tag}"
    log_warn "This will push the QCOW2 to the public registry - ensure this is intended"
    
    # Get QCOW2 creation date for metadata
    QCOW2_CREATED_DATE=$(date -u -r "$qcow2_file" "+%Y-%m-%d %H:%M:%S UTC")
    
    # Build the cache container
    podman build \
        --build-arg=QCOW2_FILE="${qcow2_file}" \
        --build-arg=QCOW2_CREATED_DATE="${QCOW2_CREATED_DATE}" \
        -f "${SCRIPT_DIR}/Containerfile-qcow2-cache" \
        -t "${QCOW2_CACHE_IMAGE}:${cache_tag}" \
        .
    
    # Push to registry
    log_info "Pushing QCOW2 cache to registry: ${QCOW2_CACHE_IMAGE}:${cache_tag}"
    podman push "${QCOW2_CACHE_IMAGE}:${cache_tag}"
    
    log_info "QCOW2 cache pushed successfully"
}

# Main function to get or build QCOW2 with caching
get_or_build_qcow2() {
    local force_rebuild="${1:-false}"
    
    if [ "$force_rebuild" = "true" ]; then
        log_info "Force rebuild requested, skipping cache check"
        return 1
    fi
    
    # Check if we should use registry caching
    if ! should_use_registry_cache; then
        log_info "Registry caching disabled, using local build"
        return 1
    fi
    
    # Try to pull from cache in order of preference:
    # 1. Commit-specific tag (most specific)
    # 2. Latest tag (fallback)
    
    if pull_cached_qcow2 "${QCOW2_CACHE_TAG_COMMIT}" "${QCOW2_FILE}"; then
        log_info "Using cached QCOW2 for commit: ${GIT_COMMIT}"
        return 0
    fi
    
    if pull_cached_qcow2 "${QCOW2_CACHE_TAG_LATEST}" "${QCOW2_FILE}"; then
        log_warn "Using latest cached QCOW2 (commit-specific not available)"
        return 0
    fi
    
    log_info "No cached QCOW2 found, will build new one"
    return 1
}

# Function to cache a newly built QCOW2 (manual operation)
cache_qcow2() {
    local qcow2_file="$1"
    
    if [ ! -f "$qcow2_file" ]; then
        log_error "Cannot cache: QCOW2 file not found: $qcow2_file"
        log_error "Please build a QCOW2 first with: make prepare-e2e-test"
        return 1
    fi
    
    log_info "Manually caching QCOW2 to registry..."
    log_warn "This will push the QCOW2 to the public registry - ensure this is intended"
    
    log_info "Caching newly built QCOW2..."
    
    # Push with commit-specific tag
    if push_qcow2_cache "$qcow2_file" "${QCOW2_CACHE_TAG_COMMIT}"; then
        log_info "QCOW2 cached with commit-specific tag: ${QCOW2_CACHE_TAG_COMMIT}"
    else
        log_error "Failed to cache QCOW2 with commit-specific tag"
        return 1
    fi
    
    # Also push as latest if this is a clean tree
    if [ "$GIT_TREE_STATE" = "clean" ]; then
        if push_qcow2_cache "$qcow2_file" "${QCOW2_CACHE_TAG_LATEST}"; then
            log_info "QCOW2 also cached as latest: ${QCOW2_CACHE_TAG_LATEST}"
        else
            log_warn "Failed to cache QCOW2 as latest, but commit-specific cache succeeded"
        fi
    else
        log_info "Skipping latest tag cache (dirty tree state)"
    fi
}

# Main entry point
case "${1:-get}" in
    "get")
        get_or_build_qcow2 "${2:-false}"
        ;;
    "cache")
        cache_qcow2 "${2:-${QCOW2_FILE}}"
        ;;
    "pull")
        pull_cached_qcow2 "${2:-${QCOW2_CACHE_TAG_LATEST}}" "${3:-${QCOW2_FILE}}"
        ;;
    "push")
        push_qcow2_cache "${2:-${QCOW2_FILE}}" "${3:-${QCOW2_CACHE_TAG_COMMIT}}"
        ;;
    *)
        echo "Usage: $0 {get|cache|pull|push} [args...]"
        echo ""
        echo "Commands:"
        echo "  get [force]     - Try to get cached QCOW2, return 0 if found, 1 if needs building"
        echo "  cache [file]    - Cache a QCOW2 file to registry (manual operation)"
        echo "  pull [tag] [file] - Pull specific cached QCOW2"
        echo "  push [file] [tag] - Push QCOW2 to specific tag (manual operation)"
        echo ""
        echo "Manual Push Operations:"
        echo "  # Push current QCOW2 to registry with commit-specific tag"
        echo "  $0 push bin/output/qcow2/disk.qcow2"
        echo ""
        echo "  # Push to specific tag"
        echo "  $0 push bin/output/qcow2/disk.qcow2 base-qcow2-v1.0"
        echo ""
        echo "  # Cache current QCOW2 (pushes to commit-specific and latest tags)"
        echo "  $0 cache bin/output/qcow2/disk.qcow2"
        echo ""
        echo "Environment variables:"
        echo "  REGISTRY_CACHE=true - Enable registry caching (auto-enabled in GitHub Actions)"
        echo "  QCOW2_CACHE_TAG     - Base tag for caching (default: base-qcow2)"
        echo "  REGISTRY_OWNER_TESTS - Registry owner for tests (default: flightctl-tests)"
        exit 1
        ;;
esac
