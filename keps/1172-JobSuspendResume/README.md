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

This feature is necessary to support advanced scheduling and placement optimizations in Kueue, particularly around topology awareness. 

If the underlying topology of a specific Job's placement becomes fragmented or broken, Kueue needs the ability to independently relocate (recreate) that single Job. Currently, lacking granular control, adjusting the placement of one Job disrupts the others in the JobSet. By enabling individual Job suspension, Kueue will be able to suspend a specific Job, safely recreate it in a new topological location, and resume it, all without affecting the uninterrupted execution of the other sibling Jobs in the JobSet.

### Goals

- Introduce fine-grained suspend and resume capabilities at the individual Job level within a JobSet.
- Provide a mechanism to suspend and resume specific child Jobs independently.
- Ensure the state of other running Jobs in the JobSet remains unaffected during these operations.

### Non-Goals

- Modifying the suspension behavior of the entire JobSet (this is already supported).
- Changing the underlying Kubernetes Job behavior itself beyond what JobSet controls.

## Proposal

We propose adding a mechanism (e.g., via an API field or annotation in the JobSet) to individually target specific Jobs within a JobSet and control their suspension state without changing the overall JobSet suspension state.

### User Stories (Optional)

#### Story 1
As a user utilizing Kueue for topology-aware scheduling, I want to relocate a single fragmented Job to a better topological location without killing or pausing the other jobs in my JobSet, so that large-scale parallel processing is more resilient to location constraints.

### Notes/Constraints/Caveats (Optional)

None currently identified.

### Risks and Mitigations

The primary risk is introducing complexity to the JobSet controller, as it now has to manage the suspension state of individual jobs rather than just propagating the JobSet's suspension down to all jobs uniformly. This will be mitigated by thorough unit and integration testing.

## Design Details

Initially we will support this via an annotation on the JobSet: `jobset.sigs.k8s.io/skip-suspend-reconciliation`. If this annotation is set, then the JobSet webhook enforces `jobSet.spec.suspend = nil and does not reconcile the field job.spec.suspend of child Jobs.

If this annotation is set:
1. The JobSet webhook will enforce `jobSet.spec.suspend = nil`.
2. The JobSet controller will not reconcile the `job.spec.suspend` field of child Jobs.

Based on usage & feedback, we will evaluate if we should modify the JobSet API to control this behavior.

We cannot use `jobSet.spec.suspend = nil` as a signal to not reconcile the field `job.spec.suspend` of child Jobs becuase the JobSet controller currently treats `jobSet.spec.suspend = nil` as a synonym for `jobSet.spec.suspend = false`. If users are unsuspending JobSets by setting `jobSet.spec.suspend = nil` instead of `false`, this change would break their workflow. 

This simple annotation-based solution gives the use case time to evolve before proposing a more complete API (such as an `updatePolicy` field in the future).

#### Future API Evolution

If the `jobset.sigs.k8s.io/skip-suspend-reconciliation` annotation proves successful, we may promote this configuration to a strongly-typed API field within the `JobSetSpec`. A dedicated policy field (e.g. `UpdatePolicy`) would cleanly move this behavior out of annotations, providing better validation, discoverability via OpenAPI schemas, and extensibility if other child Job fields need decoupled reconciliation in the future.

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
    // SuspendReconciliation controls whether the JobSet controller actively 
    // reconciles the `suspend` field of child Jobs to match the JobSet.
    // Valid values are "Sync" (default) and "Ignore".
    // +optional
    SuspendReconciliation *SuspendReconciliationMode `json:"suspendReconciliation,omitempty"`
}

type SuspendReconciliationMode string

const (
    // SuspendReconciliationSync ensures child Jobs are suspended/resumed 
    // exactly matching the JobSetSpec.Suspend field.
    SuspendReconciliationSync SuspendReconciliationMode = "Sync"
    
    // SuspendReconciliationIgnore means the JobSet controller will not attempt to 
    // modify the `suspend` field of existing child Jobs, allowing external controllers
    // (like Kueue) to manage them individually.
    SuspendReconciliationIgnore SuspendReconciliationMode = "Ignore"
)
```

### Test Plan

[ x] I/we understand the owners of the involved components may require updates to
existing tests to make this code solid enough prior to committing the changes necessary
to implement this enhancement.

##### Prerequisite testing updates


#### Unit Tests

We will add unit tests to the following packages to ensure the new `skip-suspend-reconciliation` annotation is respected:

- `jobset/pkg/controllers`: Test that the JobSet controller does not modify `job.spec.suspend` on existing child Jobs when the annotation is present.
- `jobset/pkg/webhooks`: Test that the JobSet mutating webhook successfully defaults or forces `jobSet.spec.suspend = nil` if the annotation is set.

#### Integration tests

1. Create a JobSet with the `jobset.sigs.k8s.io/skip-suspend-reconciliation` annotation and verify the webhook enforces `jobSet.spec.suspend = nil`.
2. Create a JobSet with the annotation, wait for child Jobs to be created, then manually toggle `suspend: true` and `suspend: false` on specific child Jobs via Kueue. Verify the JobSet controller does not revert these changes.
3. (Backwards Compatibility) Create a standard JobSet without the annotation. Toggle the JobSet's `suspend` field and verify all child Jobs are correctly suspended or resumed, ensuring existing behavior is unbroken.

### Graduation Criteria

N/A

## Implementation History

- 2026-02-27: Initial issue opened requesting the feature.

## Drawbacks

Increases the API surface and complexity of the JobSet controller, Job suspension is now managed by an external controller (e.g. Kueue).
