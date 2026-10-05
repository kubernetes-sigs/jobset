---
title: "Workload Aware Scheduling"
linkTitle: "Workload Aware Scheduling"
weight: 7
date: 2026-05-31
description: >
    Integrating JobSet with Kubernetes Workload Aware Scheduling APIs
no_list: true
---

JobSet can integrate with the Kubernetes Workload Aware Scheduling (WAS) APIs (`scheduling.k8s.io/v1alpha3`) to enable gang scheduling and coordinated pod placement.

- **Declarative**: set `spec.scheduling` directly on the JobSet and let the JobSet controller create and manage the `Workload`/`PodGroup` objects for you. See [Declarative Scheduling](./declarative_scheduling).

## Prerequisites

The Workload Aware Scheduling APIs require a Kubernetes 1.36+ cluster with the following alpha feature gates enabled:

- `GenericWorkload`
- `GangScheduling`
- `TopologyAwareWorkloadScheduling`
- `WorkloadAwarePreemption`

The API server must also have alpha APIs enabled via `--runtime-config=api/alpha=true`.

You can use [Kind](https://kind.sigs.k8s.io/) to create a local cluster with these feature gates enabled:

{{< include file="/examples/workload-aware-scheduling/kind.yaml" lang="yaml" >}}

```bash
kind create cluster --image=kindest/node:latest --config kind.yaml
```

## Overview

The Kubernetes WAS APIs introduce two resources that work with JobSet:

- **Workload**: Represents a higher-level scheduling unit that references the JobSet and defines pod group templates with scheduling policies.
- **PodGroup**: Groups pods that should be scheduled together according to a shared scheduling policy (e.g., gang scheduling).

Pods in the JobSet are associated with a PodGroup through the `schedulingGroup.podGroupName` field in the pod spec.

## Topics

- [Declarative Scheduling](./declarative_scheduling): Configure gang scheduling, topology constraints, sequenced startup, elastic scaling, and more declaratively via `spec.scheduling`, with JobSet managing the Workload and PodGroup objects for you.
