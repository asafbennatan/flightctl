#!/usr/bin/env bash
# Prepare cloud-init configuration for VM with run-specific data
# This script creates cloud-init data that includes our configuration mounting

set -e

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"
source "${SCRIPT_DIR}"/../functions

# Configuration
VM_CONFIG_DIR="${VM_CONFIG_DIR:-bin/vm-config}"
CLOUD_INIT_DIR="${CLOUD_INIT_DIR:-bin/cloud-init-data}"

log_info() {
    echo -e "\033[32m[CLOUD-INIT] $1\033[m"
}

log_warn() {
    echo -e "\033[33m[CLOUD-INIT] $1\033[m"
}

log_error() {
    echo -e "\033[31m[CLOUD-INIT] $1\033[m"
}

prepare_cloud_init_config() {
    log_info "Preparing cloud-init configuration with run-specific data..."
    
    # Ensure VM config is prepared first
    if [ ! -d "${VM_CONFIG_DIR}" ]; then
        log_error "VM configuration directory not found: ${VM_CONFIG_DIR}"
        log_error "Please run prepare_vm_config.sh first"
        exit 1
    fi
    
    # Create cloud-init directory
    mkdir -p "${CLOUD_INIT_DIR}"
    
    # Create user-data.yaml with our configuration
    log_info "Creating user-data.yaml..."
    cat > "${CLOUD_INIT_DIR}/user-data" << 'EOF'
#cloud-config
users:
  - name: user
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    lock_passwd: false
    passwd: $6$rounds=4096$salt$hash  # user:user

# Write files
write_files:
  - path: /usr/local/bin/setup-flightctl.sh
    permissions: '0755'
    content: |
      #!/bin/bash
      # Cloud-init script to configure flightctl-agent with run-specific data
      set -e
      
      echo "🚀 Setting up flightctl-agent with run-specific configuration..."
      
      # Wait for cloud-init to complete
      cloud-init status --wait
      
      # Configuration variables
      FLIGHTCTL_CONFIG_FILE="/mnt/flightctl-config/config.yaml"
      FLIGHTCTL_CERTS_DIR="/mnt/flightctl-config/certs"
      FLIGHTCTL_RPMS_DIR="/mnt/flightctl-config/rpms"
      FLIGHTCTL_CA_CERT="/mnt/flightctl-config/ca.pem"
      FLIGHTCTL_REGISTRY_CONFIG="/mnt/flightctl-config/registry.conf"
      
      # Wait for mount points to be available
      echo "⏳ Waiting for configuration mounts to be available..."
      for i in {1..30}; do
          if [ -f "$FLIGHTCTL_CONFIG_FILE" ] && [ -d "$FLIGHTCTL_CERTS_DIR" ]; then
              echo "✅ Configuration mounts are available"
              break
          fi
          echo "   Attempt $i/30: waiting for mounts..."
          sleep 2
      done
      
      if [ ! -f "$FLIGHTCTL_CONFIG_FILE" ] || [ ! -d "$FLIGHTCTL_CERTS_DIR" ]; then
          echo "❌ Configuration mounts not available after 60 seconds"
          exit 1
      fi
      
      # Install RPMs (mandatory - never skip)
      if [ -d "$FLIGHTCTL_RPMS_DIR" ] && [ "$(ls -A "$FLIGHTCTL_RPMS_DIR" 2>/dev/null)" ]; then
          echo "📦 Installing flightctl RPMs..."
          dnf install -y "$FLIGHTCTL_RPMS_DIR"/*.rpm
          systemctl enable flightctl-agent.service
          echo "✅ RPMs installed and service enabled"
      else
          echo "❌ RPMs not found in $FLIGHTCTL_RPMS_DIR"
          exit 1
      fi
      
      # Copy agent configuration
      if [ -f "$FLIGHTCTL_CONFIG_FILE" ]; then
          echo "⚙️  Copying agent configuration..."
          cp "$FLIGHTCTL_CONFIG_FILE" /etc/flightctl/config.yaml
          chmod 644 /etc/flightctl/config.yaml
          echo "✅ Agent configuration copied"
      else
          echo "❌ Agent configuration file not found: $FLIGHTCTL_CONFIG_FILE"
          exit 1
      fi
      
      # Copy agent certificates
      if [ -d "$FLIGHTCTL_CERTS_DIR" ] && [ "$(ls -A "$FLIGHTCTL_CERTS_DIR" 2>/dev/null)" ]; then
          echo "🔐 Copying agent certificates..."
          cp "$FLIGHTCTL_CERTS_DIR"/* /etc/flightctl/certs/
          chmod 600 /etc/flightctl/certs/*
          echo "✅ Agent certificates copied"
      else
          echo "❌ Agent certificates not found in: $FLIGHTCTL_CERTS_DIR"
          exit 1
      fi
      
      # Install CA certificate
      if [ -f "$FLIGHTCTL_CA_CERT" ]; then
          echo "🔒 Installing CA certificate..."
          cp "$FLIGHTCTL_CA_CERT" /etc/pki/ca-trust/source/anchors/ca.pem
          update-ca-trust
          echo "✅ CA certificate installed and trust updated"
      else
          echo "❌ CA certificate not found: $FLIGHTCTL_CA_CERT"
          exit 1
      fi
      
      # Configure registry (mandatory - no defaults)
      if [ -f "$FLIGHTCTL_REGISTRY_CONFIG" ]; then
          echo "📋 Configuring container registry..."
          cp "$FLIGHTCTL_REGISTRY_CONFIG" /etc/containers/registries.conf.d/custom-registry.conf
          echo "✅ Registry configuration applied"
      else
          echo "❌ Registry configuration not found: $FLIGHTCTL_REGISTRY_CONFIG"
          echo "❌ Registry configuration is mandatory and cannot be skipped"
          exit 1
      fi
      
      # Start the flightctl-agent service
      echo "🎯 Starting flightctl-agent service..."
      systemctl start flightctl-agent.service
      
      # Verify service is running
      if systemctl is-active --quiet flightctl-agent.service; then
          echo "✅ flightctl-agent service is running"
      else
          echo "❌ flightctl-agent service failed to start"
          systemctl status flightctl-agent.service
          exit 1
      fi
      
      echo "🎉 flightctl-agent setup completed successfully!"

# Run commands
runcmd:
  - /usr/local/bin/setup-flightctl.sh

# Final message
final_message: "flightctl-agent configuration completed"
EOF

    # Create meta-data.yaml
    log_info "Creating meta-data.yaml..."
    cat > "${CLOUD_INIT_DIR}/meta-data" << EOF
instance-id: flightctl-e2e-vm
local-hostname: flightctl-e2e-vm
EOF

    # Create network-config.yaml (minimal network configuration)
    log_info "Creating network-config.yaml..."
    cat > "${CLOUD_INIT_DIR}/network-config" << 'EOF'
version: 2
ethernets:
  eth0:
    dhcp4: true
EOF

    # Copy configuration files to cloud-init directory for mounting
    log_info "Copying configuration files to cloud-init directory..."
    cp -r "${VM_CONFIG_DIR}" "${CLOUD_INIT_DIR}/flightctl-config"
    
    # Create a summary
    cat > "${CLOUD_INIT_DIR}/config-summary.txt" << EOF
Cloud-Init Configuration Summary
================================
Created: $(date)
VM Config Source: ${VM_CONFIG_DIR}
Cloud-Init Target: ${CLOUD_INIT_DIR}

Files included:
- user-data: Cloud-init user data with setup script
- meta-data: VM metadata
- network-config: Network configuration
- flightctl-config/: Run-specific configuration data

This cloud-init data will:
1. Create user account with sudo access
2. Mount configuration data to /mnt/flightctl-config
3. Install flightctl RPMs
4. Configure agent with run-specific data
5. Start flightctl-agent service
EOF
    
    log_info "Cloud-init configuration prepared in: ${CLOUD_INIT_DIR}"
    log_info "Configuration summary:"
    cat "${CLOUD_INIT_DIR}/config-summary.txt"
}

clean_cloud_init_config() {
    log_info "Cleaning cloud-init configuration directory..."
    if [ -d "${CLOUD_INIT_DIR}" ]; then
        rm -rf "${CLOUD_INIT_DIR}"
        log_info "Cloud-init configuration directory cleaned"
    else
        log_info "Cloud-init configuration directory does not exist"
    fi
}

# Main execution
case "${1:-prepare}" in
    "prepare")
        prepare_cloud_init_config
        ;;
    "clean")
        clean_cloud_init_config
        ;;
    *)
        echo "Usage: $0 {prepare|clean}"
        echo ""
        echo "Commands:"
        echo "  prepare  - Prepare cloud-init configuration (default)"
        echo "  clean    - Clean cloud-init configuration directory"
        echo ""
        echo "Environment variables:"
        echo "  VM_CONFIG_DIR     - Directory with VM config (default: bin/vm-config)"
        echo "  CLOUD_INIT_DIR    - Directory to store cloud-init data (default: bin/cloud-init-data)"
        exit 1
        ;;
esac
