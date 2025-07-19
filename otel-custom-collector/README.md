# OpenTelemetry Collector with mTLS

This directory contains a custom OpenTelemetry Collector with mTLS authentication, built specifically for use with flightctl.

## Features

- **Custom Extensions**: 
  - `cnauthenticator` - CN-based authentication extension
  - `deviceidprocessor` - Device ID processing
- **Custom Processors**:
  - `transformprocessor` - Data transformation
  - `batchprocessor` - Data batching
- **Exporters**:
  - `prometheusexporter` - Prometheus metrics export
  - `debugexporter` - Debug output
- **mTLS Support**: Uses flightctl CA and certificates for secure communication

## Quick Start

### Prerequisites

- Podman installed and running
- Go 1.23+ (for building the binary)
- OpenTelemetry Collector Builder

### Building and Running

1. **Build the binary** (if not already built):
   ```bash
   ~/go/bin/builder --config otelcol-builder.yaml
   ```

2. **Build the container**:
   ```bash
   ./run-collector.sh build
   ```

3. **Start the collector**:
   ```bash
   ./run-collector.sh start
   ```

4. **Check status**:
   ```bash
   ./run-collector.sh status
   ```

5. **View logs**:
   ```bash
   ./run-collector.sh logs
   ```

## Management Script

The `run-collector.sh` script provides easy management of the container:

- `./run-collector.sh start` - Start the collector
- `./run-collector.sh stop` - Stop and remove the container
- `./run-collector.sh restart` - Restart the container
- `./run-collector.sh logs` - Show container logs
- `./run-collector.sh status` - Show container status
- `./run-collector.sh build` - Build the container image

## Configuration

The collector is configured via `config.yaml` and includes:

- **OTLP Receiver**: Listens on port 4317 with mTLS
- **Prometheus Exporter**: Exposes metrics on port 8889
- **Custom Extensions**: CN authenticator and device ID processor
- **Custom Processors**: Transform and batch processors

## Certificates

The container uses certificates from the `certs/` directory:
- `ca.crt` - CA certificate
- `otel-collector.crt` - Collector certificate
- `otel-collector.key` - Collector private key

These certificates are signed by the flightctl CA and include proper SANs for localhost.

## Ports

- **4317**: OTLP gRPC receiver (mTLS enabled)
- **8889**: Prometheus metrics endpoint

## Testing

Test the collector endpoints:

```bash
# Check Prometheus metrics
curl http://localhost:8889/metrics

# Check if ports are listening
netstat -tlnp | grep -E "(4317|8889)"
```

## Troubleshooting

### Container won't start
- Check if ports 4317 and 8889 are already in use
- Verify certificates exist in the `certs/` directory
- Check container logs: `./run-collector.sh logs`

### Binary issues
- Ensure the binary is built with the correct Go version
- Rebuild if needed: `~/go/bin/builder --config otelcol-builder.yaml`

### Certificate issues
- Verify certificates are valid and properly signed
- Check certificate paths in `config.yaml`
- Ensure CA certificate is trusted

## Architecture

```
[OTLP Client] --mTLS--> [OTLP Receiver:4317] --> [Custom Processors] --> [Exporters]
                                                      |
                                                      v
                                              [Prometheus:8889]
```

The collector uses the same CA and certificate infrastructure as flightctl, ensuring secure communication between components. 