# KEP-969: Workload-Aware Scheduling Integration

<!-- toc -->
- [Summary](#summary)
- [Motivation](#motivation)
  - [Goals](#goals)
  - [Future Goals](#future-goals)
  - [Non-Goals](#non-goals)
- [Proposal](#proposal)
  - [User Stories](#user-stories)
  - [Representative APIs](#representative-apis)
- [Risks and Mitigations](#risks-and-mitigations)
  - [Upstream API dependency](#upstream-api-dependency)
- [Design Details](#design-details)
  - [MVP goals](#mvp-goals)
  - [MultiLevel support](#multilevel-support)
  - [Gang of Gangs Support](#gang-of-gangs-support)
  - [API](#api)
  - [Defaulting](#defaulting)
  - [Validation](#validation)
  - [Controller Integration](#controller-integration)
    - [Naming Convention](#naming-convention)
    - [Conflict detection](#conflict-detection)
    - [Sequenced Startup](#sequenced-startup)
    - [Scaling](#scaling)
  - [Workload Lifecycle](#workload-lifecycle)
  - [Test Plan](#test-plan)
  - [Downstream Integrations](#downstream-integrations)
  - [Graduation Criteria](#graduation-criteria)
    - [Alpha (1.37)](#alpha-137)
    - [Alpha 2 (1.38)](#alpha-2-138)
    - [Beta (1.38)](#beta-138)
    - [GA](#ga)
- [Implementation History](#implementation-history)
- [Drawbacks](#drawbacks)
- [Alternatives](#alternatives)
  - [Template Delegation Model](#template-delegation-model)
<!-- /toc -->

## Summary

This KEP integrates the [KEP-6089 Workload-Aware Scheduling (WAS) Controller APIs][kep-6089]
with JobSet.
An optional `spec.scheduling` field lets users configure gang scheduling, topology
constraints, disruption modes, and ReplicatedJob-level resource claims.

[kep-6089]: https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/6089-was-controller-apis

## Motivation

Distributed training and HPC workloads often need all pods admitted together, placed within a
specific topology domain, or disrupted as a group. JobSet currently has no native API for these
requirements.
WAS supplies standard scheduling APIs and a `workloadbuilder` library that JobSet
can compile into `Workload` and `PodGroup` resources.

### Goals

- Add optional, centralized scheduling configuration to `JobSetSpec`.
- Support Basic and Gang policies, topology constraints, disruption modes, and shared
  ReplicatedJob-level resource claims.
- Make the scheduling configuration available to external schedulers and queueing systems, so
  they can select the appropriate workload representation and admission behavior.
- Adopt the workload builder library for Workload/PodGroup creation and validation.
- Support a composite PodGroup hierarchy (gang of gangs) so a top-level JobSet policy and
  per-ReplicatedJob policies can be used together instead of one or the other. This is
  behind the `JobSetGangOfGangs` feature gate (see
  [Gang of Gangs Support](#gang-of-gangs-support)).

Composite PodGroup is alpha in 1.37, so it sits behind its own feature gate rather than the
stable Workload/PodGroup path. A cluster that only has the beta Workload/PodGroup APIs can
still use JobSet scheduling without turning on an alpha dependency.

### Future Goals

- Partial eviction and rescheduling of a single ReplicatedJob or replica within a composite
  hierarchy.
- Always build the full composite tree once CompositePodGroup is available, so that whenever
  `spec.scheduling` is set the shape is uniform (`CompositePodGroup` for the whole JobSet, a
  `CompositePodGroup` per ReplicatedJob group, and a leaf `PodGroup` per independent Job)
  regardless of how `spec.scheduling` is configured. Alpha builds flat PodGroups in the
  non-composite modes; a uniform tree gives a higher-level controller (for example Kubeflow
  Trainer) one tree structure to reason about and a one-to-one ReplicatedJob-to-group mapping
  instead of a varying number of levels. The WAS working group leans toward this direction; it
  is to be validated with a proof-of-concept and example use cases, and finalized at the WAS
  summit before it replaces the conditional Alpha behavior.
- Separate "manage the runtime objects" from "manage the Workload". A controller that composes
  JobSets (depth-3 actuation, such as Trainer over JobSet over Jobs) should not have to
  understand per-Job PodGroup lifecycle, so JobSet should be able to materialize the CPG/PG
  subtree even when it does not own the top-level Workload. This gives three modes rather than
  two: create nothing, manage only the runtime PodGroups/CompositePodGroups, or manage
  everything including the Workload. LWS already supports this mode: a parent that owns the
  Workload sets the `scheduling.k8s.io/group-template-name` annotation, and LWS creates only the
  runtime PodGroups from that template (`pkg/schedulerprovider`; kubernetes-sigs/lws#1144 makes
  the annotations immutable, kubernetes-sigs/lws#1162 handles the parent changing `minCount`).
  JobSet already sets this same annotation on child Jobs, so adopting the same parent-driven contract
  would let a controller such as Trainer drive JobSet and LWS in a consistent way. The exact role of
  `scheduling.k8s.io/workload-name` still needs to be aligned with the LWS integration. We should also
  define how parent delegation works for both flat `PodGroup` and hierarchical `CompositePodGroup`
  objects while keeping the contract consistent.
- First-class support for dependency-ordered scheduling (`DependsOn`) within the composite
  hierarchy. Alpha works around ordering by excluding a `DependsOn`-deferred member from the
  shared gang `minCount` (the deferred Pods join opportunistically with no reserved capacity)
  and by rejecting a full-quorum Gang root with sequenced startup. A first-class mechanism would
  let a gang or CompositePodGroup account for members its controller creates later, instead of
  that workaround. The open questions are tracked upstream in
  [kubernetes/kubernetes#142855](https://github.com/kubernetes/kubernetes/issues/142855).

### Non-Goals

- Implementing Kueue admission or queue management. Kueue integration is out of scope for this
  KEP and is owned by a follow-up Kueue-side design.
- Always creating WAS objects regardless of `spec.scheduling`. By current agreement a nil
  `spec.scheduling` creates no WAS resources; JobSet does not compile an empty configuration
  into a Basic-policy tree.

## Proposal

Add `spec.scheduling` to `JobSetSpec`. Configuration is centralized at the JobSet level and can
specify defaults for all ReplicatedJobs plus targeted overrides using
`targetReplicatedJobs`. When enabled, the JobSet controller builds one `Workload`, materializes
one `PodGroup` per scheduling template, and maps child Jobs and Pods to those PodGroups.

### User Stories

| User need | Behavior |
|---|---|
| Gang-schedule a complete training JobSet | A top-level Gang policy creates one PodGroup whose `minCount` is the total pod count. |
| Schedule groups independently | A `replicatedJobs` entry creates one PodGroup per targeted ReplicatedJob; Basic, Gang, topology, and disruption settings can be overridden. |
| Use an independent driver and constrained workers | The driver can use Basic scheduling while workers use a topology-constrained Gang PodGroup. These groups are independent in alpha; there is no composite PodGroup. |
| Keep existing JobSets unchanged | A JobSet without `spec.scheduling` creates no WAS resources. |
| Avoid reserving resources for suspended workloads | Suspended JobSets have their Workload and PodGroups deleted; they are recreated on resume. |
| Combine sequencing with Gang scheduling | `DependsOn` and `InOrder` startup use one PodGroup per ReplicatedJob to avoid deadlock. |
| Scale an elastic workload | Changes to the represented pod count update the generated Gang `minCount`; other template changes require deleting and recreating the jobset with updated values |
| Share a DRA claim across a Job's pods | A `resourceClaims` entry in a `JobScheduling` policy is copied to each Job's PodGroup, and each pod references that shared claim explicitly. Pod-level claims such as a per-pod GPU claim remain independent. |
| Run a TPU multi-slice workload | A JobSet can use a PodGroup per replica and topology constraints to request coordinated placement for TPU slices. The TPU-specific pod resources and topology values remain in the JobSet pod templates. |
| Recover one failed component | Partial eviction and rescheduling of an individual ReplicatedJob remain future work; alpha restarts follow JobSet's existing group-level semantics. |

### Representative APIs

Each user story above maps to a concrete `spec.scheduling` configuration and a specific set of
generated `Workload`/`PodGroup` resources. In the names below, each `<hash>` is derived from the
corresponding source object's identity as described in [Naming Convention](#naming-convention).

**Gang-schedule a complete training JobSet**

```yaml
scheduling:
  schedulingPolicy:
    gang: {}
```

Creates: one `Workload` and one `PodGroup`, each named `<jobset-name>-<hash>`, with `minCount`
defaulted to `parallelism × replicas` summed across all ReplicatedJobs.

**Schedule groups independently**

```yaml
scheduling:
  replicatedJobs:
    - targetReplicatedJobs: [launcher]
      schedulingPolicy:
        gang: {}
    - targetReplicatedJobs: [worker]
      schedulingPolicy:
        gang: {}
```

Creates: one `Workload` named `<jobset-name>-<hash>` containing one `PodGroup` per targeted
ReplicatedJob. The PodGroups are named `<jobset-name>-launcher-<hash>` and
`<jobset-name>-worker-<hash>`.

**Schedule groups together**

```yaml
scheduling:
  replicatedJobs:
    - targetReplicatedJobs: [launcher, worker]
      schedulingPolicy:
        gang: {}
```

Creates: one `Workload` named `<jobset-name>-<hash>` containing one `PodGroup` for launcher and
worker, named `<jobset-name>-launcher-worker-<hash>`. MinCount for the gang of launcher and
worker is the sum of the number of replicas for each.

**Independent driver and constrained workers**

```yaml
scheduling:
  replicatedJobs:
    - targetReplicatedJobs: [driver]
      schedulingPolicy:
        basic: {}
    - targetReplicatedJobs: [worker]
      schedulingPolicy:
        gang: {}
      schedulingConstraints:
        topology: ...
```

Creates: one `Workload` named `<jobset-name>-<hash>` with two independent `PodGroup`s — a Basic
`PodGroup` named `<jobset-name>-driver-<hash>` and a topology-constrained Gang `PodGroup` named
`<jobset-name>-worker-<hash>`. No composite `PodGroup` links them in alpha.

**Keep existing JobSets unchanged**

```yaml
# spec.scheduling omitted
```

Creates: no `Workload` and no `PodGroup`; child Jobs and Pods are unaffected.

**Avoid reserving resources for suspended workloads**

```yaml
spec:
  suspend: true
  scheduling:
    ...
```

Creates: the existing `Workload` and `PodGroup`(s) are deleted on suspend and recreated with the
same names/mapping on resume.

**Combine sequencing with Gang scheduling**

```yaml
scheduling:
  replicatedJobs:
    - targetReplicatedJobs: [leader]
      schedulingPolicy:
        gang: {}
    - targetReplicatedJobs: [worker]
      schedulingPolicy:
        gang: {}
replicatedJobs:
  - name: leader
    dependsOn: []
  - name: worker
    dependsOn:
      - name: leader
        status: Ready
```

Creates: one `Workload` named `<jobset-name>-<hash>` containing one Gang `PodGroup` per
ReplicatedJob (`<jobset-name>-leader-<hash>`, `<jobset-name>-worker-<hash>`) instead of a single
JobSet-wide `PodGroup`, avoiding deadlock from sequential Job creation. Top-level scheduling,
including an empty `scheduling: {}`, is rejected with `DependsOn` or `InOrder` startup; the
ReplicatedJobs are configured under `spec.scheduling.replicatedJobs` instead. This example gives
each ReplicatedJob its own gang; dependent ReplicatedJobs can also share one gang in a single
entry, in which case the deferred member is excluded from `minCount` (see
[Sequenced Startup](#sequenced-startup)).

**Scale an elastic workload**

```yaml
scheduling:
  schedulingPolicy:
    gang: {}
```

Creates: the existing `Workload`/`PodGroup`(s) are preserved; only the generated Gang `minCount`
is patched in place as the represented pod count changes.

**Share a DRA claim across a Job's pods**

```yaml
scheduling:
  replicatedJobs:
    - targetReplicatedJobs: [worker]
      job:
        schedulingPolicy:
          gang: {}
        resourceClaims:
          - name: imex-channel
            resourceClaimTemplateName: imex-channel-template
```

Creates: one `Workload` containing one `PodGroup` per `worker` Job replica. Each PodGroup carries
the `resourceClaims` entry, and each pod in that Job references the shared claim explicitly,
while per-pod claims (for example a GPU claim) remain independent.

**Run a TPU multi-slice workload**

worker has three replicas.

```yaml
scheduling:
  replicatedJobs:
  - targetReplicatedJobs: [worker]
    job:
      schedulingPolicy:
        gang: {}
      schedulingConstraints:
        topology: ...
      disruptionMode:
        all: {}
      resourceClaims:
        - name: tpu-slice
          resourceClaimTemplateName: tpu-slice-template
```

Creates: one `Workload` and three PodGroups with the requested topology constraint,
coordinating placement across TPU slices; TPU-specific pod resources stay in the JobSet pod
templates.

ResourceClaims are created for each Job replica and the same topology request must be matched for each Job.

## Risks and Mitigations

### Upstream API dependency

Upstream apis for WAS are fast changing. To keep disruption minimal to end-users,
we will keep the feature alpha until scheduling APIs are stable.

## Design Details

### MVP goals

To release this API to users, we have to make some compromises.

a. The first round of this will only support Workload / PodGroup APIs.
This allows one to use v1beta1 apis only for 1.37 so users can use this API sooner.

b. The use of compositePodGroups will handle under a separate feature gate as this depends on an alpha API in 1.37.

### MultiLevel support

JobSet has three layers: JobSet, ReplicatedJobs, Jobs.

Its useful to explain what each level of scheduling can buy you.

For the top layer, the main goal is to schedule a JobSet as a gang.
All the replicated jobs and their replicas are scheduled as one single podgroup.

For the ReplicatedJobs level, the goal is to control the podgroups for each ReplicatedJob.
One podgroup will be created for the entire replicated job and their replicas.
This is useful when you want to control disruption or gang scheduling at the replicated job level.

For MVP, the top layer and replicatedJobs level are mutally exclusive.

For the last level, the goal would be that each replica of a ReplicatedJob should be a separate gang.
This enables the use of preemption at the replica level and provides a way for individual jobs within a JobSet
to use resourceClaims.

### Gang of Gangs Support

> This lands in Alpha 2 (1.38) behind the `JobSetGangOfGangs` gate, which also needs
> `JobSetWorkloadAwareSchedulingAPI`. With the gate off, the top-level and per-ReplicatedJob
> models stay mutually exclusive, same as Alpha.

At a high level, the API looks like:

```yaml
scheduling:
  schedulingPolicy: .. (if gang, all pods scheduled together)
  schedulingConstraints: ... (topology for all jobs within JobSet)
  disruptionMode: ... (disruption for all pods within jobset)
  replicatedJobs:
    targetReplicatedJobs: ..(the below policies apply to these group of replicated jobs)
    schedulingPolicy: .. (gang within all replicas of the ReplicatedJob)
    schedulingConstraints: .. (tas requirements for all replicas of ReplicatedJob)
    disruptionMode: .. (disruption within replicas of replicated job)
    job:
      gang requirements at the job level
```

With the `JobSetGangOfGangs` feature gate enabled and the CompositePodGroups (CPG) feature,
the top level scheduling field corresponds to the root level.

With compositePodGroups, the top level policy would be a CPG object.
The group of replicated jobs within a `replicatedJobs` entry would correspond to a CPG also.

And then at the job level, these would be separate PodGroups.

With the gate on, the controller compiles the JobSet into one `Workload` with a CPG tree. The
top-level policy becomes a root `CompositePodGroup`, and each `replicatedJobs` entry becomes a
child `CompositePodGroup` that points at the root through the real API field
`spec.parentCompositePodGroupName`. Composite objects follow the same
`<jobset-name>-<template-name>-<hash>` naming scheme as leaf PodGroups, with a `-cpg` suffix on
the composite template name so a composite node never collides with the shared leaf beneath it.
Under each child CPG are the leaf `PodGroup`s: one per Job replica when `job` is set, otherwise
one shared PodGroup for the targeted ReplicatedJobs. The leaves set the same
`spec.parentCompositePodGroupName` field to point at their parent CPG.

A ReplicatedJob that no `replicatedJobs` entry targets attaches its leaf PodGroup directly to the
root CPG and schedules under the top-level policy. The root CPG is the fallback, so the Alpha rule
that every ReplicatedJob must be targeted does not apply in this mode; it only holds when the gate
is off and there is no top-level policy to fall back to.

This is the one case where a top-level policy and `replicatedJobs` can be set together. In Alpha
that combination is rejected (see [Validation](#validation)) because nothing links the two levels;
with the gate on the root CompositePodGroup is that link, so the restriction is dropped.

This stays out of the initial Alpha because CompositePodGroup is alpha in 1.37. This KEP proposes
the Alpha 2 design; the `JobSetGangOfGangs` gate and the controller that compiles the multi-level
CPG tree require a separate implementation change tracked under #1365. The controller uses
the upstream `workloadbuilder` for composite-tree support; because that builder exposes no API to
materialize nested leaf PodGroups, the leaves are built directly from their templates. If the gate is enabled on a
cluster that does not serve the CompositePodGroup API, admission does not pre-check the API; the
controller skips the CompositePodGroup watch so the manager stays healthy, and gang-of-gangs
reconciles surface a clear per-JobSet error until the API is available. Enable the gate only on
clusters that serve the API.

**Interaction with sequenced startup (`DependsOn`/`InOrder`).** The composite tree keeps per-entry
gangs as separate children, so sequencing is expressed by `DependsOn` ordering child creation. This
converges only under a `Basic` root: a `Basic` root imposes no cross-child quorum, so a child whose
Jobs are deferred is fine. A `Gang` root whose `minGroupCount` spans a `DependsOn`-deferred child
can never reach quorum and deadlocks — the same failure a single top-level gang hits with sequenced
startup, one level up. With sequenced startup, the proposed Alpha 2 webhook behavior therefore allows only two root
shapes: an explicit `Basic` root, or a `Gang` root with an explicit `minGroupCount`. Every other shape is a
full-quorum Gang root and is rejected, including a root that sets only `schedulingConstraints` or
`disruptionMode` and no `schedulingPolicy`, since the controller
defaults a composite node to Gang. An explicit `minGroupCount` is accepted as the user opting
into partial quorum; the webhook does not check that the value is actually reachable before the
deferred children, so setting it equal to the full child count is still full quorum and can
deadlock. Keeping that responsibility with the user avoids the controller reasoning about
DependsOn order at admission; tightening it to reject unreachable explicit quorums is possible
future hardening.

**Representative use case (MPI / KEP-4064, from the Kubeflow Trainer integration).** Each MPI node
group is its own ReplicatedJob; listing the launcher and all groups in one `replicatedJobs` entry
puts them in a single gang, while distinct entries gang-schedule independently:

```yaml
scheduling:
  schedulingPolicy: { basic: {} }   # root: groups scheduled independently
  replicatedJobs:
    - targetReplicatedJobs: [launcher, cpu, l40s, w7900]  # one shared gang
      schedulingPolicy: { gang: {} }
```

The proposed Alpha 2 limits are intentionally separate: `replicatedJobs` **entries** are capped
at 8 (each entry compiles into at least one PodGroupTemplate and the Workload API allows at most 8
templates per node), while
`targetReplicatedJobs` **within** one entry is capped at 50 (for a shared gang the whole list
collapses into a single leaf PodGroup, so it adds one template regardless of target count). This
cap applies to the shared-gang grouping; the per-Job model (`job`) requires exactly one target and
instead produces one leaf PodGroup per Job replica.

For a jobset, with three replicated jobs with 2 replicas each, gang of gangs could be represented:

JobSet (CPG, basic, zone-level topology)
  ReplicatedJobA (CPGA, preempt all) ReplicatedJobB(CPGB, preempt all) ReplicatedJobC (CPGB, preempt all)
    Job1 (PG1, gang, rack, DRA)         Job3 (PG3, gang, rack)                  Job5 (PG5, gang, rack)
    Job2 (PG2, gang, rack, DRA)         Job4 (PG4, gang, rack)                  Job6 (PG6, gang, rack)

```yaml
scheduling:
  schedulingPolicy:
    basic: {}
  schedulingConstraints: zone
  disruptionMode: simple
  replicatedJobs:
    - targetReplicatedJobs: [ReplicatedJobA]
      schedulingPolicy:
        gang: {}
      disruptionMode:
        all: {}
      job:
        schedulingPolicy:
          gang: {}
        schedulingConstraints: rack
        resourceClaims: ... #shared claim for all pods within podgroup
    - targetReplicatedJobs: [ReplicatedJobB]
      schedulingPolicy:
        gang: {}
      disruptionMode:
        all: {}
      job:
        schedulingPolicy:
          gang: {}
        schedulingConstraints: rack
    - targetReplicatedJobs: [ReplicatedJobC]
      schedulingPolicy:
        gang: {}
      disruptionMode:
        all: {}
      job:
        schedulingPolicy:
          gang: {}
        schedulingConstraints: rack
        resourceClaims: ... #shared claim for all pods within podgroup
```

### API

The api is defined below, matching the alpha implementation.

```go
type JobSetSpec struct {
    ...
    // scheduling defines the Workload-Aware Scheduling configuration for this JobSet.
    // When nil, no scheduling objects are created and behavior is unchanged.
    // Requires the JobSetWorkloadAwareSchedulingAPI feature gate.
    // +optional
    Scheduling *JobSetScheduling `json:"scheduling,omitempty"`
}

// JobSetScheduling defines the Workload-Aware Scheduling configuration for a JobSet.
// With the JobSetGangOfGangs feature gate disabled (alpha default), a JobSet must
// configure scheduling using exactly one of two mutually exclusive models (with
// the gate enabled, a composite Gang-of-Gangs PodGroup hierarchy links the two):
//   - the top-level (level 1 / composite) model: set schedulingPolicy,
//     schedulingConstraints, and/or disruptionMode to configure a single PodGroup
//     covering the whole JobSet, and leave replicatedJobs unset. This model is
//     rejected with sequenced startup (DependsOn or InOrder); use the
//     per-ReplicatedJob model in that case.
//   - the per-ReplicatedJob (level 2 / composite) model: set replicatedJobs and
//     leave schedulingPolicy, schedulingConstraints, and disruptionMode unset at
//     the top level. Every ReplicatedJob must then be targeted by exactly one
//     replicatedJobs entry, since there is no top-level policy for an
//     untargeted ReplicatedJob to fall back to.
//
// With JobSetGangOfGangs enabled, these two models may be combined: a top-level
// policy and replicatedJobs entries are composed into a Gang-of-Gangs hierarchy,
// and any ReplicatedJob not targeted by a replicatedJobs entry falls back to the
// root CompositePodGroup under the top-level policy, linked through the
// spec.parentCompositePodGroupName field. Because CEL cannot read feature gates,
// this gate-aware mutual exclusion is enforced in the validating webhook
// (validateScheduling) rather than as a type-level CEL rule.
type JobSetScheduling struct {
    // schedulingPolicy defines the composite-level (level 1) scheduling policy for
    // the entire JobSet. Defaults to Gang when spec.scheduling is set but
    // schedulingPolicy is nil and replicatedJobs is not set.
    // Mutually exclusive with replicatedJobs: see the type-level comment.
    // +optional
    SchedulingPolicy *schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy `json:"schedulingPolicy,omitempty"`

    // schedulingConstraints defines composite-level (level 1) topology constraints
    // for the entire JobSet.
    // Mutually exclusive with replicatedJobs: see the type-level comment.
    // +optional
    SchedulingConstraints *schedulingv1alpha3.WorkloadCompositePodGroupSchedulingConstraints `json:"schedulingConstraints,omitempty"`

    // disruptionMode defines how the entire composite group (level 1) can be disrupted.
    // Mutually exclusive with replicatedJobs: see the type-level comment.
    // +optional
    DisruptionMode *schedulingv1alpha3.WorkloadCompositePodGroupDisruptionMode `json:"disruptionMode,omitempty"`

    // replicatedJobs specifies per-ReplicatedJob composite-level (level 2)
    // scheduling overrides. With JobSetGangOfGangs disabled, mutually exclusive
    // with the top-level schedulingPolicy, schedulingConstraints, and
    // disruptionMode fields, and every ReplicatedJob must be targeted by exactly
    // one entry: see the type-level comment. With JobSetGangOfGangs enabled, may
    // be combined with a top-level policy, and untargeted ReplicatedJobs fall back
    // to the root CompositePodGroup.
    // The list is limited to a maximum of 8 entries, since each entry compiles into
    // at least one PodGroupTemplate and the Workload API allows at most 8 templates.
    // +optional
    // +listType=atomic
    // +kubebuilder:validation:MaxItems=8
    ReplicatedJobs []ReplicatedJobScheduling `json:"replicatedJobs,omitempty"`
}

// ReplicatedJobScheduling targets one or more named ReplicatedJobs with
// level 2 (composite-level) scheduling configuration.
type ReplicatedJobScheduling struct {
    // targetReplicatedJobs is the list of ReplicatedJob names this policy applies to.
    // When more than one name is listed, the targeted ReplicatedJobs share a single
    // PodGroup. Every name must be unique across all replicatedJobs entries.
    // +required
    // +listType=set
    // +kubebuilder:validation:MinItems=1
    // +kubebuilder:validation:MaxItems=50
    // +kubebuilder:validation:items:MaxLength=256
    TargetReplicatedJobs []string `json:"targetReplicatedJobs,omitempty"`

    // schedulingPolicy defines the composite-level (level 2) scheduling policy (basic or
    // gang) for jobs created by the targeted ReplicatedJobs. Defaults to Gang when
    // not specified.
    // +optional
    SchedulingPolicy *schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy `json:"schedulingPolicy,omitempty"`

    // schedulingConstraints defines composite-level (level 2) topology constraints for
    // the targeted ReplicatedJobs' pods.
    // +optional
    SchedulingConstraints *schedulingv1alpha3.WorkloadCompositePodGroupSchedulingConstraints `json:"schedulingConstraints,omitempty"`

    // disruptionMode defines how pods within the targeted ReplicatedJobs can be disrupted.
    // +optional
    DisruptionMode *schedulingv1alpha3.WorkloadCompositePodGroupDisruptionMode `json:"disruptionMode,omitempty"`

    // job defines job-level (replica-level / level 3) scheduling
    // configuration, where each replica of the targeted ReplicatedJobs forms its
    // own independent gang (i.e. one PodGroup per Job) instead of sharing a single
    // PodGroup across every replica. This is part of the Gang-of-Gangs model. When
    // set, targetReplicatedJobs must contain exactly one ReplicatedJob name, and the
    // composite-level schedulingPolicy, schedulingConstraints, and disruptionMode
    // fields on this ReplicatedJobScheduling must not be set, since they
    // configure a shared PodGroup that the job field replaces with one
    // PodGroup per Job.
    // +optional
    Job *JobScheduling `json:"job,omitempty"`
}

// JobScheduling defines scheduling configuration applied at the individual
// Job (ReplicatedJob replica) level (level 3), enabling each replica to be
// scheduled as its own independent gang. The controller compiles one
// PodGroupTemplate/PodGroup per Job (replica) of the targeted ReplicatedJob, sized
// to that Job's own parallelism, rather than one PodGroup shared across all of the
// ReplicatedJob's replicas.
type JobScheduling struct {
    // +optional
    SchedulingPolicy *schedulingv1alpha3.WorkloadPodGroupSchedulingPolicy `json:"schedulingPolicy,omitempty"`
    // +optional
    SchedulingConstraints *schedulingv1alpha3.WorkloadPodGroupSchedulingConstraints `json:"schedulingConstraints,omitempty"`
    // +optional
    DisruptionMode *schedulingv1alpha3.WorkloadPodGroupDisruptionMode `json:"disruptionMode,omitempty"`
    // resourceClaims defines claims shared by all pods in each Job's PodGroup.
    // +optional
    // +listType=atomic
    // +kubebuilder:validation:MaxItems=4
    ResourceClaims []schedulingv1alpha3.WorkloadPodGroupResourceClaim `json:"resourceClaims,omitempty"`
}
```

`JobSetSpec.Scheduling` is optional and is immutable once the JobSet is running (not suspended).

### Defaulting

Defaulting applies only when `spec.scheduling` is present:

- Top-level Gang `minCount` defaults to the sum of `parallelism × replicas` across all
  ReplicatedJobs.
- Per-ReplicatedJob Gang `minCount` defaults to `parallelism × replicas` for that
  ReplicatedJob.
- A shared Gang `minCount` (one `replicatedJobs` entry targeting several ReplicatedJobs) defaults
  to the sum across those ReplicatedJobs, excluding any whose Jobs a sibling in the same entry
  defers through `DependsOn` (see [Sequenced Startup](#sequenced-startup)).

Gang `minCount` defaulting is performed **dynamically by the JobSet controller during
compilation** — the controller computes the represented pod count and writes it to
`PodGroup.spec.minCount` — rather than by the mutating webhook writing a value back into
`JobSet.spec.scheduling`.

Sequenced startup (`DependsOn` or an `InOrder` StartupPolicy) requires the per-ReplicatedJob
model: top-level scheduling is incompatible with it, so the `spec.scheduling` block (if set)
must configure `replicatedJobs`. Because an empty `spec.scheduling` block would otherwise
default to a top-level gang, the empty/top-level case with sequenced startup is rejected at
admission rather than silently falling back. Users configure Gang (or Basic) policies for
individual ReplicatedJobs under `spec.scheduling.replicatedJobs` instead.

### Validation

The validating webhook and `workloadbuilder` enforce that:

- Every policy target names an existing ReplicatedJob and targets are unique.
- Policies are Basic or Gang; disruption modes are Single or All.
- `spec.scheduling` is immutable after jobset is running.
- Top-level and per-ReplicatedJob `minCount` values do not exceed their represented pod counts.
- When a single top-level gang PodGroup is used, the total represented pod count across all
  ReplicatedJobs fits in an `int32`, so the derived `minCount` is well-defined rather than
  silently saturated.
- A single top-level PodGroup uses one `priorityClassName` across all ReplicatedJobs.
- Top-level scheduling is rejected with `DependsOn` or `InOrder` startup. This covers an
  explicit top-level `schedulingPolicy` (Basic or Gang, with or without `minGroupCount`) and
  the defaulted top-level gang represented by a `spec.scheduling` block that sets no
  `replicatedJobs` entries. Sequenced startup must use per-ReplicatedJob scheduling.
- In gang-of-gangs mode (`JobSetGangOfGangs` on), a full-quorum Gang root is rejected with
  `DependsOn`/`InOrder`, since its derived quorum could never be met while a dependent child's
  Jobs are deferred. Only two root shapes are allowed with sequenced startup: an explicit `Basic`
  root, or a `Gang` root with an explicit `minGroupCount`. A root with no `schedulingPolicy` (only
  `schedulingConstraints`/`disruptionMode`) defaults to Gang and is therefore also rejected.
- An explicit Gang `minCount` cannot exceed the represented pod count, including after a
  requested downscale; this permanently blocks downscaling
  an ElasticJobSet below that count (see [Scaling](#scaling)) unless `minCount` was left unset.
- If scheduling api is specified then there can be no scheduling api on the job api itself.
- `spec.scheduling` supports exactly one of the two models above, unless the
  `JobSetGangOfGangs` gate is on. With the gate off, setting a top-level (level 1)
  `schedulingPolicy`, `schedulingConstraints`, or `disruptionMode` together with
  `replicatedJobs` (level 2) is rejected, since there is no composite PodGroup to link a
  level 1 policy to level 2 policies. With the gate on that combination is accepted and
  compiled into the composite hierarchy from
  [Gang of Gangs Support](#gang-of-gangs-support). The two-model restriction is Alpha
  behavior only.
- When only `replicatedJobs` (level 2) is set and no top-level (level 1) field
  is set, every ReplicatedJob must be targeted by exactly one `replicatedJobs`
  entry, since there is no level 1 policy for an untargeted ReplicatedJob to fall back to.
  With `JobSetGangOfGangs` on and a top-level policy set, this rule does not apply: an
  untargeted ReplicatedJob falls back to the root CompositePodGroup and schedules under the
  top-level policy.
- Per-ReplicatedJob mode produces one PodGroup per resulting template. The upstream Workload
  API caps `podGroupTemplates` at 8 per Workload, while `JobSetSpec.ReplicatedJobs` has no such
  cap; the webhook rejects a JobSet in per-ReplicatedJob mode whose resulting PodGroup count
  would exceed 8, rather than partitioning across multiple Workloads or merging PodGroups.

### Controller Integration

When the feature is enabled and `spec.scheduling` is non-nil:

1. **Top-level mode** is selected when there are no `replicatedJobs` entries, regardless of
   whether the effective composite policy is Gang or Basic. One WorkloadItem represents the
   entire JobSet. For a Gang policy, its PodGroup uses the total represented pod count as
   `minCount`; for a Basic policy, the PodGroup has no `minCount` and each pod is admitted
   independently. Top-level mode is incompatible with sequenced startup (`DependsOn`/`InOrder`),
   which is rejected at admission (see [Sequenced Startup](#sequenced-startup)), so a JobSet that
   reaches this branch is never sequenced.
2. **Per-ReplicatedJob mode** is selected when `replicatedJobs` entries exist. One WorkloadItem
   is built for each ReplicatedJob (or, when a `replicatedJobs` entry targets more than one
   ReplicatedJob, one shared WorkloadItem for that group). A targeted policy takes precedence
   over composite defaults. Sequenced startup requires this mode.
3. **Per-Job mode (Gang-of-Gangs)** is selected per `replicatedJobs` entry when that
   entry sets `job`. Each replica (Job) of the targeted ReplicatedJob gets its
   own WorkloadItem and PodGroup instead of sharing one PodGroup across the whole ReplicatedJob,
   so replicas can be gang-scheduled and preempted independently and can carry per-replica
   resource claims. `job` requires `targetReplicatedJobs` to name exactly one
   ReplicatedJob, and is mutually exclusive with the composite-level `schedulingPolicy`,
   `schedulingConstraints`, and `disruptionMode` fields on the same entry.
4. Each WorkloadItem is compiled with `workloadbuilder`. Because the current builder supports
   only single-item trees, per-ReplicatedJob and per-Job templates are merged into one Workload.
5. The controller creates one PodGroup for each resulting template and sets the JobSet as the
   controller owner of the Workload and PodGroups.
6. Child Jobs receive:
   - `scheduling.k8s.io/group-template-name`, identifying the template; and
   - a pod-template `schedulingGroup.podGroupName` pointing to the PodGroup.

With the `JobSetGangOfGangs` gate **off**, the controller creates no `CompositePodGroup`: child
Jobs carry only `scheduling.k8s.io/group-template-name` and no parent linkage is set. With the
gate **on**, the controller builds the CompositePodGroup tree from
[Gang of Gangs Support](#gang-of-gangs-support) and sets the real field
`spec.parentCompositePodGroupName` on each child CPG and leaf PodGroup so it points at its
parent (composite node names carry a `-cpg` suffix). Child Jobs are still mapped to their leaf
via `scheduling.k8s.io/group-template-name`.

#### Naming Convention

The Workload is always named after the JobSet with a 10-character SHA-1 hash (`<jobset-name>-<hash>`). Because the current
`workloadbuilder` only supports single-item builder trees, every WorkloadItem the JobSet
produces is built independently and then merged into that one Workload.

Each WorkloadItem — which compiles to one PodGroupTemplate — is named according to how it was
produced:

- **Top-level mode** (composite Gang or Basic policy, no `replicatedJobs` entries): the JobSet
  name, `<jobset-name>-<hash>`.
- **Per-ReplicatedJob mode** (a ReplicatedJob with no `replicatedJobs` entry targeting
  it): the ReplicatedJob's own name, `<replicatedjob-name>-<hash>`.
- **Grouped ReplicatedJobs** (a `replicatedJobs` entry's `targetReplicatedJobs` names more
  than one ReplicatedJob): the target names joined with `-`, e.g. `<target-1>-<target-2>`.
- **Per-Job mode / Gang-of-Gangs** (a `replicatedJobs` entry sets `job`):
  one item per replica, `<replicatedjob-name>-<job-index>-<hash>`.

With the `JobSetGangOfGangs` gate on, the composite nodes are named the same way with a `-cpg`
suffix so a composite node never collides with the shared leaf beneath it. For a JobSet `js` with
a top-level Basic policy and two entries, `[driver]` and `[workers]`, the tree is:

```
Workload:            js-<hash>
  root CPG:          js-<hash>-cpg                 (top-level policy)
    child CPG:       js-driver-<hash>-cpg          (entry [driver])
      leaf PodGroup: js-driver-<hash>              (driver pods)
    child CPG:       js-workers-<hash>-cpg         (entry [workers])
      leaf PodGroup: js-workers-<hash>             (workers pods)
```

This scheme carries a source-identity hash on every node. A cleaner alternative, aligned with the
Kubeflow Trainer proposal, is to drop the per-template hash and prefix level-2 entries with their
index (`0-driver`, `1-workers`), which keeps names unique without a hash (ReplicatedJob names
cannot start with a digit) and avoids the repeated JobSet segment in composite nodes. Naming is for
readability and logical linking only, so it can change while this is alpha; aligning the convention
with the index-prefix model is a tracked beta item.

Generated WorkloadItem names must be unique within a Workload — for example, a ReplicatedJob
literally named `leader-worker` could collide with a group formed from `leader` and `worker`.
The controller detects this and rejects reconciliation with an error naming the collision,
rather than silently merging or overwriting a PodGroupTemplate.

The PodGroup object created for each WorkloadItem follows the same split:

- For the top-level item (composite Gang or Basic policy, no `replicatedJobs` entries), the
  PodGroup reuses the JobSet name directly: `<jobset-name>-<hash>`.
- For every other item, the PodGroup is named `<jobset-name>-<item-name>-<hash>`. If that exceeds the
  63-character DNS label limit, the JobSet name is truncated and a 10-character SHA-1 hash of
  `<jobset-name>/<item-name>` is appended instead, so the name stays deterministic and unique:
  `<truncated-jobset-name>-<hash>`.

Child Jobs are annotated with `scheduling.k8s.io/group-template-name`, set to the name of the
WorkloadItem/PodGroupTemplate that owns their pods, using the same per-level rules: the JobSet
name for top-level mode, the per-Job item name (`<replicatedjob-name>-<job-index>`) for
Gang-of-Gangs, the joined group name for a `replicatedJobs` grouping, or the
ReplicatedJob's own name otherwise.

If `spec.scheduling` is nil, all scheduling logic — and all of the naming above — is skipped
entirely: no Workload, PodGroup, or annotation is created.

#### Conflict detection

Workloads and PodGroups are namespaced, so a name the JobSet controller generates can collide
with an existing Workload or PodGroup in the same namespace — for example one created by Kueue's
Job integration for a plain Job, by another JobSet, or by any other controller that manages these
objects.

To keep generated names both deterministic and collision-resistant, every generated name is
suffixed with a 10-character SHA-1 hash (`<name of object>-<hash>`). The hash is computed over the
fully-qualified identity of the source object — its kind, namespace, and name (for per-Job items,
the Job index is included as well) — rather than over the human-readable name alone. Hashing the
object's identity (JobSet, ReplicatedJob, or Job) rather than just its display name gives two
properties:

- **Deterministic:** the same JobSet always produces the same Workload, PodGroup, and annotation
  names, so reconciliation is idempotent and the controller can locate the objects it owns by
  recomputing the name rather than by storing a reference.
- **Collision-resistant:** because the hash input includes the object's kind and namespace, a
  JobSet-owned object cannot share a name with a Workload or PodGroup that another controller
  derived from a different object, even when the human-readable prefixes happen to match.

When the base name (`<jobset-name>-<item-name>`) would exceed the 63-character DNS label limit, the
prefix is truncated and the hash is appended, as described in
[Naming Convention](#naming-convention), so the result stays within the limit while remaining
unique.

Naming alone is not relied on for correctness. The controller sets the JobSet as the controller
owner reference on every Workload and PodGroup it creates, and refuses to adopt or overwrite a
pre-existing object of the same name that it does not already own, surfacing a reconciliation error
instead of silently taking over another controller's resource.

#### Sequenced Startup

`DependsOn` and `InOrder` create Jobs sequentially, so a single top-level PodGroup requiring all
pods could never reach its `minCount`. Top-level scheduling is therefore rejected with sequenced
startup — both an explicit top-level `schedulingPolicy` and the defaulted top-level gang that an
empty `scheduling: {}` block would otherwise produce. A JobSet using `DependsOn` or `InOrder` must
instead configure per-ReplicatedJob scheduling under `spec.scheduling.replicatedJobs`. `AnyOrder`
is unaffected and may use top-level scheduling.

A `replicatedJobs` entry may still target several ReplicatedJobs that `DependsOn` one another and
share one Gang PodGroup. In that case the shared Gang `minCount` excludes the pods of any targeted
ReplicatedJob that another targeted ReplicatedJob in the same entry depends on, since those Jobs
are created only after their dependency is Ready. The dependent Job's Pods join the already-admitted
PodGroup afterward; the kube-scheduler does not reserve capacity for them, so they schedule
opportunistically into the space the gang already holds and remain Pending if none is left.

For example, an MPI JobSet with a `launcher` ReplicatedJob (1 Pod) that `dependsOn` a `node`
ReplicatedJob (49 Pods), both targeted by one `replicatedJobs` entry, produces a single Gang
PodGroup with `minCount: 49`. The 49 workers gang-schedule together; the launcher Pod joins the
same PodGroup once the workers are Ready. Including the launcher in `minCount` would deadlock: the
workers could not schedule until the launcher Pod existed, while the launcher is created only after
the workers are Ready. This is the shape the Kubeflow Trainer MPI runtime (KEP-3015) delegates to
JobSet.

#### Scaling

The controller patches the generated Gang `minCount` when ElasticJobSet scaling changes the
represented pod count, as long as `minCount` was left unset in `spec.scheduling` and is therefore
fully controller-computed. This preserves the existing Workload and PodGroups and allows the
scheduler to apply the new quorum.

Because `spec.scheduling` is immutable once a jobset is unsuspended (see [API](#api)), an *explicitly*
configured `Gang.minCount` can never be lowered after the JobSet is created. A downscale that
would make the represented pod count smaller than such an explicit `minCount` is therefore
rejected at validation time, and there is no supported way to unblock it short of recreating the
JobSet. ElasticJobSets that need to scale below their initial pod count must leave `minCount`
unset so the controller can keep computing and patching it automatically; setting an explicit
`Gang.minCount` opts a JobSet out of downscaling below that value for its lifetime.

Changes to generated template fields other than `Gang.minCount` are handled as immutable
resource changes.

### Workload Lifecycle

| Event | Action |
|---|---|
| Scheduling configured and gate enabled | Create one Workload and its PodGroups. |
| Suspend | Delete the Workload and PodGroups. |
| Resume | Recreate them. |
| Generated template changes | Patch Gang `minCount` for represented pod-count changes; delete and recreate for other immutable template changes. |
| Restart | Retain scheduling resources; recreated Jobs receive the same mapping. |
| Complete or fail | Delete the Workload and PodGroups so the scheduler releases their resources. |
| Delete | OwnerReferences provide cleanup. |
| No scheduling configuration | Create no WAS resources. |
| Gate disabled with scheduling configured | Reject the JobSet at admission. |

In gang-of-gangs mode (`JobSetGangOfGangs` enabled), "Workload and PodGroups" above includes the
root and child `CompositePodGroup` objects: they are created with the Workload, deleted on
suspend, complete, or fail, and recreated on resume, with the same OwnerReference-based cleanup.

Two lifecycle choices above are Alpha decisions with open long-term questions, raised in WG
Workload-Aware Scheduling:

- **Nil `spec.scheduling` creates nothing.** This is a declarative opt-out: a composing controller
  (for example Kubeflow Trainer) or a queueing integration can keep JobSet from creating WAS
  objects by leaving the field unset, which is simpler than detecting owner references. Always
  creating WAS objects is a Non-Goal: by current agreement a nil field stays "create nothing".
- **Suspend deletes the Workload, not just the PodGroups.** A suspended JobSet can be mutated
  before it resumes (today its Jobs' resource requests, and potentially replicas or TPU slice
  shape in the future), and such changes can alter the Workload structure itself, not only a
  gang `minCount`. Deleting and recreating the Workload on resume avoids reconciling an
  incompatible structure in place. Whether to keep this, or retain the Workload across suspend,
  is being aligned with the Job WAS controller KEPs (kubernetes/enhancements KEP-6089 and
  KEP-5547) so JobSet and Job behave consistently.

### Test Plan

- **Unit** (`pkg/controllers/scheduling_test.go`, `pkg/controllers/scheduling_reconcile_test.go`,
  `pkg/util/scheduling_test.go`): `workloadbuilder` translation for top-level and per-RJ modes,
  per-Job (Gang-of-Gangs) PodGroup templates, group/template naming and collision handling,
  `minCount` computation and defaulting, sequenced startup compiled with per-RJ scheduling,
  immutable-field recreation detection, Gang `minCount` patch-in-place on scale, and rejection of
  reconciling a pre-existing Workload the JobSet does not own.
- **Webhook** (`pkg/webhooks/jobset_webhook_test.go`): defaulting of the composite and per-RJ/per-Job
  Gang policy, no-op defaulting when the feature gate is disabled, and validation of invalid/duplicate
  targets, `minCount` exceeding represented pod counts, `spec.scheduling` immutability when not suspended, and
  feature-gate-disabled rejection. Explicitly covers disallowing level 1 and level 2
  configurations together:
  - `"top-level schedulingPolicy cannot be combined with replicatedJobs"` — a JobSet
    setting a top-level (level 1) `schedulingPolicy` together with a `replicatedJobs`
    (level 2) entry is rejected with `spec.scheduling: Forbidden: cannot set schedulingPolicy,
    schedulingConstraints, or disruptionMode together with replicatedJobs`.
  - `"replicatedJobs must cover every replicatedJob when no top-level scheduling API is
    set"` — when only level 2 is used, a ReplicatedJob left untargeted by any
    `replicatedJobs` entry is rejected, since there is no level 1 policy for it to fall
    back to.
  - `"replicatedJobs covering every replicatedJob with no top-level scheduling API is
    valid"` — the accepting counterpart, confirming level 2 alone is valid once every
    ReplicatedJob is targeted.
  - `"job cannot be combined with leaf-level schedulingPolicy"` — the analogous
    mutual-exclusion check one level down, between a `replicatedJobs` entry's leaf fields
    (level 2) and its `job` (level 3, Gang-of-Gangs per-Job).

  Also covers acceptance: a JobSet with no top-level
  `spec.scheduling` is admitted unchanged even when a ReplicatedJob's JobTemplate sets the native
  `batch/v1` Job `spec.scheduling` field directly, since the "no scheduling api on the job api
  itself" rule only applies once `spec.scheduling` is set on the JobSet.
- **Integration** (`test/integration/scheduling/scheduling_test.go`, run via
  `make test-integration` against standard `setup-envtest` binaries with WAS
  feature gates and the `scheduling.k8s.io/v1alpha3` and `scheduling.k8s.io/v1beta1` API
  versions enabled):
  - Workload/PodGroup creation for per-RJ leaf overrides, top-level Gang with no overrides, and
    Basic-only top-level and per-RJ modes; child-Job annotation with the owning template name in
    each mode.
  - No scheduling objects created when `spec.scheduling` is nil, and OwnerReferences set on the
    Workload/PodGroups for garbage collection.
  - No JobSet-level `spec.scheduling` with a native `batch/v1` Job `spec.scheduling` (WorkloadWithJob)
    set directly on a ReplicatedJob's JobTemplate (see
    `site/static/examples/scheduling/no-top-level-scheduling-per-job-gang.yaml`): JobSet creates no
    Workload/PodGroup of its own and does not modify, reject, or strip the Job template's
    `spec.scheduling`, leaving the Kubernetes Job controller to compile that Job's Workload/PodGroup
    independently; a sibling ReplicatedJob with no `spec.scheduling` on its JobTemplate remains
    ordinary, ungrouped pods.
  - Suspend/resume lifecycle: no objects on creation while suspended, deletion on suspend,
    recreation on resume, and repeated suspend-resume cycles.
  - `DependsOn` and `InOrder` startup with per-ReplicatedJob scheduling producing one Gang
    PodGroup per ReplicatedJob, and rejection of top-level scheduling with sequenced startup.
  - A shared Gang PodGroup from one `replicatedJobs` entry targeting several ReplicatedJobs that
    `DependsOn` one another (the MPI launcher + node shape): the gang `minCount` excludes the
    deferred member's pods, and the deferred Job's Pods join the PodGroup once its dependency is
    Ready.
  - Per-Job PodGroups when `job` is set (Gang-of-Gangs per-Job model).
  - Generated PodGroup name exceeding the 63-character DNS label limit at each naming level —
    long JobSet/ReplicatedJob names chosen so `<jobset-name>-<item-name>` overflows — verifying
    against the real envtest apiserver (which enforces `metav1.ObjectMeta.Name` validation,
    unlike the `podGroupName`/`schedulingPodGroupName` unit tests) that the PodGroup is still
    created successfully with the truncated-name-plus-hash form, the child Job's
    `scheduling.k8s.io/group-template-name` annotation and `schedulingGroup.podGroupName`
    reference the same (truncated) name, and the same overflowing inputs always resolve to the
    same generated name across reconciles:
    - **Per-ReplicatedJob mode**: one ReplicatedJob whose name alone pushes
      `<jobset-name>-<replicatedjob-name>` over the limit.
    - **Grouped ReplicatedJobs**: a `replicatedJobs` entry targeting multiple
      ReplicatedJobs whose joined `SchedulingGroupName` overflows the limit.
    - **Per-Job / Gang-of-Gangs mode**: a targeted ReplicatedJob with enough replicas that its
      `jobSchedulingItemName` (`<replicatedjob-name>-<job-index>`) overflows the limit for at
      least one replica, and each replica's PodGroup remains distinct after truncation.
    - Top-level mode is excluded: its PodGroup always reuses the JobSet name verbatim
      (`schedulingPodGroupName` never appends a template name in that case), so it cannot
      overflow independently of the JobSet's own name, which is already bounded by JobSet
      admission.
  - Gang `minCount` patched in place when ElasticJobSet scaling changes parallelism.
  - DRA `resourceClaims` propagation from `JobScheduling` to per-Job PodGroups, including
    direct `ResourceClaimName` references, multiple claims on one PodGroup, and preservation
    across a suspend-resume cycle.
  - Feature-gate-disabled behavior: no scheduling objects created, and reconciliation skipped even
    when `spec.scheduling` is set (the webhook is expected to have already rejected it).
- **E2E** (`test/e2e/scheduling/scheduling_test.go`, run via `make test-e2e-kind-scheduling` on a
  Kind cluster built from a Kubernetes main node image with WAS feature gates enabled — see
  `hack/e2e-scheduling-cluster.sh`):
  - Workload/PodGroup creation for per-RJ leaf overrides, top-level Gang with no overrides, and
    computed `minCount` with an empty `scheduling` block.
  - Suspend and recreate-on-resume behavior.
  - Gang-scheduling a single ReplicatedJob with multiple pods, and per-RJ PodGroups with
    `DependsOn` startup.
  - Preemption of a low-priority JobSet's PodGroup when a high-priority JobSet needs its
    resources.
  - Gang `minCount` patched in place on ElasticJobSet scaling.
  - Per-Job PodGroups when `job` is set (Gang-of-Gangs per-Job model).
  - Gang-of-Gangs (`JobSetGangOfGangs` gate) combined with `DependsOn`:
    - A `Basic` root over per-child `Gang`s with `DependsOn: Ready` builds the composite tree
      (root + per-entry child `CompositePodGroup`s, leaf `PodGroup`s linked via
      `spec.parentCompositePodGroupName`, correct leaf `minCount`s) and the JobSet completes.
    - `DependsOn: Complete` sequencing: worker Jobs are not created until the driver completes,
      and the composite tree still converges.
    - An untargeted ReplicatedJob falls back to the root composite (its leaf links straight to
      the root `CompositePodGroup`) while a targeted sibling uses `DependsOn`.
    - A full-quorum `Gang` root combined with `DependsOn` is rejected at admission.

  DRA resource-claim propagation and feature-gate-disabled behavior are covered at the
  integration level only; the E2E suite does not re-test them on a live cluster.

### Downstream Integrations

Queueing systems such as Kueue are downstream consumers of the WAS `scheduling` configuration
exposed by this KEP. How they consume it — single all-or-nothing gang admission vs. per-
ReplicatedJob admission, partial eviction, or shape-aware preemption — is out of scope here and
is owned by a follow-up Kueue-side design, tracked separately in
[kueue#13707](https://github.com/kubernetes-sigs/kueue/issues/13707).

### Graduation Criteria

This feature relies on upstream feature gates (GenericWorkload and CompositePodGroup) and APIs.
Workload and PodGroup are beta in 1.37 while CompositePodGroups are alpha in 1.37.

The jobset feature gates will discover the API to determine if the feature can be enabled.

#### Alpha (1.37)

- JobSetWorkloadAwareSchedulingAPI feature gate, API, defaulting, and validation are implemented.
- The controller creates Workloads and PodGroups through `workloadbuilder`.
- Unit, integration, and WAS-enabled E2E coverage is available.

#### Alpha 2 (1.38)

- `JobSetGangOfGangs` feature gate added (off by default, Alpha), needs
  `JobSetWorkloadAwareSchedulingAPI`.
- With the gate on and the CompositePodGroup API present, the controller compiles the
  top-level, per-ReplicatedJob, and optional per-Job policies into a CompositePodGroup tree
  rooted at one composite PodGroup (see [Gang of Gangs Support](#gang-of-gangs-support)).
- The Alpha rule rejecting a top-level policy together with `replicatedJobs` is dropped when
  the gate is on.
- Unit, integration, and E2E coverage for the tree, including parent/leaf linkage, the
  gang-of-gangs + `DependsOn` cases above, and a reconcile error (not admission rejection) when
  the CompositePodGroup API is not served.
- The webhook rejects a full-quorum `Gang` root combined with `DependsOn`/`InOrder` (which would
  deadlock); a `Basic` root, or a `Gang` root with an explicit `minGroupCount`, is required with
  sequenced startup.

#### Beta (1.38)

- If Workload / PodGroup are enabled / GA on a cluster, then JobSetWorkloadAwareSchedulingAPI will get enabled.
- If CompositePodGroups are enabled, then JobSetGangOfGangs will also get enabled.

#### GA

- No bugs

## Implementation History

- 2026-06-28: KEP created and alpha implementation added, including Workload/PodGroup
  integration, per-ReplicatedJob scheduling, sequenced-startup handling, lifecycle handling,
  immutable-spec recreation, and DRA resource-claim propagation.
- 2026-10-13: Added the gang of gangs (CompositePodGroup) design for Alpha 2, gated by the
  `JobSetGangOfGangs` feature gate.
- 2026-10-24: Implemented the gang of gangs controller and validation. Parent linkage uses the
  `spec.parentCompositePodGroupName` field rather than the annotation the original design named,
  and a full-quorum Gang root is rejected with `DependsOn`/`InOrder` startup.

## Drawbacks

- Depends on pre-stable `scheduling.k8s.io/v1beta1` APIs for phase 1.
- Adds scheduling resources and reconciliation complexity to the JobSet controller.
- Per-ReplicatedJob and sequenced modes are independently admitted in alpha because no composite
  PodGroup hierarchy is created.

## Alternatives

### Template Delegation Model

KEP-6089 also permits scheduling configuration inside an embedded Job template. This KEP rejects
that model because it splits configuration across API levels, depends on upstream Job API
changes, and does not match JobSet's existing targeted-policy pattern. Centralized configuration
keeps the API in one place and avoids that dependency.

Setting scheduling configruation within a Job Template will be rejected at validation time.
