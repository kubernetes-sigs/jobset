---
title: "Gang Scheduling"
linkTitle: "Gang Scheduling"
weight: 1
date: 2026-05-31
description: >
    Configure gang scheduling with JobSet using the Kubernetes WAS APIs
no_list: true
---

This example configures gang scheduling so that all 6 pods (3 replicas × 2 completions) in a JobSet must be schedulable before any of them are admitted.

## Step 1: Create the Workload

The [Workload](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/workload-aware-scheduling/workload.yaml) references the JobSet and defines a pod group template with a gang scheduling policy:

{{< include file="/examples/workload-aware-scheduling/workload.yaml" lang="yaml" >}}

The `controllerRef` links the Workload to the JobSet. The `podGroupTemplates` entry defines a gang scheduling policy requiring a minimum of 6 pods to be schedulable.

## Step 2: Create the PodGroup

The [PodGroup](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/workload-aware-scheduling/podgroup.yaml) references the pod group template defined in the Workload:

{{< include file="/examples/workload-aware-scheduling/podgroup.yaml" lang="yaml" >}}

## Step 3: Create the JobSet

The [JobSet](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/workload-aware-scheduling/jobset.yaml) pods reference the PodGroup through `schedulingGroup.podGroupName`:

{{< include file="/examples/workload-aware-scheduling/jobset.yaml" lang="yaml" >}}

With gang scheduling and `minCount: 6`, the scheduler ensures all 6 pods can be placed before scheduling any of them. If the cluster cannot accommodate all pods simultaneously, none will be scheduled — preventing partial scheduling where some pods run while others remain pending.
