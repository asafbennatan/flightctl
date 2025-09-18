# QCOW2 Caching Strategy for E2E Tests

This document describes the registry-based QCOW2 caching strategy implemented to reduce E2E test preparation time from ~8 minutes to ~2-3 minutes.

## Problem

The original `make prepare-e2e-test` process took ~8 minutes, with the main bottleneck being QCOW2 image creation (~5.5 minutes). The QCOW2 contained run-specific data (agent configuration, certificates, RPMs, registry settings), preventing effective caching.

## Solution

The new approach separates static and run-specific data:

- **Static data** (base OS, packages, user config) → Cached in generic QCOW2 template
- **Run-specific data** (agent config, certificates, RPMs) → Injected via cloud-init at VM startup

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    QCOW2 Caching Strategy                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────────────┐    ┌──────────────────┐    ┌─────────────┐ │
│  │ Generic QCOW2   │    │ Registry Cache   │    │ VM Startup  │ │
│  │ Template        │───▶│ (Quay.io)        │───▶│ + Cloud-Init│ │
│  │                 │    │                  │    │             │ │
│  │ • Base OS       │    │ • base-qcow2     │    │ • Mount     │ │
│  │ • Packages      │    │ • base-qcow2-    │    │   config    │ │
│  │ • User config   │    │   <git-hash>     │    │ • Install   │ │
│  │ • Custom info   │    │ • base-qcow2-    │    │   RPMs      │ │
│  │   scripts       │    │   latest         │    │ • Configure │ │
│  └─────────────────┘    └──────────────────┘    │   agent     │ │
│                                                  │ • Start     │ │
│                                                  │   service   │ │
│                                                  └─────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

## Components

### 1. Generic QCOW2 Template

**File**: `Containerfile-e2e-base-generic.local`

Contains only static data:
- Base OS (CentOS Stream 9 bootc)
- Standard packages (epel-release, python-dotenv, greenboot, podman-compose, firewalld)
- User configuration (user:user with sudo access)
- Custom info scripts directory structure
- Placeholder directories for run-specific data

### 2. Registry Caching System

**Files**: 
- `Containerfile-qcow2-cache` - Packages QCOW2 as container image
- `qcow2_cache_manager.sh` - Manages cache operations

**Registry Tags**:
- `base-qcow2-<git-hash>` - Versioned for specific commits
- `base-qcow2-latest` - Latest stable version (clean tree only)

### 3. Cloud-Init Configuration

**Files**:
- `prepare_vm_config.sh` - Prepares run-specific configuration data
- `prepare_cloud_init_config.sh` - Creates cloud-init ISO with configuration
- `setup-flightctl-cloud-init.sh` - Script that runs inside VM

**Process**:
1. Mount configuration data to `/mnt/flightctl-config`
2. Install RPMs via `dnf install`
3. Copy certificates to `/etc/flightctl/certs/`
4. Copy config to `/etc/flightctl/config.yaml`
5. Install CA cert and run `update-ca-trust`
6. Configure registry settings
7. Enable and start `flightctl-agent.service`

### 4. VM Integration

**Modified Files**:
- `vm_pool.go` - Added cloud-init support to VM creation
- `test.mk` - Added new makefile targets

**Environment Variables**:
- `CLOUD_INIT_DIR` - Path to cloud-init configuration directory
- `REGISTRY_CACHE` - Enable registry caching (auto-enabled in GitHub Actions)

## Usage

### Local Development

```bash
# Build and cache QCOW2 (first time or when base changes)
make build-generic-base
make cache-qcow2

# Prepare E2E test environment (uses cached QCOW2 + cloud-init)
make prepare-e2e-test

# Run E2E tests
make run-e2e-test
```

### Manual Cache Operations

```bash
# Try to get cached QCOW2 (returns 0 if found, 1 if needs building)
test/scripts/agent-images/qcow2_cache_manager.sh get

# Cache a QCOW2 file
test/scripts/agent-images/qcow2_cache_manager.sh cache

# Pull specific cached QCOW2
test/scripts/agent-images/qcow2_cache_manager.sh pull base-qcow2-latest

# Push QCOW2 to specific tag
test/scripts/agent-images/qcow2_cache_manager.sh push disk.qcow2 base-qcow2-abc123
```

### Environment Variables

```bash
# Enable registry caching (auto-enabled in GitHub Actions)
export REGISTRY_CACHE=true

# Override cache tag
export QCOW2_CACHE_TAG=my-custom-tag

# Override VM config directory
export VM_CONFIG_DIR=/path/to/vm/config

# Override cloud-init directory
export CLOUD_INIT_DIR=/path/to/cloud-init
```

## GitHub Actions Integration

The system is integrated into the GitHub Actions workflow:

1. **publish-base-containers** - Builds generic base container image
2. **publish-qcow2-cache** - Builds QCOW2 from generic base and caches it
3. **publish-dependent-e2e-containers** - Builds dependent container images

## Performance Benefits

### Before (Original System)
- **Total time**: ~8 minutes
- **QCOW2 creation**: ~5.5 minutes
- **Container builds**: ~2.5 minutes

### After (Cached System)
- **Total time**: ~2-3 minutes
- **QCOW2 cache hit**: ~30 seconds (download + extract)
- **Cloud-init setup**: ~1-2 minutes
- **Container builds**: ~1 minute (cached)

### Cache Hit Scenarios
- **First run**: Builds and caches QCOW2 (~5.5 minutes)
- **Subsequent runs**: Uses cached QCOW2 (~30 seconds)
- **Code changes**: Uses cached QCOW2 + cloud-init (~2-3 minutes)
- **Base image changes**: Rebuilds and caches QCOW2 (~5.5 minutes)

## Troubleshooting

### Cache Miss Issues

```bash
# Check if registry caching is enabled
echo $REGISTRY_CACHE

# Check if QCOW2 cache manager is working
test/scripts/agent-images/qcow2_cache_manager.sh get

# Force rebuild QCOW2
test/scripts/agent-images/qcow2_cache_manager.sh get force
```

### Cloud-Init Issues

```bash
# Check cloud-init configuration
ls -la bin/cloud-init-data/

# Check VM configuration
ls -la bin/vm-config/

# Debug cloud-init in VM
# SSH into VM and check:
sudo journalctl -u cloud-init
sudo journalctl -u cloud-init-local
```

### VM Startup Issues

```bash
# Check if cloud-init directory is set
echo $CLOUD_INIT_DIR

# Verify cloud-init data exists
test -d "$CLOUD_INIT_DIR" && echo "Cloud-init data exists" || echo "Cloud-init data missing"

# Check VM console output
# Set DEBUG_VM_CONSOLE=1 to see console output
```

## File Structure

```
test/scripts/agent-images/
├── Containerfile-e2e-base-generic.local    # Generic QCOW2 template
├── Containerfile-qcow2-cache               # QCOW2 cache container
├── qcow2_cache_manager.sh                  # Cache management script
├── build_generic_base.sh                   # Build generic base
├── prepare_vm_config.sh                    # Prepare VM configuration
├── prepare_cloud_init_config.sh            # Prepare cloud-init
├── setup-flightctl-cloud-init.sh           # VM setup script
└── README-QCOW2-CACHING.md                 # This documentation

bin/
├── vm-config/                              # Run-specific configuration
│   ├── config.yaml                         # Agent configuration
│   ├── certs/                              # Agent certificates
│   ├── rpms/                               # Flightctl RPMs
│   ├── ca.pem                              # E2E CA certificate
│   ├── registry.conf                       # Registry configuration
│   └── setup-flightctl.sh                  # Setup script
├── cloud-init-data/                        # Cloud-init ISO data
│   ├── user-data                           # Cloud-init user data
│   ├── meta-data                           # VM metadata
│   ├── network-config                      # Network configuration
│   └── flightctl-config/                   # Configuration data
└── output/qcow2/disk.qcow2                 # Final QCOW2 image
```

## Migration Guide

### From Original System

1. **No breaking changes** - Original system still works
2. **Gradual adoption** - Can enable caching per environment
3. **Fallback support** - Falls back to original build if cache fails

### Enabling Caching

```bash
# For local development
export REGISTRY_CACHE=true
make prepare-e2e-test

# For CI/CD
# Set REGISTRY_CACHE=true in GitHub Actions environment
```

### Disabling Caching

```bash
# Disable registry caching
unset REGISTRY_CACHE
make prepare-e2e-test
```

## Future Enhancements

1. **Multi-architecture support** - Cache QCOW2 for different architectures
2. **Incremental updates** - Only rebuild changed components
3. **Compression** - Compress QCOW2 images for faster transfers
4. **Local registry** - Support for local container registries
5. **Metrics** - Track cache hit rates and performance improvements
