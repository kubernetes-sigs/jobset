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
  - [Kueue Integration](#kueue-integration)
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
- Make the scheduling configuration available to integrations such as Kueue, so they can
  select the appropriate workload representation and admission behavior.
- Adopt the workload builder library for Workload/PodGroup creation and validation.

### Future Goals

- Creating a composite/parent PodGroup hierarchy in the alpha implementation.

Moving this to a future goal because Composite PodGroup is alpha in 1.37.
A goal of this KEP is to pave an integration path with Kueue and JobSet users.
Alpha apis are difficult to support for many users
so want to limit the use of alpha features under a separate feature gate.

### Non-Goals

- Implementing Kueue admission or queue management. Kueue integration is limited to exposing
  enough scheduling configuration for Kueue to understand the JobSet's workload shape; a
  follow-up Kueue design must define queueing and partial-eviction behavior.

## Proposal

Add `spec.scheduling` to `JobSetSpec`. Configuration is centralized at the JobSet level and can
specify defaults for all ReplicatedJobs plus targeted overrides using
`targetReplicatedJobs`. When enabled, the JobSet controller builds one `Workload`, materializes
one `PodGroup` per scheduling template, and maps child Jobs and Pods to those PodGroups.

### User Stories

| User need | Behavior |
|---|---|
| Gang-schedule a complete training JobSet | A top-level Gang policy creates one PodGroup whose `minCount` is the total pod count. |
| Schedule groups independently | A `replicatedJobs` entry creates one PodGroup per targeted ReplicatedJob; Basic, Gang, topology, disruption, and resource-claim settings can be overridden. |
| Use an independent driver and constrained workers | The driver can use Basic scheduling while workers use a topology-constrained Gang PodGroup. These groups are independent in alpha; there is no composite PodGroup. |
| Keep existing JobSets unchanged | A JobSet without `spec.scheduling` creates no WAS resources. |
| Avoid reserving resources for suspended workloads | Suspended JobSets have their Workload and PodGroups deleted; they are recreated on resume. |
| Combine sequencing with Gang scheduling | `DependsOn` and `InOrder` startup use one PodGroup per ReplicatedJob to avoid deadlock. |
| Scale an elastic workload | Changes to the represented pod count update the generated Gang `minCount`; other template changes require deleting and recreating the jobset with updated values |
| Share a DRA claim across a Job's pods | A `resourceClaims` entry on a ReplicatedJob policy is copied to that ReplicatedJob's PodGroup, and each pod references that shared claim explicitly. Pod-level claims such as a per-pod GPU claim remain independent. |
| Run a TPU multi-slice workload | A JobSet can use a PodGroup per replica and topology constraints to request coordinated placement for TPU slices. The TPU-specific pod resources and topology values remain in the JobSet pod templates. |
| Recover one failed component | Partial eviction and rescheduling of an individual ReplicatedJob remain future work; alpha restarts follow JobSet's existing group-level semantics. |

### Representative APIs

Each user story above maps to a concrete `spec.scheduling` configuration and a specific set of
generated `Workload`/`PodGroup` resources.

**Gang-schedule a complete training JobSet**

```yaml
scheduling:
  schedulingPolicy:
    gang: {}
```

Creates: one `Workload` and one `PodGroup` named `<jobset-name>`, with `minCount` defaulted to
`parallelism × replicas` summed across all ReplicatedJobs.

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

Creates: one `Workload` containing one `PodGroup` per targeted ReplicatedJob, named
`<jobset-name>-<replicatedjob-name>` (one PodGroup for `launcher`, one for `worker`).

**Schedule groups together**

```yaml
scheduling:
  replicatedJobs:
    - targetReplicatedJobs: [launcher, worker]
      schedulingPolicy:
        gang: {}
```

Creates: one `Workload` containing one `PodGroup` for launcher and worker.
MinCount for gang of launcher and worker is the number of replicas for each summed.

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
        topologyRequest: ...
```

Creates: one `Workload` with two independent `PodGroup`s — a Basic `PodGroup` for `driver` and a
topology-constrained Gang `PodGroup` for `worker`. No composite `PodGroup` links them in alpha.

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

Creates: one `Workload` containing one Gang `PodGroup` per ReplicatedJob (`<jobset-name>-leader`,
`<jobset-name>-worker`) instead of a single JobSet-wide `PodGroup`, avoiding deadlock from
sequential Job creation.

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
      schedulingPolicy:
        gang: {}
      resourceClaims:
        - name: imex-channel
          resourceClaimTemplateName: imex-channel-template
```

Creates: one `Workload`/`PodGroup` for `worker` whose `PodGroup` carries the `resourceClaims`
entry; each pod in `worker` references the shared claim explicitly, while per-pod claims (for
example a GPU claim) remain independent.

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
        topologyRequest: ...
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

> **Note (future work, Alpha 2):** JobSet's `replicatedJobs` intentionally allows
> heterogeneous ReplicatedJobs (for example a CPU driver alongside topology-constrained
> accelerator workers) to be configured under one JobSet. Some downstream topology-aware
> scheduling plugins assume the child `PodGroups` under a `CompositePodGroup` are structurally
> homogeneous in order to compute placements efficiently. When Alpha 2 starts compiling JobSet's
> scheduling configuration into `CompositePodGroup` trees, JobSet and/or the consuming scheduler
> will need to either validate/restrict which ReplicatedJob groupings can share a `CompositePodGroup`,
> or ensure the target scheduler supports heterogeneous `CompositePodGroup` children. This is not
> yet resolved and is called out here as a design constraint for the Alpha 2 work.

For a jobset, with three replicated jobs with 2 replicas each, gang of gangs could be represented:

JobSet (CPG, basic, zone-level topology)
  ReplicatedJobA (CPGA, preempt all) ReplicatedJobB(CPGB, preempt all) ReplicatedJobC (CPGB, preempt all)
    Job1 (PG1, gang, rack, DRA)         Job3 (PG3, gang, rack)                  Job5 (PG5, gang, rack)
    Job2 (PG2, gang, rack, DRA)         Job4 (PG4, gang, rack)                  Job6 (PG6, gang, rack)

```yaml
scheduling:
  schedulingPolicy: basic
  schedulingConstraints: zone
  disruptionMode: simple
  replicatedJobs:
    - targetReplicatedJobs: [ReplicatedJobA]
      disruptionMode:
        all: {}
      job:
        schedulingPolicy: {}
        schedulingConstraints: rack
        resourceClaims: ... #shared claim for all pods within podgroup
    - targetReplicatedJobs: [ReplicatedJobB]
      disruptionMode:
        all: {}
      job:
        schedulingPolicy: {}
        schedulingConstraints: rack
    - targetReplicatedJobs: [ReplicatedJobC]
      disruptionMode:
        all: {}
      job:
        schedulingPolicy: {}
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
// A JobSet must configure scheduling using exactly one of two mutually exclusive
// models, since composite Gang-of-Gangs PodGroup hierarchies linking a parent
// PodGroup to leaf PodGroups are not implemented in alpha:
//   - the top-level (level 1 / composite) model: set schedulingPolicy,
//     schedulingConstraints, and/or disruptionMode to configure a single PodGroup
//     (or, under sequenced startup, one PodGroup per ReplicatedJob) covering the
//     whole JobSet, and leave replicatedJobs unset.
//   - the per-ReplicatedJob (level 2 / leaf) model: set replicatedJobs and
//     leave schedulingPolicy, schedulingConstraints, and disruptionMode unset at
//     the top level. Every ReplicatedJob must then be targeted by exactly one
//     replicatedJobs entry, since there is no top-level policy for an
//     untargeted ReplicatedJob to fall back to.
type JobSetScheduling struct {
    // schedulingPolicy defines the composite-level (level 1) scheduling policy for
    // the entire JobSet. Defaults to Gang when spec.scheduling is set but
    // schedulingPolicy is nil and replicatedJobs is not set.
    // Mutually exclusive with replicatedJobs: see the type-level comment.
    // +optional
    SchedulingPolicy *schedulingv1alpha3.PodGroupSchedulingPolicy `json:"schedulingPolicy,omitempty"`

    // schedulingConstraints defines composite-level (level 1) topology constraints
    // for the entire JobSet.
    // Mutually exclusive with replicatedJobs: see the type-level comment.
    // +optional
    SchedulingConstraints *schedulingv1alpha3.PodGroupSchedulingConstraints `json:"schedulingConstraints,omitempty"`

    // disruptionMode defines how the entire composite group (level 1) can be disrupted.
    // Mutually exclusive with replicatedJobs: see the type-level comment.
    // +optional
    DisruptionMode *schedulingv1alpha3.DisruptionMode `json:"disruptionMode,omitempty"`

    // replicatedJobs specifies per-ReplicatedJob leaf-level (level 2)
    // scheduling overrides. Mutually exclusive with the top-level schedulingPolicy,
    // schedulingConstraints, and disruptionMode fields: see the type-level comment.
    // When set, every ReplicatedJob in the JobSet must be targeted by exactly one
    // entry.
    // +optional
    // +listType=atomic
    // +kubebuilder:validation:MaxItems=50
    ReplicatedJobs []ReplicatedJobScheduling `json:"replicatedJobs,omitempty"`
}

// ReplicatedJobScheduling targets one or more named ReplicatedJobs with
// level 2 (leaf-level) scheduling configuration.
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

    // schedulingPolicy defines the leaf-level (level 2) scheduling policy (basic or
    // gang) for jobs created by the targeted ReplicatedJobs. Defaults to Gang when
    // not specified.
    // +optional
    SchedulingPolicy *schedulingv1alpha3.PodGroupSchedulingPolicy `json:"schedulingPolicy,omitempty"`

    // schedulingConstraints defines leaf-level (level 2) topology constraints for
    // the targeted ReplicatedJobs' pods.
    // +optional
    SchedulingConstraints *schedulingv1alpha3.PodGroupSchedulingConstraints `json:"schedulingConstraints,omitempty"`

    // disruptionMode defines how pods within the targeted ReplicatedJobs can be disrupted.
    // +optional
    DisruptionMode *schedulingv1alpha3.DisruptionMode `json:"disruptionMode,omitempty"`

    // resourceClaims specifies dynamic resource claims shared by the targeted
    // ReplicatedJobs' pods.
    // +optional
    // +listType=atomic
    // +kubebuilder:validation:MaxItems=4
    ResourceClaims []schedulingv1alpha3.PodGroupResourceClaim `json:"resourceClaims,omitempty"`

    // job defines job-level (replica-level / level 3) scheduling
    // configuration, where each replica of the targeted ReplicatedJobs forms its
    // own independent gang (i.e. one PodGroup per Job) instead of sharing a single
    // PodGroup across every replica. This is part of the Gang-of-Gangs model. When
    // set, targetReplicatedJobs must contain exactly one ReplicatedJob name, and the
    // leaf-level schedulingPolicy/schedulingConstraints/disruptionMode/resourceClaims
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
    SchedulingPolicy *schedulingv1alpha3.PodGroupSchedulingPolicy `json:"schedulingPolicy,omitempty"`
    // +optional
    SchedulingConstraints *schedulingv1alpha3.PodGroupSchedulingConstraints `json:"schedulingConstraints,omitempty"`
    // +optional
    DisruptionMode *schedulingv1alpha3.DisruptionMode `json:"disruptionMode,omitempty"`
    // +optional
    // +listType=atomic
    // +kubebuilder:validation:MaxItems=4
    ResourceClaims []schedulingv1alpha3.PodGroupResourceClaim `json:"resourceClaims,omitempty"`
}
```

`JobSetSpec.Scheduling` is optional and is immutable once the JobSet is running (not suspended).

### Defaulting

Defaulting applies only when `spec.scheduling` is present:

- Top-level Gang `minCount` defaults to the sum of `parallelism × replicas` across all
  ReplicatedJobs.
- Per-ReplicatedJob Gang `minCount` defaults to `parallelism × replicas` for that
  ReplicatedJob.

Gang `minCount` defaulting is performed **dynamically by the JobSet controller during
compilation** — the controller computes the represented pod count and writes it to
`PodGroup.spec.minCount` — rather than by the mutating webhook writing a value back into
`JobSet.spec.scheduling`.

For sequenced startup, the composite policy is left unset and the controller applies
per-ReplicatedJob Gang defaults during compilation. An explicit composite Gang `minCount` is
not used in that mode.

### Validation

The validating webhook and `workloadbuilder` enforce that:

- Every policy target names an existing ReplicatedJob and targets are unique.
- Policies are Basic or Gang; disruption modes are Single or All.
- `spec.scheduling` is immutable after jobset is running.
- Top-level and per-ReplicatedJob `minCount` values do not exceed their represented pod counts.
- A single top-level PodGroup uses one `priorityClassName` across all ReplicatedJobs.
- An explicit top-level Gang `minCount` is rejected with `DependsOn` or `InOrder` startup.
- An explicit Gang `minCount` cannot exceed the represented pod count, including after a
  requested downscale; this permanently blocks downscaling
  an ElasticJobSet below that count (see [Scaling](#scaling)) unless `minCount` was left unset.
- If scheduling api is specified then there can be no scheduling api on the job api itself.
- `spec.scheduling` supports exactly one of the two models above: setting a top-level
  (level 1) `schedulingPolicy`, `schedulingConstraints`, or `disruptionMode` together
  with `replicatedJobs` (level 2) is rejected, since alpha does not implement a
  composite PodGroup to link a level 1 policy to level 2 policies. This is unconditional
  in alpha — there is no `JobSetGangOfGangs` feature gate yet to relax it — because
  composite PodGroups do not exist to reconcile the two levels; that gate and the
  composite-PodGroup-backed hierarchy are Alpha 2 (1.38) work (see
  [Gang of Gangs Support](#gang-of-gangs-support) and
  [Graduation Criteria](#graduation-criteria)).
- When only `replicatedJobs` (level 2) is set and no top-level (level 1) field
  is set, every ReplicatedJob must be targeted by exactly one `replicatedJobs`
  entry, since there is no level 1 policy for an untargeted ReplicatedJob to fall back to.
- Per-ReplicatedJob mode produces one PodGroup per resulting template. The upstream Workload
  API caps `podGroupTemplates` at 8 per Workload, while `JobSetSpec.ReplicatedJobs` has no such
  cap; the webhook rejects a JobSet in per-ReplicatedJob mode whose resulting PodGroup count
  would exceed 8, rather than partitioning across multiple Workloads or merging PodGroups.

### Controller Integration

When the feature is enabled and `spec.scheduling` is non-nil:

1. **Top-level mode** is selected when startup is not sequenced and there are no targeted
   overrides, regardless of whether the effective composite policy is Gang or Basic. One
   WorkloadItem represents the entire JobSet. For a Gang policy, its PodGroup uses the total
   represented pod count as `minCount`; for a Basic policy, the PodGroup has no `minCount` and
   each pod is admitted independently.
2. **Per-ReplicatedJob mode** is selected when overrides exist or startup is sequenced. One
   WorkloadItem is built for each ReplicatedJob (or, when a `replicatedJobs` entry targets
   more than one ReplicatedJob, one shared WorkloadItem for that group). A targeted policy takes
   precedence over composite defaults.
3. **Per-Job mode (Gang-of-Gangs)** is selected per `replicatedJobs` entry when that
   entry sets `job`. Each replica (Job) of the targeted ReplicatedJob gets its
   own WorkloadItem and PodGroup instead of sharing one PodGroup across the whole ReplicatedJob,
   so replicas can be gang-scheduled and preempted independently and can carry per-replica
   resource claims. `job` requires `targetReplicatedJobs` to name exactly one
   ReplicatedJob, and is mutually exclusive with the leaf-level `schedulingPolicy`,
   `schedulingConstraints`, `disruptionMode`, and `resourceClaims` fields on the same entry.
4. Each WorkloadItem is compiled with `workloadbuilder`. Because the current builder supports
   only single-item trees, per-ReplicatedJob and per-Job templates are merged into one Workload.
5. The controller creates one PodGroup for each resulting template and sets the JobSet as the
   controller owner of the Workload and PodGroups.
6. Child Jobs receive:
   - `scheduling.k8s.io/group-template-name`, identifying the template; and
   - a pod-template `schedulingGroup.podGroupName` pointing to the PodGroup.

The alpha implementation does not create a `CompositePodGroup`. It therefore does not set
`scheduling.k8s.io/parent-composite-podgroup`; that annotation refers to a
`CompositePodGroup`, not to the owning JobSet.

#### Naming Convention

The Workload is always named after the JobSet with a 10-character SHA-1 hash (`<jobset-name>-<hash>`). Because the current
`workloadbuilder` only supports single-item builder trees, every WorkloadItem the JobSet
produces is built independently and then merged into that one Workload.

Each WorkloadItem — which compiles to one PodGroupTemplate — is named according to how it was
produced:

- **Top-level mode** (composite Gang or Basic policy, no sequenced startup, no targeted
  overrides): the JobSet name, `<jobset-name>-<hash>`.
- **Per-ReplicatedJob mode** (a ReplicatedJob with no `replicatedJobs` entry targeting
  it): the ReplicatedJob's own name, `<replicatedjob-name>-<hash>`.
- **Grouped ReplicatedJobs** (a `replicatedJobs` entry's `targetReplicatedJobs` names more
  than one ReplicatedJob): the target names joined with `-`, e.g. `<target-1>-<target-2>`.
- **Per-Job mode / Gang-of-Gangs** (a `replicatedJobs` entry sets `job`):
  one item per replica, `<replicatedjob-name>-<job-index>-<hash>`.

Generated WorkloadItem names must be unique within a Workload — for example, a ReplicatedJob
literally named `leader-worker` could collide with a group formed from `leader` and `worker`.
The controller detects this and rejects reconciliation with an error naming the collision,
rather than silently merging or overwriting a PodGroupTemplate.

The PodGroup object created for each WorkloadItem follows the same split:

- For the top-level item (composite Gang or Basic policy, no sequenced startup), the PodGroup
  reuses the JobSet name directly: `<jobset-name>-<hash>`.
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

`DependsOn` and `InOrder` create Jobs sequentially, so a single PodGroup requiring all pods could
never reach its `minCount`. The controller therefore uses one Gang PodGroup per ReplicatedJob.
`AnyOrder` does not trigger this fallback.

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
| Delete | OwnerReferences provide cleanup. |
| No scheduling configuration | Create no WAS resources. |
| Gate disabled with scheduling configured | Reject the JobSet at admission. |

### Test Plan

- **Unit** (`pkg/controllers/scheduling_test.go`, `pkg/controllers/scheduling_reconcile_test.go`,
  `pkg/util/scheduling_test.go`): `workloadbuilder` translation for top-level and per-RJ modes,
  per-Job (Gang-of-Gangs) PodGroup templates, group/template naming and collision handling,
  `minCount` computation and defaulting, sequenced-startup fallback to per-RJ PodGroups,
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
  `make test-integration-scheduling` against an envtest `kube-apiserver` built from a
  pre-release Kubernetes tag that registers `scheduling.k8s.io/v1alpha3`, since that API has not
  shipped in a released minor version — see `hack/envtest-scheduling-setup.sh`):
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
  - `DependsOn` and `InOrder` startup falling back to one Gang PodGroup per ReplicatedJob.
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
  - DRA `resourceClaims` propagation from a `ReplicatedJobScheduling` to per-RJ PodGroups,
    including direct `ResourceClaimName` references, multiple claims on one PodGroup, and
    preservation across a suspend-resume cycle.
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

  DRA resource-claim propagation and feature-gate-disabled behavior are covered at the
  integration level only; the E2E suite does not re-test them on a live cluster.

### Kueue Integration

Kueue integration is tracked separately in [issue-13707](https://github.com/kubernetes-sigs/kueue/issues/13707)
and is planned in two phases:

- **Phase 1** (this KEP): JobSet exposes the WAS `scheduling` configuration described here.
  Kueue has two options for queueing JobSets on top of it:
  a. admitting the JobSet as a single, all-or-nothing gang, or
  b. admitting each ReplicatedJob separately via the existing Job integration in kueue.
  Solving the top-level JobSet as a single gang is the simplest option, since it means all pods
  within the JobSet are scheduled together; once this API stabilizes, Kueue's integration should
  read the `scheduling` API to build a representative aggregate `KueueWorkload`.
- **Phase 2** (tracked in [issue-13707](https://github.com/kubernetes-sigs/kueue/issues/13707),
  design owned by Kueue): finer-grained behaviors such as partial eviction, restarting a single
  failed ReplicatedJob instead of the entire JobSet, or shape-aware preemption simulation that
  reads the distinct per-ReplicatedJob `PodGroups` are out of scope for this KEP. This KEP only
  exposes the scheduling shape (Basic/Gang, per-ReplicatedJob PodGroups) that a follow-up
  Kueue-side KEP would need to consume in order to implement that granular behavior; that design
  is expected to be proposed separately in Kueue.

For b in Phase 1, this may need to be handled in Kueue. 
Kueue will have multiple `KueueWorkloads`, but JobSet will create a single workload with multiple PodGroups.
A design will be needed in Kueue to figure out how we want to map this.

### Graduation Criteria

This feature relies on upstream feature gates (GenericWorkload and CompositePodGroup) and APIs.
Workload and PodGroup are beta in 1.37 while CompositePodGroups are alpha in 1.37.

The jobset feature gates will discover the API to determine if the feature can be enabled.

#### Alpha (1.37)

- JobSetWorkloadAwareSchedulingAPI feature gate, API, defaulting, and validation are implemented.
- The controller creates Workloads and PodGroups through `workloadbuilder`.
- Unit, integration, and WAS-enabled E2E coverage is available.

#### Alpha 2 (1.38)

- JobSetGangOfGangs enabled with compositePodGroups

#### Beta (1.38)

- If Workload / PodGroup are enabled / GA on a cluster, then JobSetWorkloadAwareSchedulingAPI will get enabled.
- If CompositePodGroups are enabled, then JobSetGangOfGangs will also get enabled.

#### GA

- No bugs

## Implementation History

- 2026-06-28: KEP created and alpha implementation added, including Workload/PodGroup
  integration, per-ReplicatedJob scheduling, sequenced-startup fallback, lifecycle handling,
  immutable-spec recreation, and DRA resource-claim propagation.

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
