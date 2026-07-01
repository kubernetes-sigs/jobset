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
`targetReplicatedJob`. When enabled, the JobSet controller builds one `Workload`, materializes
one `PodGroup` per scheduling template, and maps child Jobs and Pods to those PodGroups.

### User Stories

| User need | Behavior |
|---|---|
| Gang-schedule a complete training JobSet | A top-level Gang policy creates one PodGroup whose `minCount` is the total pod count. |
| Schedule groups independently | A `replicatedJobPolicies` entry creates one PodGroup per targeted ReplicatedJob; Basic, Gang, topology, disruption, and resource-claim settings can be overridden. |
| Use an independent driver and constrained workers | The driver can use Basic scheduling while workers use a topology-constrained Gang PodGroup. These groups are independent in alpha; there is no composite PodGroup. |
| Keep existing JobSets unchanged | A JobSet without `spec.scheduling` creates no WAS resources. |
| Avoid reserving resources for suspended workloads | Suspended JobSets have their Workload and PodGroups deleted; they are recreated on resume. |
| Combine sequencing with Gang scheduling | `DependsOn` and `InOrder` startup use one PodGroup per ReplicatedJob to avoid deadlock. |
| Scale an elastic workload | Changes to the represented pod count update the generated Gang `minCount`; other template changes recreate the owned scheduling resources. |
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
  replicatedJobPolicies:
    - targetReplicatedJob: launcher
      schedulingPolicy:
        gang: {}
    - targetReplicatedJob: worker
      schedulingPolicy:
        gang: {}
```

Creates: one `Workload` containing one `PodGroup` per targeted ReplicatedJob, named
`<jobset-name>-<replicatedjob-name>` (one PodGroup for `launcher`, one for `worker`).

**Schedule groups together**

```yaml
scheduling:
  replicatedJobPolicies:
    - targetReplicatedJob: [launcher, worker]
      schedulingPolicy:
        gang: {}
```

Creates: one `Workload` containing one `PodGroup` for launcher and worker.
MinCount for gang of launcher and worker is the number of replicas for each summed.

**Independent driver and constrained workers**

```yaml
scheduling:
  replicatedJobPolicies:
    - targetReplicatedJob: driver
      schedulingPolicy:
        basic: {}
    - targetReplicatedJob: worker
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
  replicatedJobPolicies:
    - targetReplicatedJob: worker
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
  replicatedJobPolicies:
  - targetReplicatedJob: worker
    jobPolicies:
      schedulingConstraints:
        topologyRequest: ...
      gang: {}
      disruptionMode: 
        all: {}
      resourceClaim:
        - name: tpu-slice
          resourceClaimTemplate: tpu-slice-template
      
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
  schedulingConstants: ... (topology for all jobs within JobSet)
  disruptionMode: ... (disruption for all pods within jobset)
  replicatedJobPolicy:
    targetReplicatedJobs ..(the below policies apply to these group of replicated jobs)
    schedulingPolicy: .. (gang within all replicas of the ReplicatedJob)
    schedulingConstants: .. (tas requirements for all replicas of ReplicatedJob)
    disruptionMode: .. (disruption within replicas of replicated job)
    jobPolicy:
      gang requirements at the job level
```

With the `JobSetGangOfGangs` feature gate enabled and the CompositePodGroups (CPG) feature,
the top level scheduling field corresponds to the root level.

With compositePodGroups, the top level policy would be a CPG object.
The group of replicated jobs within a `replicatedJobPolicy` would correspond to a CPG also.

And then at the job level, these would be separate PodGroups.

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
  replicatedJobPolicy:
    - targetReplicatedJobs: [ReplicatedJobA]
      disruptionMode: all
      job:
        schedulingPolicy: {}
        schedulingConstraints: rack
        DRA: ... #shared claim for all pods within podgroup

    - targetReplicatedJobs: [ReplicatedJobB]
      disruptionMode: all
      disruptionMode: all
      job:
        schedulingPolicy: {}
        schedulingConstraints: rack

    - targetReplicatedJobs: [RelicatedJobC]
      disruptionMode: all
      job:
        schedulingPolicy: {}
        schedulingConstraints: rack
        DRA: ... #shared claim for all pods within podgroup

```

### API


The api is defined below. 

```go
type JobSetScheduling struct {
    SchedulingPolicy              *schedulingv1alpha3.PodGroupSchedulingPolicy `json:"schedulingPolicy,omitempty"`
    SchedulingConstraints         *schedulingv1alpha3.PodGroupSchedulingConstraints `json:"schedulingConstraints,omitempty"`
    DisruptionMode                *schedulingv1alpha3.DisruptionMode `json:"disruptionMode,omitempty"`
    ReplicatedJobPolicies []ReplicatedJobSchedulingPolicy `json:"replicatedJobPolicies,omitempty"`
}

type ReplicatedJobSchedulingPolicy struct {
    TargetReplicatedJob []string `json:"targetReplicatedJob,omitempty"`
    SchedulingPolicy               *schedulingv1alpha3.PodGroupSchedulingPolicy `json:"schedulingPolicy,omitempty"`
    SchedulingConstraints          *schedulingv1alpha3.PodGroupSchedulingConstraints `json:"schedulingConstraints,omitempty"`
    DisruptionMode                 *schedulingv1alpha3.DisruptionMode `json:"disruptionMode,omitempty"`
    ResourceClaims                 []schedulingv1alpha3.PodGroupResourceClaim `json:"resourceClaims,omitempty"`
    JobSchedulingPolicy            *JobSchedulingPolicy                        `json:"jobSchedulingPolicy,omitempty"`
}

type JobSchedulingPolicy struct {
    SchedulingPolicy               *schedulingv1alpha3.PodGroupSchedulingPolicy `json:"schedulingPolicy,omitempty"`
    SchedulingConstraints          *schedulingv1alpha3.PodGroupSchedulingConstraints `json:"schedulingConstraints,omitempty"`
    DisruptionMode                 *schedulingv1alpha3.DisruptionMode `json:"disruptionMode,omitempty"`
    ResourceClaims                 []schedulingv1alpha3.PodGroupResourceClaim `json:"resourceClaims,omitempty"`
}
```

`JobSetSpec.Scheduling` is optional and immutable.

### Defaulting

Defaulting applies only when `spec.scheduling` is present:

- Normal startup defaults a missing composite policy to Gang.
- Per-ReplicatedJob mode defaults an unset leaf policy to Gang.
- Top-level Gang `minCount` defaults to the sum of `parallelism × replicas` across all
  ReplicatedJobs.
- Per-ReplicatedJob Gang `minCount` defaults to `parallelism × replicas` for that
  ReplicatedJob.

For sequenced startup, the webhook leaves the composite policy unset and the builder applies
per-ReplicatedJob Gang defaults. An explicit composite Gang `minCount` is not used in that mode.

### Validation

The validating webhook and `workloadbuilder` enforce that:

- Every policy target names an existing ReplicatedJob and targets are unique.
- Policies are Basic or Gang; disruption modes are Single or All.
- `spec.scheduling` is immutable after creation.
- Top-level and per-ReplicatedJob `minCount` values do not exceed their represented pod counts.
- A single top-level PodGroup uses one `priorityClassName` across all ReplicatedJobs.
- An explicit top-level Gang `minCount` is rejected with `DependsOn` or `InOrder` startup.
- An explicit Gang `minCount` cannot exceed the represented pod count, including after a
  requested downscale.
- If scheduling api is specified then there can be no scheduling api on the job api itself.
- When JobSetGangOfGangs is disabled, JobSets can not have the top level api
  and the replicatedJob api specified.
- Per-ReplicatedJob mode produces one PodGroup per resulting template. The upstream Workload
  API caps `podGroupTemplates` at 8 per Workload, while `JobSetSpec.ReplicatedJobs` has no such
  cap; the webhook rejects a JobSet in per-ReplicatedJob mode whose resulting PodGroup count
  would exceed 8, rather than partitioning across multiple Workloads or merging PodGroups.

### Controller Integration

When the feature is enabled and `spec.scheduling` is non-nil:

1. **Top-level mode** is selected when startup is not sequenced, there are no targeted
   overrides, and the effective composite policy is Gang. One WorkloadItem represents the
   entire JobSet and its PodGroup uses the total represented pod count as `minCount`.
2. **Per-ReplicatedJob mode** is selected when overrides exist or startup is sequenced. One
   WorkloadItem is built for each ReplicatedJob (or, when a `replicatedJobPolicies` entry targets
   more than one ReplicatedJob, one shared WorkloadItem for that group). A targeted policy takes
   precedence over composite defaults.
3. **Per-Job mode (Gang-of-Gangs)** is selected per `replicatedJobPolicies` entry when that
   entry sets `jobSchedulingPolicy`. Each replica (Job) of the targeted ReplicatedJob gets its
   own WorkloadItem and PodGroup instead of sharing one PodGroup across the whole ReplicatedJob,
   so replicas can be gang-scheduled and preempted independently and can carry per-replica
   resource claims. `jobSchedulingPolicy` requires `targetReplicatedJob` to name exactly one
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

Top-level PodGroups use the JobSet name. Per-ReplicatedJob names use
`<jobset-name>-<replicatedjob-name>`, shortened with a deterministic hash when necessary to
fit the DNS label limit. If `spec.scheduling` is nil, all scheduling logic is skipped.

#### Sequenced Startup

`DependsOn` and `InOrder` create Jobs sequentially, so a single PodGroup requiring all pods could
never reach its `minCount`. The controller therefore uses one Gang PodGroup per ReplicatedJob.
`AnyOrder` does not trigger this fallback.

#### Scaling

The controller patches the generated Gang `minCount` when ElasticJobSet scaling changes the
represented pod count. This preserves the existing Workload and PodGroups and allows the
scheduler to apply the new quorum. If a downscale would make the represented pod count smaller
than an explicitly configured `Gang.minCount`, reconciliation fails validation and leaves the
explicit minimum unchanged; the user must lower that minimum before scaling below it.

Changes to generated template fields other than `Gang.minCount` are handled as immutable
resource changes: the controller deletes and recreates the owned Workload and PodGroups with the
new values.

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
  targets, `minCount` exceeding represented pod counts, `spec.scheduling` immutability, and
  feature-gate-disabled rejection.
- **Integration** (`test/integration/scheduling/scheduling_test.go`, run via
  `make test-integration-scheduling` against an envtest `kube-apiserver` built from a
  pre-release Kubernetes tag that registers `scheduling.k8s.io/v1alpha3`, since that API has not
  shipped in a released minor version — see `hack/envtest-scheduling-setup.sh`):
  - Workload/PodGroup creation for per-RJ leaf overrides, top-level Gang with no overrides, and
    Basic-only top-level and per-RJ modes; child-Job annotation with the owning template name in
    each mode.
  - No scheduling objects created when `spec.scheduling` is nil, and OwnerReferences set on the
    Workload/PodGroups for garbage collection.
  - Suspend/resume lifecycle: no objects on creation while suspended, deletion on suspend,
    recreation on resume, and repeated suspend-resume cycles.
  - `DependsOn` and `InOrder` startup falling back to one Gang PodGroup per ReplicatedJob.
  - Per-Job PodGroups when `jobSchedulingPolicy` is set (Gang-of-Gangs per-Job model).
  - Gang `minCount` patched in place when ElasticJobSet scaling changes parallelism.
  - DRA `resourceClaims` propagation from a `ReplicatedJobSchedulingPolicy` to per-RJ PodGroups,
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
  - Per-Job PodGroups when `jobSchedulingPolicy` is set (Gang-of-Gangs per-Job model).

  DRA resource-claim propagation and feature-gate-disabled behavior are covered at the
  integration level only; the E2E suite does not re-test them on a live cluster.

### Kueue Integration

Kueue integration is tracked separately in [issue-13707](https://github.com/kubernetes-sigs/kueue/issues/13707).
Kueue has two options for queueing jobsets:

a. JobSet is admitted as a single gang
b. ReplicatedJobs are admitted separately via the Job integration

Solving the top level JobSet is the simplest option as this basically means that all the pods within the JobSets are scheduled together.
Once this API stabilies, Kueue's integration should read the `scheduling` API to build a representive `KueueWorkload`.

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

- Depends on pre-stable `scheduling.k8s.io/v1alpha3` APIs.
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
