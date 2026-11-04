---
name: jobset-e2e-setup
description: Sets up a local Kind cluster to build, deploy, and run JobSet's end-to-end (e2e) test suites (test/e2e and test/e2e/customconfigs), including how to enable alpha Kubernetes feature gates (e.g. MutablePodResourcesForSuspendedJobs) cluster-wide and JobSet's own feature gates via its Configuration ConfigMap. Use this when asked to run/verify JobSet e2e tests manually, verify behavior against a specific Kubernetes version, or debug e2e failures on a real cluster instead of envtest.
---

# JobSet E2E Test Setup

Manual workflow for standing up a real Kind cluster to build, deploy, and run
JobSet's e2e suites (`test/e2e/...`), as an alternative/complement to
`make test-e2e-kind` (which builds a Kubernetes node image from source and is
slow). This uses official `kindest/node` images instead, which is much faster
and lets you pick an exact Kubernetes version and enable specific alpha
feature gates.

Use this when you need to:
- Verify behavior on a specific Kubernetes minor version (e.g. 1.36).
- Enable a Kubernetes alpha feature gate cluster-wide (kube-apiserver /
  kube-controller-manager / scheduler / kubelet) that JobSet's own feature
  depends on.
- Iterate quickly on e2e test failures without rebuilding a Kubernetes node
  image each time.

## Prerequisites

- `kind`, `docker`, `kubectl`, `kustomize`, `ginkgo` available on `PATH` (or
  under `./bin/` in the repo, built via `make kind ginkgo kustomize`).
- A `kindest/node:vX.Y.Z` image, either already pulled/cached locally or
  pullable from the network. Check what's cached first:
  ```bash
  docker images | grep kindest/node
  ```
  If the exact patch version isn't available, pick the closest cached
  `vX.Y.*` tag for that minor version rather than failing — patch differences
  essentially never matter for e2e behavior.

## 1. Create the Kind cluster

If you need to enable Kubernetes feature gates cluster-wide (e.g. an alpha
gate your JobSet change depends on), write a Kind config with a top-level
`featureGates` map — this propagates to kube-apiserver, kube-controller-manager,
scheduler, and kubelet via kubeadm:

```yaml
# /tmp/kind-featuregates.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
featureGates:
  "SomeAlphaFeature": true
nodes:
- role: control-plane
```

```bash
kind create cluster --name jobset-verify \
  --image kindest/node:v1.36.1 \
  --config /tmp/kind-featuregates.yaml \
  --wait 2m
```

Verify the gate actually landed on kube-apiserver:

```bash
kubectl --context kind-jobset-verify -n kube-system \
  get pod -l component=kube-apiserver \
  -o jsonpath='{.items[0].spec.containers[0].command}' | tr ',' '\n' | grep -i feature
```

If you don't need any cluster-wide Kubernetes feature gates, omit
`--config` and just pass `--image`.

## 2. Build and load the JobSet controller image

Do **not** use `make image-build` directly — it defaults to a multi-platform
buildx build which fails with the local `docker` driver. Use the
`kind-image-build` target instead (single platform + `--load`):

```bash
cd /path/to/jobset
IMAGE_TAG=jobset/verify:local make kind-image-build
kind load docker-image jobset/verify:local --name jobset-verify
```

## 3. Point the manager Deployment at the built image

Use the **global** `kustomize` binary (`which kustomize`), not
`./bin/kustomize`, unless you've built it via `make kustomize` first — a
missing binary here silently breaks a `&&`-chained `cd` and leaves your shell
in the wrong directory for subsequent commands. Run this as its own isolated
command:

```bash
(cd config/components/manager && kustomize edit set image controller=jobset/verify:local)
```

This edits the **tracked** `config/components/manager/kustomization.yaml`
file. **Remember to revert it when done** (see cleanup section) so it doesn't
show up as an unintended diff:

```bash
git checkout -- config/components/manager/kustomization.yaml
```

## 4. Deploy JobSet

```bash
kubectl config use-context kind-jobset-verify
kubectl apply --server-side -k test/e2e/config/default
kubectl -n jobset-system rollout status deployment/jobset-controller-manager --timeout=120s
```

(`test/e2e/config/default` is a Kustomize overlay over `config/default` used
specifically for e2e; see `test/e2e/config/default/kustomization.yaml`.)

## 5. Run the e2e tests

Set the same env vars `hack/e2e-test.sh` uses:

```bash
export NAMESPACE=jobset-system
export JOBSET_E2E_TESTS_DUMP_NAMESPACE=true   # dumps JobSet/Job YAML on failure

# Core e2e suite:
ginkgo -v ./test/e2e/

# Suite that toggles JobSet feature gates at runtime (see below):
ginkgo -v ./test/e2e/customconfigs/...

# Focus on one test while iterating:
ginkgo -v --focus "SomeTestName" ./test/e2e/customconfigs/...
```

`ginkgo` picks up the current kubeconfig context automatically via
`sigs.k8s.io/controller-runtime/pkg/client/config`.

### Enabling a JobSet feature gate for a test

JobSet's own feature gates (`pkg/features/features.go`) are enabled at
runtime via the `jobset-manager-config` ConfigMap, not via image rebuilds.
The `test/e2e/customconfigs` package exists specifically for tests that need
a non-default JobSet feature gate: it uses
`util.UpdateJobSetConfigurationAndRestart` to patch the ConfigMap and roll
the controller-manager Deployment, then restores the default config in
`AfterSuite`. Add new feature-gate-dependent e2e tests there rather than in
`test/e2e/e2e_test.go`. Pattern (see `test/e2e/customconfigs/restartjob_test.go`
or `inplacerestart_test.go` for full examples):

```go
var _ = ginkgo.Describe("MyFeature", ginkgo.Ordered, func() {
    ginkgo.BeforeAll(func() {
        util.UpdateJobSetConfigurationAndRestart(ctx, k8sClient, defaultCfg, func(cfg *configv1alpha1.Configuration) {
            cfg.FeatureGates = map[string]bool{string(features.MyFeature): true}
        })
    })
    // ... ginkgo.BeforeEach/AfterEach for namespace setup+dump, mirroring
    // the other files in this package ...
})
```

## Cleanup

Always clean up after verification — don't leave dangling clusters or dirty
tracked files:

```bash
kind delete cluster --name jobset-verify
docker rmi jobset/verify:local
git checkout -- config/components/manager/kustomization.yaml
```

**Do not** run broad `kubectl delete ns` selectors (e.g. by label or
`--field-selector metadata.name!=default`) to clean up test namespaces —
it's easy to accidentally match and delete core namespaces
(`kube-system`, `kube-node-lease`, `local-path-storage`, `jobset-system`)
and break the cluster. If you need to bulk-delete leftover e2e test
namespaces, target them by generated-name prefix explicitly, e.g.:

```bash
kubectl get ns -o name | grep '^namespace/e2e-' | while read -r ns; do kubectl delete "$ns"; done
```

When in doubt, just delete and recreate the whole Kind cluster — it's fast.
