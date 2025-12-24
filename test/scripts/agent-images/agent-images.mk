# Agent images build system
#
# Image hierarchy:
#   golden -> base -> v2, v3, v4, v5, v6, v7, v8, v9, v10
#   sleep-app:v1, sleep-app:v2, sleep-app:v3 (standalone)
#
# Scripts:
#   build_image.sh      - Build container image, optionally push to registry
#   convert_to_qcow2.sh - Convert container image to bootc qcow2

AGENT_IMAGES_DIR := test/scripts/agent-images
BUILD_IMAGE := $(AGENT_IMAGES_DIR)/build_image.sh
CONVERT_QCOW2 := $(AGENT_IMAGES_DIR)/convert_to_qcow2.sh

# Get registry address at runtime (empty if registry not available)
REGISTRY_ADDRESS = $$(source test/scripts/functions && registry_address 2>/dev/null || echo "")

.PHONY: e2e-agent-images golden-qcow2-image clean-e2e-agent-images
.PHONY: flightctl-device-golden flightctl-device-base
.PHONY: flightctl-device-v2 flightctl-device-v3 flightctl-device-v4 flightctl-device-v5
.PHONY: flightctl-device-v6 flightctl-device-v7 flightctl-device-v8 flightctl-device-v9 flightctl-device-v10
.PHONY: sleep-app-v1 sleep-app-v2 sleep-app-v3

# --- flightctl-device images ---

flightctl-device-golden:
	@$(BUILD_IMAGE) $(AGENT_IMAGES_DIR)/Containerfile-e2e-golden.local flightctl-device:golden $(REGISTRY_ADDRESS)

flightctl-device-base: flightctl-device-golden
	@$(AGENT_IMAGES_DIR)/prepare_agent_config.sh
	@BUILD_ARGS="--build-arg=REGISTRY_ADDRESS=$(REGISTRY_ADDRESS)" \
		$(BUILD_IMAGE) $(AGENT_IMAGES_DIR)/Containerfile-e2e-base.local flightctl-device:base $(REGISTRY_ADDRESS)

flightctl-device-v2: flightctl-device-base
	@$(BUILD_IMAGE) $(AGENT_IMAGES_DIR)/Containerfile-e2e-v2.local flightctl-device:v2 $(REGISTRY_ADDRESS)

flightctl-device-v3: flightctl-device-base
	@$(BUILD_IMAGE) $(AGENT_IMAGES_DIR)/Containerfile-e2e-v3.local flightctl-device:v3 $(REGISTRY_ADDRESS)

flightctl-device-v4: flightctl-device-base
	@$(BUILD_IMAGE) $(AGENT_IMAGES_DIR)/Containerfile-e2e-v4.local flightctl-device:v4 $(REGISTRY_ADDRESS)

flightctl-device-v5: flightctl-device-base
	@$(BUILD_IMAGE) $(AGENT_IMAGES_DIR)/Containerfile-e2e-v5.local flightctl-device:v5 $(REGISTRY_ADDRESS)

flightctl-device-v6: flightctl-device-base
	@$(BUILD_IMAGE) $(AGENT_IMAGES_DIR)/Containerfile-e2e-v6.local flightctl-device:v6 $(REGISTRY_ADDRESS)

flightctl-device-v7: flightctl-device-base
	@$(BUILD_IMAGE) $(AGENT_IMAGES_DIR)/Containerfile-e2e-v7.local flightctl-device:v7 $(REGISTRY_ADDRESS)

flightctl-device-v8: flightctl-device-base
	@$(BUILD_IMAGE) $(AGENT_IMAGES_DIR)/Containerfile-e2e-v8.local flightctl-device:v8 $(REGISTRY_ADDRESS)

flightctl-device-v9: flightctl-device-base
	@$(BUILD_IMAGE) $(AGENT_IMAGES_DIR)/Containerfile-e2e-v9.local flightctl-device:v9 $(REGISTRY_ADDRESS)

flightctl-device-v10: flightctl-device-base
	@$(BUILD_IMAGE) $(AGENT_IMAGES_DIR)/Containerfile-e2e-v10.local flightctl-device:v10 $(REGISTRY_ADDRESS)

# --- sleep-app images ---

sleep-app-v1:
	@$(BUILD_IMAGE) $(AGENT_IMAGES_DIR)/Containerfile-sleep-app-v1 sleep-app:v1 $(REGISTRY_ADDRESS)

sleep-app-v2:
	@$(BUILD_IMAGE) $(AGENT_IMAGES_DIR)/Containerfile-sleep-app-v2 sleep-app:v2 $(REGISTRY_ADDRESS)

sleep-app-v3:
	@$(BUILD_IMAGE) $(AGENT_IMAGES_DIR)/Containerfile-sleep-app-v3 sleep-app:v3 $(REGISTRY_ADDRESS)

# --- Aggregate targets ---

e2e-agent-images: deploy-e2e-extras rpm bin/flightctl-agent bin/e2e-certs flightctl-device-golden flightctl-device-base flightctl-device-v2 flightctl-device-v3 flightctl-device-v4 flightctl-device-v5 \
                  flightctl-device-v6 flightctl-device-v8 flightctl-device-v9 flightctl-device-v10 \
                  sleep-app-v1 sleep-app-v2 sleep-app-v3
	@echo "All e2e agent images are ready"

# Golden qcow2 disk image
# Make skips this if disk.qcow2 already exists (e.g. downloaded from cache)
bin/output/qcow2/disk.qcow2:
	$(MAKE) flightctl-device-golden
	@echo "Transferring localhost/flightctl-device:golden to root storage..."
	podman save localhost/flightctl-device:golden | sudo podman load
	sudo $(CONVERT_QCOW2) localhost/flightctl-device:golden bin/output/qcow2/disk.qcow2

golden-qcow2-image: bin/output/qcow2/disk.qcow2
	@echo "Golden qcow2 image is ready at bin/output/qcow2/disk.qcow2"

clean-e2e-agent-images:
	sudo rm -f bin/output/qcow2/disk.qcow2
	rm -rf bin/dnf-cache
	rm -rf bin/osbuild-cache
	rm -rf bin/rpm
	rm -rf bin/brew-rpm
	podman rmi -f flightctl-device:golden flightctl-device:base || true
	podman rmi -f flightctl-device:v2 flightctl-device:v3 flightctl-device:v4 flightctl-device:v5 || true
	podman rmi -f flightctl-device:v6 flightctl-device:v7 flightctl-device:v8 flightctl-device:v9 flightctl-device:v10 || true
	podman rmi -f sleep-app:v1 sleep-app:v2 sleep-app:v3 || true
