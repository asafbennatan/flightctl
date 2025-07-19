# Certificate Management with cert-manager

This guide explains how to use cert-manager for centralized certificate management in flightctl.

## Overview

When `certManager.enabled: true` is set in the Helm values, flightctl will:

1. **Create a CA Certificate** - cert-manager generates a self-signed CA
2. **Create a CA Issuer** - Uses the CA to sign other certificates
3. **Mount certificates in flightctl-api** - The API service gets access to all required certificate files
4. **Generate otel-collector certificate** - Signed by the same CA

**Important**: With cert-manager enabled, flightctl-api will **exit with an error** if any required certificate files are missing, instead of generating them automatically. This ensures that all certificates are managed centrally by cert-manager.

## Prerequisites

1. **Install cert-manager** in your cluster:
   ```bash
   kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.15.0/cert-manager.yaml
   ```

2. **Wait for cert-manager to be ready**:
   ```bash
   kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=cert-manager -n cert-manager
   ```

## Deployment

1. **Enable cert-manager** in your values:
   ```yaml
   certManager:
     enabled: true
   ```

2. **Deploy flightctl**:
   ```bash
   helm install flightctl ./deploy/helm/flightctl --values your-values.yaml
   ```

## What Gets Created

### 1. CA Certificate
- **Resource**: `Certificate` named `flightctl-ca`
- **Secret**: `flightctl-ca-secret` containing:
  - `tls.crt` - CA certificate
  - `tls.key` - CA private key

### 2. CA Issuer
- **Resource**: `ClusterIssuer` named `flightctl-issuer`
- **Purpose**: Signs certificates using the CA

### 3. Service Certificates
- **flightctl-api**: Certificate for the API service
- **otel-collector**: Certificate for the telemetry collector

## Required Certificate Files

When cert-manager is enabled, flightctl-api expects these certificate files to exist:

- **CA Certificate**: `/root/.flightctl/ca.crt` and `/root/.flightctl/ca.key`
- **Server Certificate**: `/root/.flightctl/server.crt` and `/root/.flightctl/server.key`
- **Client Enrollment Certificate**: `/root/.flightctl/client-enrollment.crt` and `/root/.flightctl/client-enrollment.key`

If any of these files are missing, flightctl-api will exit with an error message indicating which files are expected.

## File Locations in Pods

### flightctl-api Pod
The certificate files are mounted at:
- `/root/.flightctl/ca.crt` - CA certificate (from `flightctl-ca-secret`)
- `/root/.flightctl/ca.key` - CA private key (from `flightctl-ca-secret`)
- `/root/.flightctl/server.crt` - Server certificate (from `flightctl-api-tls`)
- `/root/.flightctl/server.key` - Server private key (from `flightctl-api-tls`)
- `/root/.flightctl/client-enrollment.crt` - Client enrollment certificate
- `/root/.flightctl/client-enrollment.key` - Client enrollment private key

### otel-collector Pod
The service certificate is mounted at:
- `/etc/otel-collector/tls/tls.crt` - Service certificate
- `/etc/otel-collector/tls/tls.key` - Service private key
- `/etc/otel-collector/tls/ca.crt` - CA certificate

## Verification

Check that certificates are created:
```bash
kubectl get certificates -n flightctl
kubectl get secrets -n flightctl | grep tls
```

## Troubleshooting

1. **Check cert-manager logs**:
   ```bash
   kubectl logs -n cert-manager deployment/cert-manager
   ```

2. **Check certificate status**:
   ```bash
   kubectl describe certificate flightctl-ca -n flightctl
   ```

3. **Verify CA secret exists**:
   ```bash
   kubectl get secret flightctl-ca-secret -n flightctl
   ``` 