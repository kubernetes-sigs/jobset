#!/usr/bin/env bash
# Test the scheduling node image defaults, overrides, and cache without Docker.
# Run with: bash hack/e2e-scheduling-cluster_test.sh

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Each case runs in a subshell so exported defaults do not leak between cases.
while IFS='|' read -r name version image test_arch cached expected_version expected_image; do
    (
        unset WAS_K8S_VERSION WAS_NODE_IMAGE
        if [[ -n "$version" ]]; then export WAS_K8S_VERSION="$version"; fi
        if [[ -n "$image" ]]; then export WAS_NODE_IMAGE="$image"; fi
        export KIND=/bin/true KUSTOMIZE=/bin/true KIND_CLUSTER_NAME=was-unit-test
        # shellcheck source=hack/e2e-scheduling-cluster.sh
        source "$SCRIPT_DIR/e2e-scheduling-cluster.sh"

        [[ "$WAS_K8S_VERSION" == "$expected_version" ]]
        [[ "$WAS_NODE_IMAGE" == "$expected_image" ]]

        function docker {
            [[ "$*" == "image inspect $expected_image" ]] || exit 1
            [[ "$cached" == true ]]
        }
        function go {
            [[ "$*" == 'env GOARCH' ]] || exit 1
            echo "$test_arch"
        }
        # Any attempt to fetch a moving CI version must fail the test.
        # shellcheck disable=SC2329 # Invoked indirectly by the sourced helper.
        function curl { exit 1; }

        kind_args=()
        # shellcheck disable=SC2329 # Invoked through KIND.
        function mock_kind {
            kind_args=("$@")
        }
        KIND=mock_kind
        build_scheduling_node_image
        if [[ "$cached" == true ]]; then
            [[ ${#kind_args[@]} == 0 ]]
        else
            [[ "${kind_args[*]}" == "build node-image --image=$expected_image https://dl.k8s.io/$expected_version/kubernetes-server-linux-$test_arch.tar.gz" ]]
        fi

        # Cluster creation must use the same image that was built or cached.
        # shellcheck disable=SC2329 # Invoked through KIND.
        function mock_kind {
            if [[ "$*" != 'get clusters' ]]; then
                kind_args=("$@")
            fi
        }
        create_scheduling_cluster
        [[ "${kind_args[*]}" == "create cluster --name was-unit-test --image $expected_image --config hack/kind-config-scheduling.yaml --wait 2m" ]]
        echo "PASS: $name"
    )
done <<'CASES'
default release|||amd64|false|v1.37.0|jobset/kind-node:v1.37.0
cached release|||amd64|true|v1.37.0|jobset/kind-node:v1.37.0
release override|v1.37.1||amd64|false|v1.37.1|jobset/kind-node:v1.37.1
image override||custom/was:release|amd64|false|v1.37.0|custom/was:release
arm64 release|||arm64|false|v1.37.0|jobset/kind-node:v1.37.0
CASES
