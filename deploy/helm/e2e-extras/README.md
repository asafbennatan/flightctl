# Flightctl E2E Extras Helm Chart

This Helm chart provides essential supporting services for Flightctl's End-to-End (E2E) testing environment.

## Components

- **Container Registry**: Local Docker registry for testing agent image updates
- **Git Server**: SSH-based Git repository for testing GitOps workflows  
- **Prometheus**: Metrics collection and monitoring
- **Jaeger**: Distributed tracing (optional)

## Certificate Management

This chart **requires** cert-manager for certificate management. Manual certificate generation is not supported.

### Prerequisites

- cert-manager installed in the cluster
- `flightctl-issuer` ClusterIssuer exists (created by main Flightctl chart with cert-manager enabled)

### Benefits

- Uses Flightctl's existing CA to sign registry certificates
- Automatic certificate renewal and lifecycle management
- Consistent with main Flightctl certificate infrastructure
- No manual certificate generation required

### CA Certificate for Agent Images

The deployment script automatically extracts the CA certificate from `flightctl-ca-secret` for use in agent images:

```bash
# CA certificate is extracted to bin/e2e-certs/pki/CA/ca.pem
# This is used by agent images to trust the registry certificates
```

## Configuration

### Default Values (`values.yaml`)
- cert-manager enabled by default
- Uses flightctl-issuer for certificate generation

### Development Values (`values.dev.yaml`)  
- cert-manager enabled for development environments
- NodePorts configured for local access

## Usage

### Prerequisites Check
The deployment script verifies cert-manager availability:

```bash
# Will fail if cert-manager or flightctl-issuer not available
make deploy-e2e-extras
```

### Manual Deployment
```bash
# Deploy with cert-manager (required)
helm upgrade --install flightctl-e2e-extras ./deploy/helm/e2e-extras \
  --set certManager.enabled=true
```

## Troubleshooting

### cert-manager Issues
```bash
# Check if cert-manager is installed
kubectl get pods -n cert-manager

# Check if flightctl-issuer exists
kubectl get clusterissuer flightctl-issuer

# Check certificate status
kubectl get certificate -n flightctl-e2e
```

### Deployment Failures
If deployment fails with certificate errors:

1. **Ensure cert-manager is installed**:
   ```bash
   kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.15.0/cert-manager.yaml
   ```

2. **Ensure Flightctl is deployed with cert-manager**:
   ```bash
   helm upgrade --install flightctl ./deploy/helm/flightctl \
     --set certManager.enabled=true
   ```

3. **Verify flightctl-issuer exists**:
   ```bash
   kubectl get clusterissuer flightctl-issuer
   ```

### Certificate Verification
```bash
# Check certificate was created
kubectl get certificate e2e-registry-cert -n flightctl-e2e

# Check certificate secret
kubectl get secret e2e-registry-tls -n flightctl-e2e

# View certificate details
kubectl describe certificate e2e-registry-cert -n flightctl-e2e
```

### CA Certificate Extraction
```bash
# Manually extract CA certificate if needed
test/scripts/extract_ca_cert.sh

# Verify CA certificate exists
ls -la bin/e2e-certs/pki/CA/ca.pem
``` 