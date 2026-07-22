#!/usr/bin/env bash
# Shared helper functions for creating, deploying to, and tearing down a Kind
# cluster with Kubernetes WAS (Workload-Aware Scheduling) feature gates enabled.
#
# Sourced by:
#   - hack/e2e-scheduling-test.sh   (CI: create → deploy → test → cleanup)
#   - Makefile targets               (dev: create, delete, verify independently)
#
# Required environment variables (set defaults via Makefile or caller):
#   KUSTOMIZE, KIND       — paths to tool binaries
#   KIND_CLUSTER_NAME     — Kind cluster name (default: was-test)
#   K8S_MAIN_NODE_IMAGE   — Kind node image name (default: k8s-main:latest)
#   IMAGE_TAG             — JobSet controller image tag
#   ARTIFACTS             — directory for logs and test artifacts
#   E2E_TARGET_FOLDER     — kustomize config folder (default: scheduling)

set -o errexit
set -o nounset
set -o pipefail

# Resolve tool paths to absolute to survive subshell cd operations.
# Assignments are split from `export` (SC2155) so a failing `realpath` (e.g.
# a nonexistent binary) trips `errexit` instead of silently exporting an
# empty value.
KUSTOMIZE="$(cd "$PWD" && realpath "${KUSTOMIZE:-$PWD/bin/kustomize}")"
export KUSTOMIZE
KIND="$(cd "$PWD" && realpath "${KIND:-$PWD/bin/kind}")"
export KIND
export KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-was-test}"
export K8S_MAIN_NODE_IMAGE="${K8S_MAIN_NODE_IMAGE:-k8s-main:latest}"
export E2E_TARGET_FOLDER="${E2E_TARGET_FOLDER:-scheduling}"
export NAMESPACE="${NAMESPACE:-jobset-system}"
export ARTIFACTS="${ARTIFACTS:-$PWD/artifacts}"

# build_scheduling_node_image builds a Kind node image from the latest
# Kubernetes CI build (main branch) to ensure the scheduling.k8s.io API
# group is present. Follows the same caching pattern as Kueue's
# build_kind_node_image function: uses a namespaced image tag and reuses
# an existing image when available.
function build_scheduling_node_image {
    echo "==> Fetching latest Kubernetes CI build version..."
    local k8s_ci_version
    k8s_ci_version="$(curl -sL https://dl.k8s.io/ci/latest.txt)"

    # Use a namespaced image tag to avoid overwriting stock kindest/node images.
    # Replace '+' with '-' since '+' is invalid in Docker image tags.
    local sanitized_version="${k8s_ci_version//+/-}"
    export K8S_MAIN_NODE_IMAGE="jobset/kind-node:${sanitized_version}"

    # Reuse an existing image if present.
    if docker image inspect "$K8S_MAIN_NODE_IMAGE" &>/dev/null; then
        echo "==> Reusing existing node image: $K8S_MAIN_NODE_IMAGE"
        return 0
    fi

    echo "==> Building Kind node image: $K8S_MAIN_NODE_IMAGE (K8s ${k8s_ci_version})"
    local arch
    arch="$(go env GOARCH)"
    $KIND build node-image --image="${K8S_MAIN_NODE_IMAGE}" \
        "https://dl.k8s.io/ci/${k8s_ci_version}/kubernetes-server-linux-${arch}.tar.gz"
}

# create_scheduling_cluster creates a Kind cluster with WAS feature gates.
# Fails if the cluster already exists (use delete_scheduling_cluster first).
function create_scheduling_cluster {
    if $KIND get clusters 2>/dev/null | grep -q "^${KIND_CLUSTER_NAME}$"; then
        echo "Cluster '$KIND_CLUSTER_NAME' already exists."
        echo "Delete it first: make kind-cluster-scheduling-delete KIND_CLUSTER_NAME=$KIND_CLUSTER_NAME"
        return 1
    fi

    echo "==> Creating Kind cluster '$KIND_CLUSTER_NAME' with WAS feature gates..."
    $KIND create cluster \
        --name "$KIND_CLUSTER_NAME" \
        --image "${K8S_MAIN_NODE_IMAGE}" \
        --config hack/kind-config-scheduling.yaml \
        --wait 2m
}

# kind_load_image loads the JobSet controller image into the Kind cluster.
# Retries a few times to handle transient containerd startup delays.
function kind_load_image {
    echo "==> Loading JobSet image into Kind..."
    local max_retries=5
    local retry_delay=5
    for i in $(seq 1 "$max_retries"); do
        if $KIND load docker-image "$IMAGE_TAG" --name "$KIND_CLUSTER_NAME"; then
            return 0
        fi
        if [ "$i" -lt "$max_retries" ]; then
            echo "    Retry $i/$max_retries: waiting ${retry_delay}s for containerd..."
            sleep "$retry_delay"
        fi
    done
    echo "ERROR: failed to load image after $max_retries attempts"
    return 1
}

# deploy_scheduling_jobset deploys the JobSet controller with
# WorkloadAwareScheduling=true via the scheduling kustomize overlay.
function deploy_scheduling_jobset {
    echo "==> Deploying JobSet controller with WorkloadAwareScheduling=true..."
    # Run the mutation in a subshell so this helper does not overwrite an EXIT
    # trap installed by the script that sourced it. Restore the actual image,
    # rather than assuming the repository's default has not changed.
    (
        local original_image
        original_image="$(awk '/^- name: controller$/{getline; name=$2; getline; tag=$2; print name ":" tag; exit}' config/components/manager/kustomization.yaml)"
        trap 'cd config/components/manager && "$KUSTOMIZE" edit set image controller="$original_image"' EXIT
        (cd config/components/manager && "$KUSTOMIZE" edit set image controller="$IMAGE_TAG")
        kubectl apply --server-side -k "test/e2e/config/$E2E_TARGET_FOLDER"
        echo "==> Waiting for JobSet controller to be ready..."
        kubectl rollout status deployment/jobset-controller-manager -n "$NAMESPACE" --timeout=120s
    )
}

# verify_scheduling_apis checks that the scheduling.k8s.io API types are
# registered in the cluster.
function verify_scheduling_apis {
    echo "==> Verifying scheduling.k8s.io API types..."
    # Every cluster exposes the built-in v1 priorityclasses.scheduling.k8s.io
    # resource, so checking for the API group alone isn't enough to confirm
    # the alpha Workload/PodGroup resources are enabled.
    if kubectl api-resources --api-group=scheduling.k8s.io 2>/dev/null | grep -Eq '(^|[[:space:]])(workloads|podgroups)([[:space:]]|$)'; then
        kubectl api-resources --api-group=scheduling.k8s.io | grep -E 'workloads|podgroups'
        echo ""
        echo "✅ Kind cluster '$KIND_CLUSTER_NAME' is ready with WAS feature gates enabled."
    else
        echo "⚠️  scheduling.k8s.io Workload/PodGroup API types not found."
        echo "   The cluster is running but WAS features may not work."
        return 1
    fi
}

# collect_scheduling_logs saves controller and pod logs before teardown.
function collect_scheduling_logs {
    mkdir -p "$ARTIFACTS"
    echo "==> Collecting logs..."
    kubectl logs -n "$NAMESPACE" deployment/jobset-controller-manager > "$ARTIFACTS/was-controller-manager.log" 2>&1 || true
    kubectl describe pods -n "$NAMESPACE" > "$ARTIFACTS/was-system-pods.log" 2>&1 || true
    $KIND export logs "$ARTIFACTS/was-kind-logs" --name "$KIND_CLUSTER_NAME" 2>/dev/null || true
}

# delete_scheduling_cluster collects logs and deletes the Kind cluster.
function delete_scheduling_cluster {
    collect_scheduling_logs
    echo "==> Deleting Kind cluster '$KIND_CLUSTER_NAME'..."
    $KIND delete cluster --name "$KIND_CLUSTER_NAME"
    echo "✅ Cluster '$KIND_CLUSTER_NAME' deleted. Logs saved to $ARTIFACTS/"
}
