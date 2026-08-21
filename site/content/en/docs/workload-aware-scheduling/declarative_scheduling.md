---
title: "Declarative Scheduling"
linkTitle: "Declarative Scheduling"
weight: 5
date: 2026-08-06
description: >
    Configure Workload-Aware Scheduling declaratively through the JobSet spec, with JobSet managing the Workload and PodGroup objects for you
no_list: true
---

The other pages in this section show how to hand-write `Workload` and `PodGroup` objects alongside a JobSet, and point pods at a PodGroup via `schedulingGroup.podGroupName`. JobSet also supports a **declarative** integration: you describe the scheduling behavior you want directly on the JobSet with `spec.scheduling`, and the JobSet controller creates, updates, and deletes the matching `Workload`/`PodGroup` objects for you.

This page walks through that `spec.scheduling` field using the example manifests in [`site/static/examples/scheduling`](https://github.com/kubernetes-sigs/jobset/tree/main/site/static/examples/scheduling), one use case at a time.

## Prerequisites

This feature is alpha and off by default, gated by two independent switches:

1. **The JobSet controller-manager feature gate** `JobSetWorkloadAwareSchedulingAPI`, enabled via the manager's `Configuration` (the `jobset-manager-config` ConfigMap consumed through `--config`):

   ```yaml
   apiVersion: config.jobset.x-k8s.io/v1alpha1
   kind: Configuration
   featureGates:
     JobSetWorkloadAwareSchedulingAPI: true
   ```

2. **The Kubernetes cluster's own WAS feature gates and runtime-config**, same as the [general Workload Aware Scheduling prerequisites](/docs/workload-aware-scheduling/), plus two additional gates needed by some of the use cases below:

   - `GenericWorkload`
   - `WorkloadWithJob`
   - `TopologyAwareWorkloadScheduling` — required for the topology-constrained examples
   - `DRAWorkloadResourceClaims` — required for the shared DRA claim example
   - API server `--runtime-config=scheduling.k8s.io/v1alpha3=true,scheduling.k8s.io/v1beta1=true`

   The `scheduling.k8s.io` API isn't in a released Kubernetes minor yet, so these examples require a cluster built from a recent Kubernetes CI build. See [`hack/kind-config-scheduling.yaml`](https://github.com/kubernetes-sigs/jobset/blob/main/hack/kind-config-scheduling.yaml) and [`hack/e2e-scheduling-cluster.sh`](https://github.com/kubernetes-sigs/jobset/blob/main/hack/e2e-scheduling-cluster.sh) for how CI builds such a cluster with Kind, or run `make kind-cluster-scheduling` to create one locally.

## How It Works

`spec.scheduling` is optional and immutable once a JobSet is created. If it is left unset, nothing changes: no `Workload` or `PodGroup` objects are created, and existing JobSets are unaffected. Setting it — even to an empty `{}` — tells the controller to compile exactly one `Workload`, owned by the JobSet, containing one or more `PodGroupTemplate`s, and to keep matching `PodGroup` objects in sync with it.

`spec.scheduling` supports two mutually exclusive models — a JobSet must use exactly one, since composite Gang-of-Gangs PodGroup hierarchies linking a parent PodGroup to leaf PodGroups aren't implemented in alpha:

- **`schedulingPolicy`**, **`schedulingConstraints`**, and **`disruptionMode`** at the top level of `spec.scheduling` configure a single composite PodGroup (or, under sequenced startup, one PodGroup per `ReplicatedJob`) covering the whole JobSet. Leave `replicatedJobs` unset when using this model.
- **`replicatedJobs`** lets you target one or more `ReplicatedJob`s by name with their own leaf-level policy, producing one `PodGroup` per policy entry. Every `ReplicatedJob` in the JobSet must be targeted by exactly one entry, since there's no top-level policy for an untargeted `ReplicatedJob` to fall back to. Leave the top-level `schedulingPolicy`, `schedulingConstraints`, and `disruptionMode` unset when using this model.
- **`job`** nested inside a `replicatedJobs` entry goes one level deeper still, giving each Job *replica* of a `ReplicatedJob` its own `PodGroup` ("gang-of-gangs").

Child Jobs are annotated with `scheduling.k8s.io/group-template-name` so you can trace which `PodGroupTemplate` each Job belongs to.

## Use Case: Whole-JobSet Gang Scheduling

The most common pattern: every pod across every `ReplicatedJob` must be schedulable before any of them are admitted. Set a top-level `gang` policy and no `replicatedJobs` — the controller creates a single `PodGroup` sized to every pod in the JobSet.

{{< include file="/examples/scheduling/gang-scheduling.yaml" lang="yaml" >}}

A single-`ReplicatedJob` variant of the same idea:

{{< include file="/examples/scheduling/single-gang.yaml" lang="yaml" >}}

```bash
kubectl get workloads,podgroups,jobs -n default
kubectl describe workload gang-training -n default
```

## Use Case: No Scheduling (Backward Compatibility)

Existing JobSets that don't set `spec.scheduling` must keep working exactly as before: no `Workload`, no `PodGroup`, no scheduling annotations on child Jobs.

{{< include file="/examples/scheduling/no-scheduling-baseline.yaml" lang="yaml" >}}

```bash
kubectl get workloads -n default   # should not list no-sched-baseline
kubectl get podgroups -n default   # should not list no-sched-baseline
```

## Use Case: Independent PodGroups per ReplicatedJob

Sometimes different `ReplicatedJob`s should be scheduled independently of one another rather than as one giant gang — for example, a driver that can start on its own while workers are gang-scheduled among themselves. Give each `ReplicatedJob` its own entry in `replicatedJobs`, and each gets its own `PodGroup`, sized from its own parallelism × replicas.

{{< include file="/examples/scheduling/per-rj-independent.yaml" lang="yaml" >}}

```bash
kubectl get podgroups -n default -o custom-columns=NAME:.metadata.name,MINCOUNT:.spec.schedulingPolicy.gang.minCount
```

## Use Case: One Shared PodGroup Across Multiple ReplicatedJobs

The opposite grouping: multiple `ReplicatedJob`s that must be admitted together as a single gang. List them all in one `targetReplicatedJobs` entry, and the controller creates one `PodGroup` whose `minCount` is the sum of both `ReplicatedJob`s' pod counts.

{{< include file="/examples/scheduling/per-rj-grouped.yaml" lang="yaml" >}}

```bash
kubectl get podgroup per-rj-grouped-launcher-worker -n default -o jsonpath='{.spec.schedulingPolicy.gang.minCount}{"\n"}'
```

## Use Case: Gang-of-Gangs — Independent PodGroup per Job Replica

`job` goes one level finer than `replicatedJobs`: instead of one `PodGroup` covering every replica of a `ReplicatedJob`, each Job replica gets its **own** `PodGroup`, sized to just that Job's own parallelism. This suits workloads where each replica is an independently-schedulable gang, such as per-replica launcher/worker sets.

{{< include file="/examples/scheduling/per-ij-job-independent.yaml" lang="yaml" >}}

```bash
kubectl get workloads,podgroups,jobs -n default
```

## Use Case: Topology-Aware Placement

Combine gang scheduling with `schedulingConstraints.topology` to require that all pods in a gang land in the same topology domain — for example, co-locating pods on the same rack for RDMA-sensitive training, or coordinating placement across TPU slices. `topology-constrained.yaml` below uses `replicatedJobs` to give the driver an independent `basic` policy while workers get a `gang` policy with the rack constraint — every `ReplicatedJob` is targeted by its own entry since no top-level `schedulingPolicy` is set.

{{< include file="/examples/scheduling/topology-constrained.yaml" lang="yaml" >}}

{{< include file="/examples/scheduling/tpu-topology-gang.yaml" lang="yaml" >}}

Both examples require the `TopologyAwareWorkloadScheduling` feature gate; without it the constraint is silently dropped on write and the controller will continuously try to reconcile the resulting drift.

```bash
kubectl get podgroup topo-training-workers -n default -o yaml
```

## Use Case: Sequenced Startup

When `ReplicatedJob`s use `dependsOn` (or an `InOrder` `StartupPolicy`) to create Jobs sequentially, not all pods exist at the same time — so a single JobSet-wide gang could never be satisfied. The controller detects sequenced startup and automatically falls back to one `PodGroup` per `ReplicatedJob`, even if you asked for a single top-level gang.

{{< include file="/examples/scheduling/sequenced-startup-gang.yaml" lang="yaml" >}}

```bash
kubectl get jobs -n default   # worker's Job appears only after leader is Ready
```

## Use Case: Suspend and Resume

Suspending a JobSet (`spec.suspend: true`) shouldn't hold a scheduling reservation for pods that don't exist. While suspended, the controller deletes the `Workload`/`PodGroup`; resuming recreates them under the same names.

{{< include file="/examples/scheduling/suspend-resume.yaml" lang="yaml" >}}

```bash
kubectl patch jobset gang-suspend -n default --type=merge -p '{"spec":{"suspend":true}}'
kubectl get workloads,podgroups -n default   # gone
kubectl patch jobset gang-suspend -n default --type=merge -p '{"spec":{"suspend":false}}'
kubectl get workloads,podgroups -n default   # recreated
```

## Use Case: Elastic Scaling

For an [ElasticJobSet](/docs/tasks/), scaling a `ReplicatedJob`'s parallelism/completions patches the corresponding `PodGroup`'s `Gang.minCount` in place rather than deleting and recreating the `Workload`.

{{< include file="/examples/scheduling/elastic-gang.yaml" lang="yaml" >}}

{{< include file="/examples/scheduling/elastic-gang-scale-patch.yaml" lang="yaml" >}}

The upstream `Gang.minCount` field is required, so JobSet's mutating webhook fills it in automatically at creation time from the computed pod count, and marks it as auto-derived (rather than user-chosen) via an internal annotation. The webhook only runs on `CREATE`, and `spec.scheduling` is immutable, so the value stored in `spec.scheduling` itself never changes after creation — but the controller checks that annotation on every reconcile and, when present, recomputes `minCount` from the live `ReplicatedJob`s' parallelism/completions instead of trusting the stale stored value, patching the `Workload`/`PodGroup` in place.

Because `spec.scheduling` is immutable, scale by patching only `spec.replicatedJobs` rather than re-applying a JobSet whose `spec.scheduling` differs even incidentally (e.g. a missing field) from what's already stored — that fails with `spec.scheduling: Invalid value: Value is immutable`, since the API server can't tell that the rest of `spec.scheduling` was meant to stay the same.

```bash
kubectl get podgroup gang-elastic -n default -o jsonpath='{.spec.schedulingPolicy.gang.minCount}{"\n"}'
# after applying elastic-gang-scale-patch.yaml, or an equivalent patch to
# spec.replicatedJobs, this reflects the new pod count (2 -> 4)
```

## Use Case: Shared DRA Resource Claims

`replicatedJobs[].resourceClaims` attaches a PodGroup-level DRA `ResourceClaimTemplate` reference so pods in the group share a single allocated claim (for example, an NVLink/IMEX channel) instead of each pod claiming its own device.

{{< include file="/examples/scheduling/dra-resource-claims.yaml" lang="yaml" >}}

This requires the `DRAWorkloadResourceClaims` feature gate and a real DRA driver/`DeviceClass` to fully allocate; without one, the PodGroup-to-claim wiring can still be verified directly even if the pods stay `Pending`.

```bash
kubectl get podgroup dra-shared-claim-worker -n default -o jsonpath='{.spec.resourceClaims}{"\n"}'
```

## Use Case: Preemption

`disruptionMode: {all: {}}` (set on several examples above, such as `gang-scheduling.yaml`) tells the scheduler to preempt the entire gang together rather than individual pods, so a higher-priority JobSet doesn't leave a lower-priority one half-evicted. Pair it with a `PriorityClass` on the pod template. See [Preemption](./preemption) for the concepts behind workload-aware preemption in general.

