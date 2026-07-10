# KEP-1150: Mutable JobSet Requests and Limits on Suspended State

<!-- toc -->
- [Summary](#summary)
- [Motivation](#motivation)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Proposal](#proposal)
  - [User Stories](#user-stories)
    - [Story 1: Kueue right-sizing before admission](#story-1-kueue-right-sizing-before-admission)
  - [Notes/Constraints/Caveats](#notesconstraintscaveats)
  - [Risks and Mitigations](#risks-and-mitigations)
- [Design Details](#design-details)
  - [API Changes](#api-changes)
  - [Feature Gate](#feature-gate)
  - [Webhook Changes](#webhook-changes)
  - [Controller Changes](#controller-changes)
  - [Test Plan](#test-plan)
    - [Unit Tests](#unit-tests)
    - [Integration Tests](#integration-tests)
    - [E2E Tests](#e2e-tests)
  - [Graduation Criteria](#graduation-criteria)
- [Implementation History](#implementation-history)
- [Drawbacks](#drawbacks)
- [Alternatives](#alternatives)
<!-- /toc -->

## Summary

This KEP proposes allowing container and init-container resource
requests/limits within a JobSet's `ReplicatedJob` Pod templates to be
mutated while the JobSet is suspended. This mirrors capabilities JobSet
already provides for other Pod template fields (annotations, labels,
`nodeSelector`, `tolerations`, `schedulingGates`) and builds on the
upstream Kubernetes [KEP-5440], which allows a suspended Job's Pod
template resources to be mutated via the
`MutablePodResourcesForSuspendedJobs` feature gate.

[KEP-5440]: https://github.com/kubernetes/enhancements/issues/5440

## Motivation

JobSet suspends its child Jobs to integrate with external
admission/scheduling systems such as Kueue. These integrations often need
to adjust a workload's requested resources *after* submission but *before*
it starts — for example, Kueue right-sizing requests to fit available
quota or a chosen `ResourceFlavor`.

Today, `spec.replicatedJobs` is immutable after creation, except for a
hand-picked set of Pod template fields allowed to change while suspended.
This KEP adds container/init-container `resources` to that allow-list.

### Goals

- Allow `spec.replicatedJobs[].template.spec.template.spec.containers[].resources`
  and `initContainers[].resources` to be mutated while a JobSet is
  suspended (mirroring existing mutable-while-suspended fields).
- Propagate resource mutations to child `Job` objects on resume.
- Gate this behind a new `SuspendedJobResourceMutation` feature gate,
  disabled by default at alpha.

### Non-Goals

- Introducing new API fields. This KEP only relaxes an existing
  immutability constraint.
- Resource mutation for running (non-suspended) JobSets or active Jobs.
- Mutating any container field other than `resources` while suspended.

## Proposal

### User Stories

#### Story 1: Kueue right-sizing before admission

A user submits a suspended JobSet pending Kueue admission:

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: training-job
spec:
  suspend: true
  replicatedJobs:
  - name: workers
    replicas: 4
    template:
      spec:
        template:
          spec:
            containers:
            - name: trainer
              image: my-trainer:latest
              resources:
                requests:
                  cpu: "4"
                  nvidia.com/gpu: "1"
```

Kueue patches the container resources while suspended, then unsuspends:

```bash
kubectl patch jobset training-job --type json --patch '[
  {"op": "replace",
   "path": "/spec/replicatedJobs/0/template/spec/template/spec/containers/0/resources",
   "value": {"requests": {"cpu": "2", "nvidia.com/gpu": "1"}}}
]'
```

On resume, the updated resources propagate to child Jobs and their Pods.

### Notes/Constraints/Caveats

- **Upstream dependency:** This feature requires the Kubernetes
  `MutablePodResourcesForSuspendedJobs` feature gate ([KEP-5440]),
  introduced as alpha in Kubernetes 1.35. Without it, JobSet's webhook
  accepts the update but kube-apiserver rejects the child Job update.
  Cluster operators must enable both feature gates.
- **Propagation timing:** As with other mutable-while-suspended fields,
  changes propagate to child Jobs only on resume, not immediately.

### Risks and Mitigations

- **Risk:** Enabling `SuspendedJobResourceMutation` without the upstream
  Kubernetes feature gate causes confusing failures (webhook accepts,
  controller rejected by kube-apiserver).
  **Mitigation:** Documented in feature gate description. A worked
  example demonstrating correct setup is planned for the implementation PR.

## Design Details

### API Changes

None. This KEP relaxes an existing immutability check, gated by a feature
gate. No fields are added, removed, or changed.

### Feature Gate

A new `SuspendedJobResourceMutation` feature gate is added in
`pkg/features/features.go`.

| Stage | Default | Since |
|---|---|---|
| Alpha | `false` | v0.13 |

### Webhook Changes

`jobSetWebhook.ValidateUpdate` already builds a "munged" copy of the new
spec, overwriting mutable Pod template fields with old values before the
immutability comparison.

When `SuspendedJobResourceMutation` is enabled, a new
`mungeContainerResources` helper copies each container's `Resources` from
the old spec (matched by name), leaving all other container fields subject
to the normal immutability check. When disabled, container resources remain
fully immutable while suspended.

### Controller Changes

`JobSetReconciler.resumeJob` already propagates mutable Pod template
fields to child Jobs on resume. This KEP extends it to also propagate
container/init-container resources (matched by container name) when
`SuspendedJobResourceMutation` is enabled.

### Test Plan

[x] I/we understand the owners of the involved components may require updates to
existing tests to make this code solid enough prior to committing the changes necessary
to implement this enhancement.

#### Unit Tests

- `pkg/webhooks`: table-driven tests covering resource mutation allowed
  while suspended, rejected while running, and rejected when the feature
  gate is disabled.
- `pkg/controllers`: direct unit tests for `mergeContainerResources`
  covering container name matching, init-container handling, and no-op
  behavior when the feature gate is disabled.

#### Integration Tests

- `test/integration/webhook`: resource mutation allowed while suspended,
  rejected while running, with feature gate enabled and disabled.
- `test/integration/controller`: extends existing "resume a suspended
  jobset" test to mutate resources while suspended and assert propagation
  to child Jobs.

#### E2E Tests

- A test file will be added in the implementation PR, enabling the feature
  gate via ConfigMap, creating a suspended JobSet, mutating resources,
  resuming, and asserting propagation. Requires Kind with Kubernetes >=
  1.35 and `MutablePodResourcesForSuspendedJobs` enabled.

### Graduation Criteria

**Alpha (v0.13)**
- Feature gate added, disabled by default.
- Webhook and controller support with unit, integration, and e2e tests.
- Upstream dependency documented.

**Beta**
- Positive feedback from at least one integrator (e.g. Kueue).
- No significant bugs for at least one minor release.
- Feature gate default-enabled once minimum supported Kubernetes is 1.36+
  (all supported versions include `MutablePodResourcesForSuspendedJobs`).

**Stable**
- Feature gate locked to `true` and eventually removed, following upstream
  `MutablePodResourcesForSuspendedJobs` graduation.

## Implementation History

- 2026-07-09: Initial KEP draft submitted (PR #1274).

## Drawbacks

- Adds another feature-gated special case to the webhook and resume path.
- Only useful with the upstream `MutablePodResourcesForSuspendedJobs`
  feature gate, which JobSet does not control.

## Alternatives

- **Delete and recreate child Jobs on resize.** Avoids the upstream
  dependency but loses Job state (events, restart counters) and adds
  delete/recreate race complexity. Rejected in favor of upstream
  mutability support.
- **Require full JobSet re-creation.** Defeats the purpose of
  admission-time right-sizing integrations like Kueue.
