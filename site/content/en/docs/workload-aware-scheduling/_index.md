---
title: "Workload Aware Scheduling"
linkTitle: "Workload Aware Scheduling"
weight: 7
date: 2026-05-31
description: >
    Integrating JobSet with Kubernetes Workload Aware Scheduling APIs
no_list: true
---

JobSet can integrate with the Kubernetes Workload Aware Scheduling (WAS) APIs (`scheduling.k8s.io/v1beta1`) to enable gang scheduling and coordinated pod placement.

- **Declarative**: set `spec.scheduling` directly on the JobSet and let the JobSet controller create and manage the `Workload`/`PodGroup` objects for you. See [Declarative Scheduling](./declarative_scheduling).

## Prerequisites

The JobSet WAS integration requires Kubernetes 1.37+ with the following feature gates enabled for the examples in this section:

- `GenericWorkload`
- `TopologyAwareWorkloadScheduling` — required for topology constraints
- `DRAWorkloadResourceClaims` — required for shared DRA resource claims

The API server must also enable the WAS APIs via `--runtime-config=scheduling.k8s.io/v1beta1=true`. JobSet does not require the `WorkloadWithJob` feature gate.

You can use [Kind](https://kind.sigs.k8s.io/) to create a local cluster with these feature gates enabled:

{{< include file="/examples/workload-aware-scheduling/kind.yaml" lang="yaml" >}}

```bash
kind build node-image --image=jobset/kind-node:v1.37.0 \
  https://dl.k8s.io/v1.37.0/kubernetes-server-linux-amd64.tar.gz
kind create cluster --image=jobset/kind-node:v1.37.0 --config kind.yaml
```

Use `kubernetes-server-linux-arm64.tar.gz` instead on ARM64 hosts. From a JobSet repository checkout, `make kind-cluster-scheduling` builds the pinned Kubernetes `v1.37.0` node image, creates the cluster, and deploys JobSet with `JobSetWorkloadAwareSchedulingAPI` enabled. See [Declarative Scheduling](./declarative_scheduling) for the controller feature gate configuration.

## Overview

The Kubernetes WAS APIs introduce two resources that work with JobSet:

- **Workload**: Represents a higher-level scheduling unit that references the JobSet and defines pod group templates with scheduling policies.
- **PodGroup**: Groups pods that should be scheduled together according to a shared scheduling policy (e.g., gang scheduling).

Pods in the JobSet are associated with a PodGroup through the `schedulingGroup.podGroupName` field in the pod spec.

## Topics

- [Declarative Scheduling](./declarative_scheduling): Configure gang scheduling, topology constraints, sequenced startup, elastic scaling, and more declaratively via `spec.scheduling`, with JobSet managing the Workload and PodGroup objects for you.
