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
  - [Risks and Mitigations](#risks-and-mitigations)
    - [Internal Scaling (KEP-3715 Integration)](#internal-scaling-kep-3715-integration)
    - [JobSet Policy &amp; State Management](#jobset-policy--state-management)
- [Design Details](#design-details)
  - [API Details](#api-details)
  - [Implementation](#implementation)
  - [Graduation Criteria](#graduation-criteria)
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
  - [1. Horizontal Job Scaling (<code>replicas</code> mutability)](#1-horizontal-job-scaling--mutability)
    - [Proposed Implementation Mechanics](#proposed-implementation-mechanics)
    - [Identified Risks &amp; Mitigations for Job-Level Scaling](#identified-risks--mitigations-for-job-level-scaling)
  - [2. UpdatePolicy API](#2-updatepolicy-api)
<!-- /toc -->

## Summary

This KEP outlines the proposal to support "elasticity" within JobSets. It achieves this by allowing the `parallelism` and `completions` fields of the underlying Job template to be mutated. This allows external controllers or users to dynamically scale the number of pods within those Indexed Jobs during execution, seamlessly integrating with Kubernetes Elastic Indexed Jobs (KEP-3715). Support for scaling the number of Job replicas (`replicas` mutability) is deferred to future work.

## Motivation

Currently, most fields in a JobSet are immutable after creation. This poses a challenge for distributed training workloads that are designed to be elastic, such as PyTorch Elastic (Torchrun), which can handle a varying number of workers and gracefully recover from hardware failures or preemptions. 

To fully support these large-scale elastic training workloads, both Pod-level scaling (adjusting the number of worker pods within a Job) and Job-level scaling (adjusting the number of ReplicatedJobs, e.g., dropping from 4 nodes to 3) are ultimately required. However, to deliver value iteratively and reduce immediate complexity, this proposal focuses first on supporting Elastic Indexed Jobs (Pod-level scaling) by making `parallelism` and `completions` mutable. Job-level scaling (`replicas` mutability) is planned for a future iteration.

### Goals

- Leverage KEP-3715 (Elastic Indexed Job) to support internal scaling of child Jobs by allowing `parallelism` and `completions` to be mutated in tandem.
- Ensure the JobSet Validating Admission Webhook correctly validates these mutations to prevent invalid configurations from stalling the controller.
- Maintain the stability of existing JobSet features like `DependsOn` and `ExclusivePlacement` during scaling events.

### Non-Goals

- Horizontal Job Scaling: Supporting mutability of jobSet.spec.replicatedJobs[].replicas. This is an acknowledged requirement for large-scale training but is explicitly out of scope for this initial implementation and is deferred to future work.
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
As a cluster admin using Kueue for resource management, I want Kueue to gracefully handle quota adjustments when my JobSets dynamically scale.

Currently, if an external autoscaler attempts to change the siz-=e of a running JobSet, the API rejects it. With elastic JobSets, an external controller can scale the workload, and Kueue can react to these size changes by updating the underlying `Workload` quota consumption dynamically, without suspending or restarting the training job:

```
Cluster 100 GPUs @ 80% → Kueue scales: parallelism 20→25
Cluster 100 GPUs @ 95% → Kueue scales: parallelism 25→20
```

### Risks and Mitigations

#### Internal Scaling (KEP-3715 Integration)
- **Stale Failure Counts:** When horizontal pod scaling reduces the completions/parallelism of a child Job, previously failed pods outside the new range still count against that child Job's `backoffLimit`.
  - *Mitigation:* The JobSet controller will not attempt to reset or mask the underlying `status.Failed` count of child Jobs; users must configure their `FailurePolicy` with the understanding that scale-down events do not forgive previous pod failures.
- **Race Conditions on Job Completion:** Changing completion semantics dynamically could conflict with the Job controller declaring a child Job finished.
  - *Mitigation:* This is handled upstream by the native Job controller, which uses `Update` when setting the finished status (triggering object modified errors on concurrent spec changes). Additionally, `spec.completions` is strictly immutable once a Job reaches a terminal condition, preventing the JobSet controller from improperly modifying finished Jobs.

#### JobSet Policy & State Management
- **Mutating Terminal JobSets:** A user or external autoscaler might attempt to scale a JobSet up or down after the JobSet has already met its `SuccessPolicy` (Completed) or exhausted its `FailurePolicy` (Failed).
  - *Mitigation:* The validating webhook will implement a strict block against late-stage mutations. If `jobset.status.conditions` contains a condition of type `Completed` or `Failed` with status `True`, any updates to `parallelism`, or `completions` will be rejected with an API validation error (e.g., `Forbidden: Cannot mutate a JobSet in a terminal state`).

## Design Details

### API Details

The validation webhook for `jobset.x-k8s.io/v1alpha2` will be modified to allow updates to:

1. spec.replicatedJobs[i].template.spec.parallelism (Horizontal Pod Scaling)
2. spec.replicatedJobs[i].template.spec.completions (Horizontal Pod Scaling)

```golang

type ReplicatedJob struct {
  // Name of the ReplicatedJob.
  Name string `json:"name"`

  // Replicas is the number of desired Jobs for this ReplicatedJob.
  // This field remains immutable in this iteration.
  Replicas int32 `json:"replicas"`

  // Template is now partially mutable.
  // Specifically, parallelism and completions can be updated
  // to support Elastic Indexed Jobs.
  Template batchv1.JobTemplateSpec `json:"template"`
}

```

### Implementation

The JobSet controller will implement reconciliation logic to support internal elasticity. This ensures the JobSet state remains synchronized with the template.spec parameters.

### Graduation Criteria
To ensure stability for existing workloads, the ElasticJobSet feature will be introduced incrementally using a standard feature gate.

#### Horizontal Pod Scaling (Pod-level)

- **Template Propagation:** When the controller detects a valid update to `spec.replicatedJobs[i].template.spec.parallelism` and `completions`, it iterates through all existing healthy child Jobs.
- **Opportunistic Patching:** The controller issues a `PATCH` request to each existing child Job to synchronize its `spec.parallelism` and `spec.completions` with the new values in the JobSet template.
- **Eventual Consistency:** Because Kubernetes lacks multi-object atomic transactions, child Jobs are patched sequentially. If the controller is interrupted mid-reconciliation, some Jobs may scale before others. The controller relies on its level-triggered reconciliation loop to eventually synchronize all child Jobs.
- **Application Synchronization:** The JobSet controller assumes that elastic workloads (like PyTorch Elastic) utilize internal rendezvous mechanisms to handle asynchronous scaling of worker groups, ensuring that early-scaled pods wait for the full topology to synchronize before resuming computation.

#### Policy Evaluation Priority
To prevent "scaling into a fire" or mutating completed workloads, the controller will prioritize state transitions and policy evaluations over scaling operations:

- **FailurePolicy Precedence:** If any Job in the JobSet transitions to a Failed state, the controller will pause internal scaling operations until the `FailurePolicy` has reconciled the failed resource. If the policy triggers a restart, scaling resumes after the restart is initiated. If the policy fails the JobSet, scaling is aborted.
- **SuccessPolicy Precedence:** If the JobSet meets the criteria of its `SuccessPolicy` (e.g., `SuccessPolicy: All` or `SuccessPolicy: Any`), the JobSet is marked as `Completed`. 
- **Terminal State Freeze:** Once a JobSet reaches a terminal state (`Completed` or `Failed`), all ongoing, pending, or future scaling operations are strictly ignored by the controller, and any further API updates to `parallelism`, or `completions` will be rejected by the webhook.

#### Graceful Termination:
During any scale-down event (reducing internal parallelism), the controller relies on the terminationGracePeriodSeconds of the Jobs and Pods. This allows applications like PyTorch Elastic to perform final checkpoints or clean shutdowns.

### Status Management

The JobSet.Status.ReplicatedJobStatus must reflect the scaling operation.

Conditions: A new condition Scaling may be added to the JobSet status to indicate a scale-up or scale-down is in progress.

### Defaulting/Validation

- **Mutable Fields:** `template.spec.parallelism`, and `template.spec.completions`.
- **Minimum Value Constraints:** Both mutable scaling fields (`parallelism`, and `completions`) must be strictly `>= 1`. Setting `parallelism` to `0` natively suspends a Job, which conflicts with JobSet's explicit top-level `suspend` field. Elasticity cannot be used as a suspension mechanism.
- **Elastic Indexed Job Constraints:** Horizontal pod-level scaling inherits the strict validation rules of upstream Elastic Indexed Jobs:
- **Synchronous Webhook Validation:** Because the JobSet controller does not deeply validate the underlying Job template, mutations to these scaling fields will be manually validated in the JobSet Validating Admission Webhook. This ensures invalid configurations are rejected at the API server level, preventing the controller from stalling during reconciliation.
- **Terminal State Freeze:** If the JobSet has a `Completed` or `Failed` condition set to `True`, any mutation to the scaling fields will be rejected.
- **Immutable Fields:** All other fields in the `template.spec` (e.g., `container.image`, `resources`, `nodeSelector`) remain strictly immutable.

### Test Plan

#### Unit Tests
- `jobset_webhook_test.go`: 
  - Ensure that updates to `parallelism`, and `completions` are accepted, while updates to other template fields are rejected.
  - Verify that scaling requests are rejected if the requested values are `< 1`.
  - Verify that scaling requests are rejected if the JobSet is already in a terminal state (`Failed` or `Completed`).

#### Integration Tests
- **Horizontal Pod Scaling (Pod-level):**
  - Verify that updating `parallelism` and `completions` on the JobSet propagates to existing child Jobs via `PATCH` requests.
- **Topology & Placement:**
  - Test scaling in a JobSet that uses `ExclusivePlacement` to ensure pods are still placed correctly on new nodes.
- **Policy & State Interactions (New):**
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

While this initial implementation focuses strictly on Pod-level scaling (`parallelism` and `completions`), the ultimate goal is to support full elasticity, including Job-level scaling. The following items capture the design ideas, user stories, and identified risks for these future iterations.

### 1. Horizontal Job Scaling (`replicas` mutability)
Future iterations will introduce mutability to `jobSet.spec.replicatedJobs[].replicas`. This will allow the JobSet controller to natively handle scaling down (e.g., in response to a hardware failure) and scaling up (when new nodes become available) without requiring a full teardown.

#### Proposed Implementation Mechanics

- **Index Sorting:** The controller will fetch all child Jobs owned by the JobSet and sort them numerically by their index suffix (e.g., `workers-0`, `workers-1`).
- **Scale Up:** If `spec.replicas` > actual Job count, the controller creates new Jobs for the missing indices sequentially.
- **Scale Down (LIFO):** If actual Job count > `spec.replicas`, the controller deletes Jobs starting from the highest index downward. This preserves lower-indexed Jobs, which often host "leader" or "master" processes.
- **Status Management (Active/Ready Counts):** During a ReplicatedJob scale-down, the `active` count in the JobSet status must be decremented as soon as the child Job deletion is initiated (foreground deletion), rather than waiting for the pods to fully terminate. This ensures the JobSet status immediately reflects the new target `replicas` count.

#### Identified Risks & Mitigations for Job-Level Scaling

- **Resource Leakage:** Deleting a replica might leave orphaned pods.
  - *Mitigation:* The controller must use foreground deletion (`metav1.DeletePropagationForeground`) along with OwnerReferences to ensure all child pods are deterministically terminated before the Job replica is removed.
- **Index Fragmentation:** Scaling down and then up could lead to gaps in naming or indexing.
  - *Mitigation:* Strictly enforcing the LIFO (Last-In, First-Out) scale-down and sequential scale-up logic prevents fragmentation.
- **Scale-down Deletion Triggering FailurePolicy:** When scaling reduces `replicas`, the controller deletes the highest-indexed child Jobs. The reconciliation loop might misinterpret this deletion as an unexpected Job failure.
  - *Mitigation:* Because deleting a healthy Job does not inherently trigger a `Failed` condition, controlled deletions from a `spec.replicas` scale-down will naturally bypass `FailurePolicy` evaluation.
- **Premature Success on Scale-down (`SuccessPolicy: All`):** If a JobSet is waiting on one specific Job to complete, and a scale-down event deletes that exact pending Job, the JobSet might instantly evaluate `SuccessPolicy: All` as true.
  - *Mitigation:* While this is technically mathematically correct for elasticity, future implementations will need to ensure this behavior is strictly documented and handled safely so workloads don't prematurely exit.

### 2. UpdatePolicy API

As the adoption of elastic JobSets expands, we anticipate the need for more granular control over how mutations are applied. Future iterations could introduce an `updatePolicy` API to configure scaling behaviors, such as defining which specific Job indices should be prioritized for deletion during a scale-down event, or how disruption budgets are respected during an active training cycle.