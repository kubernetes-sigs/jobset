#!/usr/bin/env bash
# E2E test script for Workload-Aware Scheduling integration.
#
# Creates a Kind cluster with WAS feature gates enabled, deploys JobSet with
# WorkloadAwareScheduling, runs the scheduling-specific Ginkgo e2e tests, and
# cleans up on exit. Intended for CI.
#
# For interactive dev use, prefer the individual make targets:
#   make kind-cluster-scheduling          # create cluster + deploy
#   make kind-cluster-scheduling-delete   # tear down

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source shared cluster lifecycle functions.
# shellcheck source=hack/e2e-scheduling-cluster.sh
source "$SCRIPT_DIR/e2e-scheduling-cluster.sh"

export GINKGO="${GINKGO:-$PWD/bin/ginkgo}"
export JOBSET_E2E_TESTS_DUMP_NAMESPACE=true
export E2E_TEST_PATH="${E2E_TEST_PATH:-./test/e2e/scheduling/...}"

KUBECONFIG=""

function cleanup {
    if [ "$USE_EXISTING_CLUSTER" == 'false' ]; then
        collect_scheduling_logs
        $KIND delete cluster --name "$KIND_CLUSTER_NAME" || true
        [ -n "$KUBECONFIG" ] && rm -f "$KUBECONFIG"
    fi
    (cd config/components/manager && $KUSTOMIZE edit set image controller=us-central1-docker.pkg.dev/k8s-staging-images/jobset/jobset:main)
}

function startup {
    if [ "$USE_EXISTING_CLUSTER" == 'false' ]; then
        KUBECONFIG="$(mktemp)"
        export KUBECONFIG
        build_scheduling_node_image
        create_scheduling_cluster
    fi
}

trap cleanup EXIT
startup
kind_load_image
deploy_scheduling_jobset
$GINKGO --junit-report=junit.xml --output-dir="$ARTIFACTS" -v "$E2E_TEST_PATH"
