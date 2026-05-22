# KEP-NNNN: Publish resolved exclusive-topology value as a pod label

<!-- toc -->
- [Summary](#summary)
- [Motivation](#motivation)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Proposal](#proposal)
  - [User Story](#user-story)
  - [Risks and Mitigations](#risks-and-mitigations)
- [Design Details](#design-details)
  - [API](#api)
  - [Implementation](#implementation)
  - [Validation](#validation)
  - [Conflict policy](#conflict-policy)
  - [Test Plan](#test-plan)
    - [Unit Tests](#unit-tests)
    - [Integration tests](#integration-tests)
  - [Graduation Criteria](#graduation-criteria)
- [Implementation History](#implementation-history)
- [Drawbacks](#drawbacks)
- [Alternatives](#alternatives)
  - [Init container with <code>get nodes</code> RBAC](#init-container-with--rbac)
  - [Direct env var injection into containers](#direct-env-var-injection-into-containers)
  - [Post-scheduling controller patching of leaders](#post-scheduling-controller-patching-of-leaders)
  - [<code>pods/binding</code> admission webhook for leaders](#-admission-webhook-for-leaders)
<!-- /toc -->

## Summary

JobSet's existing `alpha.jobset.sigs.k8s.io/exclusive-topology` annotation pins all
pods of a Job to a single topology domain (rack, zone, node pool, NVLink island,
etc.) by writing the resolved value into `pod.spec.nodeSelector` for follower
pods. Containers, however, cannot read `spec.nodeSelector` through the Kubernetes
[Downward API][downward], so workloads that need their own topology identifier at
runtime — e.g. Ray's "Label Locality" feature expecting
`ray start --labels=ray.io/gpu-domain=<rack>` — currently require an init
container that calls `GET /api/v1/nodes/<name>` with cluster-scoped `get nodes`
RBAC.

This KEP proposes a small, opt-in alpha extension: a new annotation
`alpha.jobset.sigs.k8s.io/exclusive-topology-label-key` whose value is a pod
label key. When set alongside `exclusive-topology`, the JobSet pod mutating
webhook copies the resolved topology value (which it already computes for
`spec.nodeSelector`) onto follower pods under that label key, so containers can
consume it via the standard Downward API
(`fieldRef: metadata.labels['…']`). Leader pods are explicitly out of scope in
alpha — the value is not knowable at pod-create admission time.

[downward]: https://kubernetes.io/docs/concepts/workloads/pods/downward-api/

## Motivation

Distributed runtimes such as [Ray][ray] (with its alpha-stage [Label Locality
scheduling][label-locality]), MPI launchers building hostfiles per rack, and
collective-communication libraries that key NCCL/NVLink topology by domain all
need a stable, runtime-readable rack/domain identifier inside the container.

Kubernetes does not expose arbitrary node labels through the Downward API
([reference][downward-fields]). KEP-4742 (`PodTopologyLabelsAdmission`) covers
this only for a hardcoded set of standard topology labels
(`topology.kubernetes.io/zone`, `topology.kubernetes.io/region`,
`kubernetes.io/hostname`) and explicitly leaves non-standard labels to custom
mutating admission webhooks.

JobSet's exclusive-topology mechanism already resolves a topology value during
pod admission: the pod mutating webhook in `pkg/webhooks/pod_webhook.go` reads
the leader pod's bound node and writes
`node.Labels[topologyKey]` into `pod.spec.nodeSelector` for follower pods. The
value is right there at admission time. This proposal makes that value visible
to containers without requiring every workload to roll its own init container
plus `get nodes` RBAC.

[ray]: https://github.com/ray-project/ray
[label-locality]: https://github.com/ray-project/ray/pull/61442
[downward-fields]: https://kubernetes.io/docs/concepts/workloads/pods/downward-api/#available-fields

### Goals

- Publish the resolved exclusive-topology value onto follower pods as a label
  under a user-chosen key.
- Provide an opt-in alpha API via annotation; preserve backward compatibility
  with every existing `exclusive-topology` user.
- Reuse the value already computed by the existing pod mutating webhook (no new
  Kubernetes API calls per pod beyond what is already made).
- Surface invalid annotation combinations at admission time with clear errors.

### Non-Goals

- Labeling **leader** pods. At pod-create admission time, a leader pod's
  `spec.nodeName` is empty and the bound node is not yet known. Any leader-path
  solution requires either a `pods/binding` subresource webhook or a
  post-scheduling controller patch (the latter creates a permanent empty-value
  footgun for any container that captures the label via `fieldRef` env var at
  start). Both add new webhook infrastructure and are deferred to a follow-up
  KEP. Ray workers are followers, so the alpha use case still works without
  leader labeling; the head pod typically doesn't need rack info.
- Direct injection of env vars into containers. Labels are more general; the
  Downward API can promote a label to either an env var or a file. Avoiding env
  injection sidesteps design questions around which containers, init container
  vs. main container, conflict policy with user-defined env, etc.
- Propagation of arbitrary node labels onto pods. This proposal only publishes
  the value of the node label identified by the existing
  `exclusive-topology` annotation.
- Replacing the existing `alpha.jobset.sigs.k8s.io/node-selector` fast path.
  That path bypasses the pod mutating webhook entirely; this new annotation is
  incompatible with it and is rejected at admission when both are set.

## Proposal

Add one new alpha annotation,
`alpha.jobset.sigs.k8s.io/exclusive-topology-label-key`. Its value is the
destination pod label key. When set alongside the existing
`alpha.jobset.sigs.k8s.io/exclusive-topology` annotation, the JobSet pod
mutating webhook copies the resolved topology value onto each follower pod's
`metadata.labels` under that key. Containers consume it via the standard
Downward API.

Like `exclusive-topology`, the new annotation can be set at the JobSet level or
on a ReplicatedJob template, with the ReplicatedJob-level setting overriding
the JobSet-level setting.

### User Story

A team running [Ray][ray] on a Kubernetes cluster with multi-rack NVLink-domain
GB200/GB300 nodes wants Ray's Label Locality feature to pin a placement group
to a single rack. Ray Label Locality is triggered by a Ray-level node label
`ray.io/gpu-domain` whose value identifies the rack. The team's cluster
operator has labeled each Kubernetes node with the rack id
(e.g. `topology.example.com/nvlink-domain=rack-DH2-096`).

Without this proposal, the team must add an init container to every Ray worker
pod that calls `kubectl get node $POD_NODE_NAME -o jsonpath=...` and writes the
value to a shared emptyDir, then start `ray start` with
`--labels=ray.io/gpu-domain=$(cat /labels/rack)`. This requires creating a
`ServiceAccount` with cluster-scoped `get nodes` permission per workload
namespace.

With this proposal, the team sets two annotations on their JobSet:

```yaml
metadata:
  annotations:
    alpha.jobset.sigs.k8s.io/exclusive-topology: topology.example.com/nvlink-domain
    alpha.jobset.sigs.k8s.io/exclusive-topology-label-key: ray.io/gpu-domain
```

and reads the value via Downward API in their Ray worker container spec:

```yaml
env:
  - name: RAY_GPU_DOMAIN
    valueFrom:
      fieldRef:
        fieldPath: metadata.labels['ray.io/gpu-domain']
command: ["/bin/bash", "-c"]
args:
  - exec ray start --address=... --labels="ray.io/gpu-domain=$RAY_GPU_DOMAIN" --block
```

No `get nodes` RBAC, no init container, no shared volume.

### Risks and Mitigations

- **Label-key collisions with user-supplied pod labels.** If the destination
  label key is already set on the pod template with a value different from the
  resolved topology value, the pod mutating webhook returns an admission error
  and the pod is rejected. If the existing value already matches, the webhook
  is a no-op.
- **Mismatched JobSet vs. ReplicatedJob scoping.** The JobSet validating
  webhook ensures the new annotation is only valid when paired with
  `exclusive-topology` on the same scope (JobSet or ReplicatedJob template),
  matching the existing scoping rules.
- **Cluster-wide failure modes are unchanged.** The only new webhook code runs
  in the existing pod mutating webhook's follower path, which is already gated
  on `exclusive-topology` being set. Workloads that don't opt into the new
  annotation see no behavior change.

## Design Details

### API

One new constant in `api/jobset/v1alpha2/jobset_types.go`:

```go
// ExclusiveTopologyLabelKey is an annotation that can be set on the JobSet or on
// a ReplicatedJob template. When set alongside ExclusiveKey, the JobSet pod
// mutating webhook copies the resolved exclusive-topology value (read from the
// leader pod's Node label identified by ExclusiveKey) onto follower pods under
// this label key, so the container can read it via the Downward API
// (e.g. fieldRef: metadata.labels['ray.io/gpu-domain']).
//
// Applies only to follower pods (job completion index != 0). Leader pods are not
// labeled because the topology value is not knowable at pod-create admission
// time. Requires alpha.jobset.sigs.k8s.io/exclusive-topology on the same scope.
// Incompatible with alpha.jobset.sigs.k8s.io/node-selector.
ExclusiveTopologyLabelKey string = "alpha.jobset.sigs.k8s.io/exclusive-topology-label-key"
```

### Implementation

Four code sites:

1. **Annotation propagation** (`pkg/controllers/jobset_controller.go`,
   `labelAndAnnotateObject`): the new annotation is copied from
   `JobSet.metadata.annotations` and `ReplicatedJob.Template.metadata.annotations`
   into the child Job's annotations and pod template annotations, with the
   ReplicatedJob-level value overriding the JobSet-level value. Identical
   pattern to the existing `ExclusiveKey` and `NodeSelectorStrategyKey`
   propagation. Annotation only, not propagated as a label (the label value is
   computed at admission time, not at controller-time).

2. **Follower mutation** (`pkg/webhooks/pod_webhook.go`, `setNodeSelector`):
   after the existing
   `pod.Spec.NodeSelector[topologyKey] = topologyValue` write, when the
   `ExclusiveTopologyLabelKey` annotation is set, the new helper
   `setExclusiveTopologyLabel(pod, outKey, topologyValue)` writes the value to
   `pod.Labels[outKey]`. The helper returns a non-nil error if the label is
   already present with a different value; the error surfaces as an admission
   rejection.

3. **JobSet validation** (`pkg/webhooks/jobset_webhook.go`, `ValidateCreate`
   and `ValidateUpdate`): a new helper
   `validateExclusiveTopologyLabelKeyAnnotations` checks every annotation set
   (JobSet-level and per-ReplicatedJob-template) and rejects:
   - the annotation set without `exclusive-topology` on the same scope,
   - the annotation set together with `node-selector` strategy on the same scope,
   - empty values, whitespace-padded values, or values that fail
     `validation.IsQualifiedName`,
   - the new annotation and `node-selector` strategy splitting across the
     JobSet and ReplicatedJob template scopes (per-scope checks pass but
     the runtime would silently no-op the label publish because the pod
     mutating webhook bails on `node-selector` strategy).

4. **No new feature gate.** This is an opt-in extension to an existing opt-in
   alpha annotation with no default behavior change. When the new annotation
   is absent the controller and webhook paths run identically to today, and
   when the existing `exclusive-topology` annotation is absent the new
   annotation has no effect (and is rejected by the JobSet validating
   webhook). The cluster-wide kill-switch a feature gate would provide is
   therefore equivalent to the per-workload kill-switch the annotation
   already provides: not setting it. `kep.yaml` carries `feature-gates: []`
   and `disable-supported: false` to make this explicit. If maintainers
   prefer a runtime gate during alpha, one can be added following the
   `InPlaceRestart` precedent: one entry in `pkg/features/features.go` and a
   runtime check at the webhook entry points (JobSet `ValidateCreate` /
   `ValidateUpdate`, controller `labelAndAnnotateObject`, pod webhook
   `setExclusiveTopologyLabel`). Defining the off-behavior at each of those
   surfaces would be part of that follow-up.

### Validation

`validation.IsQualifiedName` from `k8s.io/apimachinery/pkg/util/validation`
enforces Kubernetes label-key syntax (optional `<prefix>/<name>` with
DNS-subdomain prefix and DNS-1123 name). Leading and trailing whitespace are
rejected without trimming — Kubernetes annotation values are literal strings
and silent normalization would create surprising API behavior.

### Conflict policy

If the destination label is already present on the pod with the same value (as
might happen if the user pre-populates it in their template), the webhook is a
no-op. If it's present with a different value, the webhook returns an admission
error. Silent overwrite would violate template ownership; skipping with only a
warning would start the container without the runtime signal the user
explicitly opted in to.

### Test Plan

#### Unit Tests

- `pkg/webhooks/pod_webhook_test.go`: extended follower-with-exclusive-placement
  table-driven cases covering (a) annotation present, label written;
  (b) existing label same value, no-op; (c) existing label different value,
  rejected; (d) leader pod with annotation, no label written (leader path
  unchanged).
- `pkg/webhooks/jobset_webhook_test.go`: new `exclusiveTopologyLabelKeyTests`
  group covering (e) valid pairing accepted on JobSet level;
  (f) valid pairing accepted on ReplicatedJob template;
  (g) annotation without `exclusive-topology` rejected;
  (h) annotation with `node-selector` strategy rejected;
  (i) empty value rejected; (j) whitespace-padded value rejected;
  (k) invalid label-key syntax rejected;
  (l) annotation on ReplicatedJob without `exclusive-topology` on RJ rejected.
- `pkg/controllers/jobset_controller_test.go`: extended exclusive-topology
  cases assert the new annotation propagates from JobSet/ReplicatedJob onto
  child Jobs and pod templates with the standard override semantics.

#### Integration tests

- `test/integration/webhook/jobset_webhook_test.go`: new ginkgo entries verify
  end-to-end through the API server that JobSets are rejected when the new
  annotation is invalid, and accepted in valid pairings.

### Graduation Criteria

Alpha → Beta:

- At least one external adopter using the feature in a non-toy workload.
- No reported correctness bugs against the alpha API in a release cycle.
- Design accepted for an eventual leader-path extension (likely via
  `pods/binding` admission webhook in a follow-up KEP).
- Annotation key promotion: rename to `jobset.sigs.k8s.io/exclusive-topology-label-key`
  alongside the existing `alpha.jobset.sigs.k8s.io/exclusive-topology-label-key`
  for one release of overlap, then deprecate the alpha form.

## Implementation History

- 2026-05-13: KEP and alpha implementation drafted in a single PR.

## Drawbacks

- **Leader pods are not labeled** in alpha. Workers are the pods that actually
  need the rack label, and workers are followers, so the gap doesn't block the
  alpha use case. It becomes a real limitation only when a workload needs
  leader-side visibility.
- **Two annotations are required** for the feature
  (`exclusive-topology` + `exclusive-topology-label-key`). This is intentional:
  the new annotation does not duplicate the placement signal, only the
  visibility signal.
- **Naming is verbose.** `alpha.jobset.sigs.k8s.io/exclusive-topology-label-key`
  is long. The verbosity is intentional: alpha APIs should optimize for
  unambiguous review, not elegance. A shorter rename (`topology-label`)
  would be ambiguous about whether it configures the topology source.

## Alternatives

### Init container with `get nodes` RBAC

Current status quo. Every workload that wants the rack value adds an init
container plus cluster-scoped `get nodes` RBAC for its `ServiceAccount`. This
works but: (a) is per-workload boilerplate, (b) requires cluster-scoped read
access on every namespace that runs such a workload, (c) duplicates work the
JobSet pod webhook already does. The motivation for this KEP is to eliminate
this boilerplate.

### Direct env var injection into containers

The webhook could write an env var into `pod.spec.containers[*].env` instead of
a label. Pros: no DNS-1123 label-value syntax concerns (label values copied
verbatim from `node.Labels` already satisfy that grammar, but env values are
unrestricted). Cons: (a) needs decisions about which containers to mutate, init
containers, sidecars, conflict policy with user-defined env; (b) labels are
strictly more general — the Downward API can promote any label to either an env
var or a file at user's discretion. This is the same shape KEP-4742 chose for
the analogous in-tree problem: stamp a label, let the workload pick how to
read it.

### Post-scheduling controller patching of leaders

A reconciler could watch leader pods and patch their `metadata.labels` once
`spec.nodeName` is set. This works for Downward API _volume_ mounts (kubelet
re-reads the volume contents), but breaks for `fieldRef` env vars, which are
captured at container start. Containers that consume the label via `fieldRef`
env vars would see an empty string permanently. This is a worse failure mode
than honestly not supporting leaders in alpha.

### `pods/binding` admission webhook for leaders

The correct way to label leaders before container start is a mutating
admission webhook on the `pods/binding` subresource — exactly the pattern
KEP-4742 (`PodTopologyLabelsAdmission`) uses in-tree. The webhook would observe
the Binding, read the bound node, and issue a Patch to the Pod's
`metadata.labels`. This works for both leaders and followers symmetrically.
However, it introduces: (a) a second webhook configuration with `failurePolicy`
trade-offs (a strict failure policy can wedge cluster-wide binding on any
webhook outage), (b) new `get`/`patch pods` cluster-wide RBAC, (c) a side-effect
Patch pattern that some reviewers dislike.

Too much surface for a first iteration. Punting leader-path support to a
follow-up KEP keeps the alpha proposal small and lets the worker-only path
unblock real usage now. The binding-webhook design can be debated on its own
merits later.
