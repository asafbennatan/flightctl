#!/usr/bin/env bash
# Prepare run-specific configuration for VM mounting
# This script creates the configuration data that will be mounted into VMs

set -e

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"
source "${SCRIPT_DIR}"/../functions

# Configuration
VM_CONFIG_DIR="${VM_CONFIG_DIR:-bin/vm-config}"
REGISTRY_ADDRESS=$(registry_address)

log_info() {
    echo -e "\033[32m[VM-CONFIG] $1\033[m"
}

log_warn() {
    echo -e "\033[33m[VM-CONFIG] $1\033[m"
}

log_error() {
    echo -e "\033[31m[VM-CONFIG] $1\033[m"
}

prepare_vm_config() {
    log_info "Preparing VM configuration for run-specific data injection..."
    
    # Create VM config directory
    mkdir -p "${VM_CONFIG_DIR}"
    
    # Prepare agent configuration
    log_info "Preparing agent configuration..."
    if [ -f "bin/agent/etc/flightctl/config.yaml" ]; then
        cp "bin/agent/etc/flightctl/config.yaml" "${VM_CONFIG_DIR}/config.yaml"
        log_info "Agent configuration copied"
    else
        log_error "Agent configuration not found: bin/agent/etc/flightctl/config.yaml"
        log_error "Please run prepare_agent_config.sh first"
        exit 1
    fi
    
    # Prepare agent certificates
    log_info "Preparing agent certificates..."
    if [ -d "bin/agent/etc/flightctl/certs" ] && [ "$(ls -A "bin/agent/etc/flightctl/certs" 2>/dev/null)" ]; then
        mkdir -p "${VM_CONFIG_DIR}/certs"
        cp "bin/agent/etc/flightctl/certs"/* "${VM_CONFIG_DIR}/certs/"
        log_info "Agent certificates copied"
    else
        log_error "Agent certificates not found: bin/agent/etc/flightctl/certs"
        log_error "Please run prepare_agent_config.sh first"
        exit 1
    fi
    
    # Prepare RPMs
    log_info "Preparing RPMs..."
    if [ -d "bin/rpm" ] && [ "$(ls -A "bin/rpm" 2>/dev/null)" ]; then
        mkdir -p "${VM_CONFIG_DIR}/rpms"
        cp "bin/rpm"/*.rpm "${VM_CONFIG_DIR}/rpms/"
        log_info "RPMs copied"
    elif [ -d "bin/brew-rpm" ] && [ "$(ls -A "bin/brew-rpm" 2>/dev/null)" ]; then
        mkdir -p "${VM_CONFIG_DIR}/rpms"
        cp "bin/brew-rpm"/*.rpm "${VM_CONFIG_DIR}/rpms/"
        log_info "Brew RPMs copied"
    else
        log_warn "No RPMs found in bin/rpm or bin/brew-rpm"
        log_warn "VMs will need to install flightctl-agent via other means"
    fi
    
    # Prepare CA certificate
    log_info "Preparing CA certificate..."
    if [ -f "bin/e2e-certs/pki/CA/ca.pem" ]; then
        cp "bin/e2e-certs/pki/CA/ca.pem" "${VM_CONFIG_DIR}/ca.pem"
        log_info "CA certificate copied"
    else
        log_error "CA certificate not found: bin/e2e-certs/pki/CA/ca.pem"
        log_error "Please run create_e2e_certs.sh first"
        exit 1
    fi
    
    # Prepare registry configuration
    log_info "Preparing registry configuration..."
    cat > "${VM_CONFIG_DIR}/registry.conf" << EOF
[[registry]]
location = "${REGISTRY_ADDRESS}"
insecure = true
EOF
    log_info "Registry configuration created"
    
    # Prepare cloud-init script
    log_info "Preparing cloud-init script..."
    cp "${SCRIPT_DIR}/setup-flightctl-cloud-init.sh" "${VM_CONFIG_DIR}/setup-flightctl.sh"
    chmod +x "${VM_CONFIG_DIR}/setup-flightctl.sh"
    log_info "Cloud-init script prepared"
    
    # Create a summary file
    cat > "${VM_CONFIG_DIR}/config-summary.txt" << EOF
VM Configuration Summary
========================
Created: $(date)
Registry Address: ${REGISTRY_ADDRESS}
Git Commit: $(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
Git Tag: $(git describe --tags --exclude latest 2>/dev/null || echo "latest")

Files prepared:
- config.yaml: Agent configuration
- certs/: Agent certificates
- rpms/: Flightctl RPMs
- ca.pem: E2E CA certificate
- registry.conf: Container registry configuration
- setup-flightctl.sh: Cloud-init setup script

Mount this directory to /mnt/flightctl-config in VMs
EOF
    
    log_info "VM configuration prepared in: ${VM_CONFIG_DIR}"
    log_info "Configuration summary:"
    cat "${VM_CONFIG_DIR}/config-summary.txt"
}

clean_vm_config() {
    log_info "Cleaning VM configuration directory..."
    if [ -d "${VM_CONFIG_DIR}" ]; then
        rm -rf "${VM_CONFIG_DIR}"
        log_info "VM configuration directory cleaned"
    else
        log_info "VM configuration directory does not exist"
    fi
}

# Main execution
case "${1:-prepare}" in
    "prepare")
        prepare_vm_config
        ;;
    "clean")
        clean_vm_config
        ;;
    *)
        echo "Usage: $0 {prepare|clean}"
        echo ""
        echo "Commands:"
        echo "  prepare  - Prepare VM configuration data (default)"
        echo "  clean    - Clean VM configuration directory"
        echo ""
        echo "Environment variables:"
        echo "  VM_CONFIG_DIR - Directory to store VM config (default: bin/vm-config)"
        exit 1
        ;;
esac
