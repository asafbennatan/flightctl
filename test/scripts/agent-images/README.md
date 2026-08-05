# E2E Agent images

We generate multiple agent images for testing purposes, each with a different
services running, but all connected to our flightctl service for management.

This work is performed by the `create_agent_images.sh` script in this
directory.

And can be triggered from the top-level makefile with: `make e2e-agent-images`

The `AGENT_OS_ID` parameter controls which OS flavor to build:

```bash
# Build for default OS (cs9-bootc): bootc base + variants + bundle + qcow2,
# plus the package-mode OCI test image (create_package_mode_image.sh)
make e2e-agent-images

# Build for CS10 bootc only (no package-mode OCI)
AGENT_OS_ID=cs10-bootc make e2e-agent-images
```

## Build Process

`make e2e-agent-images` delegates to:

1. **Bootc agent images**: `create_agent_images.sh` → `scripts/build.sh --base`, then
   `scripts/build_and_qcow2.sh` (variants, bundle, qcow2)
2. **App images**: `create_application_image.sh`
3. **Package-mode OCI** (CS9 only): `create_package_mode_image.sh` builds
   `containerfiles/cs9-regular/Containerfile` and writes
   `bin/agent-artifacts/agent-images-bundle-cs9-regular.tar`

Bootc `AGENT_OS_ID` values are `cs9-bootc` (default) and `cs10-bootc`. Package-mode is
not an OS flavor; it is an extra OCI image for testcontainers.

## OS Flavors and Tagging

Bootc flavors with dedicated Containerfiles:

- **cs9-bootc** - Based on CentOS Stream 9 bootc (default)
- **cs10-bootc** - Based on CentOS Stream 10 bootc

Package-mode testcontainer image:

- **cs9-regular** Containerfile under `containerfiles/cs9-regular/` (built by
  `create_package_mode_image.sh`, not via `AGENT_OS_ID`)

### Building Different Flavors

```bash
# Build cs9-bootc images (default, community)
./scripts/build.sh --base

# Build cs10-bootc images (community)
AGENT_OS_ID=cs10-bootc ./scripts/build.sh --base

# Build Red Hat variants
DISTRO=redhat AGENT_OS_ID=cs9-bootc ./scripts/build.sh --base
DISTRO=redhat AGENT_OS_ID=cs10-bootc ./scripts/build.sh --base

# Package-mode OCI only
./create_package_mode_image.sh
```

### Image Tagging

Images are tagged with OS flavor identifiers for easy selection:

**Base Images:**
- `quay.io/flightctl/flightctl-device:base-cs9-bootc-${TAG}` (canonical)
- `quay.io/flightctl/flightctl-device:base-cs10-bootc-${TAG}` (canonical)
- `quay.io/flightctl/flightctl-device:base` (latest flavor)
- `quay.io/flightctl/flightctl-device:base-cs9-bootc`
- `quay.io/flightctl/flightctl-device:base-${TAG}`
- `quay.io/flightctl/flightctl-device:base-cs9-regular` / `:base-cs9-regular-${TAG}` (package-mode OCI)

**Variant Images:**
- `quay.io/flightctl/flightctl-device:v2-cs9-bootc-${TAG}`
- `quay.io/flightctl/flightctl-device:v2-cs10-bootc-${TAG}`
- `quay.io/flightctl/flightctl-device:v2` (latest flavor)
- `quay.io/flightctl/flightctl-device:v2-cs9-bootc`

This allows selecting specific OS versions in deployment configurations.

## Directory Structure

The build system now uses a modular structure:

```text
agent-images/
├── base/                  # Shared files for base images
├── containerfiles/        # OS flavor-specific Containerfiles
│   ├── cs9-bootc/         # CentOS Stream 9 bootc
│   │   └── Containerfile
│   ├── cs9-regular/       # Package-mode base image
│   │   └── Containerfile
│   ├── cs9-bootc-redhat/  # RHEL 9 bootc
│   │   └── Containerfile
│   ├── cs10-bootc/        # CentOS Stream 10 bootc
│   │   └── Containerfile
│   └── cs10-bootc-redhat/ # RHEL 10 bootc
│       └── Containerfile
├── variants/              # Variant-specific files
│   ├── v2/, v3/, ..., v10/   # Each contains Containerfile and variant-specific files
├── apps/                  # Application images (Containerfile.<app-name>.<version>)
├── common/                # Shared files used by variants/apps
├── scripts/               # Build automation scripts
│   ├── build.sh           # Main build script (base, variants, apps)
│   ├── build_and_qcow2.sh # Orchestrates parallel builds
│   ├── bundle.sh          # Create image bundles
│   ├── qcow2.sh           # Generate QCOW2 disk images
│   └── upload-images.sh   # Upload images to registry
├── create_agent_images.sh        # Bootc agent images wrapper
├── create_application_image.sh   # App OCI images
└── create_package_mode_image.sh  # Package-mode agent OCI (CS9)
```

The images are built using the `Containerfile` files in the respective directories. For functionality or service deployment changes, update the appropriate `containerfiles/*/Containerfile`, `variants/vX/Containerfile`, or create new variants as needed.

## Build Scripts

The `scripts/` directory contains modular build automation:

- **`build.sh`** - Main build script with options: `--base`, `--variants`, `--apps`
- **`build_and_qcow2.sh`** - Orchestrates bootc variant, bundle, and QCOW2 builds
- **`bundle.sh`** - Creates tar bundles of built images for distribution
- **`qcow2.sh`** - Generates bootable QCOW2 disk images using bootc-image-builder
- **`upload-images.sh`** - Uploads image bundles to container registries
- **`../create_package_mode_image.sh`** - Builds the package-mode agent OCI image + bundle

Use `./scripts/build.sh --help` for detailed usage and options.

### Image Tagging

Each image is tagged with multiple tags for flexibility:

| Tag Pattern               | Example                                             |
|---------------------------|-----------------------------------------------------|
| `<name>-${OS_ID}-${TAG}`  | `quay.io/flightctl/flightctl-device:base-cs9-bootc-v0.5.0` |
| `<name>`                  | `quay.io/flightctl/flightctl-device:base`           |
| `<name>-${OS_ID}`         | `quay.io/flightctl/flightctl-device:base-cs9-bootc` |
| `<name>-${TAG}`           | `quay.io/flightctl/flightctl-device:base-v0.5.0`    |

Where `<name>` is `base`, `v2`, `v3`, etc.

### Build Outputs

| Name   | QCOW2 Image                      | Container Image Tags                        |
|--------|----------------------------------|---------------------------------------------|
| base   | `bin/output/qcow2/disk.qcow2`    | `base`, `base-${OS_ID}`, `base-${TAG}`, `base-${OS_ID}-${TAG}` |
| v2     | N/A                              | `v2`, `v2-${OS_ID}`, `v2-${TAG}`, `v2-${OS_ID}-${TAG}` |
| v3     | N/A                              | `v3`, `v3-${OS_ID}`, `v3-${TAG}`, `v3-${OS_ID}-${TAG}` |

> **Note:** `qcow2.sh` writes the disk image to `bin/output/agent-qcow2-${OS_ID}/qcow2/disk.qcow2`.
> When using `create_agent_images.sh` for bootc flavors, the image is moved to `bin/output/qcow2/disk.qcow2`.
> Package-mode (`cs9-regular`) produces an OCI bundle only (`bin/agent-artifacts/agent-images-bundle-cs9-regular.tar`).

### Local Usage and Registry Remapping

Images are built locally with the default repository prefix `quay.io/flightctl/flightctl-device`
(configurable via `IMAGE_REPO`). For e2e testing, images are typically uploaded to a local
registry and the `quay.io/flightctl` prefix is remapped to the local registry address.

To configure registry remapping in a QCOW2 image, use `inject_agent_files_into_qcow.sh`:

```bash
./test/scripts/inject_agent_files_into_qcow.sh --registry-address <host>:5000
```

This creates a containers registry config at `/etc/containers/registries.conf.d/flightctl-remap.conf`
that remaps `quay.io/flightctl` to the local registry:

```toml
[[registry]]
prefix = "quay.io/flightctl"
location = "<host>:5000/flightctl"
```

With this config, when the agent pulls `quay.io/flightctl/flightctl-device:v2`, it will
actually pull from `<host>:5000/flightctl/flightctl-device:v2`.

## Credentials

All images are built with the following credentials:
- user: `user`
- password: `user`

## Image descriptions
### base
This image is the base image for all other images. It contains the following services:
- `flightctl-agent` - The agent service that connects to the flightctl service configured
   with the `test/script/prepare_agent_config.sh` script to be connected to our local
   flightctl service.

The installed flightctl-agent will be either a locally compiled rpm or a downloaded
rpm based on the `FLIGHTCTL_RPM` variable, please see [test-docs](../../README.md) for more information.

It is configured to trust our locally generated CA created in `test/scripts/create_e2e_certs.sh`

### v2
This image builds on top of the base image, and adds the following services, useful
to test agent reporting of systemd services:
 * test-e2e-dummy which just runs a sleep 3600 for 1h
 * test-e2e-crashing which runs /bin/false and attempts restart every few minutes

### v3
This image builds on top of the base image, and adds the following services, useful
 * test-e2e-another-dummy which just runs a sleep 3600 for 1h
