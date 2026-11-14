#!/usr/bin/env bash
# E2E test script for Workload-Aware Scheduling integration.
#
# Creates a Kind cluster with WAS feature gates enabled, deploys JobSet with
# JobSetWorkloadAwareSchedulingAPI, runs the scheduling-specific Ginkgo e2e tests, and
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

KUBECONFIG="${KUBECONFIG:-}"

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

# Specs labeled gang-of-gangs need the controller running with JobSetGangOfGangs
# on, which changes how every JobSet with spec.scheduling compiles. The rest
# cover the default (gate off) path. Both run in the same cluster: pass 1 against
# the default overlay, pass 2 after redeploying with the gate-on overlay.
GANG_OF_GANGS_LABEL="gang-of-gangs"
E2E_GANG_OF_GANGS_TARGET_FOLDER="${E2E_GANG_OF_GANGS_TARGET_FOLDER:-scheduling-gang-of-gangs}"

trap cleanup EXIT
startup
kind_load_image

echo "==> Pass 1: JobSetGangOfGangs off, specs not labeled ${GANG_OF_GANGS_LABEL}"
deploy_scheduling_jobset
pass1=0
$GINKGO --junit-report=junit.xml --output-dir="$ARTIFACTS" --label-filter="!${GANG_OF_GANGS_LABEL}" -v "$E2E_TEST_PATH" || pass1=$?

echo "==> Pass 2: JobSetGangOfGangs on, specs labeled ${GANG_OF_GANGS_LABEL}"
E2E_TARGET_FOLDER="$E2E_GANG_OF_GANGS_TARGET_FOLDER" deploy_scheduling_jobset
# Applying the overlay only updates the manager ConfigMap; restart the
# controller so it reads the new feature gates.
kubectl rollout restart deployment/jobset-controller-manager -n "$NAMESPACE"
kubectl rollout status deployment/jobset-controller-manager -n "$NAMESPACE" --timeout=120s
pass2=0
$GINKGO --junit-report=junit-gang-of-gangs.xml --output-dir="$ARTIFACTS" --label-filter="${GANG_OF_GANGS_LABEL}" -v "$E2E_TEST_PATH" || pass2=$?

if [ "$pass1" -ne 0 ] || [ "$pass2" -ne 0 ]; then
    echo "E2E failed: pass 1 (gate off) exit=${pass1}, pass 2 (gate on) exit=${pass2}"
    exit 1
fi
