# E2E Agent images

We generate multiple agent images for testing purposes, each with a different
services running, but all connected to our flightctl service for management.

Build from the top-level makefile with: `make e2e-agent-images`

## Build Scripts

- `build_image.sh` - Builds a container image from a Containerfile
- `convert_to_qcow2.sh` - Converts a container image to a bootc qcow2 disk

## Image Hierarchy

```
golden -> base -> v2, v3, v4, v5, v6, v7, v8, v9, v10
sleep-app:v1, sleep-app:v2, sleep-app:v3 (standalone)
```

The images are built using the `Containerfile-*` files in this directory, functionality
or service deployment changes should be implemented on those container files, or if
additional transition images are required we should create and document new Containerfiles.

| Name   | Image                         | Bootc Containers                 |
|------  |-------------------------------|----------------------------------|
| golden | `bin/output/qcow2/disk.qcow2` | flightctl-device:golden          |
| base   | N/A                           | flightctl-device:base            |
| v2     | N/A                           | flightctl-device:v2              |
| v3     | N/A                           | flightctl-device:v3              |

## Credentials

All images are built with the following credentials:
- user: `user`
- password: `user`

## Image descriptions

### golden
Minimal bootc base image with cloud-init, greenboot, and podman-compose installed.
Used as the base for qcow2 generation.

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
