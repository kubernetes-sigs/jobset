# KEP-498: Group Scheduling Support

<!--
This is the title of your KEP. Keep it short, simple, and descriptive. A good
title can help communicate what the KEP is and should be considered as part of
any review.
-->

<!--
A table of contents is helpful for quickly jumping to sections of a KEP and for
highlighting any additional information provided beyond the standard KEP
template.

Ensure the TOC is wrapped with
  <code>&lt;!-- toc --&rt;&lt;!-- /toc --&rt;</code>
tags, and then generate with `hack/update-toc.sh`.
-->

<!-- toc -->
- [Motivation](#motivation)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Proposal](#proposal)
  - [User Stories (Optional)](#user-stories-optional)
    - [Story 1: Group scheduling of all replicated jobs](#story-1-group-scheduling-of-all-replicated-jobs)
  - [Example JobSet spec using group scheduling for all replicated jobs:](#example-jobset-spec-using-group-scheduling-for-all-replicated-jobs)
    - [Story 2: Group scheduling of a specific replicated job](#story-2-group-scheduling-of-a-specific-replicated-job)
  - [Example JobSet spec using group scheduling for all replicated jobs:](#example-jobset-spec-using-group-scheduling-for-all-replicated-jobs-1)
  - [Notes/Constraints/Caveats (Optional)](#notesconstraintscaveats-optional)
  - [Risks and Mitigations](#risks-and-mitigations)
- [Design Details](#design-details)
  - [Proposed Group Scheduling API](#proposed-group-scheduling-api)
  - [Constraints](#constraints)
  - [Implmentation](#implmentation)
    - [Example JobSet spec AFTER webhook injection:](#example-jobset-spec-after-webhook-injection)
  - [Test Plan](#test-plan)
      - [Prerequisite testing updates](#prerequisite-testing-updates)
    - [Unit Tests](#unit-tests)
    - [Integration tests](#integration-tests)
  - [Graduation Criteria](#graduation-criteria)
- [Implementation History](#implementation-history)
- [Drawbacks](#drawbacks)
- [Alternatives](#alternatives)
<!-- /toc -->


## Motivation

Many users of managed K8s services on cloud providers make use of NAP (node auto provisioning) which creates node pools for pending/unschedulable pods, based on those pods requirements (i.e., CPU/memory requirements, GPU/TPU requirements, etc).

Since node pool provisioning takes a variable amount of time, users are running into issues where the first slice finishes provisioning and pods land there and begin running, but eventually timeout before the other slices all finish provisioning and pods land there and become ready.

For statically provisioned clusters, we have recommended users use Kueue to handle group scheduling of JobSets once sufficient resources are available.

However, JobSets running independently (without Kueue) on dynmaically provisioned clusters, we need to support group
scheduling semantics in order to avoid these timeout issues.

### Goals

- Enhance the JobSet API with options allowing the user to define a group scheduling policy.

### Non-Goals

- Upstream changes that would improve/simplify group scheduling support in JobSet.

## Proposal

### User Stories (Optional)

#### Story 1: Group scheduling of all replicated jobs

As a user, in order to make efficient use of expensive accelerator (GPU/TPU) resources, I use node
auto-provisioning to provision infrastructure on an as-needed basis when a pending workload requires
it, then deprovision it once it's no longer in use. Since the completion time of these provisioning
operations is variable, I want to make sure JobSet pods which land on the earliest provisioned nodes
do not start executing the main container until all the infrastructure is provisioned and
all pods have started. Otherwise, the distributed initialization step will timeout in the earliest
pods while waiting for the remaining provisioning to complete.


### Example JobSet spec using group scheduling for all replicated jobs:

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: my-jobset
  namespace: default
spec:
  # main containers in pods part of all replicatedJob will be blocked
  # from execution until all pods have started, or timeout after 5min.
  groupSchedulingConfig:
    timeoutAfterSeconds: 300
  replicatedJobs:
    - name: workers
      replicas: 3
      template:
        spec:
          backoffLimit: 0
          completions: 100
          parallelism: 100
          template:
            spec:
              containers:
                - name: pytorch
                  image: pytorch/pytorch:latest
                  command:
                    - bash
                    - -c
                    - train.py
```


#### Story 2: Group scheduling of a specific replicated job

As a user, I have a JobSet with 2 replicated jobs:
- `workers` which contains my primary batch workload, running on nodes with accelerator chips (GPUs)
- `auxiliary` which contains auxiliary workloads (proxy server, metrics service) running on CPU nodes.

I want my batch workload workers to be scheduled as a group, but the auxiliary pods can start up
individually at any time.

### Example JobSet spec using group scheduling for all replicated jobs:

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: my-jobset
  namespace: default
spec:
  # main containers in pods part of replicatedJob `workers` will be blocked
  # from execution until all pods have started, or timeout after 5min.
  groupSchedulingConfig:
    timeoutAfterSeconds: 300
    targetReplicatedJobs:
    - workers
  replicatedJobs:
    - name: workers
      replicas: 3
      template:
        spec:
          backoffLimit: 0
          completions: 100
          parallelism: 100
          template:
            spec:
              containers:
                - name: pytorch
                  image: pytorch/pytorch:latest
                  command:
                    - bash
                    - -c
                    - train.py
    - name: auxiliary
      replicas: 1
      template:
        spec:
          backoffLimit: 0
          completions: 1
          parallelism: 1
          template:
            spec:
              containers:
                - name: auxiliary
                  image: python:3.10
                  command:
                    - bash
                    - -c
                    - run.py
```

### Notes/Constraints/Caveats (Optional)

<!--
What are the caveats to the proposal?
What are some important details that didn't come across above?
Go in to as much detail as necessary here.
This might be a good place to talk about core concepts and how they relate.
-->

### Risks and Mitigations

<!--
What are the risks of this proposal, and how do we mitigate? Think broadly.
For example, consider both security and how this will impact the larger
Kubernetes ecosystem.

How will security be reviewed, and by whom?

How will UX be reviewed, and by whom?

Consider including folks who also work outside the SIG or subproject.
-->

## Design Details

### Proposed Group Scheduling API

```go

// JobSetSpec defines the desired state of JobSet
type JobSetSpec struct {
        …
        // GroupSchedulingConfig defines the desired group-scheduling behavior
        // for the JobSet. If unspecified, every pod part of this JobSet will
        // start as soon as possible, without waiting for the others.
        GroupSchedulingConfig *GroupSchedulingConfig `json:groupSchedulingConfig`
}

// GroupSchedulingConfig defines the desired group-scheduling behavior for the
// JobSet. If defined, the main container in pods part of the
// TargetReplicatedJobs will be blocked from executing until all pods in these
// jobs have started.
type GroupSchedulingConfig struct {
        // Timeout defines the period after which the injected initContainer
        // (which blocks execution of the main container until all pods are started)
        // will timeout and exit with an error if not all pods have started yet.
        TimeoutAfterSeconds *int32 `json:"timeoutAfterSeconds"

        // TargetReplicatedJobs are the names of the replicated jobs which will
        // be subject to group-scheduling.
        // A null or empty list will apply to all replicatedJobs.
        // +optional
        // +listType=atomic
        TargetReplicatedJobs []string `json:"targetReplicatedJobs,omitempty"`
}
```

### Constraints

- In Order Startup Policy is incompatible with a group scheduling config that spans
multiple replicated jobs, since they are conceptually mutually exclusive. We must
add validation to our JobSet webhook to enforce this.
- The initContainer name will have a reserved name `group-scheduling-init` that is
reserved and cannot be used by the main container.

### Implmentation


A new pod controller will be added: `group_scheduling_pod_controller.go`, which
reconciles on leader pods which are scheduled and whose JobSet have a group scheduling
config defined.

When a GroupSchedulingConfig is defined:

 1. A bash initContainer will be injected into the JobTemplateSpec of every 
    TargetReplicatedJob by the existing pod mutating webhook. 
    The initContainer will have a simple bash loop, checking for a particular
    directory path to exist. Once the path exists, it will exit successfully.
 2. A shared ConfigMap will be created and mounted into the initContainer as an 
    optional volume mount. The ConfigMap will be empty to start, so the mount 
    path will not exist in the initContainer yet.
 3. The pod controller will reconcile on leader pods, listing and
    counting the number of pods which have started in the JobSet. If the started 
    count is less than the expected count, re-enqueue the pod for reconciliation 
    after a short interval.
 4. Once the expected number of pods for the JobSet have started (derived from the
    JobSet spec), the pod controller will update the ConfigMap with some arbitrary data
    so that it is no longer empty. This will trigger Kubelet to populate
    the initContainer mount point. 
 5. Now that the mount point directory path exists in the initContainer, it
    will exit and allow the main container to start.


#### Example JobSet spec AFTER webhook injection:

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: my-jobset
  namespace: default
spec:
  # main containers in pods part of all replicatedJob will be blocked
  # from execution until all pods have started, or timeout after 5min.
  groupSchedulingConfig:
    timeoutAfterSeconds: 300
  replicatedJobs:
    - name: workers
      replicas: 3
      template:
        spec:
          backoffLimit: 0
          completions: 100
          parallelism: 100
          template:
            spec:
              initContainers:
                # Check if the started file is present before exiting and starting the main container.
                - name: group-scheduling-init
                  image: bash
                  command: # TODO: add support for configurable timeout parameter
                    - bash
                    - -c
                    - while true; do if [ -f /mnt/group-scheduling/started ]; then break; fi; sleep 1; done
                  volumeMounts:
                    - mountPath: /mnt/group-scheduling
                      name: group-scheduling
              containers:
                - name: pytorch
                  image: pytorch/pytorch:latest
                  command:
                    - bash
                    - -c
                    - train.py
              volumes:
                - name: group-scheduling
                  configMap:
                    # Format: {jobset-name}-started-threshold
                    name: my-jobset-group-scheduling
                    optional: true
```


### Test Plan

The testing plan will focus on:
- Unit and integration tests to validate the webhook injection works as intended
- E2E tests, since we need pods to be created to inject the initContainer and ConfigMap volume mount.
- Scale tests, performance benchmarking

##### Prerequisite testing updates

<!--
Based on reviewers feedback describe what additional tests need to be added prior
implementing this enhancement to ensure the enhancements have also solid foundations.
-->

#### Unit Tests

<!--
In principle every added code should have complete unit test coverage, so providing
the exact set of tests will not bring additional value.
However, if complete unit test coverage is not possible, explain the reason of it
together with explanation why this is acceptable.
-->

<!--
Additionally, try to enumerate the core package you will be touching
to implement this enhancement and provide the current unit coverage for those
in the form of:
- <package>: <date> - <current test coverage>

This can inform certain test coverage improvements that we want to do before
extending the production code to implement this enhancement.
-->

- `controllers`: `04/05/2024` - `25.4%`

#### Integration tests
- JobSet webhook integration tests validating that for JobSets with group scheduling
config defined, the ConfigMap is created, and the correct child Jobs have the expected
initContainers and ConfigMap volume mounts injected.
- JobSet controller integration tests validating that for JobSets with group scheduling
config defined, the ConfigMap used for broadcasting the startup signal is created.

### Graduation Criteria

<!--

Clearly define what it means for the feature to be implemented and
considered stable.

If the feature you are introducing has high complexity, consider adding graduation
milestones with these graduation criteria:
- [Maturity levels (`alpha`, `beta`, `stable`)][maturity-levels]
- [Feature gate][feature gate] lifecycle
- [Deprecation policy][deprecation-policy]

[feature gate]: https://git.k8s.io/community/contributors/devel/sig-architecture/feature-gates.md
[maturity-levels]: https://git.k8s.io/community/contributors/devel/sig-architecture/api_changes.md#alpha-beta-and-stable-versions
[deprecation-policy]: https://kubernetes.io/docs/reference/using-api/deprecation-policy/
-->


## Implementation History

- KEP published April 5th, 2024

## Drawbacks

Once the ConfigMap has data, it takes up to a full Kubelet sync period (1 minute) for the mount
path to exist on the nodes. This will delay startup of the workload. We can force Kubelet to
update the pod earlier if we update the pod annotaitons, but this would require O(number of pods)
calls to the apiserver and writes etcd, which is not scalable.

## Alternatives

One alternative would be for the initContainer to be running Go code which has a ConfigMap Lister
to maintain a local cache of ConfigMaps in the cluster and receive updates via server-sent events.
The initContainer in each pod could increment a counter in the ConfigMap, and once the ConfigMap
counter is equal to the expected number of pods in the JobSet, the initContainer would exit. 

However, even using Listers to reduce strain on the apiserver, this is not a scalable since there
would be lots of request retries to increment the counter (sine ConfigMap resource version will
be changing with every write, and writes referencing an old resource version will be rejected
and need to be retried).