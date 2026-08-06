# Updated PR Descriptions for Execution Attempts Tracking

---

## PR #1292: KEP-1282: Add Execution Attempts Tracking

### Summary
This PR introduces **KEP-1282**, proposing an `ExecutionAttempts` monotonic counter in `JobSetStatus` and propagating it as a pod annotation (`jobset.sigs.k8s.io/execution-attempt`). This mechanism enables external logging and telemetry systems (such as FluentBit) to cleanly slice, filter, and isolate log streams for different execution lifecycles of a JobSet—especially during queueing preemption and resumption cycles (e.g., via Kueue).

### Key Design Updates & PR Feedback Addressing

#### 1. Renaming to `ExecutionAttempts`
- **Field Name**: Renamed `executeAttempts` -> `executionAttempts` (`*int32`).
- **Annotation Key**: Renamed `jobset.sigs.k8s.io/execute-attempt` -> `jobset.sigs.k8s.io/execution-attempt`.
- **Justification**: Aligns grammatically with noun-based naming conventions across Kubernetes and JobSet APIs.

#### 2. Upgrade Semantics & Migration Logic (`nil` -> `restarts`)
- **Controller Upgrades with Running Workloads**: When the JobSet controller is upgraded while JobSets are actively running in the cluster, existing JobSets will initially have `Status.ExecutionAttempts == nil`.
- **Migration Rule**:
  - If `Status.ExecutionAttempts == nil` and the JobSet has already started executing (i.e., `Status.Restarts > 0` or active child Jobs exist), the controller initializes `Status.ExecutionAttempts` to the current `Status.Restarts` value.
  - If `Status.ExecutionAttempts == nil` and the JobSet is new (unsuspended on create, or first resumed from an initially suspended state without prior execution), it is initialized to `0`.
- **Justification**: Initializing running workloads to `Status.Restarts` ensures that the invariant `ExecutionAttempts >= Restarts` is always preserved and prevents resetting the execution attempt count to `0` mid-run for actively executing JobSets.

#### 3. Idempotency & Invariant Guarantees
- **Monotonic Invariant**: Guarantees that `ExecutionAttempts >= Restarts` throughout the entire JobSet lifecycle.
- **Increment Idempotency**: `ExecutionAttempts` increments **exactly once per lifecycle event** (once per `suspend -> unsuspend` transition, and once per failure-triggered restart). It is independent of the number of child Jobs and is immune to duplicate increments from controller reconciliation retries or requeues.

#### 4. Atomicity of Updates on Resume
- When resuming a suspended JobSet, the controller updates the `jobset.sigs.k8s.io/execution-attempt` annotation on child Jobs and Pod templates **before or atomically with** clearing `spec.suspend` (`spec.suspend = false`). This prevents race conditions where child Pods could be created with stale attempt annotations before the controller finishes updating them.

#### 5. Feature Gate Disabled Behavior
- The functionality is guarded by the `ExecutionAttemptsTracking` alpha feature gate.
- When `ExecutionAttemptsTracking` is disabled, the controller leaves `Status.ExecutionAttempts` unset (`nil`) and does not inject the annotation into child Jobs or Pod templates.

---

## PR #1283: feat: Add ExecutionAttempts monotonic counter to JobSet

### Summary
This PR implements **KEP-1282**, introducing the `ExecutionAttempts` field to `JobSet.Status` and propagating the `jobset.sigs.k8s.io/execution-attempt` annotation down to child Jobs and their Pod templates. This provides a monotonic execution token that increments across both failure-triggered restarts and suspend/resume cycles, allowing log aggregation and telemetry systems to isolate individual run iterations cleanly.

### Key Changes & Implementation Details

#### 1. Codebase Renaming (`ExecuteAttempts` -> `ExecutionAttempts`)
- Renamed API struct fields, JSON tags, constants, feature gate references, and comments across the codebase from `ExecuteAttempts` -> `ExecutionAttempts` and `execute-attempt` -> `execution-attempt`.
- Updated all SDK generators, CRD definitions, OpenAPI schemas, and generated deepcopy methods.

#### 2. Upgrade Migration Logic in Controller
- In `pkg/controllers/jobset_controller.go` (`resumeJobsIfNecessary`), added migration logic for reconciling JobSets with `Status.ExecutionAttempts == nil`:
  - **Running Workloads (`nil` -> `restarts`)**: If `Status.Restarts > 0` or active child Jobs exist, `Status.ExecutionAttempts` is initialized to `Status.Restarts`. This maintains baseline history and prevents attempt counts from resetting to `0` during controller upgrades.
  - **New Workloads**: Initialized to `0` when execution first begins.

#### 3. Idempotency & Invariant Enforcement
- **Idempotency**: Leveraged condition transition checks (`setJobSetResumedCondition` returning `true` only on state change from suspended to unsuspended) to ensure increment operations execute exactly once per event, preventing drift or double-counting during transient reconciliation errors.
- **Invariant Guarantee**: Strictly enforces `ExecutionAttempts >= Restarts`.

#### 4. Atomicity on Resume & Downward Propagation
- During child Job construction (`constructJob`) and resumption (`resumeJob`), the controller injects `jobset.sigs.k8s.io/execution-attempt` into child Jobs and Pod templates prior to clearing `spec.suspend = false`.
- Ensures both newly created jobs and existing resumed jobs (including those with mutated pod templates from Kueue) receive the correct attempt token.

#### 5. Test Verification & Coverage
- **Unit Tests (`pkg/controllers/jobset_controller_test.go`)**: Added test coverage for upgrade migration (`nil` -> `restarts` vs. `nil` -> `0`), increment idempotency, and feature gate disabled behavior.
- **Integration Tests (`test/integration/controller/jobset_controller_test.go`)**:
  - Added an upgrade migration test verifying that a running JobSet with `Restarts > 0` and `nil` attempts migrates to `Restarts` upon resumption.
  - Verified idempotency across multiple suspend/resume cycles.
  - Verified feature gate disabled behavior.
