#!/bin/bash

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
CERT_MANAGER_VERSION="v1.15.0"
NAMESPACE="${NAMESPACE:-flightctl}"
HELM_CHART_PATH="./deploy/helm/flightctl"

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

# Function to check if cert-manager is installed
check_cert_manager() {
    if kubectl get namespace cert-manager >/dev/null 2>&1; then
        return 0
    else
        return 1
    fi
}

# Function to install cert-manager
install_cert_manager() {
    print_status "Installing cert-manager ${CERT_MANAGER_VERSION}..."
    
    kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml
    
    print_status "Waiting for cert-manager to be ready..."
    kubectl wait --for=condition=ready pod -l app.kubernetes.io/instance=cert-manager -n cert-manager --timeout=300s
    
    print_status "cert-manager installed successfully"
}

# Function to create namespace
create_namespace() {
    if ! kubectl get namespace ${NAMESPACE} >/dev/null 2>&1; then
        print_status "Creating namespace ${NAMESPACE}..."
        kubectl create namespace ${NAMESPACE}
    else
        print_status "Namespace ${NAMESPACE} already exists"
    fi
}

# Function to deploy flightctl with cert-manager
deploy_flightctl() {
    print_status "Deploying flightctl with cert-manager enabled..."
    
    # Build dependencies
    helm dependency build ${HELM_CHART_PATH}
    
    # Deploy with cert-manager enabled
    helm upgrade --install flightctl ${HELM_CHART_PATH} \
        --namespace ${NAMESPACE} \
        --create-namespace \
        --set certManager.enabled=true \
        --set global.auth.type=k8s \
        --set keycloak.enabled=false \
        --wait \
        --timeout=10m
    
    print_status "flightctl deployed successfully with cert-manager"
}

# Function to verify deployment
verify_deployment() {
    print_status "Verifying deployment..."
    
    # Check if certificates are created
    if kubectl get certificate -n ${NAMESPACE} >/dev/null 2>&1; then
        print_status "Certificates found:"
        kubectl get certificate -n ${NAMESPACE}
    else
        print_warning "No certificates found yet (they may still be being created)"
    fi
    
    # Check if pods are running
    print_status "Checking pod status..."
    kubectl get pods -n ${NAMESPACE}
    
    # Check if secrets are created
    print_status "Checking certificate secrets..."
    kubectl get secrets -n ${NAMESPACE} | grep -E "(tls|ca)"
}

# Main execution
main() {
    print_status "Starting flightctl deployment with cert-manager..."
    
    # Check if kubectl is available
    if ! command -v kubectl >/dev/null 2>&1; then
        print_error "kubectl is not installed or not in PATH"
        exit 1
    fi
    
    # Check if helm is available
    if ! command -v helm >/dev/null 2>&1; then
        print_error "helm is not installed or not in PATH"
        exit 1
    fi
    
    # Create namespace
    create_namespace
    
    # Install cert-manager if not present
    if ! check_cert_manager; then
        install_cert_manager
    else
        print_status "cert-manager is already installed"
    fi
    
    # Deploy flightctl
    deploy_flightctl
    
    # Verify deployment
    verify_deployment
    
    print_status "Deployment completed successfully!"
    print_status "You can now access flightctl with certificates managed by cert-manager"
}

# Run main function
main "$@" 