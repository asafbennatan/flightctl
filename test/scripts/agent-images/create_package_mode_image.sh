#!/usr/bin/env bash
set -euo pipefail

# Build the package-mode agent OCI image (cs9-regular Containerfile) and bundle it.
# Not an AGENT_OS_ID flavor — invoked from e2e-agent-images for CS9 builds.

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"
ROOT_DIR="$(cd "$SCRIPT_DIR/../../.." && pwd)"
BASE_DIR="${SCRIPT_DIR}"
CONTAINERFILE="${BASE_DIR}/containerfiles/cs9-regular/Containerfile"

source "${SCRIPT_DIR}/../functions"

SOURCE_GIT_TAG="${SOURCE_GIT_TAG:-$(${ROOT_DIR}/hack/current-version)}"
SOURCE_GIT_TREE_STATE="${SOURCE_GIT_TREE_STATE:-$(cd "${ROOT_DIR}" && { [ -z "$(git status --porcelain 2>/dev/null)" ] && echo clean || echo dirty; })}"
SOURCE_GIT_COMMIT="${SOURCE_GIT_COMMIT:-$(cd "${ROOT_DIR}" && git rev-parse --short "HEAD^{commit}" 2>/dev/null || echo "unknown")}"
TAG="${TAG:-$SOURCE_GIT_TAG}"
IMAGE_REPO="${IMAGE_REPO:-quay.io/flightctl/flightctl-device}"
ARTIFACTS_OUTPUT_DIR="${ARTIFACTS_OUTPUT_DIR:-${ROOT_DIR}/bin/agent-artifacts}"
BUNDLE_TAR="${ARTIFACTS_OUTPUT_DIR}/agent-images-bundle-cs9-regular.tar"

PKG_IMG_CANONICAL="${IMAGE_REPO}:base-cs9-regular-${TAG}"
PKG_IMG_OS="${IMAGE_REPO}:base-cs9-regular"

if [ ! -f "${CONTAINERFILE}" ]; then
  echo "[ERROR] Containerfile not found: ${CONTAINERFILE}" >&2
  exit 1
fi

# RPM source selection (same inputs as create_agent_images.sh).
BUILD_ARGS=""
RPM_DIR="${RPM_DIR:-rpm}"

validate_rpm_dir() {
  local rpm_dir="$1"
  if [[ ! "${rpm_dir}" =~ ^[A-Za-z0-9._+-]+$ ]] || [[ "${rpm_dir}" == *"/"* ]] || [[ "${rpm_dir}" == *".."* ]]; then
    echo "Invalid RPM_DIR: ${rpm_dir}" >&2
    exit 1
  fi
}

validate_copr_repo() {
  local copr_repo="$1"
  if [[ ! "${copr_repo}" =~ ^@?[A-Za-z0-9._+-]+/[A-Za-z0-9._+-]+$ ]]; then
    echo "Invalid RPM_COPR_REPO: ${copr_repo}" >&2
    exit 1
  fi
}

validate_copr_package() {
  local copr_package="$1"
  if [[ ! "${copr_package}" =~ ^[A-Za-z0-9._:+-]+$ ]]; then
    echo "Invalid RPM_COPR_PACKAGE: ${copr_package}" >&2
    exit 1
  fi
}

if [ -n "${BREW_BUILD_URL:-}" ]; then
  if ! download_brew_rpms "${ROOT_DIR}/bin/brew-rpm" "${BREW_BUILD_URL}" "flightctl-agent-*" "flightctl-selinux*"; then
    exit 1
  fi
  RPM_DIR="brew-rpm"
  validate_rpm_dir "${RPM_DIR}"
  BUILD_ARGS="--build-arg RPM_DIR=brew-rpm"
elif [ -n "${FLIGHTCTL_RPM:-}" ]; then
  RPM_COPR_REPO=$(copr_repo)
  RPM_COPR_PACKAGE=$(package_agent)
  if [ "${RPM_COPR_PACKAGE}" != "flightctl-agent" ]; then
    RPM_COPR_PACKAGE="${RPM_COPR_PACKAGE}.el9"
  fi
  validate_copr_repo "${RPM_COPR_REPO}"
  validate_copr_package "${RPM_COPR_PACKAGE}"
  BUILD_ARGS="--build-arg RPM_COPR_REPO=${RPM_COPR_REPO} --build-arg RPM_COPR_PACKAGE=${RPM_COPR_PACKAGE}"
else
  validate_rpm_dir "${RPM_DIR}"
  BUILD_ARGS="--build-arg RPM_DIR=${RPM_DIR}"
fi

if [ -n "${PODMAN_BUILD_EXTRA_FLAGS:-}" ]; then
  PODMAN_BUILD_EXTRA_FLAGS="${PODMAN_BUILD_EXTRA_FLAGS} ${BUILD_ARGS}"
else
  PODMAN_BUILD_EXTRA_FLAGS="${BUILD_ARGS}"
fi

cd "${ROOT_DIR}"
mkdir -p "${ARTIFACTS_OUTPUT_DIR}"

echo -e "\033[32mBuilding package-mode agent image ${PKG_IMG_CANONICAL}\033[m"
# Tag only cs9-regular refs — do not overwrite bootc :base / :base-${TAG} tags.
sudo -E podman build \
  --pull=missing --layers=true --network=host \
  ${PODMAN_BUILD_EXTRA_FLAGS} \
  --build-context "project-bin=${ROOT_DIR}/bin" \
  --build-context "variant-context=${BASE_DIR}/base" \
  --build-arg SOURCE_GIT_TAG="${SOURCE_GIT_TAG}" \
  --build-arg SOURCE_GIT_TREE_STATE="${SOURCE_GIT_TREE_STATE}" \
  --build-arg SOURCE_GIT_COMMIT="${SOURCE_GIT_COMMIT}" \
  --label "io.flightctl.e2e.component=device" \
  -f "${CONTAINERFILE}" \
  -t "${PKG_IMG_CANONICAL}" \
  -t "${PKG_IMG_OS}" \
  "${BASE_DIR}"

echo -e "\033[32mBundling package-mode agent image\033[m"
sudo -E "${SCRIPT_DIR}/scripts/bundle.sh" \
  --filter "label=io.flightctl.e2e.component=device" \
  --filter "reference=${IMAGE_REPO}:base-cs9-regular*" \
  --output-path "${BUNDLE_TAR}"

sudo chown -R "${USER}:$(id -gn "${USER}")" "${ARTIFACTS_OUTPUT_DIR}" || true
echo -e "\033[32mPackage-mode agent bundle: ${BUNDLE_TAR}\033[m"
