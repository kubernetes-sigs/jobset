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
    - [Resource &amp; Index Management](#resource--index-management)
    - [Internal Scaling (KEP-3715 Integration)](#internal-scaling-kep-3715-integration)
    - [JobSet Policy &amp; State Management](#jobset-policy--state-management)
- [Design Details](#design-details)
  - [API Details](#api-details)
  - [Implementation](#implementation)
  - [Graduation Criteria](#graduation-criteria)
    - [Index Sorting:](#index-sorting)
    - [Horizontal Job Scaling (ReplicatedJob level)](#horizontal-job-scaling-replicatedjob-level)
    - [Horizontal Pod Scaling (Pod-level)](#horizontal-pod-scaling-pod-level)
    - [Policy Evaluation Priority](#policy-evaluation-priority)
    - [Graceful Termination:](#graceful-termination)
  - [Status Management](#status-management)
  - [Defaulting/Validation](#defaultingvalidation)
  - [Test Plan](#test-plan)
    - [Unit Tests](#unit-tests)
    - [Integration Tests](#integration-tests)
- [Implementation History](#implementation-history)
- [Drawbacks](#drawbacks)
- [Alternatives](#alternatives)
- [Future Work](#future-work)
<!-- /toc -->

## Summary

This KEP outlines the proposal to support "elasticity" within JobSets. It achieves this by making the `replicas` field of a `ReplicatedJob` mutable, and by allowing the `parallelism` and `completions` fields of the underlying Job template to be mutated in tandem. This allows external controllers or users to dynamically scale the number of Job replicas in a JobSet (horizontal scaling), as well as scale the number of pods within those Indexed Jobs during its execution.

## Motivation

Currently, most fields in a JobSet are immutable after creation. This poses a challenge for distributed training workloads that are designed to be elastic. For example, PyTorch Elastic (Torchrun) can handle a varying number of nodes. If a JobSet starts with 4 replicas and one node fails or is preempted, the JobSet should ideally scale down to 3 replicas and allow the training to continue, rather than failing the entire workload. 

Furthermore, the number of worker pods within those individual Jobs may need to dynamically scale up or down based on cluster resource availability, requiring the underlying Job's `parallelism` and `completions` to be mutable as well.

### Goals

- Support scaling the number of replicas in a `ReplicatedJob` after the JobSet has been created.
- Leverage KEP-3715 (Elastic Indexed Job) to support internal scaling of child Jobs by allowing `parallelism` and `completions` to be mutated in tandem.
- Ensure the JobSet controller correctly manages the lifecycle (creation/deletion) of child Jobs based on the updated replica count.
- Maintain the stability of existing JobSet features like `DependsOn` and `ExclusivePlacement` during scaling events.

### Non-Goals

- Supporting mutability of the `JobTemplate` or container images mid-run.
- Automated scaling within the JobSet controller (scaling should be triggered by external actors like HPA or Kueue).

## Proposal

### User Stories

#### Story 1
As a Kubeflow Trainer v2 user, I want PyTorch elastic training that scales worker count during failures or when GPUs become available.

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
              command: ["torchrun", "--nnodes=2:8", "--nproc_per_node=1", "train.py"]
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
As a cluster admin using Kueue for resource management, I want Kueue to gracefully handle quota adjustments when my JobSets dynamically scale.

Currently, if an external autoscaler attempts to change the siz-=e of a running JobSet, the API rejects it. With elastic JobSets, an external controller can scale the workload, and Kueue can react to these size changes by updating the underlying `Workload` quota consumption dynamically, without suspending or restarting the training job:

```
Cluster 100 GPUs @ 80% → Kueue scales: replicas 20→25
Cluster 100 GPUs @ 95% → Kueue scales: replicas 25→20
```

### Risks and Mitigations

#### Resource & Index Management
- **Resource Leakage:** Deleting a replica might leave orphaned pods if the child Job is not cleaned up properly.
  - *Mitigation:* The controller will use foreground deletion along with OwnerReferences to ensure all child pods are deterministically and fully terminated before the Job replica is removed.
- **Index Fragmentation:** Scaling down and then up could lead to gaps in naming or indexing.
  - *Mitigation:* The controller will always scale down by removing the highest-indexed Jobs and scale up by appending new indices sequentially.

#### Internal Scaling (KEP-3715 Integration)
- **Stale Failure Counts:** When horizontal pod scaling reduces the completions/parallelism of a child Job, previously failed pods outside the new range still count against that child Job's `backoffLimit`.
  - *Mitigation:* The JobSet controller will not attempt to reset or mask the underlying `status.Failed` count of child Jobs; users must configure their `FailurePolicy` with the understanding that scale-down events do not forgive previous pod failures.
- **Race Conditions on Job Completion:** Changing completion semantics dynamically could conflict with the Job controller declaring a child Job finished.
  - *Mitigation:* This is handled upstream by the native Job controller, which uses `Update` when setting the finished status (triggering object modified errors on concurrent spec changes). Additionally, `spec.completions` is strictly immutable once a Job reaches a terminal condition, preventing the JobSet controller from improperly modifying finished Jobs.

#### JobSet Policy & State Management
- **Scale-down Deletion Triggering FailurePolicy:** When horizontal scaling reduces `replicas`, the JobSet controller must delete the highest-indexed child Jobs. The reconciliation loop might misinterpret this deletion as an unexpected Job failure.
  - *Mitigation:* Because deleting a healthy Job does not inherently trigger a Failed condition, controlled deletions from a 
  `spec.replicas` scale-down will naturally bypass `FailurePolicy` evaluation.
- **Premature Success on Scale-down (`SuccessPolicy: All`):** If a JobSet is waiting on one specific Job to complete, and a scale-down event deletes that exact pending Job, the JobSet might instantly evaluate `SuccessPolicy: All` as true.
  - *Mitigation:* While this is the mathematically correct behavior for elasticity, it will be clearly documented. If the target `replicas` count is met by the remaining completed Jobs, the JobSet successfully completes.
- **Mutating Terminal JobSets:** A user or external autoscaler might attempt to scale a JobSet up or down after the JobSet has already met its `SuccessPolicy` (Completed) or exhausted its `FailurePolicy` (Failed).
  - *Mitigation:* The validating webhook will implement a strict block against late-stage mutations. If `jobset.status.conditions` contains a condition of type `Completed` or `Failed` with status `True`, any updates to `replicas`, `parallelism`, or `completions` will be rejected with an API validation error (e.g., `Forbidden: Cannot mutate a JobSet in a terminal state`).

## Design Details

### API Details

The validation webhook for `jobset.x-k8s.io/v1alpha2` will be modified to allow updates to:

1. spec.replicatedJobs[i].replicas (Horizontal Job scaling)
2. spec.replicatedJobs[i].template.spec.parallelism (Horizontal Pod Scaling)
3. spec.replicatedJobs[i].template.spec.completions (Horizontal Pod Scaling)

```golang

type ReplicatedJob struct {
  // Name of the ReplicatedJob.
  Name string `json:"name"`

  // Replicas is the number of desired Jobs for this ReplicatedJob.
  // This field is changed from immutable to mutable.
  Replicas int32 `json:"replicas"`

  // Template is now partially mutable.
  // Specifically, parallelism and completions can be updated
  // to support Elastic Indexed Jobs.
  Template batchv1.JobTemplateSpec `json:"template"`
}

```

### Implementation

The JobSet controller will implement a multi-path reconciliation logic to support both horizontal and internal elasticity. This ensures the JobSet state remains synchronized with both spec.replicas and the template.spec parameters.

### Graduation Criteria
To ensure stability for existing workloads, this feature will be introduced incrementally using a standard feature gate.

#### Index Sorting:
The controller fetches all child Jobs owned by the JobSet and sorts them numerically by their index suffix (e.g., workers-0, workers-1).

#### Horizontal Job Scaling (ReplicatedJob level)

Scale Up: If spec.replicas > actual Job count, the controller creates new Jobs for the missing indices sequentially (e.g., if scaling from 2 to 4, it creates indices 2 and 3).

Scale Down (LIFO): If actual Job count > spec.replicas, the controller deletes Jobs starting from the highest index downward. This preserves lower-indexed Jobs, which often host "leader" or "master" processes.

#### Horizontal Pod Scaling (Pod-level)

- **Template Propagation:** When the controller detects a valid update to `spec.replicatedJobs[i].template.spec.parallelism` and `completions`, it iterates through all existing healthy child Jobs.
- **Opportunistic Patching:** The controller issues a `PATCH` request to each existing child Job to synchronize its `spec.parallelism` and `spec.completions` with the new values in the JobSet template.
- **Eventual Consistency:** Because Kubernetes lacks multi-object atomic transactions, child Jobs are patched sequentially. If the controller is interrupted mid-reconciliation, some Jobs may scale before others. The controller relies on its level-triggered reconciliation loop to eventually synchronize all child Jobs.
- **Application Synchronization:** The JobSet controller assumes that elastic workloads (like PyTorch Elastic) utilize internal rendezvous mechanisms to handle asynchronous scaling of worker groups, ensuring that early-scaled pods wait for the full topology to synchronize before resuming computation.

#### Policy Evaluation Priority
To prevent "scaling into a fire" or mutating completed workloads, the controller will prioritize state transitions and policy evaluations over scaling operations:

- **FailurePolicy Precedence:** If any Job in the JobSet transitions to a Failed state, the controller will pause both horizontal and internal scaling operations until the `FailurePolicy` has reconciled the failed resource. If the policy triggers a restart, scaling resumes after the restart is initiated. If the policy fails the JobSet, scaling is aborted.
- **SuccessPolicy Precedence:** If the JobSet meets the criteria of its `SuccessPolicy` (e.g., `SuccessPolicy: All` or `SuccessPolicy: Any`), the JobSet is marked as `Completed`. 
- **Terminal State Freeze:** Once a JobSet reaches a terminal state (`Completed` or `Failed`), all ongoing, pending, or future scaling operations are strictly ignored by the controller, and any further API updates to `replicas`, `parallelism`, or `completions` will be rejected by the webhook.

#### Graceful Termination:
During any scale-down event (either removing a Job replica or reducing internal parallelism), the controller relies on the terminationGracePeriodSeconds of the Jobs and Pods. This allows applications like PyTorch Elastic to perform final checkpoints or clean shutdowns.

### Status Management

The JobSet.Status.ReplicatedJobStatus must reflect the scaling operation.

Ready/Active Counts: During a scale-down, the active count must be decremented as soon as the Job deletion is initiated.

Conditions: A new condition Scaling may be added to the JobSet status to indicate a scale-up or scale-down is in progress.

### Defaulting/Validation

- **Mutable Fields:** `replicas`, `template.spec.parallelism`, and `template.spec.completions`.
- **Minimum Value Constraints:** All three mutable scaling fields (`replicas`, `parallelism`, and `completions`) must be strictly `>= 1`. Setting `parallelism` to `0` natively suspends a Job, which conflicts with JobSet's explicit top-level `suspend` field. Elasticity cannot be used as a suspension mechanism.
- **Elastic Indexed Job Constraints:** Horizontal pod-level scaling inherits the strict validation rules of upstream Elastic Indexed Jobs:
- **Synchronous Webhook Validation:** Because the JobSet controller does not deeply validate the underlying Job template, mutations to these scaling fields will be manually validated in the JobSet Validating Admission Webhook. This ensures invalid configurations are rejected at the API server level, preventing the controller from stalling during reconciliation.
- **Terminal State Freeze:** If the JobSet has a `Completed` or `Failed` condition set to `True`, any mutation to the scaling fields will be rejected.
- **Immutable Fields:** All other fields in the `template.spec` (e.g., `container.image`, `resources`, `nodeSelector`) remain strictly immutable.

### Test Plan

#### Unit Tests
- `jobset_webhook_test.go`: 
  - Ensure that updates to `replicas`, `parallelism`, and `completions` are accepted, while updates to other template fields are rejected.
  - Verify that scaling requests are rejected if the requested values are `< 1`.
  - Verify that scaling requests are rejected if the JobSet is already in a terminal state (`Failed` or `Completed`).
- `jobset_controller_test.go`: 
  - Verify the logic that calculates which Job indices to delete during a scale-down.
  - Verify the logic that differentiates between an intentional scale-down deletion and an unexpected Job failure.

#### Integration Tests
- **Horizontal Job Scaling (ReplicatedJob level):**
  - Verify that a JobSet correctly scales up from 1 to 3 replicas.
  - Verify that a JobSet correctly scales down from 3 to 1 replica, ensuring the Jobs with index 1 and 2 are the ones deleted.
- **Horizontal Pod Scaling (Pod-level):**
  - Verify that updating `parallelism` and `completions` on the JobSet propagates to existing child Jobs via `PATCH` requests.
- **Topology & Placement:**
  - Test scaling in a JobSet that uses `ExclusivePlacement` to ensure pods are still placed correctly on new nodes.
- **Policy & State Interactions (New):**
  - Verify that scaling down a `ReplicatedJob` (deleting a child Job) does *not* trigger the JobSet's `FailurePolicy`.
  - Verify that if a child Job fails, the `FailurePolicy` evaluates and executes (e.g., restarting the JobSet) and correctly aborts or defers any pending scaling operations.

## Implementation History
Mar 21, 2024: Issue #463 opened as an RFC.

Jul 30, 2024: Discussions regarding framework requirements for PyTorch v2.

Feb 04, 2026: KEP formally proposed for v1alpha2.

## Drawbacks
Application Awareness: This feature is only useful if the application running inside the JobSet can handle its membership changing dynamically.

Controller Overhead: Frequent scaling updates could increase the load on the JobSet controller and the Kubernetes API server.

## Alternatives
HorizontalPodAutoscaler (HPA) on Jobs: HPA can scale the parallelism of a standard Job, but it cannot manage the multi-job coordination that JobSet provides (e.g., DNS, multi-interface networking).

Manual Re-creation: Deleting the JobSet and creating a new one with the desired size. This causes a full restart of all workers, which is inefficient for large-scale training.

## Future Work
- **UpdatePolicy API:** As the adoption of elastic JobSets expands, we anticipate the need for more control over how mutations are applied. Future iterations could introduce an `updatePolicy` API to configure scaling behaviors, such as defining which specific Job indices should be prioritized for deletion during a scale-down event.