# KEP-463: Support Elastic JobSets

<!--
This is the title of your KEP. Keep it short, simple, and descriptive. A good
title can help communicate what the KEP is and should be considered as part of
any review.
-->

<!--
A table of contents is helpful for quickly jumping to sections of a KEP and for
highlighting any additional information provided beyond the standard KEP
template.

Ensure the TOC is wrapped with
  <code>&lt;!-- toc --&rt;&lt;!-- /toc --&rt;</code>
tags, and then generate with `hack/update-toc.sh`.
-->

<!-- toc -->
- [Summary](#summary)
- [Motivation](#motivation)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Proposal](#proposal)
  - [User Stories](#user-stories)
    - [Story 1](#story-1)
    - [Story 2](#story-2)
    - [Story 3](#story-3)
  - [Risks and Mitigations](#risks-and-mitigations)
- [Design Details](#design-details)
  - [API Details](#api-details)
  - [Implementation](#implementation)
  - [Failure Handling Priority](#failure-handling-priority)
  - [Defaulting/Validation](#defaultingvalidation)
  - [Test Plan](#test-plan)
    - [Unit Tests](#unit-tests)
    - [Integration Tests](#integration-tests)
- [Implementation History](#implementation-history)
- [Drawbacks](#drawbacks)
- [Alternatives](#alternatives)
<!-- /toc -->

## Summary

This KEP outlines the proposal to support "elasticity" within JobSets by making the `replicas` field of a `ReplicatedJob` mutable. This allows external controllers or users to scale the number of Job replicas in a JobSet up or down dynamically during its execution.

## Motivation

Currently, most fields in a JobSet are immutable after creation. This poses a challenge for distributed training workloads that are designed to be elastic. For example, PyTorch Elastic (Torchrun) can handle a varying number of nodes. If a JobSet starts with 4 replicas and one node fails or is preempted, the JobSet should ideally scale down to 3 replicas and allow the training to continue, rather than failing the entire workload.

### Goals

- Support scaling the number of replicas in a `ReplicatedJob` after the JobSet has been created.
- Ensure the JobSet controller correctly manages the lifecycle (creation/deletion) of child Jobs based on the updated replica count.
- Maintain the stability of existing JobSet features like `StartupPolicy` and `ExclusivePlacement` during scaling events.

### Non-Goals

- Supporting mutability of the `JobTemplate` or container images mid-run.
- Automated scaling within the JobSet controller (scaling should be triggered by external actors like HPA or Kueue).

## Proposal

### User Stories

#### Story 1
As a Kubeflow Training Operator v2 user, I want PyTorch elastic training that scales worker count during failures or when GPUs become available.

Currently JobSet rejects `parallelism`/`completions` changes needed by `elasticPolicy`. With this KEP:

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: pytorch-elastic
spec:
  replicatedJobs:
  - name: workers
    replicas: 1
    template:
      spec:
        completionMode: Indexed
        parallelism: 2      # Can scale: 2→8
        completions: 2
        template:
          spec:
            containers:
            - name: trainer
              image: pytorch/torchserve
              command: ["torchrun", "--nproc_per_node=1", "train.py"]
              resources:
                limits:
                  nvidia.com/gpu: 1
# kubectl patch jobset pytorch-elastic -p '{"spec":{"replicatedJobs":[{"replicas":1,"template":{"spec":{"parallelism":8,"completions":8}}}]}}'
```

#### Story 2
As a Google Cloud TPU user, I want my training to continue when one TPU slice fails by scaling down replicas.

TPU slices (16 nodes each) fail atomically. Currently entire JobSet fails. With elastic JobSets:

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: tpu-training
spec:
  replicatedJobs:
  - name: workers
    replicas: 4          # One Job per TPU slice
    template:
      spec:
        completionMode: Indexed
        parallelism: 16    # 16 TPU v4 cores per slice
        completions: 16
        template:
          spec:
            nodeSelector:
              cloud.google.com/gke-tpu-topology: 4x4
# TPU slice 2 fails → scale: replicas: 4→3 (controller deletes workers-3)
```

#### Story 3
As a cluster admin, I want JobSets to integrate with Kueue elastic quotas for dynamic GPU scaling.

Kueue cannot scale JobSets today. With elastic support, Kueue scales based on cluster utilization:

```
Cluster 100 GPUs @ 80% → Kueue scales: replicas 20→25
Cluster 100 GPUs @ 95% → Kueue scales: replicas 25→20
```

### Risks and Mitigations

- **Resource Leakage:** Deleting a replica might leave orphaned pods if the child Job is not cleaned up properly.
  - *Mitigation:* The controller will rely on OwnerReferences and the standard Job deletion lifecycle to ensure all pods are terminated.
- **Index Fragmentation:** Scaling down and then up could lead to gaps in naming or indexing.
  - *Mitigation:* The controller will always scale down by removing the highest-indexed Jobs and scale up by appending new indices sequentially.

## Design Details

### API Details

The validation webhook for `jobset.x-k8s.io/v1alpha2` will be modified to allow updates to `spec.replicatedJobs[i].replicas`.

```golang

type ReplicatedJob struct {
	// Name of the ReplicatedJob.
	Name string `json:"name"`

	// Replicas is the number of desired Jobs for this ReplicatedJob.
	// This field is changed from immutable to mutable.
	Replicas int32 `json:"replicas"`

	// ... (Template and other fields remain immutable)
}

```

### Implementation

The JobSet controller will implement a Reconcile-to-Desired pattern. This ensures that the state of child Jobs always migrates toward the spec.replicas count while maintaining index integrity.

Index Sorting: The controller fetches all Jobs owned by the JobSet and sorts them numerically by their index suffix (e.g., workers-0, workers-1).

Scale Up: If desired > actual, the controller creates new Jobs for the missing indices sequentially (e.g., if scaling from 2 to 4, it creates indices 2 and 3).

Scale Down (LIFO): If actual > desired, the controller deletes Jobs starting from the highest index downward (Last-In, First-Out). This preserves the lower-indexed Jobs, which often host "leader" or "master" processes in distributed frameworks.

Graceful Termination: During scale-down, the controller issues a standard Delete call for the Job. It relies on the Job and Pod terminationGracePeriodSeconds to allow the application (e.g., PyTorch Elastic) to perform a final checkpoint or clean shutdown before the process is killed.

### Failure Handling Priority
To prevent "scaling into a fire," the controller will prioritize stability over elasticity:

If any Job in the JobSet is in a Failed state, the controller will pause all scaling operations.

The FailurePolicy must first reconcile the failed Job (either by restarting the set or hitting the backoffLimit) before any scale-up or scale-down requests are processed

### Status Management

The JobSet.Status.ReplicatedJobStatus must reflect the scaling operation.

Ready/Active Counts: During a scale-down, the active count must be decremented as soon as the Job deletion is initiated.

Conditions: A new condition Scaling may be added to the JobSet status to indicate a scale-up or scale-down is in progress.

### Defaulting/Validation

Validation: Ensure replicas >= 0.

Immutability: All other fields within ReplicatedJob (Name, Template, Network config) remain immutable to prevent identity drift.

### Test Plan

#### Unit Tests
jobset_webhook_test.go: Ensure that updates to replicas are accepted while updates to template are rejected.

jobset_controller_test.go: Verify the logic that calculates which Job indices to delete during a scale-down.

#### Integration Tests
Verify that a JobSet correctly scales up from 1 to 3 replicas.

Verify that a JobSet correctly scales down from 3 to 1 replica, ensuring the Jobs with index 1 and 2 are the ones deleted.

Test scaling in a JobSet that uses ExclusivePlacement to ensure pods are still placed correctly on new nodes.

### Implementation History
Mar 21, 2024: Issue #463 opened as an RFC.

Jul 30, 2024: Discussions regarding framework requirements for PyTorch v2.

Feb 04, 2026: KEP formally proposed for v1alpha2.

### Drawbacks
Application Awareness: This feature is only useful if the application running inside the JobSet can handle its membership changing dynamically.

Controller Overhead: Frequent scaling updates could increase the load on the JobSet controller and the Kubernetes API server.

### Alternatives
HorizontalPodAutoscaler (HPA) on Jobs: HPA can scale the parallelism of a standard Job, but it cannot manage the multi-job coordination that JobSet provides (e.g., DNS, multi-interface networking).

Manual Re-creation: Deleting the JobSet and creating a new one with the desired size. This causes a full restart of all workers, which is inefficient for large-scale training.