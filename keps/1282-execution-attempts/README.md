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
    - [1. Initialization and Upgrade Migration](#1-initialization-and-upgrade-migration)
    - [2. Increment on resume (with Idempotency)](#2-increment-on-resume-with-idempotency)
    - [3. Increment on restart (with Idempotency)](#3-increment-on-restart-with-idempotency)
    - [4. Atomicity of Updates on Resume and Data Propagation](#4-atomicity-of-updates-on-resume-and-data-propagation)
    - [5. Feature Gate Disabled Behavior](#5-feature-gate-disabled-behavior)
- [Test Plan](#test-plan)
  - [Unit Tests](#unit-tests)
  - [Integration Tests](#integration-tests)
<!-- /toc -->

## Summary

This KEP proposes adding an `ExecutionAttempts` counter to the JobSet status and propagating it as a pod annotation (`jobset.sigs.k8s.io/execution-attempt`). This allows external logging and telemetry systems to identify and isolate log streams for different execution cycles of a JobSet, especially when it is repeatedly suspended and resumed (e.g., by Kueue).

## Motivation

Logging deployment streams currently rely on the `jobset.sigs.k8s.io/restart-attempt` annotation to tag logs per execution cycle. However, when queueing workloads (e.g., via Kueue), JobSets are frequently evicted via suspension (terminating pods) and later resumed (spawning new pods).

Because JobSet resumptions do not increment the `restart-attempt` counter (which only increments during failure policy evaluations), logs from multiple distinct execution phases share an identical identifier. Consequently, users cannot properly isolate log streams by their scheduling lifecycle, severely impeding debugging efforts (e.g., isolating logs to investigate node-level hardware failures during a specific execution pass).

### Goals

- Provide a persistent, monotonic counter that increments on **both** failure-triggered restarts and suspension-driven resumptions.  
- Inject this new metric as a pod annotation (`jobset.sigs.k8s.io/execution-attempt`) to allow external logging systems (like FluentBit) to cleanly slice log iterations.
- Support seamless upgrades of the JobSet controller while JobSets are running in the cluster without resetting attempt counts.

### Non-Goals

- Alter the lifecycle definitions for `jobset.sigs.k8s.io/restart-attempt`. The existing restart tracking acts strictly on failure conditions and will remain unchanged.

## Proposal

Introduce a new pod annotation (`jobset.sigs.k8s.io/execution-attempt`) to the JobSet API to track the total number of execution iterations a JobSet has undergone.

To track this robustly across controller restarts, a new state field `executionAttempts` will be introduced within `JobSet.status`.

### Example

1. **Initial Scheduled Run:** A JobSet starts. Pods land on Nodepools A and B. Both `restart-attempt` and `execution-attempt` evaluate to `"0"`.  
2. **Preemption (Eviction):** Kueue evicts the JobSet (`suspend: true`). Active pods are deleted.  
3. **Resumed Execution:** Kueue readmits the JobSet (`suspend: false`). New pods land on Nodepools C and D.  
   * Current state: `restart-attempt` remains `"0"`.  
   * Proposed state: `execution-attempt` increments to `"1"`.

The new annotation provides a distinct monotonic execution token to filter telemetry data accurately.

## Design Details

### API Changes

Add `ExecutionAttempts` to `JobSetStatus` struct:

```go
type JobSetStatus struct {
    ...
	// executionAttempts tracks the number of execution lifecycles.
	// +optional
	ExecutionAttempts *int32 `json:"executionAttempts,omitempty"`
    ...
}
```

We use a pointer `*int32` to allow the field to be `nil` initially.
*   `nil` indicates the JobSet has never run, or the feature gate is disabled.
*   `0` indicates the first run.
*   `>0` indicates subsequent resumptions or restarts.

Define a new constant for the annotation key:
```go
const (
    ...
	// ExecutionAttemptsKey is the annotation key for the execution attempt count.
	ExecutionAttemptsKey = "jobset.sigs.k8s.io/execution-attempt"
)
```

### Controller Logic

#### 1. Initialization and Upgrade Migration
When the JobSet controller reconciles a JobSet where `Status.ExecutionAttempts` is `nil`, it distinguishes between a new JobSet and an existing running JobSet (e.g., during a controller upgrade while workloads are actively running in the cluster):
*   **Upgrade Semantics for Running Workloads:** If `Status.ExecutionAttempts` is `nil` but the JobSet has already started executing (i.e., `Status.Restarts > 0` or active child Jobs exist), the controller initializes `Status.ExecutionAttempts` to the current `Status.Restarts` value. This migration rule ensures that existing workloads maintain the invariant `ExecutionAttempts >= Restarts` and prevents resetting the attempt count to `0` mid-run after a controller upgrade.
*   **New Workloads:** If `Status.ExecutionAttempts` is `nil` and the JobSet has not started executing yet (new JobSet created unsuspended, or first resumed from an initially suspended state), the controller initializes `Status.ExecutionAttempts` to `0`.

#### 2. Increment on resume (with Idempotency)
When the JobSet transitions from suspended to unsuspended (`spec.suspend` transitions from `true` to `false`):
*   If `Status.ExecutionAttempts` is not `nil`, it is incremented by 1.
*   The suspended condition is updated to `false`.
*   **Idempotency:** The increment operation is idempotent and occurs **exactly once per event** (once per suspend->unsuspend transition), regardless of the number of child Jobs or how many times reconciliation is retried due to transient conflicts or requeues.

#### 3. Increment on restart (with Idempotency)
When a failure policy triggers a recreate of all jobs (which is a restart):
*   `Status.ExecutionAttempts` is incremented by 1.
*   **Idempotency:** The increment occurs **exactly once per failure restart event**, regardless of the number of child Jobs or reconciliation retries.

#### 4. Atomicity of Updates on Resume and Data Propagation
*   **Atomicity on Resume:** When resuming a suspended JobSet, the controller MUST update the `jobset.sigs.k8s.io/execution-attempt` annotation on child Jobs and Pod templates **before or atomically with** clearing `spec.suspend` (`spec.suspend = false`). This prevents any race condition where child Pods are created with stale attempt annotations before the controller has updated them.
*   **Data Propagation:** During child Job creation (in `constructJob`) and during Job resumption (in `resumeJob`), the controller injects the current `Status.ExecutionAttempts` value (defaulting to `0` if nil) into the Job's annotations and its Pod template annotations using the `jobset.sigs.k8s.io/execution-attempt` key. This ensures both newly created jobs and existing resumed jobs (which might have updated templates from Kueue) receive the correct annotation.

#### 5. Feature Gate Disabled Behavior
The execution attempts tracking feature is controlled by the `ExecutionAttemptsTracking` feature gate (alpha).
*   When the `ExecutionAttemptsTracking` feature gate is **disabled**, the controller will leave `Status.ExecutionAttempts` unset (`nil`) and will not inject or update the `jobset.sigs.k8s.io/execution-attempt` annotation on child Jobs or Pods.

## Test Plan

### Unit Tests
*   Verify `ExecutionAttempts` initialization and upgrade migration logic (`nil` -> `restarts` when active jobs or restarts exist) in controller unit tests (`pkg/controllers/jobset_controller_test.go`).
*   Verify increment idempotency across repeated reconciliation attempts.
*   Verify annotation propagation in job construction tests.
*   Verify feature gate disabled behavior (no status update, no annotation injection).

### Integration Tests
*   Verify suspend/resume cycles increment `ExecutionAttempts` exactly once per cycle and propagate annotations to child Jobs and Pods (`test/integration/controller/jobset_controller_test.go`).
*   Verify failure-triggered restarts increment `ExecutionAttempts`.
*   Verify that an existing running JobSet with `nil` `ExecutionAttempts` initializes to `Status.Restarts` upon reconciliation.
*   Verify that initially suspended JobSet starts with `ExecutionAttempts` unset and initializes to `0` on first resume.
