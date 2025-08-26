#!/usr/bin/env bash

set -eo pipefail

# Directory path for source files
SOURCE_DIR="deploy"

# Load shared functions
SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"
source "${SCRIPT_DIR}"/shared.sh
source "${SCRIPT_DIR}"/secrets.sh

setup_network_route() {
    # Wait a moment for the network to be fully established
    sleep 3
    
    echo "Setting up network route for podman networks..."
    
    # For standalone services, we typically use the default podman network
    # Check if flightctl network exists first, otherwise use default podman network
    local target_network="podman"
    if sudo podman network exists flightctl; then
        target_network="flightctl"
        echo "  Using flightctl network"
    else
        echo "  Using default podman network"
    fi
    
    # Get the subnet of the target network
    local network_subnet=$(sudo podman network inspect "$target_network" --format '{{range .Subnets}}{{.Subnet}}{{end}}' 2>/dev/null)
    
    if [[ -n "$network_subnet" ]]; then
        # Get the bridge interface name for the network
        local bridge_name=$(sudo podman network inspect "$target_network" --format '{{.NetworkInterface}}' 2>/dev/null)
        
        # If bridge name is empty, try to find it by looking for cni- prefixed interfaces
        if [[ -z "$bridge_name" ]]; then
            bridge_name=$(ip link show | grep -o 'cni-podman[0-9]*' | head -n1)
        fi
        
        # If still empty, fall back to podman0 (default)
        if [[ -z "$bridge_name" ]]; then
            bridge_name="podman0"
        fi
        
        echo "  Network: $target_network"
        echo "  Network subnet: $network_subnet"
        echo "  Bridge interface: $bridge_name"
        
        # Check if route already exists
        if ip route show table 75 | grep -q "$network_subnet"; then
            echo "  Route already exists, removing old route first..."
            sudo ip route del "$network_subnet" dev "$bridge_name" table 75 2>/dev/null || true
        fi
        
        if sudo ip route add "$network_subnet" dev "$bridge_name" table 75 metric 10; then
            echo "  ✓ Successfully added route: $network_subnet dev $bridge_name table 75 metric 10"
        else
            echo "  ✗ Warning: Failed to add network route for $network_subnet"
        fi
    else
        echo "  ✗ Warning: Could not determine network subnet for $target_network"
    fi
}

deploy_service() {
    local service_name=$1
    local service_full_name="flightctl-${service_name}.service"

    echo "Starting Deployment for $service_full_name"

    # Stop the service if it's running
    systemctl stop "$service_full_name" || true

    echo "Performing install for $service_full_name"
    # Handle pre-startup logic for each service
    if [[ "$service_name" == "db" ]]; then
        podman volume rm flightctl-db || true
        podman volume create --opt device=tmpfs --opt type=tmpfs --opt o=nodev,noexec flightctl-db
        ensure_postgres_secrets
    elif [[ "$service_name" == "kv" ]]; then
        ensure_kv_secrets
    else
        echo "No pre-startup logic for $service_name"
    fi

    echo "Installing quadlet files for $service_full_name"

    render_service "$service_name" "${SOURCE_DIR}" "standalone"
    start_service "$service_full_name"
    
    # Add network route for flightctl network after service starts
    setup_network_route

    echo "Deployment completed for $service_full_name"
}

main() {
    if [[ $# -ne 1 ]]; then
        echo "Usage: $0 <service_name>"
        echo "Available services: db, kv, alertmanager"
        exit 1
    fi

    # Validate service name
    local service_name="$1"
    if [[ ! "$service_name" =~ ^(db|kv|alertmanager)$ ]]; then
        echo "Error: Invalid service name: $service_name"
        echo "Available services: db, kv, alertmanager"
        exit 1
    fi

    deploy_service "$service_name"
}

# Execute the main function with all command line arguments
main "$@"
