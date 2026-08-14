---
title: "Job-Level Scheduling (Delegated PodGroup)"
linkTitle: "Job-Level Scheduling"
weight: 2
date: 2026-08-14
description: >
    Enable gang scheduling for a JobSet-owned Job using the native Kubernetes
    Job `spec.scheduling` field and a delegated PodGroup
no_list: true
---

Kubernetes' `WorkloadWithJob` feature gate adds a native `spec.scheduling` field
directly to `batch/v1 Job`. This page explains how that field interacts with
JobSet-owned Jobs, and how to get a working gang-scheduled `PodGroup` for a
JobSet's child Jobs today, with no JobSet controller code changes required.

## Prerequisites

- A Kubernetes 1.38+ cluster with the following feature gates enabled on both
  `kube-apiserver` and `kube-controller-manager`:
  - `GenericWorkload`
  - `WorkloadWithJob`
  - `TopologyAwareWorkloadScheduling` (if you also need topology constraints)
- The `scheduling.k8s.io/v1beta1` `Workload` and `PodGroup` APIs available
  (`kubectl api-resources --api-group=scheduling.k8s.io`).

See the [WAS Kind cluster skill]({{< param github_repo >}}/tree/main/.pi/skills/was-kind-cluster)
or `hack/kind-config-scheduling.yaml` / `make kind-cluster-scheduling` for a
ready-made local dev cluster with these gates enabled.

## Why `spec.scheduling` alone is not enough

`ReplicatedJob.template` is a full `batchv1.JobTemplateSpec`, so JobSet already
passes through anything you set on `template.spec`, including the native
`scheduling` field, verbatim to the Job it creates (JobSet's `constructJob()`
does a plain `DeepCopy()` of the template spec).

However, the upstream Job controller only **automatically** materializes a
`Workload`/`PodGroup` for a Job when that Job is a *root* workload, i.e. it has
no controller owner (see `getManagementMode` in
`kubernetes/pkg/controller/job/job_scheduling_manager.go`). A JobSet's child
Jobs are always owned by the JobSet (`ownerReferences[].controller: true`), so
they are never root workloads — setting `template.spec.scheduling` by itself
on a `ReplicatedJob` has **no effect**: the Job controller will not create a
`Workload` or `PodGroup` for it.

## The delegated PodGroup model

For a non-root Job, the Job controller will still materialize a **PodGroup**
(not a Workload) on the Job's behalf, but only when the Job is explicitly
delegated to do so:

1. A parent-owned `Workload` must already exist, with `spec.controllerRef`
   naming the parent controller (in our case, the JobSet) by `apiGroup`,
   `kind`, and `name`, and a `podGroupTemplates` entry describing the desired
   scheduling policy (e.g. gang with a `minCount`).
2. The child Job must carry the annotation
   `scheduling.k8s.io/group-template-name: <template-name>`, naming the
   `podGroupTemplates` entry to use.

When both are true, the Job controller looks up the parent Workload (matching
purely by `apiGroup`/`kind`/`name`, not by UID), finds the named template, and
creates a `PodGroup` owned by the Job, with `spec.workloadRef` pointing back at
the Workload's template.

Because JobSet copies `ReplicatedJob.template.metadata.annotations` onto the
Job it creates, you can set the delegation annotation directly in your
JobSet spec — no JobSet code changes needed:

### Step 1: Create the parent Workload

The [Workload]({{< param github_repo >}}/blob/main/site/static/examples/workload-aware-scheduling/job-level-scheduling/workload.yaml)
must be created **before** the JobSet. Its `controllerRef` names the JobSet by
group/kind/name (the JobSet does not need to exist yet — the reference is
matched by name, not by UID):

{{< include file="/examples/workload-aware-scheduling/job-level-scheduling/workload.yaml" lang="yaml" >}}

### Step 2: Create the JobSet with the delegation annotation

The [JobSet]({{< param github_repo >}}/blob/main/site/static/examples/workload-aware-scheduling/job-level-scheduling/jobset.yaml)
sets `scheduling.k8s.io/group-template-name` in the ReplicatedJob's
`template.metadata.annotations`, naming the `workers` PodGroupTemplate defined
on the Workload above:

{{< include file="/examples/workload-aware-scheduling/job-level-scheduling/jobset.yaml" lang="yaml" >}}

Once applied, the resulting child Job (`js-delegated-workers-0`) is annotated
with the delegation key, and the Job controller creates a `PodGroup` owned by
that Job:

```bash
$ kubectl get podgroups
NAME                             POLICY   WORKLOAD       STATUS
js-delegated-workers-<hash>      Gang     js-delegated   Scheduled

$ kubectl get podgroup js-delegated-workers-<hash> -o yaml
spec:
  workloadRef:
    workloadName: js-delegated
    templateName: workers
  schedulingPolicy:
    gang:
      minCount: 2
```

## Ordering and timing constraints

The Job controller only creates scheduling objects for a Job the *first* time
it observes it with no pods yet (its "new Job" check). Keep the following in
mind:

- **Create the Workload before the JobSet.** If the JobSet's child Job is
  created first and the Workload appears afterward, the Job controller may
  already have moved past its "new Job" window (its pods may already be
  running) by the time the Workload shows up, and the PodGroup will never be
  created for that Job.
- **Do not rely on `spec.suspend` to buy time.** A Job that has ever carried a
  `Suspended` status condition is treated as not-new *permanently*, even if it
  was suspended from creation and never actually ran a pod. Suspending a
  JobSet to "pause" child Job creation while you set up the Workload will
  prevent delegation from ever succeeding for that Job.
- The delegation annotation must be present on the Job **at creation time**.
  Setting `template.metadata.annotations` in the JobSet spec satisfies this
  automatically, since JobSet creates the Job in a single `Create` call with
  the annotation already populated.

## Relationship to `schedulingGroup.podGroupName`

This is a different mechanism from the one described in
[Gang Scheduling](./gang_scheduling): there, a `PodGroup` is created directly
by the user (or by a controller) and pods reference it explicitly via
`schedulingGroup.podGroupName` on the pod spec. That approach works for any
Job (root or not) but requires you to construct and own the `PodGroup`
yourself, including keeping its `minCount` and other fields consistent with
your JobSet's replica configuration.

The delegated-annotation approach shown on this page instead lets the
upstream Job controller construct the runtime `PodGroup` for you, using the
scheduling policy from the parent Workload's template, and is the model to
use for Jobs (like JobSet's) that are owned by another controller.
