---
title: "Topology Aware Scheduling"
linkTitle: "Topology Aware Scheduling"
weight: 3
date: 2026-05-31
description: >
    Configure topology aware scheduling with JobSet using the Kubernetes WAS APIs
no_list: true
---

Topology Aware Scheduling (TAS) ensures that all pods in a gang are placed within the same network topology domain (e.g., the same rack, block, or data center zone). This is critical for distributed training workloads where inter-pod communication latency directly impacts performance.

## Prerequisites

In addition to the [general WAS prerequisites](/docs/workload-aware-scheduling/), you must:

1. **Enable the `TopologyAwareWorkloadScheduling` feature gate.** This feature gate is included in the [kind cluster configuration](/docs/workload-aware-scheduling/) and is required for topology-aware placement.

2. **Label your nodes with topology keys.** The scheduler uses node labels to determine topology domains. Apply labels that represent your topology hierarchy — for example, rack, block, or zone:

    ```bash
    kubectl label node <node-name> topology.example.com/rack=rack-1
    ```

    Every node that should participate in topology-aware placement must have the topology label defined in the PodGroup's `schedulingConstraints.topology` field.

## How It Works

The PodGroup's `schedulingConstraints.topology` field tells the scheduler which topology domain to consider when placing pods. The scheduler finds a single topology domain that can accommodate the entire gang and co-locates all pods within it.

For example, setting `topology.example.com/rack` as the topology key ensures all pods in the gang land on nodes within the same rack, minimizing network hops between them.

## Step 1: Create the Workload

The [Workload](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/workload-aware-scheduling/tas/workload.yaml) references the JobSet and defines a pod group template with a gang scheduling policy:

{{< include file="/examples/workload-aware-scheduling/tas/workload.yaml" lang="yaml" >}}

The `controllerRef` links the Workload to the JobSet. The `podGroupTemplates` entry defines a gang scheduling policy requiring all 4 pods to be schedulable.

## Step 2: Create the PodGroup

The [PodGroup](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/workload-aware-scheduling/tas/podgroup.yaml) references the pod group template and adds topology constraints:

{{< include file="/examples/workload-aware-scheduling/tas/podgroup.yaml" lang="yaml" >}}

The key addition compared to basic gang scheduling is `schedulingConstraints.topology`:

- **`key: topology.example.com/rack`**: The scheduler will find a single rack (i.e., a set of nodes sharing the same `topology.example.com/rack` label value) that can fit all 4 pods and schedule them there.

## Step 3: Create the JobSet

The [JobSet](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/workload-aware-scheduling/tas/jobset.yaml) pods reference the PodGroup through `schedulingGroup.podGroupName`:

{{< include file="/examples/workload-aware-scheduling/tas/jobset.yaml" lang="yaml" >}}

This JobSet creates 4 pods total (2 replicas × 2 completions). All 4 pods will be gang-scheduled onto nodes within the same `topology.example.com/rack` domain.

## Verify Topology Placement

After applying all three resources, verify that the pods were co-located within the same topology domain:

```bash
kubectl get pods -l jobset.sigs.k8s.io/jobset-name=js -o wide
```

Check that all pods landed on nodes sharing the same topology label value:

```bash
kubectl get nodes -L topology.example.com/rack
```
