#!/bin/bash

# OpenTelemetry Collector Certificate Setup Script
# This script automates the process of generating and setting up certificates for the FlightCtl OpenTelemetry collector

set -e

# Configuration
COLLECTOR_NAME="svc-otel-collector"
SIGNER_NAME="flightctl.io/server-svc"
CERT_DIR="/etc/otel-collector/certs"
CONFIG_DIR="/root/.flightctl"
TEMP_DIR="/tmp/otel-collector-certs"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Function to check if user has required permissions
check_permissions() {
    if [[ ! -f "./bin/flightctl" ]]; then
        print_error "FlightCtl CLI not found at ./bin/flightctl"
        exit 1
    fi

    # Check if user is authenticated
    if ! ./bin/flightctl get devices >/dev/null 2>&1; then
        print_error "Not authenticated with FlightCtl. Please run './bin/flightctl login' first"
        exit 1
    fi
}

# Function to create directories
create_directories() {
    print_status "Creating certificate directories..."
    
    sudo mkdir -p "$CERT_DIR"
    sudo mkdir -p "$CONFIG_DIR"
    mkdir -p "$TEMP_DIR"
    
    print_status "Directories created successfully"
}

# Function to generate certificates following the correct sequence
generate_certificates() {
    print_status "Generating certificates for OpenTelemetry collector..."
    
    # Step 1: Create CSR using the correct command format
    print_status "Creating Certificate Signing Request..."
    ./bin/flightctl certificate request \
        -n "$COLLECTOR_NAME" \
        -s "$SIGNER_NAME" \
        -x "100d" \
        -o "" \
        -d "$TEMP_DIR"
    
    print_status "CSR created successfully"
    
    # Step 2: Get the CSR name (it will have a random suffix)
    CSR_NAME=$(./bin/flightctl get csr | grep "$COLLECTOR_NAME" | awk '{print $1}' | head -1)
    
    if [[ -z "$CSR_NAME" ]]; then
        print_error "Failed to find CSR for $COLLECTOR_NAME"
        exit 1
    fi
    
    print_status "Found CSR: $CSR_NAME"
    
    # Step 3: Approve the CSR
    print_status "Approving CSR..."
    ./bin/flightctl approve csr/"$CSR_NAME"
    
    print_status "CSR approved successfully"
    
    # Step 4: Wait a moment for the certificate to be issued
    sleep 2
    
    # Step 5: Get the issued certificate
    print_status "Retrieving issued certificate..."
    ./bin/flightctl get csr/"$CSR_NAME" -o yaml > "$TEMP_DIR/csr.yaml"
    
    # Extract the certificate from the CSR status
    if command_exists yq; then
        yq eval '.status.certificate' "$TEMP_DIR/csr.yaml" | base64 -d > "$TEMP_DIR/${COLLECTOR_NAME}.crt"
    else
        # Fallback to grep/sed if yq is not available
        grep -A 1 "certificate:" "$TEMP_DIR/csr.yaml" | tail -1 | sed 's/^[[:space:]]*//' | base64 -d > "$TEMP_DIR/${COLLECTOR_NAME}.crt"
    fi
    
    print_status "Certificate extracted successfully"
}

# Function to extract CA certificate from enrollment config
extract_ca_certificate() {
    print_status "Extracting CA certificate from enrollment config..."
    
    # Get the enrollment config and extract CA certificate
    ./bin/flightctl enrollmentconfig > "$TEMP_DIR/enrollment-config.yaml"
    
    # Extract the CA certificate data
    if command_exists yq; then
        yq eval '.enrollment-service.service.certificate-authority-data' "$TEMP_DIR/enrollment-config.yaml" | base64 -d > "$TEMP_DIR/ca.crt"
    else
        # Fallback to grep/sed if yq is not available
        grep -A 1 "certificate-authority-data:" "$TEMP_DIR/enrollment-config.yaml" | tail -1 | sed 's/^[[:space:]]*//' | base64 -d > "$TEMP_DIR/ca.crt"
    fi
    
    print_status "CA certificate extracted successfully"
}

# Function to copy certificates
copy_certificates() {
    print_status "Copying certificates to final location..."
    
    # Copy certificates to the collector's certificate directory
    sudo cp "$TEMP_DIR/${COLLECTOR_NAME}.crt" "$CERT_DIR/server.crt"
    sudo cp "$TEMP_DIR/${COLLECTOR_NAME}.key" "$CERT_DIR/server.key"
    sudo cp "$TEMP_DIR/ca.crt" "$CERT_DIR/ca.crt"

    # Set proper permissions
    sudo chmod 600 "$CERT_DIR/server.key"
    sudo chmod 644 "$CERT_DIR/server.crt"
    sudo chmod 644 "$CERT_DIR/ca.crt"
    
    print_status "Certificates copied and permissions set"
}

# Function to create configuration file
create_config() {
    print_status "Creating OpenTelemetry collector configuration..."
    
    cat > "$CONFIG_DIR/config.yaml" << EOF
service:
  logLevel: info

otelCollector:
  otlp:
    endpoint: "0.0.0.0:4317"
    tls:
      certFile: "$CERT_DIR/server.crt"
      keyFile: "$CERT_DIR/server.key"
      clientCAFile: "$CERT_DIR/ca.crt"
    auth:
      authenticator: "cnauthenticator"
  
  prometheus:
    endpoint: "0.0.0.0:9464"
  
  extensions:
    cnauthenticator:
      printCN: true
      logLevel: "info"
  
  pipelines:
    metrics:
      receivers: ["otlp"]
      processors: ["deviceid", "transform"]
      exporters: ["prometheus"]
EOF
    
    print_status "Configuration file created at $CONFIG_DIR/config.yaml"
}

# Function to verify setup
verify_setup() {
    print_status "Verifying certificate setup..."
    
    # Check if certificate files exist
    if [[ ! -f "$CERT_DIR/server.crt" ]]; then
        print_error "Server certificate file is missing"
        exit 1
    fi
    
    if [[ ! -f "$CERT_DIR/server.key" ]]; then
        print_error "Server private key file is missing"
        exit 1
    fi
    
    if [[ ! -f "$CERT_DIR/ca.crt" ]]; then
        print_error "CA certificate file is missing"
        exit 1
    fi
    
    # Verify certificate validity
    if ! openssl verify -CAfile "$CERT_DIR/ca.crt" "$CERT_DIR/server.crt" >/dev/null 2>&1; then
        print_error "Certificate validation failed"
        exit 1
    fi
    
    # Check certificate subject
    SUBJECT=$(openssl x509 -in "$CERT_DIR/server.crt" -noout -subject 2>/dev/null | sed 's/subject=//')
    print_status "Certificate subject: $SUBJECT"
    
    # Check certificate expiration
    EXPIRY=$(openssl x509 -in "$CERT_DIR/server.crt" -noout -enddate 2>/dev/null | sed 's/notAfter=//')
    print_status "Certificate expires: $EXPIRY"
    
    print_status "Certificate setup verified successfully"
}

# Function to cleanup temporary files
cleanup() {
    print_status "Cleaning up temporary files..."
    rm -rf "$TEMP_DIR"
    print_status "Cleanup completed"
}

# Function to display next steps
show_next_steps() {
    echo
    print_status "OpenTelemetry collector certificate setup completed successfully!"
    echo
    echo "Next steps:"
    echo "1. Start the OpenTelemetry collector service:"
    echo "   sudo systemctl enable flightctl-otel-collector-enabled.target"
    echo "   sudo systemctl start flightctl-otel-collector-enabled.target"
    echo
    echo "2. Verify the collector is running:"
    echo "   curl http://localhost:9464/metrics"
    echo "   sudo systemctl status flightctl-otel-collector"
    echo
    echo "3. For Kubernetes deployment, see the documentation:"
    echo "   docs/user/otel-collector-certificates.md"
    echo
}

# Main execution
main() {
    echo "FlightCtl OpenTelemetry Collector Certificate Setup"
    echo "=================================================="
    echo
    
    check_permissions
    create_directories
    generate_certificates
    extract_ca_certificate
    copy_certificates
    create_config
    verify_setup
    cleanup
    show_next_steps
}

# Handle script arguments
case "${1:-}" in
    --help|-h)
        echo "Usage: $0 [OPTIONS]"
        echo
        echo "Options:"
        echo "  --help, -h    Show this help message"
        echo "  --verify      Only verify existing certificate setup"
        echo
        echo "This script sets up TLS certificates for the FlightCtl OpenTelemetry collector."
        echo "It follows the correct sequence:"
        echo "1. Creates CSR using ./bin/flightctl certificate request"
        echo "2. Approves the CSR"
        echo "3. Extracts CA certificate from enrollment config"
        echo "4. Copies certificates to proper locations"
        exit 0
        ;;
    --verify)
        verify_setup
        exit 0
        ;;
    "")
        main
        ;;
    *)
        print_error "Unknown option: $1"
        echo "Use --help for usage information"
        exit 1
        ;;
esac 