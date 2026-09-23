#!/usr/bin/env bash
# Tears down the WAS Kind cluster.
#
# This is a thin wrapper around `make kind-cluster-scheduling-delete`.
#
# Usage:
#   ./scripts/teardown.sh
#   KIND_CLUSTER_NAME=my-cluster ./scripts/teardown.sh

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
cd "$REPO_ROOT"

# Forward environment overrides to the make target.
MAKE_ARGS=()
[ -n "${KIND_CLUSTER_NAME:-}" ] && MAKE_ARGS+=("WAS_KIND_CLUSTER_NAME=$KIND_CLUSTER_NAME")

make kind-cluster-scheduling-delete "${MAKE_ARGS[@]+"${MAKE_ARGS[@]}"}"
