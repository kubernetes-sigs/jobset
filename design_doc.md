# \[External\] JobSet \- Add Execution Attempt Annotation

# Motivation

Logging deployment streams currently rely on the `jobset.sigs.k8s.io/restart-attempt` annotation to tag logs per execution cycle. However, when queueing workloads (e.g., via Kueue), JobSets are frequently evicted via suspension (terminating pods) and later resumed (spawning new pods).

Because JobSet resumptions do not increment the `restart-attempt` counter (which only increments during failure policy evaluations), logs from multiple distinct execution phases share an identical identifier. Consequently, users cannot properly isolate log streams by their scheduling lifecycle, severely impeding debugging efforts (e.g., isolating logs to investigate node-level hardware failures during a specific execution pass).

# Goals

- Provide a persistent, monotonic counter that increments on **both** failure-triggered restarts and suspension-driven resumptions.  
- Inject this new metric as a pod annotation to allow external logging systems (like FluentBit) to cleanly slice log iterations.

# Non-goals

- Alter the lifecycle definitions for `jobset.sigs.k8s.io/restart-attempt`. The existing restart tracking acts strictly on failure conditions and will remain unchanged.

# Proposed API

Introduce a new pod annotation (e.g., `jobset.sigs.k8s.io/execute-attempt`) to the JobSet API to track the total number of execution iterations a JobSet has undergone.

To track this robustly across controller restarts, a new state field `executeAttempts` (or `runAttempts`) will be introduced within `JobSet.status`.

## Example

1. **Initial Scheduled Run:** A JobSet starts. Pods land on Nodepools A and B. Both `restart-attempt` and `execute-attempt` evaluate to `"0"`.  
2. **Preemption (Eviction):** Kueue evicts the JobSet (`suspend: true`). Active pods are deleted.  
3. **Resumed Execution:** Kueue readmits the JobSet (`suspend: false`). New pods land on Nodepools C and D.  
   * Current state: `restart-attempt` remains `"0"`.  
   * Proposed state: `execute-attempt` increments to `"1"`.

The new annotation provides a distinct monotonic execution token to filter telemetry data accurately.

# Behavior

## 1\. Increment on restart

When a child Job failure triggers a JobSet restart (matching the existing `restart-attempt` conditions), the JobSet controller will increment the `.status.executeAttempts` field.

## 2\. Increment on resume

When the JobSet controller detects the `jobset.spec.suspend` configuration transitioning from `true` to `false`, it will similarly increment the `.status.executeAttempts` field before the new child Jobs (and their pods) are generated.

## 3\. Data propagation

During child Job creation, the JobSet controller will inject the current `.status.executeAttempts` value into the child Job and corresponding Pod templates as the `jobset.sigs.k8s.io/execute-attempt` annotation.  
