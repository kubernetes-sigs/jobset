# KEP-1186: Add ActiveDeadlineSeconds for JobSet

<!-- toc -->
- [Summary](#summary)
- [Motivation](#motivation)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Proposal](#proposal)
  - [User Stories](#user-stories)
    - [Story 1](#story-1)
    - [Story 2](#story-2)
  - [Notes/Constraints/Caveats](#notesconstraintscaveats)
  - [Risks and Mitigations](#risks-and-mitigations)
- [Design Details](#design-details)
  - [API Changes](#api-changes)
  - [Semantics](#semantics)
    - [1. When the timer starts](#1-when-the-timer-starts)
    - [2. Interaction with suspend](#2-interaction-with-suspend)
    - [3. Interaction with failurePolicy restarts](#3-interaction-with-failurepolicy-restarts)
      - [What Job does for activeDeadlineSeconds and backoffLimit](#what-job-does-for-activedeadlineseconds-and-backofflimit)
  - [Controller Logic](#controller-logic)
  - [Feature Gate Disabled Behavior](#feature-gate-disabled-behavior)
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

This KEP proposes adding an optional `spec.activeDeadlineSeconds` field to the
JobSet API. It bounds the total time a JobSet may be continuously active. When
the deadline is exceeded, the JobSet controller marks the JobSet as `Failed`
with a dedicated reason and deletes the active child Jobs, matching
`batch/v1.Job.spec.activeDeadlineSeconds`.

The timer is measured from a new `.status.startTime`, does not run while the
JobSet is suspended, and resets on resume.

## Motivation

JobSet has no way to bound total execution time. A distributed training or HPC
run that hangs (deadlocked collective, stuck rendezvous, wedged data loader)
can hold expensive accelerators indefinitely until a human notices. `batch/v1`
Jobs solve this with `activeDeadlineSeconds`; JobSet users expect the same
guardrail at the JobSet level.

`ttlSecondsAfterFinished` already exists, but it only cleans up after a JobSet
finishes. It cannot force an unfinished, wedged JobSet to stop. This KEP covers
that case.

The deadline targets a JobSet that is continuously active but making no
progress (a hang). Crash-loops are already bounded by `maxRestarts`, and each
global restart or resume starts a fresh run. So the deadline and `maxRestarts`
cover different failure modes: the deadline catches a single wedged attempt,
`maxRestarts` caps how many times the JobSet restarts.

### Goals

- Add `spec.activeDeadlineSeconds` to bound the continuous active runtime of a
  JobSet.
- On expiry, transition the JobSet to a terminal `Failed` state with a distinct
  condition reason and delete active child Jobs.
- Do not accrue the deadline while the JobSet is suspended.
- Ship behind a feature gate, disabled by default in alpha.

### Non-Goals

- Per-`ReplicatedJob` or per-Job deadlines. Users can already set
  `activeDeadlineSeconds` on the Job template inside a `ReplicatedJob` for
  per-Job bounds; this KEP is the JobSet-level aggregate deadline.
- Changing `ttlSecondsAfterFinished` semantics or the failure policy restart
  counters.
- A "deadline exceeded → restart" action. Expiry is terminal `Failed`.

## Proposal

Introduce `spec.activeDeadlineSeconds *int64` and a `.status.startTime
*metav1.Time`. The controller measures elapsed active time from `startTime`; on
each reconcile it either requeues for the remaining duration or, if the deadline
has passed, fails the JobSet and deletes its active Jobs.

### User Stories

#### Story 1

As an ML platform operator, I run large training JobSets on scarce GPUs. If a
run wedges (NCCL deadlock, stuck checkpoint restore), I want it to be
force-failed after N hours so the accelerators are freed and my queue
(e.g. Kueue) can schedule the next workload, without manual intervention.

#### Story 2

As a batch/HPC user, I submit JobSets with a known worst-case runtime per
attempt. I set `activeDeadlineSeconds` so a single continuously-active run that
overruns its budget is failed instead of hanging. The bound applies to one
active attempt, not to total wall-clock across suspends and restarts (see
Notes).

### Notes/Constraints/Caveats

- The deadline is continuous active time, not wall-clock since creation:
  suspended intervals do not count. A Kueue-managed JobSet is frequently
  suspended and resumed.
- `activeDeadlineSeconds` is independent of `maxRestarts`. A JobSet can be
  failed by exhausting restarts or by exceeding the deadline. When a child-Job
  failure and an elapsed deadline are observed in the same reconcile, the
  deadline is evaluated first, so the JobSet fails with `DeadlineExceeded`. This
  is deliberate: the failure policy may restart the JobSet (`RestartJobSet`
  resets `startTime`; `RestartJob` returns early), which would let a wedged or
  over-budget workload bypass the deadline. Checking the deadline before the
  failure policy closes that bypass. A genuinely completed JobSet still wins over
  the deadline (success policy runs first).
- The bound is per continuous-active attempt, not a JobSet lifetime cap.
  Resume and global restart (`RestartJobSet`) each begin a fresh run and reset
  the timer. `maxRestarts` bounds the number of global restart attempts.
- `RestartJobSetAndIgnoreMaxRestarts` bypasses `maxRestarts`, so a JobSet driven
  only by that action can restart without limit and receive a fresh per-attempt
  deadline each time.
- The timer starts at first unsuspend, before pods are ready. Time spent waiting
  for startup ordering (`DependsOn`, coordinator, leader readiness) counts
  against the deadline, so a JobSet stuck in startup is failed once the deadline
  passes.
- `.status.startTime` is a JobSet start timestamp modeled on upstream
  `batch/v1.Job`: it is set whenever the JobSet is active (unsuspended), cleared
  on suspend, and reset on resume, **independent of whether
  `activeDeadlineSeconds` is set**. It is, however, **maintained only while the
  `JobSetActiveDeadlineSeconds` feature gate is enabled** — the reconcile loop
  skips `startTime` maintenance entirely when the gate is off, so the alpha
  feature has no status side effects while disabled (changed in review from the
  original always-on design). Ecosystem tools (CLI printers, dashboards) can
  rely on it as the JobSet's active-start time whenever the gate is on.
- **Externally managed JobSets (`spec.managedBy`).** When `managedBy` names a
  controller other than the built-in `jobset.sigs.k8s.io/jobset-controller`, the
  built-in controller skips reconciliation entirely (it only runs
  `ttlSecondsAfterFinished` cleanup once a terminal condition exists, matching
  existing behavior). Both `.status.startTime` maintenance and the
  `activeDeadlineSeconds` check run *after* that `managedByExternalController`
  early-return in `Reconcile`, so the built-in controller neither sets
  `startTime` nor enforces the deadline for an externally managed JobSet. The
  managing controller (e.g. MultiKueue) owns `startTime` and deadline
  enforcement, exactly as it already owns child-Job and status management. This
  falls out of the existing skip; no extra gating code is added. An integration
  test validates this assumption (see [Integration tests](#integration-tests)).

### Risks and Mitigations

- **Clock skew** between controller reconciles: handled the same way as the
  existing `ttl_after_finished.go`: comparisons use `clock.Clock`, and a
  start time in the future is logged and treated as not yet expired.
- **User confusion vs. Job-level `activeDeadlineSeconds`**: the two are
  complementary (JobSet aggregate vs. per-Job).
- **Interaction with in-flight restarts**: the restart and timer interaction is
  defined under [Semantics](#semantics).

## Design Details

### API Changes

Add to `JobSetSpec`:

```go
type JobSetSpec struct {
    ...
    // activeDeadlineSeconds is the maximum duration in seconds, relative to
    // status.startTime, that the JobSet may be continuously active before the
    // JobSet controller marks it Failed and deletes its active Jobs.
    //
    // The timer does not accrue while the JobSet is suspended: startTime is
    // cleared on suspend and reset when the JobSet resumes. The value must be a
    // positive integer. The field is mutable: operators may raise or lower the
    // deadline on a running JobSet, matching batch/v1.Job. The controller
    // recomputes the remaining time from status.startTime on the next reconcile,
    // so no timer state is reset on update.
    // +kubebuilder:validation:Minimum=1
    // +optional
    ActiveDeadlineSeconds *int64 `json:"activeDeadlineSeconds,omitempty"`
}
```

Add to `JobSetStatus`:

```go
// startTime is the timestamp from which activeDeadlineSeconds is measured, and
// also serves as the JobSet's active-start time. It is set when the JobSet
// first becomes active (unsuspended), cleared when the JobSet is suspended, and
// reset to the current time when the JobSet resumes, matching
// batch/v1.Job.status.startTime. It is maintained only while the
// JobSetActiveDeadlineSeconds feature gate is enabled, independent of whether
// activeDeadlineSeconds is set.
// +optional
StartTime *metav1.Time `json:"startTime,omitempty"`
```

For observability, `.status.startTime` is exposed as a printer column. It uses
`type=date` (not `type=string`) so `kubectl` renders it as a relative age,
consistent with the existing `Age` column:

```go
// +kubebuilder:printcolumn:name="StartTime",type=date,JSONPath=`.status.startTime`,description="Time the JobSet became active"
```

Validation:

- `+kubebuilder:validation:Minimum=1` enforces a positive value at the API
  server; no webhook code is needed for the range check.
- Feature-gate guard: a validating webhook rejects *setting or changing*
  `activeDeadlineSeconds` while the `JobSetActiveDeadlineSeconds` gate is
  disabled, on both create and update. An unchanged value is left alone (so a
  JobSet created while the gate was on can still be updated), and clearing the
  field is always allowed. This keeps the alpha field from being introduced
  while the feature is off. (Added in response to review; see
  [Implementation History](#implementation-history).)
- Mutability: the field is mutable, matching `batch/v1.Job`, where
  `activeDeadlineSeconds` is in the allowed set for spec updates
  (`ValidateJobSpecUpdate`). Operators and platform tools may raise or lower the
  deadline on a running JobSet without restarting it, and may add it to a JobSet
  that did not set it. Because the controller computes the remaining time
  dynamically from `.status.startTime` on each reconcile, an update takes effect
  on the next reconcile with no extra state to track: the new deadline is
  `startTime + activeDeadlineSeconds` evaluated against the current time.
  Lowering the value below the already-elapsed active time fails the JobSet on
  the next reconcile; removing the field stops enforcement. No CEL transition
  rules are needed.

### Semantics

These three decisions were discussed on the issue thread and are resolved here
to match upstream `batch/v1.Job`. The upstream behavior below was verified
against `k8s.io/kubernetes/pkg/controller/job/job_controller.go` (functions
`syncJob` and `pastActiveDeadline`).

#### 1. When the timer starts

The timer starts from a new `.status.startTime`, set when the JobSet first
becomes active (unsuspended), not from `metadata.creationTimestamp`.

Upstream sets `.status.startTime` on first start only when the Job is not
suspended:

```go
// job_controller.go, syncJob
if job.Status.StartTime == nil && !jobSuspended(&job) {
    now := metav1.NewTime(jm.clock.Now())
    job.Status.StartTime = &now
}
```

Using `creationTimestamp` is simpler but cannot express "does not run while
suspended," which JobSets under Kueue require.

#### 2. Interaction with suspend

The deadline measures continuous active time:

- On suspend: clear `.status.startTime` (set to `nil`).
- On resume: set `.status.startTime = now`.
- While suspended: the deadline check short-circuits and never fails the JobSet.

Upstream `syncJob` does the same, with the inline comment
*"ActiveDeadlineSeconds is interpreted as the number of seconds a Job is
continuously active."* Upstream gates the suspend-time nil-out behind the
`MutableSchedulingDirectivesForSuspendedJobs` gate, which is now GA. JobSet has
no equivalent gate, so it clears `startTime` on suspend unconditionally (still
behind the JobSet feature gate for this feature).

#### 3. Interaction with failurePolicy restarts

JobSet has two restart granularities and they are handled differently:

- **`RestartJob` / `RestartJobAndIgnoreMaxRestarts`** (single Job): only the
  failed Job is recreated (`failurePolicyRecreateJob`); the global generation
  `Status.Restarts` is untouched. This is analogous to a Job `backoffLimit`
  retry, which does not reset the upstream timer. Therefore `startTime` is left
  unchanged.

- **`RestartJobSet` / `RestartJobSetAndIgnoreMaxRestarts`** (global): increments
  `Status.Restarts` and recreates all child Jobs, which is a fresh run like
  resume. Therefore `startTime` is reset to now.

##### What Job does for activeDeadlineSeconds and backoffLimit

This answers the question raised on the issue thread. In upstream `batch/v1.Job`
(`job_controller.go`, `syncJob`):

- `activeDeadlineSeconds` and `backoffLimit` are independent failure
  conditions. Both terminate the Job as `Failed`.
- When a pod fails and the Job retries it (up to `backoffLimit`), the retry does
  not reset `.status.startTime`. The deadline therefore spans the whole
  continuous-active period, across all pod retries. `startTime` is set once on
  first non-suspended start and only reset on resume.
- When both conditions are reached in the same sync, `backoffLimit` is evaluated
  first, then `activeDeadlineSeconds`:

```go
// job_controller.go, syncJob: evaluate failure scenarios
if exceedsBackoffLimit || pastBackoffLimitOnFailure(&job, pods) {
    finishedCondition = backoffLimitExceeded
} else if jm.pastActiveDeadline(&job) {
    finishedCondition = deadlineExceeded
}
```

Mapping to JobSet: `maxRestarts` is the JobSet analog of `backoffLimit`, and
single-Job `RestartJob` retries are the analog of pod-level backoff retries.
`RestartJob` retries do not reset the timer.

JobSet **diverges from upstream ordering here on purpose.** Upstream evaluates
`backoffLimit` before `activeDeadlineSeconds`, which is safe because a
`backoffLimit`-exceeded Job is *terminally failed* — no restart, the timer is
never reset, so the order only affects the reported reason. JobSet's failure
policy is different: it can *restart* (`RestartJobSet` resets `startTime`;
`RestartJob` returns early), which would reset or bypass the deadline entirely.
So JobSet evaluates the deadline **before** the failure policy: an expired
JobSet fails with `DeadlineExceeded` instead of being restarted, and only a
non-expired JobSet reaches the failure policy (where `maxRestarts` is enforced
as usual). The success policy still runs first, so a completed JobSet is never
failed by the deadline.

Summary of alpha semantics:

| Event                     | `.status.startTime` |
|---------------------------|---------------------|
| First unsuspended start   | set to now          |
| Suspend                   | cleared (nil)       |
| Resume                    | reset to now        |
| `RestartJob` (single)     | unchanged           |
| `RestartJobSet` (global)  | reset to now        |
| Deadline exceeded         | JobSet failed       |

### Controller Logic

Two pieces of behavior are added to `JobSetReconciler.Reconcile`
(`pkg/controllers/jobset_controller.go`): `startTime` maintenance and the
deadline check.

**Placement in the reconcile loop.** The existing order is: `jobSetFinished`
early-exit (cleanup + TTL requeue) → failure policy (early return) → success
policy (early return on completion) → `syncExecutionAttempts` →
`reconcileReplicatedJobs` → suspend/resume handling. This feature adds
`startTime` maintenance at the top of the loop (right after the `jobSetFinished`
early-exit) so `.status.startTime` is populated before any deadline or failure
evaluation, then — for JobSets that set `activeDeadlineSeconds` — reorders the
middle so the deadline check sits between the success and failure policies:
`jobSetFinished` → **`startTime` maintenance** →
**success policy** (early return on completion) → **deadline check** (early
return on expiry) → **failure policy** → `syncExecutionAttempts` → … Running
`startTime` maintenance first guarantees `StartTime` is set on the first
unsuspended reconcile, so the deadline check in the same reconcile has a valid
basis. The success policy runs before the deadline check, so a completed JobSet
is never failed by the deadline. The deadline check runs before the failure
policy, so an expired JobSet fails with `DeadlineExceeded` rather than being
restarted by the failure policy (which would reset `startTime` via
`RestartJobSet` or return early via `RestartJob`, bypassing the deadline).
Because `jobSetFinished` short-circuits at the top of the loop, an
already-finished JobSet is never re-failed by the deadline. All of this sits
below the `managedByExternalController` early-return, so for an externally
managed JobSet neither `startTime` maintenance nor the deadline check runs (see
[Notes/Constraints/Caveats](#notesconstraintscaveats)).

**`startTime` maintenance** is gated by the `JobSetActiveDeadlineSeconds` feature
gate but not by `activeDeadlineSeconds`: while the gate is on,
`.status.startTime` tracks the JobSet's active-start time whenever the JobSet is
active (whether or not `activeDeadlineSeconds` is set), matching upstream
`batch/v1.Job`; while the gate is off, `startTime` is not touched at all.
`startTime` is an idempotent timestamp, so maintenance does not track a
suspend→resume transition; it derives everything from the current suspend state
(`isSuspended = jobSetSuspended(js)`):

- Not suspended with `StartTime == nil`: set `StartTime = now`. This covers both
  the first unsuspended start and resume after suspend (suspend clears
  `StartTime`, so resume re-sets it on the next reconcile).
- Suspended (`isSuspended`): set `StartTime = nil`.
- Global restart: the reconcile loop calls `resetStartTimeOnGlobalRestart`,
  which sets `StartTime = now` when the failure policy bumped `Status.Restarts`
  in this reconcile (a global `RestartJobSet`), since it begins a fresh run. A
  single-Job `RestartJob` (which does not bump `Status.Restarts`) does not touch
  it.

Because the gate wraps `startTime` maintenance, disabling the gate makes the
feature fully inert (no `startTime` writes); only the deadline check reads
`startTime`, and both are gated. The success→deadline→failure reorder is
additionally scoped to JobSets that actually set `activeDeadlineSeconds`: a
JobSet that does not use the field keeps the original failure→success ordering
even when the gate is on, so enabling the cluster-wide gate never changes the
terminal outcome of a JobSet that does not use the feature.

All writes go through the existing `statusUpdateOpts` so they are persisted
atomically with the other status changes in that reconcile.

**Deadline check.** Pseudocode of a new `executeActiveDeadlinePolicy`, called
after the success-policy block:

```text
// returns (expired, requeueAfter, err)
if !features.Enabled(JobSetActiveDeadlineSeconds) { return false, 0, nil }
if js.Spec.ActiveDeadlineSeconds == nil { return false, 0, nil }  // unset
if jobSetSuspended(js) { return false, 0, nil }                   // paused while suspended
if js.Status.StartTime == nil { return false, 0, nil }            // not active yet

deadline  := js.Status.StartTime.Add(activeDeadline)
remaining := deadline.Sub(clock.Now())
if remaining > 0 {
    return false, remaining, nil   // requeue after remaining, wake exactly at expiry
}

// Deadline exceeded: delete active child Jobs first, then record the failure so
// the condition and metric are emitted only once resources are actually freed.
// A delete error retries from a not-yet-failed state, avoiding metric inflation.
if err := r.deleteJobs(ctx, ownedJobs.active); err != nil {
    return true, 0, err
}
setJobSetFailedCondition(js, DeadlineExceededReason,
    fmt.Sprintf("JobSet was active for longer than the specified deadline of %ds",
        *js.Spec.ActiveDeadlineSeconds), opts)
metrics.JobSetActiveDeadlineExceeded(js.Name, js.Namespace)
return true, 0, nil   // expired: caller returns early after the status update
```

- On expiry the JobSet is failed via the existing `setJobSetFailedCondition`
  helper (new reason `DeadlineExceededReason`), so it goes through the same
  terminal-state + metrics machinery as other failures. In the same reconcile,
  the active child Jobs are deleted via `r.deleteJobs(ctx, ownedJobs.active)`
  and `Reconcile` returns early, so accelerators are freed immediately and no
  later step re-syncs or recreates child Jobs after expiry. This matches
  upstream Job, which deletes active pods in the same sync that records the
  `DeadlineExceeded` failure. Setting the condition also makes `jobSetFinished`
  true, so any stragglers are still cleaned up by the finished-JobSet branch on
  a later pass.
- **Time-based requeue.** A wedged JobSet produces no child Job or Pod events,
  so the controller would otherwise never wake to enforce the deadline. When the
  deadline is set, the JobSet is active, and `remaining > 0`, the returned
  `remaining` is threaded into the reconcile's final
  `ctrl.Result{RequeueAfter: remaining}`, the same mechanism as the TTL policy.
  Child events in the meantime re-evaluate the remaining time; the requeue
  guarantees a wake at expiry.

New constant:

```go
// In pkg/constants/constants.go
DeadlineExceededReason = "DeadlineExceeded"
```

New metric:

```go
// jobset_active_deadline_exceeded_total, labeled by name/namespace
// (matching the existing metrics.JobSetFailed label convention),
// incremented once when a JobSet is failed due to the deadline.
```

### Feature Gate Disabled Behavior

Gated by `JobSetActiveDeadlineSeconds` (`featuregate.Alpha`, default `false`).
This follows the same gate-disabled convention as KEP-1282 (execution-attempts),
which gates its own `JobSet.status` field: the gate wraps the whole feature, so
both `.status.startTime` maintenance and the deadline check are skipped when the
gate is off and a disabled feature has no observable side effects. (This is a
change from the original 1186 design, where `startTime` was maintained
regardless of the gate; that design was the outlier versus 1282.)

- Gate off: `executeActiveDeadlinePolicy` is a no-op and `.status.startTime` is
  not touched — left `nil` for a JobSet that starts while the gate is off, and
  any existing value **preserved without further updates**. An
  `activeDeadlineSeconds` value already in the spec is retained (not stripped)
  but never enforced; the webhook prevents *adding or changing* the field while
  the gate is off.
- Gate on (or re-enabled): maintenance and the deadline check resume, and
  maintenance **resumes from the persisted `startTime`** when one exists.
  Maintenance derives `startTime` from the current suspend state, so a JobSet
  that was active (with a `startTime`) before the gate was turned off keeps that
  timestamp: if it has been continuously active past its deadline it is failed
  on the first reconcile after re-enable. A JobSet with no persisted `startTime`
  gets `startTime = now` and measures the deadline from there.
- Gate on to off downgrade: maintenance and enforcement stop; a JobSet already
  failed for the deadline stays failed (terminal state is immutable), and
  `startTime` is left at its last value (no further writes).

### Test Plan

[x] I/we understand the owners of the involved components may require updates to
existing tests to make this code solid enough prior to committing the changes
necessary to implement this enhancement.

#### Prerequisite testing updates

No prerequisite test changes are required. The existing failure-policy,
suspend/resume, and TTL controller tests already exercise the status-update and
requeue paths this feature builds on.

#### Unit Tests

- `pkg/controllers`: `executeActiveDeadlinePolicy` for the cases not set, not
  started, suspended, remaining > 0 (requeues, no status change), remaining <= 0
  (sets the `Failed` condition, deletes active child Jobs, and signals early
  return), and future startTime (clock skew) treated as not expired.
- `pkg/controllers`: `StartTime` is maintained whenever the gate is on,
  independent of `activeDeadlineSeconds`: set whenever the JobSet is active
  (including when the field is unset), cleared on suspend, reset on resume and on
  the global `resetStartTimeOnGlobalRestart` path. When the gate is off it is not
  maintained at all.
- `pkg/controllers` (reconcile-level): success beats an expired deadline in the
  same reconcile (Completed, not `DeadlineExceeded`); an expired deadline beats a
  simultaneous child failure (`DeadlineExceeded`, `Status.Restarts` unchanged);
  and a JobSet without `activeDeadlineSeconds` keeps the original
  failure→success ordering with the gate on (the reorder does not apply to
  non-users).
- `pkg/metrics` / `pkg/controllers`: `jobset_active_deadline_exceeded_total`
  increments by one when a JobSet is failed by the deadline.
- `pkg/controllers`: when a child-Job failure and an elapsed deadline are
  observed in the same reconcile, the JobSet fails with `DeadlineExceeded` (the
  deadline is checked before the failure policy) and is not restarted.
- `pkg/controllers`: `startTime` transitions: first start sets it, suspend
  clears it, resume resets it, `RestartJob` leaves it unchanged, `RestartJobSet`
  resets it.
- `pkg/controllers`: precedence: a JobSet that completes in the same reconcile
  where the deadline would expire is marked `Completed` by the success policy
  (which runs before the deadline check), not failed by the deadline.
- `pkg/webhooks` / CEL: `activeDeadlineSeconds` validation covering Minimum=1
  (reject zero and negative), and allowing updates on a running JobSet (raise,
  lower, add, and remove) to confirm the field is mutable when the gate is on.
- `pkg/webhooks`: the feature-gate guard — setting `activeDeadlineSeconds` on
  create or update while the gate is off is rejected, while an unchanged value
  and clearing the field are allowed.
- Target: cover the new code paths in the failure/ttl-adjacent packages, which
  are currently well covered.

#### Integration tests

Added under `test/integration/` (envtest):

- A JobSet with a short `activeDeadlineSeconds` transitions to `Failed` with
  reason `DeadlineExceeded` after the deadline, and its child Jobs are deleted.
- A JobSet suspended before the deadline does not fail; on resume the timer
  restarts (does not fail based on pre-suspend elapsed time).
- A JobSet completing before the deadline is unaffected.
- A global `RestartJobSet` resets the timer; a single-Job `RestartJob` does not.
- Updating `activeDeadlineSeconds` on a running JobSet takes effect without a
  restart: lowering it below the elapsed active time fails the JobSet on the
  next reconcile; raising it extends the deadline.
- `RestartJobSetAndIgnoreMaxRestarts` resets the timer each attempt (fresh
  per-attempt deadline, unbounded by `maxRestarts`).
- Feature gate off: `activeDeadlineSeconds` is never enforced and
  `.status.startTime` is not maintained; enabling the gate on an already-running
  JobSet resumes `startTime` maintenance and enforcement, from the persisted
  `startTime` if one exists (matching KEP-1282's re-enable semantics), otherwise
  from the enable point.
- Externally managed JobSet (`managedBy` set to a non-built-in controller):
  validates the assumption that the built-in controller skips this feature for
  externally managed JobSets. With the gate on and the deadline elapsed, the
  built-in controller neither sets `.status.startTime` nor fails the JobSet,
  confirming the behavior falls out of the existing `managedBy` skip with no
  extra gating code.

### Graduation Criteria

**Alpha (v0.13.0)**

- API field + status field implemented behind `JobSetActiveDeadlineSeconds`
  (default off).
- Semantics above implemented; unit + integration tests passing.
- Docs updated on the JobSet site.

**Beta (v0.14.0)**

- Gate on by default.
- `jobset_active_deadline_exceeded_total` metric wired and documented.
- Kind e2e coverage for the expiry and suspend/resume paths.
- No open bugs on suspend/restart timer interactions.

**Stable (v0.16.0)**

- Gate locked on; soak time with no semantic regressions.

## Implementation History

- 2026-08-26: KEP drafted (provisional) from issue #1186 discussion.
- 2026-09-05: Documented the activeDeadlineSeconds interaction with managedBy
  (follow-up to the #1306 KEP review).
- 2026-09-05: Alpha implementation (API field, `.status.startTime`, feature gate
  `JobSetActiveDeadlineSeconds`, controller enforcement, metric, unit and
  integration tests).
- 2026-09-07: Review follow-ups. Gate `.status.startTime` maintenance behind the
  feature gate so a disabled feature has no side effects; scope the
  success→deadline→failure reorder to JobSets that set `activeDeadlineSeconds`
  so non-users keep failure→success ordering; add a validating-webhook guard
  that rejects setting the field while the gate is off (create and update);
  change the `StartTime` printer column to `type=date`; and add reconcile-level
  ordering unit tests and a metric-increment test.

## Drawbacks

- Adds a second time-based failure path (alongside `maxRestarts`) that operators
  must reason about together.
- Introduces a new mutable status field (`.status.startTime`) whose lifecycle is
  coupled to suspend/resume/restart, adding controller complexity.

## Alternatives

- **Timer from `creationTimestamp`, no suspend pause** (issue option 1a/2a):
  simplest, no new status field, but wrong under Kueue where suspend/resume is
  routine. A queued JobSet could "expire" while it was never actually running.
  Rejected.
- **Deadline exceeded triggers a restart instead of failure**: inconsistent with
  Job and with the "hard upper bound" motivation. Rejected.
- **Per-ReplicatedJob deadline field**: already achievable by setting
  `activeDeadlineSeconds` on the Job template; out of scope (Non-Goal).
