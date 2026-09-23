---
title: "Preemption"
linkTitle: "Preemption"
weight: 2
date: 2026-05-31
description: >
    Workload-aware preemption for gang-scheduled JobSets
no_list: true
---

When the `WorkloadAwarePreemption` feature gate is enabled, the scheduler can preempt entire pod groups to make room for higher-priority gang-scheduled workloads.

## The Problem

Without workload-aware preemption, the scheduler preempts individual pods. This can cause problems for gang-scheduled workloads — preempting some pods from a running gang breaks the entire workload, and freeing only a subset of the required resources may not be enough to admit the pending gang.

## How It Works

With workload-aware preemption, the scheduler understands that pods belong to a group and makes preemption decisions at the group level. This means:

- **All-or-nothing preemption**: The scheduler will only preempt a pod group if doing so frees enough resources to admit the pending gang in full.
- **No partial disruption**: Rather than evicting individual pods and breaking a running workload, the scheduler preempts the entire lower-priority pod group.

Two key fields on the PodGroup enable this:

- **`priorityClassName`**: Associates the PodGroup with a Kubernetes `PriorityClass`, giving the scheduler a priority value to compare when deciding what to preempt.
- **`disruptionMode: {all: {}}`**: Ensures the entire gang is preempted together rather than individual pods.

## Example: Gang Preemption

This example demonstrates PodGroup-level gang preemption using two JobSets — a low-priority JobSet that occupies the cluster, and a high-priority JobSet that preempts it.

Each JobSet creates 4 pods (2 replicas × 2 completions). The resource requests are sized so that only one JobSet can fit on the cluster at a time.

### Step 1: Create the PriorityClasses

Create a [low-priority](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/workload-aware-scheduling/preemption/low-priority.yaml) and [high-priority](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/workload-aware-scheduling/preemption/high-priority.yaml) PriorityClass:

{{< include file="/examples/workload-aware-scheduling/preemption/low-priority.yaml" lang="yaml" >}}

{{< include file="/examples/workload-aware-scheduling/preemption/high-priority.yaml" lang="yaml" >}}

### Step 2: Apply the low-priority JobSet

The [low-priority JobSet](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/workload-aware-scheduling/preemption/low-priority-jobset.yaml) includes its Workload and PodGroup. The PodGroup sets `priorityClassName: low-priority` and `disruptionMode: {all: {}}`:

{{< include file="/examples/workload-aware-scheduling/preemption/low-priority-jobset.yaml" lang="yaml" >}}

Wait for all 4 pods to be running:

```bash
kubectl get pods -l jobset.sigs.k8s.io/jobset-name=lp-js
```

### Step 3: Apply the high-priority JobSet

The [high-priority JobSet](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/workload-aware-scheduling/preemption/high-priority-jobset.yaml) uses the same structure but with `priorityClassName: high-priority`:

{{< include file="/examples/workload-aware-scheduling/preemption/high-priority-jobset.yaml" lang="yaml" >}}

### Step 4: Observe preemption

The scheduler preempts the entire low-priority PodGroup as a gang to make room for the high-priority JobSet:

```bash
kubectl get pods -l 'jobset.sigs.k8s.io/jobset-name in (lp-js,hp-js)'
kubectl get events --field-selector reason=Preempted
```

All 4 low-priority pods are preempted together because of `disruptionMode: {all: {}}`, and all 4 high-priority pods schedule and run.
