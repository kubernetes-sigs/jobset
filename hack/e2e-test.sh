export KUSTOMIZE=$PWD/bin/kustomize
export GINKGO=$PWD/bin/ginkgo
export KIND=$PWD/bin/kind
# E2E tests for Kind can use jobset-system
# For testing against existing clusters, one needs to set this environment variable
export NAMESPACE="jobset-system"
export JOBSET_E2E_TESTS_DUMP_NAMESPACE=true

# Use a temporary KUBECONFIG so that the script does not mess with the current user's kubeconfig.
KUBECONFIG=""

function cleanup {
    if [ $USE_EXISTING_CLUSTER == 'false' ] 
    then
        if [ ! -d "$ARTIFACTS" ]; then
            mkdir -p "$ARTIFACTS"
        fi
        kubectl logs -n jobset-system deployment/jobset-controller-manager > $ARTIFACTS/jobset-controller-manager.log || true
        kubectl describe pods -n jobset-system > $ARTIFACTS/jobset-system-pods.log || true
        $KIND export logs $ARTIFACTS || true
        $KIND delete cluster --name $KIND_CLUSTER_NAME || { echo "You need to run make kind-image-build before this script"; exit -1; }
        rm "$KUBECONFIG"
    fi
    (cd config/components/manager && $KUSTOMIZE edit set image controller=us-central1-docker.pkg.dev/k8s-staging-images/jobset/jobset:main)
}
function startup {
    if [ $USE_EXISTING_CLUSTER == 'false' ] 
    then
        KUBECONFIG="$(mktemp)"
        if [ -z "$KUBECONFIG" ]; then
            echo "Failed to generate temporary KUBECONFIG" 1>&2
            exit 1
        fi
        export KUBECONFIG
        $KIND create cluster --name $KIND_CLUSTER_NAME --image $E2E_KIND_VERSION --wait 1m
        kubectl get nodes > $ARTIFACTS/kind-nodes.log || true
        kubectl describe pods -n kube-system > $ARTIFACTS/kube-system-pods.log || true
    fi
}
function kind_load {
    $KIND load docker-image $IMAGE_TAG --name $KIND_CLUSTER_NAME
}
function jobset_deploy {
    echo "cd config/components/manager && $KUSTOMIZE edit set image controller=$IMAGE_TAG"
    (cd config/components/manager && $KUSTOMIZE edit set image controller=$IMAGE_TAG)
    echo "kubectl apply --server-side -k test/e2e/config" 
    kubectl apply --server-side -k test/e2e/config
}
trap cleanup EXIT
startup
kind_load
jobset_deploy
$GINKGO --junit-report=junit.xml --output-dir=$ARTIFACTS -v ./test/e2e/...