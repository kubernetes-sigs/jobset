---
title: "DRA Integration"
linkTitle: "DRA Integration"
weight: 4
date: 2026-07-15
description: >
    Share DRA devices across a gang-scheduled JobSet using PodGroup-level ResourceClaims
no_list: true
---

When the `DRAWorkloadResourceClaims` feature gate is enabled, you can associate [Dynamic Resource Allocation](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/) (DRA) ResourceClaims with a PodGroup. This creates a single ResourceClaim that is shared across all pods in the gang i.e. the claim is reserved for the PodGroup rather than for individual pods.

## Prerequisites

In addition to the [general WAS prerequisites](/docs/workload-aware-scheduling/), you must:

**Enable an additional feature gate.** Add `DRAWorkloadResourceClaims` to your cluster configuration:

{{< include file="/examples/workload-aware-scheduling/dra/kind.yaml" lang="yaml" >}}

```bash
kind create cluster --image=kindest/node:latest --config kind.yaml
```

## How It Works

Without DRA PodGroup integration, each pod creates its own ResourceClaim. With `DRAWorkloadResourceClaims` enabled, you can declare **`resourceClaims`** on both the Workload's `podGroupTemplates` and the PodGroup's `spec`. When a pod's `resourceClaims` entry matches a PodGroup's entry (same name and same template), the scheduler reserves the resulting ResourceClaim for the **PodGroup** instead of the individual pod.

This means:

- **One claim, many pods**: A single ResourceClaim is created from the template and shared across all pods in the gang.
- **PodGroup-level reservation**: The claim's `status.reservedFor` references the PodGroup, not individual pods — avoiding the 256-entry reservation limit.
- **Lifecycle management**: Claims created from templates are owned by the PodGroup and cleaned up when the PodGroup is deleted.

## Step 1: Create the ResourceClaimTemplate

The [ResourceClaimTemplate](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/workload-aware-scheduling/dra/resourceclaimtemplate.yaml) defines the device request. This example requests a single GPU from the `gpu.example.com` DeviceClass:

{{< include file="/examples/workload-aware-scheduling/dra/resourceclaimtemplate.yaml" lang="yaml" >}}

## Step 2: Create the Workload

The [Workload](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/workload-aware-scheduling/dra/workload.yaml) references the JobSet and defines a pod group template with gang scheduling and a ResourceClaim:

{{< include file="/examples/workload-aware-scheduling/dra/workload.yaml" lang="yaml" >}}

The key addition compared to basic gang scheduling is **`resourceClaims`** in the `podGroupTemplates` entry. This declares that pods in this group share a GPU claim created from `gpu-claim-template`.

## Step 3: Create the PodGroup

The [PodGroup](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/workload-aware-scheduling/dra/podgroup.yaml) references the Workload's pod group template and carries the same `resourceClaims`:

{{< include file="/examples/workload-aware-scheduling/dra/podgroup.yaml" lang="yaml" >}}

When the PodGroup is created, Kubernetes generates a ResourceClaim from the template. The claim name appears in `status.resourceClaimStatuses`.

## Step 4: Create the JobSet

The [JobSet](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/workload-aware-scheduling/dra/jobset.yaml) creates 6 pods (3 replicas × 2 completions). Each pod references the PodGroup through `schedulingGroup.podGroupName` and declares the same `resourceClaims` entry:

{{< include file="/examples/workload-aware-scheduling/dra/jobset.yaml" lang="yaml" >}}

Because the pod's `resourceClaims` entry matches the PodGroup's entry, this triggers PodGroup-level reservation as described above.

## Verify

Check that the PodGroup is scheduled and the ResourceClaim is allocated:

```bash
kubectl get podgroup training-workers
kubectl get resourceclaim
```

The ResourceClaim should show `allocated,reserved`. Verify that the reservation targets the PodGroup:

```bash
kubectl get resourceclaim -o jsonpath='{.items[0].status.reservedFor[0]}'
```

The output should reference `podgroups` rather than `pods`:

```json
{"apiGroup":"scheduling.k8s.io","name":"training-workers","resource":"podgroups","uid":"..."}
```

Check that all 6 pods are running:

```bash
kubectl get pods -l jobset.sigs.k8s.io/jobset-name=training-job
```
