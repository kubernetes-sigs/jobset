# KEP-1172: Support granular suspend/resume for individual Jobs within a JobSet

<!-- toc -->
- [Summary](#summary)
- [Motivation](#motivation)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Proposal](#proposal)
  - [User Stories (Optional)](#user-stories-optional)
    - [Story 1](#story-1)
  - [Notes/Constraints/Caveats (Optional)](#notesconstraintscaveats-optional)
  - [Risks and Mitigations](#risks-and-mitigations)
- [Design Details](#design-details)
  - [Test Plan](#test-plan)
      - [Prerequisite testing updates](#prerequisite-testing-updates)
    - [Unit Tests](#unit-tests)
    - [Integration tests](#integration-tests)
  - [Graduation Criteria](#graduation-criteria)
- [Implementation History](#implementation-history)
- [Drawbacks](#drawbacks)
- [Alternatives](#alternatives)
<!-- /toc -->

## Summary

This KEP introduces fine-grained suspend and resume capabilities at the individual Job level within a JobSet. This allows specific child Jobs to be independently suspended and resumed without altering the execution state of other actively running Jobs within the same JobSet.

## Motivation

This feature is necessary to support advanced scheduling and placement optimizations in Kueue or similar schedulers, particularly around topology awareness. See corresponding [Kueue issue](https://github.com/kubernetes-sigs/kueue/issues/9940).

Currently, the JobSet API assumes that if one Job changes (recreated, suspended, etc), then all Jobs should change accordingly. However, there are use cases that require only a single Job being changed without affecting the rest.

Elastic training use case: If the underlying topology of a specific Job's placement becomes fragmented or broken, the scheduler needs the ability to independently relocate (recreate) that single Job. Currently, lacking granular control, adjusting the placement of one Job disrupts the others in the JobSet. By enabling individual Job suspension, the scheduler will be able to suspend a specific Job, safely recreate it in a new topological location, and resume it, all without affecting the uninterrupted execution of the other sibling Jobs in the JobSet.

### Goals

- Introduce fine-grained suspend and resume capabilities at the individual Job level within a JobSet.
- Provide a mechanism to suspend and resume specific child Jobs independently.
- Ensure the state of other running Jobs in the JobSet remains unaffected during these operations.

### Non-Goals

- Modifying the suspension behavior of the entire JobSet (this is already supported).
- Changing the underlying Kubernetes Job behavior itself beyond what JobSet controls.

## Proposal

We propose adding a mechanism (e.g., via an API field or annotation in the JobSet) to individually target specific Jobs within a JobSet and control their suspension state without changing the overall JobSet suspension state.

### User Stories

#### Story 1
As a user utilizing Kueue for topology-aware scheduling, I want to relocate a single fragmented Job to a better topological location without killing or pausing the other jobs in my JobSet, so that large-scale parallel processing is more resilient to location constraints.

### Notes/Constraints/Caveats (Optional)

N/A

### Risks and Mitigations

The primary risk is introducing complexity to the JobSet controller, as it now has to manage the suspension state of individual jobs rather than just propagating the JobSet's suspension down to all jobs uniformly. This will be mitigated by thorough unit and integration testing.

## Design Details

Initially we will support this via an annotation on the JobSet: `jobset.sigs.k8s.io/reconciliation-mode: Independent`. If this annotation is set, then the JobSet webhook enforces `jobSet.spec.suspend = nil` and does not reconcile the field job.spec.suspend of child Jobs.

If this annotation is set to `Independent`:
1. The JobSet webhook will enforce `jobSet.spec.suspend = nil`.
2. The JobSet controller will not reconcile the `job.spec.suspend` field of child Jobs.

Based on usage & feedback, we will evaluate if we should modify the JobSet API to control this behavior.

We cannot use `jobSet.spec.suspend = nil` as a signal to not reconcile the field `job.spec.suspend` of child Jobs because the JobSet controller currently treats `jobSet.spec.suspend = nil` as a synonym for `jobSet.spec.suspend = false`. If users are unsuspending JobSets by setting `jobSet.spec.suspend = nil` instead of `false`, this change would break their workflow. 

This simple annotation-based solution gives the use case time to evolve before proposing a more complete API (such as an `updatePolicy` field in the future).

#### Interaction with existing JobSet features:

1. DependsOn
With `reconciliation-mode: Independent`, suspending an already running child Job should have no effect on the dependsOn logic. The "gate" for dependsOn is only checked at the time of Job creation or re-creation on failure. It should be up to the scheduler to choose what order to suspend/unsuspend Jobs. This feature will still respect dependsOn ordering at Job creation time.

2. FailurePolicy
With `reconciliation-mode: Independent`, suspending a Job will not trigger the failure policy action for the ReplicatedJob since the Job doesn't "fail". Jobs being suspended or unsuspended will not count towards maxRestarts.

JobSet failure policy applies to suspended jobs too i.e. if job A is suspended & job B fails, job A will also be restarted. If a Job fails and then is suspended by Kueue before the JobSet controller processes the failure, the controller will still respect the FailurePolicy.

As a follow up, Kueue should support failure policy actions with this feature. We can also add a new field to track number of per job restarts that includes suspensions + failure policy restarts.

3. SuccessPolicy
With `reconciliation-mode: Independent`, if the SuccessPolicy is set to All, the JobSet will never reach a Succeeded state as long as any Job is suspended.
If a SuccessPolicy targets a specific ReplicatedJob (e.g., "Succeed if the 'leader' Job completes"), suspending Jobs in other ReplicatedJobs will not prevent the JobSet from succeeding once the leader finishes.

Once a JobSet meets its SuccessPolicy, the controller deletes or cleans up all Jobs including suspended ones.

4. PVCs
With `reconciliation-mode: Independent`, when a Job is suspended, its Pods are terminated, but the Job object and its associated PVCs should persist. When the Job is resumed, the new Pods will mount the same PVCs.

#### Future API Evolution

If the `jobset.sigs.k8s.io/reconciliation-mode: Independent` annotation proves successful, we may promote this configuration to a strongly-typed API field within the `JobSetSpec`. A dedicated policy field (e.g. `UpdatePolicy`) would cleanly move this behavior out of annotations, providing better validation, discoverability via OpenAPI schemas, and extensibility if other child Job fields need decoupled reconciliation in the future.

Such an API could look like:

```go
type JobSetSpec struct {
    // ...
    // UpdatePolicy defines how the JobSet controller handles updates and reconciliation
    // of child Jobs after they have been created.
    // +optional
    UpdatePolicy *JobSetUpdatePolicy `json:"updatePolicy,omitempty"`
}

type JobSetUpdatePolicy struct {
    // ReconciliationMode controls whether the JobSet controller actively 
    // reconciles the `suspend` field of child Jobs to match the JobSet.
    // Valid values are "Sync" (default) and "Independent".
    // +optional
    ReconciliationMode *ReconciliationMode `json:"reconciliationMode,omitempty"`
}

type ReconciliationMode string

const (
    // Sync ensures child Jobs are suspended/resumed 
    // exactly matching the JobSetSpec.Suspend field.
    Sync ReconciliationMode = "Sync"
    
    // Independent means the JobSet controller will not attempt to 
    // modify the `suspend` field of existing child Jobs, allowing external controllers
    // (like Kueue) to manage them individually.
    Independent ReconciliationMode = "Independent"
)
```

Kueue will support this by supporting a Workload per Job for partial admission/partial recovery of JobSets. See corresponding [Kueue issue](https://github.com/kubernetes-sigs/jobset/pull/1178).

### Test Plan

[ x] I/we understand the owners of the involved components may require updates to
existing tests to make this code solid enough prior to committing the changes necessary
to implement this enhancement.

#### Unit Tests

We will add unit tests to the following packages to ensure the new `jobset.sigs.k8s.io/reconciliation-mode: Independent` annotation is respected:

- `jobset/pkg/controllers`: Test that the JobSet controller does not modify `job.spec.suspend` on existing child Jobs when the annotation is present.
- `jobset/pkg/webhooks`: Test that the JobSet mutating webhook successfully defaults or forces `jobSet.spec.suspend = nil` if the annotation is set.

#### Integration tests

1. Basic Functionality
- Independent Sync: Verify that when reconciliation-mode: Independent is set, changing JobSet.spec.suspend from false to true does not suspend Jobs that were already running, and vice versa.

- Granular Control: Manually suspend 1 of 3 Jobs in a ReplicatedJob and verify the other 2 continue running. Verify the JobSet status remains Active.

2. Policy Interactions with `reconciliation-mode: Independent`:
DependsOn + Suspension: Create a JobSet where Job B depends on Job A. Suspend one replica of Job A. Verify Job B is never created. Resume the replic, and verify Job B is then created.

- FailurePolicy + Restart: Set a FailurePolicy with maxRestarts: 1. Suspend one Job, then force a failure in another Job. Verify the JobSet restarts and check if the previously suspended Job is recreated as "running" (demonstrating the state loss during restart).

- SuccessPolicy (All): Suspend one Job and let all others succeed. Verify the JobSet does not transition to Succeeded. Unsuspend the Job, let it finish, and verify the JobSet then succeeds.

- SuccessPolicy (Any): Define a policy where only the first ReplicatedJob needs to succeed. Suspend a Job in the second ReplicatedJob. Verify the JobSet still succeeds when the first finishes.

3. PVC Retention with reconciliation-mode: Independent
Suspend a Job that has a JobSet-managed PVC. Verify the PVC is not deleted. Resume the Job and verify it successfully mounts the same PVC.

4. JobSet Deletion with reconciliation-mode: Independent: Delete a JobSet while some child Jobs are suspended. Verify all Jobs (running and suspended) and their associated resources are cleaned up correctly.

5. (Backwards Compatibility) Create a standard JobSet without the annotation. Toggle the JobSet's `suspend` field and verify all child Jobs are correctly suspended or resumed, ensuring existing behavior is unbroken.

### Graduation Criteria

N/A

## Implementation History

- 2026-02-27: Initial issue opened requesting the feature.

## Drawbacks

Increases the API surface and complexity of the JobSet controller, suspension is now controlled by an external controller (e.g. Kueue) at the Job level and is not uniformly applied to all Jobs by JobSet controller.

## Alternatives

Use the RestartJob API to control restarting individual Jobs if workers fail naturally. This does not cover all cases, eg. if there is bad hardware, Kueue wants the ability to evict all pods off the nodes via Job suspension.
