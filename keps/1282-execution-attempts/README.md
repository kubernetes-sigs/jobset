# KEP-1282: Add Execution Attempts Tracking

<!-- toc -->
- [Summary](#summary)
- [Motivation](#motivation)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Proposal](#proposal)
  - [Example](#example)
- [Design Details](#design-details)
  - [API Changes](#api-changes)
  - [Controller Logic](#controller-logic)
    - [1. Initialization](#1-initialization)
    - [2. Increment on resume](#2-increment-on-resume)
    - [3. Increment on restart](#3-increment-on-restart)
    - [4. Data propagation](#4-data-propagation)
- [Test Plan](#test-plan)
  - [Unit Tests](#unit-tests)
  - [Integration Tests](#integration-tests)
<!-- /toc -->

## Summary

This KEP proposes adding an `ExecuteAttempts` counter to the JobSet status and propagating it as a pod annotation (`jobset.sigs.k8s.io/execute-attempt`). This allows external logging and telemetry systems to identify and isolate log streams for different execution cycles of a JobSet, especially when it is repeatedly suspended and resumed (e.g., by Kueue).

## Motivation

Logging deployment streams currently rely on the `jobset.sigs.k8s.io/restart-attempt` annotation to tag logs per execution cycle. However, when queueing workloads (e.g., via Kueue), JobSets are frequently evicted via suspension (terminating pods) and later resumed (spawning new pods).

Because JobSet resumptions do not increment the `restart-attempt` counter (which only increments during failure policy evaluations), logs from multiple distinct execution phases share an identical identifier. Consequently, users cannot properly isolate log streams by their scheduling lifecycle, severely impeding debugging efforts (e.g., isolating logs to investigate node-level hardware failures during a specific execution pass).

### Goals

- Provide a persistent, monotonic counter that increments on **both** failure-triggered restarts and suspension-driven resumptions.  
- Inject this new metric as a pod annotation to allow external logging systems (like FluentBit) to cleanly slice log iterations.

### Non-Goals

- Alter the lifecycle definitions for `jobset.sigs.k8s.io/restart-attempt`. The existing restart tracking acts strictly on failure conditions and will remain unchanged.

## Proposal

Introduce a new pod annotation (`jobset.sigs.k8s.io/execute-attempt`) to the JobSet API to track the total number of execution iterations a JobSet has undergone.

To track this robustly across controller restarts, a new state field `executeAttempts` will be introduced within `JobSet.status`.

### Example

1. **Initial Scheduled Run:** A JobSet starts. Pods land on Nodepools A and B. Both `restart-attempt` and `execute-attempt` evaluate to `"0"`.  
2. **Preemption (Eviction):** Kueue evicts the JobSet (`suspend: true`). Active pods are deleted.  
3. **Resumed Execution:** Kueue readmits the JobSet (`suspend: false`). New pods land on Nodepools C and D.  
   * Current state: `restart-attempt` remains `"0"`.  
   * Proposed state: `execute-attempt` increments to `"1"`.

The new annotation provides a distinct monotonic execution token to filter telemetry data accurately.

## Design Details

### API Changes

Add `ExecuteAttempts` to `JobSetStatus` struct:

```go
type JobSetStatus struct {
    ...
	// executeAttempts tracks the number of execution lifecycles.
	// +optional
	ExecuteAttempts *int32 `json:"executeAttempts,omitempty"`
    ...
}
```

We use a pointer `*int32` to allow the field to be `nil` initially.
*   `nil` indicates the JobSet has never run.
*   `0` indicates the first run.
*   `>0` indicates subsequent resumptions or restarts.

Define a new constant for the annotation key:
```go
const (
    ...
	// ExecuteAttemptsKey is the annotation key for the execution attempt count.
	ExecuteAttemptsKey = "jobset.sigs.k8s.io/execute-attempt"
)
```

### Controller Logic

#### 1. Initialization
When the JobSet starts executing for the first time (either because it was created unsuspended, or when it is first resumed from an initially suspended state), the controller initializes `Status.ExecuteAttempts` to `0`.

#### 2. Increment on resume
When the JobSet transitions from suspended to unsuspended:
*   If `Status.ExecuteAttempts` is not `nil`, it is incremented by 1.
*   The suspended condition is updated to `false`.

#### 3. Increment on restart
When a failure policy triggers a recreate of all jobs (which is a restart):
*   `Status.ExecuteAttempts` is incremented by 1.

#### 4. Data propagation
During child Job creation (in `constructJob`) and during Job resumption (in `resumeJob`), the controller injects the current `Status.ExecuteAttempts` value (defaulting to `0` if nil) into the Job's annotations and its Pod template annotations using the `jobset.sigs.k8s.io/execute-attempt` key.
This ensures both newly created jobs and existing resumed jobs (which might have updated templates from Kueue) receive the correct annotation.

## Test Plan

### Unit Tests
*   Verify `ExecuteAttempts` initialization and increment logic in controller unit tests (`pkg/controllers/jobset_controller_test.go`).
*   Verify annotation propagation in job construction tests.

### Integration Tests
*   Verify suspend/resume cycles increment `ExecuteAttempts` and propagate annotations to child Jobs and Pods (`test/integration/controller/jobset_controller_test.go`).
*   Verify failure-triggered restarts increment `ExecuteAttempts`.
*   Verify that initially suspended JobSet starts with `ExecuteAttempts` unset and initializes to `0` on first resume.
