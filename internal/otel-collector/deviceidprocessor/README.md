# Device ID Processor

The Device ID Processor is an OpenTelemetry Collector processor that enriches metrics with fleet information from the Flight Control API using intelligent caching.

## Overview

This processor takes a device fingerprint from the context and queries the Flight Control API to determine the device's owner (fleet). It uses the existing KV store (Redis) to cache fleet information, significantly improving performance by avoiding repeated API calls.

The processor adds `fleet_name` attribute only when a device is owned by a fleet. If a device has no fleet owner or if the API call fails, no attributes are added.

## Features

### Intelligent Caching
- **KV Store Integration**: Uses the existing Flight Control KV store (Redis) for caching
- **24-Hour TTL**: Fleet information is cached for 24 hours to reduce API load
- **Automatic Refresh**: Cache entries are automatically refreshed when they expire
- **Graceful Degradation**: Falls back gracefully if caching or API calls fail

### Performance Benefits
- **Reduced API Calls**: Subsequent requests for the same device use cached data
- **Faster Processing**: Cache hits are much faster than API calls
- **Scalable**: Multiple collector instances can share the same cache

## Configuration

The processor automatically uses the main Flight Control configuration and doesn't require additional configuration. It leverages:

- **Service Configuration**: Uses `Service.BaseUrl` and `Service.CertStore` for API connectivity
- **KV Store Configuration**: Uses `KV.Hostname`, `KV.Port`, and `KV.Password` for caching

### Example Configuration

The processor is automatically configured when included in the pipeline:

```yaml
service:
  pipelines:
    metrics:
      processors: [deviceid]
      receivers: [otlp]
      exporters: [prometheus]
```

## Cache Management

### Cache Keys
Fleet information is cached using keys in the format:
```
deviceid:fleet:{device_fingerprint}
```

### Cache Structure
Each cache entry contains:
```json
{
  "fleet_name": "production-fleet",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### Cache Behavior
- **Cache Hit**: Returns cached fleet name immediately
- **Cache Miss**: Fetches from API and stores result in cache
- **Cache Expiry**: Entries older than 24 hours are considered expired
- **Cache Errors**: Logged but don't prevent API fallback

## Usage

### 1. Automatic Setup
The processor automatically initializes when the OpenTelemetry Collector starts, using the main Flight Control configuration.

### 2. Device Fingerprint Context
The processor expects device fingerprints to be available in the context:

```go
ctx := context.WithValue(context.Background(), "device_fingerprint", "device-123")
```

### 3. Metric Enrichment
The processor automatically enriches metrics with `fleet_name` attribute only when:
- Device belongs to a fleet
- API call succeeds
- Cache or API returns valid fleet information

If a device has no fleet owner or if the API call fails, no attributes are added to the metrics.

## Behavior

### Success Cases
1. **Device owned by a fleet**: Adds `fleet_name` attribute with the fleet name
2. **Device not owned by a fleet**: No attributes added
3. **Device not found**: No attributes added

### Error Handling
- **API connection failures**: No attributes added
- **Authentication failures**: No attributes added
- **Invalid device fingerprint**: No attributes added
- **Cache failures**: Falls back to API call, no attributes added if API also fails

The processor is designed to be resilient and will not fail the entire pipeline if the Flight Control API is unavailable.

## Example Output

### Device in Fleet
```json
{
  "metrics": [
    {
      "name": "cpu_usage",
      "dataPoints": [
        {
          "attributes": {
            "fleet_name": "production-fleet"
          },
          "value": 75.5
        }
      ]
    }
  ]
}
```

### Device Not in Fleet
```json
{
  "metrics": [
    {
      "name": "cpu_usage",
      "dataPoints": [
        {
          "attributes": {},
          "value": 75.5
        }
      ]
    }
  ]
}
```

## Monitoring

### Cache Performance
Monitor cache performance through:
- Cache hit/miss ratios in logs
- KV store metrics (Redis metrics)
- Processor latency metrics

### Error Handling
The processor gracefully handles:
- KV store connection failures
- API unavailability
- Invalid device fingerprints
- Cache serialization errors

## Troubleshooting

### Common Issues

1. **Cache Connection Failures**
   - Verify KV store (Redis) is running and accessible
   - Check KV store configuration in main config

2. **API Connection Issues**
   - Verify Flight Control API is accessible
   - Check service configuration and certificates

3. **Cache Performance**
   - Monitor cache hit rates
   - Consider adjusting cache TTL if needed

### Debug Logging
Enable debug logging to see cache operations:
```yaml
service:
  logLevel: debug
```

## Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Metrics       │    │   Device ID     │    │   Flight        │
│   Input         │───▶│   Processor     │───▶│   Control API   │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                              │
                              ▼
                       ┌─────────────────┐
                       │   KV Store      │
                       │   (Redis)       │
                       │   Cache         │
                       └─────────────────┘
```

The processor provides a high-performance, scalable solution for enriching metrics with fleet information while minimizing API load through intelligent caching. It only adds fleet information when available and gracefully handles all error conditions without adding unwanted attributes. 