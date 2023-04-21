export KUSTOMIZE=$PWD/bin/kustomize
export GINKGO=$PWD/bin/ginkgo
export KIND=$PWD/bin/kind
function cleanup {
    if [ $USE_EXISTING_CLUSTER == 'false' ] 
    then 
        $KIND delete cluster --name $KIND_CLUSTER_NAME
    fi
    (cd config/manager && $KUSTOMIZE edit set image controller=gcr.io/k8s-staging-jobset/jobset:main)
}
function startup {
    if [ $USE_EXISTING_CLUSTER == 'false' ] 
    then 
        $KIND create cluster --name $KIND_CLUSTER_NAME --image $E2E_KIND_VERSION
    fi
}
function kind_load {
    $KIND load docker-image $IMAGE_TAG --name $KIND_CLUSTER_NAME
}
function jobset_deploy {
    echo "cd config/manager && $KUSTOMIZE edit set image controller=$IMAGE_TAG"
    (cd config/manager && $KUSTOMIZE edit set image controller=$IMAGE_TAG)
    echo "$KUSTOMIZE build test/e2e/config | kubectl apply --server-side -f -" 
    $KUSTOMIZE build test/e2e/config | kubectl apply --server-side -f -
}
trap cleanup EXIT
startup
kind_load
jobset_deploy
$GINKGO -v ./test/e2e/...
