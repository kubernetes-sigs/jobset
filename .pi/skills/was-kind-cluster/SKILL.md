---
name: was-kind-cluster
description: Creates a Kind cluster with Workload-Aware Scheduling (WAS) feature gates enabled, deploys JobSet with the WorkloadAwareScheduling feature gate turned on, and provides example YAMLs for verifying gang scheduling, topology-constrained workers, and basic scheduling E2E. Use when setting up a local dev/test environment for WAS integration.
---

# WAS Kind Cluster

Sets up a local Kind cluster with Kubernetes WAS feature gates enabled, deploys the JobSet controller with `WorkloadAwareScheduling=true`, and provides example JobSet manifests for manual E2E verification.

## Prerequisites

- Docker running
- `go` installed (for building JobSet and the Kind node image from K8s source)
- No existing Kind cluster named `was-test` (or set `WAS_KIND_CLUSTER_NAME` to override)
- Sufficient disk space and memory for `kind build node-image` (compiles Kubernetes from source)

## Quick Start

All operations are available as Makefile targets:

```bash
# Build image, create cluster, deploy JobSet with WAS enabled
make kind-cluster-scheduling


# Tear down the cluster (collects logs first)
make kind-cluster-scheduling-delete
```

To use a custom cluster name:

```bash
make kind-cluster-scheduling WAS_KIND_CLUSTER_NAME=my-was-cluster
make kind-cluster-scheduling-delete WAS_KIND_CLUSTER_NAME=my-was-cluster
```

### Legacy wrapper scripts

The `scripts/` directory contains thin wrappers around the make targets for
backward compatibility:

```bash
./scripts/setup.sh       # → make kind-cluster-scheduling
./scripts/teardown.sh    # → make kind-cluster-scheduling-delete
```

## Step-by-Step

If you prefer to run steps individually:

### 1. Build the Kind node image from Kubernetes source

The WAS (Workload-Aware Scheduling) APIs require Kubernetes 1.36+. The setup
uses `kind build node-image` to compile a node image from the K8s source tag
to ensure the `scheduling.k8s.io` API group is available.

### 2. Build the JobSet image for Kind

```bash
make kind-image-build
```

### 3. Create the Kind cluster with WAS feature gates and deploy

```bash
make kind-cluster-scheduling
```

### 4. Apply an example and verify

```bash
# Gang scheduling example
kubectl apply -f site/static/examples/scheduling/gang-scheduling.yaml
kubectl get workloads,podgroups,jobs -n default
kubectl describe workload gang-training -n default

# Topology-constrained example
kubectl apply -f site/static/examples/scheduling/topology-constrained.yaml
kubectl get workloads,podgroups,jobs -n default

# No-scheduling baseline (should produce zero scheduling objects)
kubectl apply -f site/static/examples/scheduling/no-scheduling-baseline.yaml
kubectl get workloads,podgroups -n default  # expect empty

```

### 5. Verification checklist

After applying each example, confirm:

| Check | Command |
|-------|---------|
| Workload exists (gang/topology only) | `kubectl get workload <name>` |
| PodGroups exist per ReplicatedJob | `kubectl get podgroups -l jobset.sigs.k8s.io/jobset-name=<name>` |
| Child Jobs have scheduling annotations | `kubectl get jobs -o jsonpath='{range .items[*]}{.metadata.name}: {.metadata.annotations}{"\n"}{end}'` |
| OwnerReferences point to JobSet | `kubectl get workload <name> -o jsonpath='{.metadata.ownerReferences}'` |
| No-scheduling baseline has zero scheduling objects | `kubectl get workloads,podgroups` |

### 6. Run the automated E2E scheduling tests

```bash
make test-e2e-kind-scheduling
```

## Architecture

The WAS cluster lifecycle is implemented in shared scripts under `hack/`:

| File | Purpose |
|------|---------|
| `hack/e2e-scheduling-cluster.sh` | Shared functions: create/delete cluster, deploy JobSet, verify APIs |
| `hack/e2e-scheduling-test.sh` | CI entrypoint: create → deploy → Ginkgo tests → cleanup |
| `hack/kind-config-scheduling.yaml` | Kind cluster config with WAS feature gates |
| `test/e2e/config/scheduling/` | Kustomize overlay that enables `WorkloadAwareScheduling` feature gate |

Makefile targets:

| Target | Description |
|--------|-------------|
| `kind-cluster-scheduling` | Build images, create cluster, deploy controller |
| `kind-cluster-scheduling-delete` | Collect logs and delete cluster |
| `test-e2e-kind-scheduling` | Full CI: create → deploy → Ginkgo e2e → cleanup |

## Examples

See the [site/static/examples/scheduling/](../../../site/static/examples/scheduling/) directory for ready-to-apply manifests:

- **[gang-scheduling.yaml](../../../site/static/examples/scheduling/gang-scheduling.yaml)** — All pods gang-scheduled atomically
- **[single-gang.yaml](../../../site/static/examples/scheduling/single-gang.yaml)** — Single ReplicatedJob gang scheduling
- **[topology-constrained.yaml](../../../site/static/examples/scheduling/topology-constrained.yaml)** — Workers rack-constrained with Basic driver
- **[no-scheduling-baseline.yaml](../../../site/static/examples/scheduling/no-scheduling-baseline.yaml)** — No `spec.scheduling`; verifies zero scheduling objects

## Troubleshooting

```bash
# Check controller logs for scheduling reconciliation
kubectl logs -n jobset-system deployment/jobset-controller-manager | grep -i scheduling

# Check feature gate is enabled
kubectl get configmap -n jobset-system jobset-manager-config -o yaml | grep -A2 featureGates

# Check WAS API types are registered
kubectl api-resources | grep scheduling.k8s.io
```
