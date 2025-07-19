#!/bin/bash

# Start Redis port forwarding in the background
echo "Starting Redis port forwarding..."
kubectl port-forward -n flightctl-internal svc/flightctl-kv 6379:6379 --context kind-kind &
REDIS_PF_PID=$!

# Wait a moment for port forwarding to establish
sleep 2

# Start the flightctl-api with the correct credentials
echo "Starting flightctl-api..."
DB_PASSWORD="75ahN-haCjy-csOzX-WpdLv" \
KV_PASSWORD="BOOFs-Y6dJa-Lsnj0-6f0h3" \
SERVICE_ADDRESS=":2443" \
AGENT_ENDPOINT_ADDRESS=":6443" \
FLIGHTCTL_DISABLE_AUTH=true \
./bin/flightctl-api

# Clean up port forwarding when API exits
echo "Cleaning up..."
kill $REDIS_PF_PID 2>/dev/null || true 