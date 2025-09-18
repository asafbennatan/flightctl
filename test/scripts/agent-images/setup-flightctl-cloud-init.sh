#!/bin/bash
# Cloud-init script to configure flightctl-agent with run-specific data
# This script is injected into the generic QCOW2 template at VM startup

set -e

# Configuration variables (will be set by the VM creation process)
FLIGHTCTL_CONFIG_FILE="${FLIGHTCTL_CONFIG_FILE:-/mnt/flightctl-config/config.yaml}"
FLIGHTCTL_CERTS_DIR="${FLIGHTCTL_CERTS_DIR:-/mnt/flightctl-config/certs}"
FLIGHTCTL_RPMS_DIR="${FLIGHTCTL_RPMS_DIR:-/mnt/flightctl-config/rpms}"
FLIGHTCTL_CA_CERT="${FLIGHTCTL_CA_CERT:-/mnt/flightctl-config/ca.pem}"
FLIGHTCTL_REGISTRY_CONFIG="${FLIGHTCTL_REGISTRY_CONFIG:-/mnt/flightctl-config/registry.conf}"

echo "🚀 Setting up flightctl-agent with run-specific configuration..."

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
    echo "❌ RPM installation is mandatory and cannot be skipped"
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
