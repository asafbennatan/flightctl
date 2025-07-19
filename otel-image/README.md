# OpenTelemetry Sample Application

This directory contains a Go application that generates fake telemetry data using OpenTelemetry SDK with mTLS support and configurable collector endpoint.

## Files

- `otel-sample-app/main.go` - Main Go application source code
- `otel-sample-app/go.mod` - Go module dependencies
- `otel-sample-app/config.yaml` - Example configuration file (for reference)
- `otel-sample-app.service` - Systemd service file
- `Containerfile-sample-otel.local` - Container configuration with multi-stage build
- `fleet.yaml` - Flightctl fleet configuration for deployment

## Application Features

- **OpenTelemetry SDK**: Uses official OpenTelemetry Go SDK
- **mTLS Support**: Configurable mutual TLS authentication
- **Configurable Collector**: Endpoint can be configured via YAML
- **Fake Telemetry**: Generates realistic trace data with:
  - Multiple operation types (requests, database queries, API calls, etc.)
  - Random processing times
  - Child spans
  - HTTP attributes
- **Systemd Integration**: Runs as a systemd service

## Configuration

The application reads configuration from `/etc/otel-sample/config.yaml`. This file is managed by Flightctl through the fleet configuration and will be deployed automatically.

Example configuration structure:

```yaml
collector:
  endpoint: "localhost:4317"  # OTLP collector endpoint
  # Connection mode: "insecure", "tls", or "mtls"
  mode: "insecure"
  tls:
    ca_file: "/etc/otel-sample/certs/ca.crt"  # CA certificate for server verification
  mtls:
    cert_file: "/etc/otel-sample/certs/otel.crt"  # Client certificate
    key_file: "/etc/otel-sample/certs/otel.key"   # Client private key
    ca_file: "/etc/otel-sample/certs/ca.crt"      # CA certificate for server verification

service:
  name: "otel-sample-app"     # Service name for traces
  version: "1.0.0"           # Service version

telemetry:
  interval: "5s"             # Interval between telemetry generation
```

## Connection Modes

The application supports three connection modes:

### Insecure Mode (`mode: "insecure"`)
- No TLS encryption
- Suitable for local development or trusted networks
- No certificates required

### TLS Mode (`mode: "tls"`)
- TLS encryption with server certificate verification
- Requires CA certificate for server verification
- No client certificate needed

### mTLS Mode (`mode: "mtls"`)
- Mutual TLS authentication
- Requires client certificate and key
- Requires CA certificate for server verification
- Most secure option for production environments

## Service Management

- **Check status:** `systemctl status otel-sample-app.service`
- **View logs:** `journalctl -u otel-sample-app.service -f`
- **Stop service:** `systemctl stop otel-sample-app.service`
- **Restart service:** `systemctl restart otel-sample-app.service`
- **Disable service:** `systemctl disable otel-sample-app.service`

## Deployment

### Using Flightctl Fleet

The application is deployed using Flightctl fleet configuration (`fleet.yaml`):

1. **Fleet Configuration**: Defines the deployment template with:
   - OS image specification
   - Configuration management (inline config and certificates)
   - Resource monitoring
   - Systemd service management

2. **Configuration Management**: The config file is managed by Flightctl and deployed to `/etc/otel-sample/config.yaml`

3. **Certificate Management**: Certificates are managed through Flightctl's certificate system

### Building

The container uses a multi-stage build:
1. **Build stage**: Uses Go Alpine image to compile the application
2. **Final stage**: Uses the flightctl base image and copies only the binary

This results in a smaller, more secure final image without build dependencies. 