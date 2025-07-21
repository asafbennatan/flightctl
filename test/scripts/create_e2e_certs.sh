#!/usr/bin/env bash

# This script is used to generate SSH keys for E2E testing
# Certificates are now managed by cert-manager

set -x -euo pipefail
SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"

source "${SCRIPT_DIR}"/functions

# ensure pub/private key for SSH access to agents
mkdir -p bin/.ssh/

# if bin/.ssh/id_rsa exists we just exit
if [ ! -f bin/.ssh/id_rsa ]; then
  echo "bin/.ssh/id_rsa does not exist, creating ssh-keygen"
  ssh-keygen -t rsa -b 4096 -f bin/.ssh/id_rsa -N "" -C "e2e test key"
fi
