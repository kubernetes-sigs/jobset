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
#   WAS_NODE_IMAGE        — Kind node image (default: kindest/node:v1.36.1)
#   IMAGE_TAG             — JobSet controller image tag
#   ARTIFACTS             — directory for logs and test artifacts
#   E2E_TARGET_FOLDER     — kustomize config folder (default: default)

set -o errexit
set -o nounset
set -o pipefail

# Resolve tool paths to absolute to survive subshell cd operations.
KUSTOMIZE="$(cd "$PWD" && realpath "${KUSTOMIZE:-$PWD/bin/kustomize}")"
export KUSTOMIZE
KIND="$(cd "$PWD" && realpath "${KIND:-$PWD/bin/kind}")"
export KIND
export KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-was-test}"
export WAS_NODE_IMAGE="${WAS_NODE_IMAGE:-kindest/node:v1.36.1}"
export E2E_TARGET_FOLDER="${E2E_TARGET_FOLDER:-default}"
export NAMESPACE="${NAMESPACE:-jobset-system}"
export ARTIFACTS="${ARTIFACTS:-$PWD/artifacts}"

# ensure_scheduling_node_image pulls the WAS node image if it is not
# already present locally.
function ensure_scheduling_node_image {
    if docker image inspect "$WAS_NODE_IMAGE" &>/dev/null; then
        echo "==> Reusing existing node image: $WAS_NODE_IMAGE"
        return 0
    fi

    echo "==> Pulling node image: $WAS_NODE_IMAGE"
    docker pull "$WAS_NODE_IMAGE"
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
        --image "${WAS_NODE_IMAGE}" \
        --config hack/kind-config-scheduling.yaml \
        --wait 2m
}

# label_scheduling_nodes applies topology labels to worker nodes so that
# topology-aware scheduling tests can verify co-location constraints.
function label_scheduling_nodes {
    echo "==> Labelling worker nodes with topology.kubernetes.io/rack..."
    local workers=()
    while IFS= read -r node; do
        workers+=("$node")
    done < <(kubectl get nodes --no-headers -l '!node-role.kubernetes.io/control-plane' -o custom-columns=NAME:.metadata.name)
    local half=$(( ${#workers[@]} / 2 ))
    for i in "${!workers[@]}"; do
        if [ "$i" -lt "$half" ]; then
            kubectl label node "${workers[$i]}" topology.kubernetes.io/rack=rack1 --overwrite
        else
            kubectl label node "${workers[$i]}" topology.kubernetes.io/rack=rack2 --overwrite
        fi
    done
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

# deploy_scheduling_jobset deploys the JobSet controller via the
# e2e kustomize overlay.
function deploy_scheduling_jobset {
    echo "==> Deploying JobSet controller..."
    (cd config/components/manager && $KUSTOMIZE edit set image controller="$IMAGE_TAG")

    local deploy_status=0
    kubectl apply --server-side -k "test/e2e/config/$E2E_TARGET_FOLDER" || deploy_status=$?
    if [ "$deploy_status" -eq 0 ]; then
        echo "==> Waiting for JobSet controller to be ready..."
        kubectl rollout status deployment/jobset-controller-manager -n "$NAMESPACE" --timeout=120s || deploy_status=$?
    fi

    # Reset kustomize image to default so the working tree stays clean,
    # regardless of whether the deployment succeeded.
    (cd config/components/manager && $KUSTOMIZE edit set image controller=us-central1-docker.pkg.dev/k8s-staging-images/jobset/jobset:main)

    return "$deploy_status"
}

# verify_scheduling_apis checks that the scheduling.k8s.io API types are
# registered in the cluster.
function verify_scheduling_apis {
    echo "==> Verifying scheduling.k8s.io API types..."
    if kubectl api-resources 2>/dev/null | grep -q 'scheduling.k8s.io'; then
        kubectl api-resources | grep 'scheduling.k8s.io'
        echo ""
        echo "✅ Kind cluster '$KIND_CLUSTER_NAME' is ready with WAS feature gates enabled."
    else
        echo "⚠️  scheduling.k8s.io API types not found."
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
