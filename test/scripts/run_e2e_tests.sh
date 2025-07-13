#!/usr/bin/env bash
set -x -euo pipefail
SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"
source "${SCRIPT_DIR}"/functions

REPORTS=${1}
GO_E2E_DIRS=("${@:2}")
GINKGO_FOCUS=${GINKGO_FOCUS:-""}
#Filtering e2e tests by labels
GINKGO_LABEL_FILTER=${GINKGO_LABEL_FILTER:-""}
#Parallel execution - default to 1 but can be overridden
GINKGO_PROCS=${GINKGO_PROCS:-1}
#Output interceptor mode for parallel execution - 'dup' shows output from all nodes
GINKGO_OUTPUT_INTERCEPTOR_MODE=${GINKGO_OUTPUT_INTERCEPTOR_MODE:-"dup"}
FOCUS_FLAG=""


go install github.com/onsi/ginkgo/v2/ginkgo

GOBIN=$(go env GOBIN)

export API_ENDPOINT=https://$(get_endpoint_host flightctl-api-route)
export REGISTRY_ENDPOINT=$(registry_address)

CMD=("${GOBIN}/ginkgo" run)

if [[ -n "${GINKGO_FOCUS}" ]]; then
  CMD+=("--focus" "${GINKGO_FOCUS}")
fi

if [[ -n "${GINKGO_LABEL_FILTER}" ]]; then
  CMD+=("--label-filter" "${GINKGO_LABEL_FILTER}")
fi

CMD+=(--timeout 120m --race -vv --junit-report "${REPORTS}/junit_e2e_test.xml" --github-output --procs "${GINKGO_PROCS}" --show-node-events --trace --force-newlines --output-interceptor-mode "${GINKGO_OUTPUT_INTERCEPTOR_MODE}")

CMD+=("${GO_E2E_DIRS[@]}")

echo "Running e2e tests with ${GINKGO_PROCS} parallel processes..."
echo "Output interceptor mode: ${GINKGO_OUTPUT_INTERCEPTOR_MODE} (dup=show all output, swap=clean output)"

# Run the command
"${CMD[@]}"