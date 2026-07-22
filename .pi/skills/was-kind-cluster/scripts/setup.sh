#!/usr/bin/env bash
# Sets up a Kind cluster with WAS feature gates enabled and deploys JobSet
# with WorkloadAwareScheduling=true.
#
# This is a thin wrapper around `make kind-cluster-scheduling`.
#
# Usage:
#   ./scripts/setup.sh
#   KIND_CLUSTER_NAME=my-cluster ./scripts/setup.sh

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
cd "$REPO_ROOT"

# Forward environment overrides to the make target.
MAKE_ARGS=()
[ -n "${KIND_CLUSTER_NAME:-}" ] && MAKE_ARGS+=("WAS_KIND_CLUSTER_NAME=$KIND_CLUSTER_NAME")
[ -n "${E2E_KIND_VERSION:-}" ] && MAKE_ARGS+=("WAS_E2E_KIND_VERSION=$E2E_KIND_VERSION")

make kind-cluster-scheduling "${MAKE_ARGS[@]+"${MAKE_ARGS[@]}"}"

SKILL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
echo ""
echo "==> Next steps:"
echo "  kubectl apply -f $SKILL_DIR/examples/gang-scheduling.yaml"
echo "  kubectl get workloads,podgroups,jobs"
echo ""
echo "  To tear down: make kind-cluster-scheduling-delete"
