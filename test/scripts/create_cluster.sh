#!/usr/bin/env bash

kind create cluster --config test/scripts/kind_cluster.yaml

# Install cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.15.1/cert-manager.yaml

# Wait for cert-manager to be ready
kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=cert-manager -n cert-manager --timeout=300s

if [ "$GATEWAY" ]; then
    test/scripts/gateway/install-gateway.sh
fi

echo ""
