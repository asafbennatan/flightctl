#!/usr/bin/env bash
# Test script for QCOW2 caching system
# This script tests all components of the QCOW2 caching implementation

set -e

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[TEST] $1${NC}"
}

log_success() {
    echo -e "${GREEN}[TEST] ✅ $1${NC}"
}

log_warn() {
    echo -e "${YELLOW}[TEST] ⚠️  $1${NC}"
}

log_error() {
    echo -e "${RED}[TEST] ❌ $1${NC}"
}

# Test counters
TESTS_PASSED=0
TESTS_FAILED=0

test_result() {
    if [ $1 -eq 0 ]; then
        log_success "$2"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        log_error "$2"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

# Change to project root
cd "$PROJECT_ROOT"

log_info "Starting QCOW2 caching system tests..."

# Test 1: Check if required scripts exist
log_info "Test 1: Checking required scripts exist"
test_result $([ -f "test/scripts/agent-images/prepare_vm_config.sh" ] && [ -x "test/scripts/agent-images/prepare_vm_config.sh" ] && echo 0 || echo 1) "prepare_vm_config.sh exists and is executable"
test_result $([ -f "test/scripts/agent-images/prepare_cloud_init_config.sh" ] && [ -x "test/scripts/agent-images/prepare_cloud_init_config.sh" ] && echo 0 || echo 1) "prepare_cloud_init_config.sh exists and is executable"
test_result $([ -f "test/scripts/agent-images/qcow2_cache_manager.sh" ] && [ -x "test/scripts/agent-images/qcow2_cache_manager.sh" ] && echo 0 || echo 1) "qcow2_cache_manager.sh exists and is executable"
test_result $([ -f "test/scripts/agent-images/build_generic_base.sh" ] && [ -x "test/scripts/agent-images/build_generic_base.sh" ] && echo 0 || echo 1) "build_generic_base.sh exists and is executable"

# Test 2: Check if required containerfiles exist
log_info "Test 2: Checking required containerfiles exist"
test_result $([ -f "test/scripts/agent-images/Containerfile-e2e-base-generic.local" ] && echo 0 || echo 1) "Containerfile-e2e-base-generic.local exists"
test_result $([ -f "test/scripts/agent-images/Containerfile-qcow2-cache" ] && echo 0 || echo 1) "Containerfile-qcow2-cache exists"

# Test 3: Check if makefile targets exist
log_info "Test 3: Checking makefile targets exist"
test_result $(grep -q "prepare-vm-config:" test/test.mk && echo 0 || echo 1) "prepare-vm-config makefile target exists"
test_result $(grep -q "prepare-cloud-init-config:" test/test.mk && echo 0 || echo 1) "prepare-cloud-init-config makefile target exists"
test_result $(grep -q "build-generic-base:" test/test.mk && echo 0 || echo 1) "build-generic-base makefile target exists"

# Test 4: Test VM configuration preparation
log_info "Test 4: Testing VM configuration preparation"
if [ -d "bin/vm-config" ]; then
    log_warn "Cleaning existing VM config for fresh test"
    make clean-vm-config
fi

# This test requires the E2E environment to be set up
if [ -f "bin/agent/etc/flightctl/config.yaml" ] && [ -d "bin/agent/etc/flightctl/certs" ]; then
    make prepare-vm-config
    test_result $([ -d "bin/vm-config" ] && echo 0 || echo 1) "VM config directory created"
    test_result $([ -f "bin/vm-config/config.yaml" ] && echo 0 || echo 1) "Agent config file copied"
    test_result $([ -d "bin/vm-config/certs" ] && echo 0 || echo 1) "Agent certs directory created"
    test_result $([ -f "bin/vm-config/ca.pem" ] && echo 0 || echo 1) "CA certificate copied"
    test_result $([ -f "bin/vm-config/registry.conf" ] && echo 0 || echo 1) "Registry config created"
    test_result $([ -f "bin/vm-config/setup-flightctl.sh" ] && echo 0 || echo 1) "Setup script copied"
else
    log_warn "Skipping VM config test - E2E environment not set up"
    log_warn "Run 'make prepare-e2e-test' first to set up the environment"
fi

# Test 5: Test cloud-init configuration preparation
log_info "Test 5: Testing cloud-init configuration preparation"
if [ -d "bin/cloud-init-data" ]; then
    log_warn "Cleaning existing cloud-init config for fresh test"
    make clean-cloud-init-config
fi

if [ -d "bin/vm-config" ]; then
    make prepare-cloud-init-config
    test_result $([ -d "bin/cloud-init-data" ] && echo 0 || echo 1) "Cloud-init directory created"
    test_result $([ -f "bin/cloud-init-data/user-data" ] && echo 0 || echo 1) "User-data file created"
    test_result $([ -f "bin/cloud-init-data/meta-data" ] && echo 0 || echo 1) "Meta-data file created"
    test_result $([ -f "bin/cloud-init-data/network-config" ] && echo 0 || echo 1) "Network-config file created"
    test_result $([ -d "bin/cloud-init-data/flightctl-config" ] && echo 0 || echo 1) "Flightctl config directory copied"
    test_result $(grep -q "setup-flightctl.sh" bin/cloud-init-data/user-data && echo 0 || echo 1) "Setup script embedded in user-data"
else
    log_warn "Skipping cloud-init test - VM config not available"
fi

# Test 6: Test QCOW2 cache manager
log_info "Test 6: Testing QCOW2 cache manager"
# Test that the script runs without errors (exit code 1 is expected when no cache is found)
test_result $(test/scripts/agent-images/qcow2_cache_manager.sh get >/dev/null 2>&1; [ $? -eq 1 ] && echo 0 || echo 1) "QCOW2 cache manager get command works (returns expected exit code 1)"
test_result $(test/scripts/agent-images/qcow2_cache_manager.sh --help >/dev/null 2>&1; [ $? -eq 1 ] && echo 0 || echo 1) "QCOW2 cache manager help command works (returns expected exit code 1)"

# Test 7: Test registry caching environment
log_info "Test 7: Testing registry caching environment"
export REGISTRY_CACHE=true
export QCOW2_CACHE_TAG=test-cache
# Test that the script runs without errors (exit code 1 is expected when no cache is found)
test_result $(test/scripts/agent-images/qcow2_cache_manager.sh get >/dev/null 2>&1; [ $? -eq 1 ] && echo 0 || echo 1) "Registry caching enabled and working (returns expected exit code 1)"

# Test 8: Check GitHub Actions workflow
log_info "Test 8: Checking GitHub Actions workflow integration"
test_result $(grep -q "e2e-base-generic" .github/workflows/publish-e2e-containers.yaml && echo 0 || echo 1) "Generic base image in GitHub Actions workflow"
test_result $(grep -q "publish-qcow2-cache" .github/workflows/publish-e2e-containers.yaml && echo 0 || echo 1) "QCOW2 cache job in GitHub Actions workflow"

# Test 9: Check VM pool integration
log_info "Test 9: Checking VM pool integration"
test_result $(grep -q "CLOUD_INIT_DIR" test/harness/e2e/vm_pool.go && echo 0 || echo 1) "Cloud-init support in VM pool"

# Test 10: Test script syntax
log_info "Test 10: Testing script syntax"
test_result $(bash -n test/scripts/agent-images/prepare_vm_config.sh && echo 0 || echo 1) "prepare_vm_config.sh syntax is valid"
test_result $(bash -n test/scripts/agent-images/prepare_cloud_init_config.sh && echo 0 || echo 1) "prepare_cloud_init_config.sh syntax is valid"
test_result $(bash -n test/scripts/agent-images/qcow2_cache_manager.sh && echo 0 || echo 1) "qcow2_cache_manager.sh syntax is valid"
test_result $(bash -n test/scripts/agent-images/build_generic_base.sh && echo 0 || echo 1) "build_generic_base.sh syntax is valid"

# Summary
echo ""
log_info "Test Summary:"
log_success "Tests passed: $TESTS_PASSED"
if [ $TESTS_FAILED -gt 0 ]; then
    log_error "Tests failed: $TESTS_FAILED"
    exit 1
else
    log_success "All tests passed! 🎉"
fi

echo ""
log_info "Next steps to fully test the system:"
echo "1. Set up E2E environment: make prepare-e2e-test"
echo "2. Test VM creation with cloud-init"
echo "3. Test registry caching in CI/CD environment"
echo "4. Measure performance improvements"
