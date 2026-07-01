# KEP-969: Workload-Aware Scheduling Integration

<!-- toc -->
- [Summary](#summary)
- [Motivation](#motivation)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Proposal](#proposal)
  - [User Stories](#user-stories)
- [Risks and Mitigations](#risks-and-mitigations)
- [Design Details](#design-details)
  - [API](#api)
  - [Defaulting](#defaulting)
  - [Validation](#validation)
  - [Controller Integration](#controller-integration)
    - [Sequenced Startup](#sequenced-startup)
    - [Scaling](#scaling)
  - [Workload Lifecycle](#workload-lifecycle)
  - [Test Plan](#test-plan)
  - [Graduation Criteria](#graduation-criteria)
    - [Alpha](#alpha)
    - [Beta](#beta)
- [Implementation History](#implementation-history)
- [Drawbacks](#drawbacks)
- [Alternatives](#alternatives)
  - [Template Delegation Model](#template-delegation-model)
<!-- /toc -->

## Summary

This KEP integrates the [KEP-6089 Workload-Aware Scheduling (WAS) Controller APIs][kep-6089]
with JobSet. An optional `spec.scheduling` field lets users configure gang scheduling, topology
constraints, disruption modes, and ReplicatedJob-level resource claims. The feature is opt-in and
behind the `WorkloadAwareScheduling` alpha feature gate.

[kep-6089]: https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/6089-was-controller-apis

## Motivation

Distributed training and HPC workloads often need all pods admitted together, placed within a
specific topology domain, or disrupted as a group. JobSet currently has no native API for these
requirements. WAS supplies standard scheduling APIs and a `workloadbuilder` library that JobSet
can compile into `Workload` and `PodGroup` resources.

### Goals

- Add optional, centralized scheduling configuration to `JobSetSpec`.
- Support Basic and Gang policies, topology constraints, disruption modes, and shared
  ReplicatedJob-level resource claims.
- Support both one gang for the entire JobSet and independent gangs per ReplicatedJob.
- Make the scheduling configuration available to integrations such as Kueue, so they can
  select the appropriate workload representation and admission behavior.
- Preserve existing behavior when scheduling is omitted.

### Non-Goals

- Implementing scheduler-side Gang, TAS, or WAP behavior.
- Implementing Kueue admission or queue management. Kueue integration is limited to exposing
  enough scheduling configuration for Kueue to understand the JobSet's workload shape; a
  follow-up Kueue design must define queueing and partial-eviction behavior.
- Adding scheduling fields to the upstream Job API.
- Creating a composite/parent PodGroup hierarchy in the alpha implementation.
- Delegated lifecycle management of scheduling objects.

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
| Run a TPU multi-slice workload | A JobSet can use a top-level Gang and topology constraints to request coordinated placement for TPU slices. The TPU-specific pod resources and topology values remain in the JobSet pod templates. |
| Recover one failed component | Partial eviction and rescheduling of an individual ReplicatedJob remain future work; alpha restarts follow JobSet's existing group-level semantics. |

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| Upstream `scheduling.k8s.io/v1alpha3` APIs are unstable | Keep the feature alpha and disabled by default. |
| Alpha has no composite PodGroup | Document that only top-level Gang without overrides or sequencing is JobSet-wide atomic; evaluate a hierarchy before graduation. |
| Workload and PodGroup template specs change | Patch the generated Gang `minCount` for represented pod-count changes; detect other template drift and delete/recreate the owned resources. |
| `workloadbuilder` API changes | Isolate the mapping code and pin the Kubernetes dependency version. |

## Design Details

### API

`JobSetScheduling` contains composite defaults and targeted ReplicatedJob policies:

```go
type JobSetScheduling struct {
    Policy              *schedulingv1alpha3.PodGroupSchedulingPolicy `json:"policy,omitempty"`
    Constraints         *schedulingv1alpha3.PodGroupSchedulingConstraints `json:"constraints,omitempty"`
    Disruption          *schedulingv1alpha3.DisruptionMode `json:"disruption,omitempty"`
    ReplicatedJobPolicies []ReplicatedJobSchedulingPolicy `json:"replicatedJobPolicies,omitempty"`
}

type ReplicatedJobSchedulingPolicy struct {
    TargetReplicatedJob string `json:"targetReplicatedJob,omitempty"`
    Policy               *schedulingv1alpha3.PodGroupSchedulingPolicy `json:"policy,omitempty"`
    Constraints          *schedulingv1alpha3.PodGroupSchedulingConstraints `json:"constraints,omitempty"`
    Disruption           *schedulingv1alpha3.DisruptionMode `json:"disruption,omitempty"`
    ResourceClaims       []schedulingv1alpha3.PodGroupResourceClaim `json:"resourceClaims,omitempty"`
}
```

`JobSetSpec.Scheduling` is optional and immutable. The composite type intentionally has no
`resourceClaims` field; shared claims are scoped to ReplicatedJob PodGroups.

For DRA, the PodGroup-level `resourceClaims` entry describes the shared claim, but does not
implicitly add a claim to every pod. The generated pod template must contain a matching
`PodResourceClaim` whose `resourceClaimName` refers to the shared claim (for example, the claim
created from `imex-channel-template`). A separate `PodResourceClaim` using
`gpu-template` remains a per-pod claim. In other words, the generated worker pod wiring is
conceptually:

```yaml
resources:
  claims:
    - name: imex-channel
      resourceClaimName: <shared-imex-claim>
    - name: gpu
      resourceClaimTemplateName: gpu-template
```

The shared claim is declared once on the ReplicatedJob's PodGroup policy and referenced by each
pod; the GPU claim is independently created for each pod.

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

- The feature gate is enabled.
- Every policy target names an existing ReplicatedJob and targets are unique.
- Policies are Basic or Gang; disruption modes are Single or All.
- `spec.scheduling` is immutable after creation.
- Top-level and per-ReplicatedJob `minCount` values do not exceed their represented pod counts.
- A single top-level PodGroup uses one `priorityClassName` across all ReplicatedJobs.
- An explicit top-level Gang `minCount` is rejected with `DependsOn` or `InOrder` startup.
- An explicit Gang `minCount` cannot exceed the represented pod count, including after a
  requested downscale.

### Controller Integration

When the feature is enabled and `spec.scheduling` is non-nil:

1. **Top-level mode** is selected when startup is not sequenced, there are no targeted
   overrides, and the effective composite policy is Gang. One WorkloadItem represents the
   entire JobSet and its PodGroup uses the total represented pod count as `minCount`.
2. **Basic-only top-level mode** is selected when startup is not sequenced, there are no
   targeted overrides, and the effective composite policy is Basic. It still creates one
   Workload and one PodGroup for the entire JobSet, but the PodGroup uses Basic semantics
   rather than requiring all pods to be admitted together.
3. **Per-ReplicatedJob mode** is selected when overrides exist or startup is sequenced. One
   WorkloadItem is built for each ReplicatedJob. A targeted policy takes precedence over
   composite defaults.
4. Each WorkloadItem is compiled with `workloadbuilder`. Because the current builder supports
   only single-item trees, per-ReplicatedJob templates are merged into one Workload.
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

- **Unit:** builder modes, defaults, minCount calculations, overrides, resource claims,
  sequencing, naming, and immutable-resource recreation.
- **Webhook:** invalid targets, duplicate targets, policy and minCount validation, priority
  consistency, sequencing restrictions, immutability, and feature-gate behavior.
- **Integration:** Workload/PodGroup creation, ownership, Basic-only top-level mode,
  suspend/resume, scaling, DRA claim propagation, and gate-disabled behavior.
- **E2E:** top-level Basic and Gang, per-ReplicatedJob policies, suspension, sequencing,
  preemption, DRA claims, and scaling on a WAS-enabled cluster.

### Graduation Criteria

#### Alpha

- Feature gate, API, defaulting, and validation are implemented.
- The controller creates Workloads and PodGroups through `workloadbuilder`.
- Unit, integration, and WAS-enabled E2E coverage is available.

#### Beta

- Early-adopter feedback is collected.
- Gang and topology behavior is verified on a real WAS-enabled cluster.
- Composite hierarchy and delegated lifecycle management are evaluated.
- The feature gate may be enabled by default after the API and lifecycle design stabilize.

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
