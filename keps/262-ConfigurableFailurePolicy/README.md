# KEP-262: Configurable Failure Policy

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
    - [Story 1: FailJobSet](#story-1-failjobset)
    - [Story 2: RestartJobSet](#story-2-restartjobset)
    - [Story 3: RestartJobSetAndIgnoreMaxRestarts](#story-3-restartjobsetandignoremaxrestarts)
    - [Story 4: Different failure policies for different replicated jobs](#story-4-different-failure-policies-for-different-replicated-jobs)
  - [Notes/Constraints/Caveats (Optional)](#notesconstraintscaveats-optional)
  - [Risks and Mitigations](#risks-and-mitigations)
- [Design Details](#design-details)
  - [Proposed Failure Policy API](#proposed-failure-policy-api)
  - [Constraints](#constraints)
  - [Implementation](#implementation)
  - [Test Plan](#test-plan)
      - [Prerequisite testing updates](#prerequisite-testing-updates)
    - [Unit Tests](#unit-tests)
    - [Integration tests](#integration-tests)
  - [Graduation Criteria](#graduation-criteria)
- [Implementation History](#implementation-history)
- [Drawbacks](#drawbacks)
- [Alternatives](#alternatives)
- [Future Work](#future-work)
<!-- /toc -->


## Motivation

JobSet's current FailurePolicy API only allows specifying the max number of JobSet restarts to attempt before marking it as failed.

Users have requested that we should also allow them to configure the failure conditions under which the JobSet will be restarted,
or when the JobSet should be failed immediately without going through the allowed number of restarts. 

This will allow users to make more efficient use of their computing resources, by avoiding `maxRestarts` number of unnecessary 
JobSet restarts in the case of a non-retriable failure (e.g., an application code bug), while still restarting in the event of
retriable failures (e.g., a host maintenace event).

This is especially important for large scale distributed training workloads on expensive hardware accelerators. Recreating the 
entire JobSet may take ~1 minute or so (per latest scale tests on ~15k nodes), and if `maxRestarts`
is set to a very high number to allow for disruptions (which occur often at a large scale like this), the workload could
waste at least `maxRestarts` number of minutes repeatedly recreating despite the fact that it is doomed to fail due to a non-retriable error like an application code bug. This is very expensive and wasteful on expensive, large scale clusters.

We need to define a more configurable Failure Policy API which will allow users to define their own restart and failure semantics, 
based on the type of child job failure occurring.

### Goals

- Enhance the Failure Policy API with options allowing the user to control the restart and failure policies of the JobSet.

### Non-Goals

- Handling every possible type of restart and failure semantics. We just want to provide enough flexibility to handle common use
cases.

## Proposal

### User Stories (Optional)

#### Story 1: FailJobSet

As a user, in order to use my computing resources as efficiently as possible, I want to 
configure my JobSet to restart in the event of a child job failure due to a retriable error
like host maintenance, but to fail the JobSet immediately without any unnecessary restart 
attempts in the event of an non-retriable application code error. 

**Example Failure Policy Configuration for this use case**:

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: fail-jobset-example
spec:
  # Failure Policy configured to fail the JobSet immediately if a job failure reason for any child job was NOT due to a SIGTERM (e.g. host maintenance).
  # Otherwise, restart up to 10 times.
  failurePolicy:
    rules:
    - action: FailJobSet
      onJobFailureReasons: 
      - PodFailurePolicy
    maxRestarts: 10
  replicatedJobs:
  - name: buggy-job 
    replicas: 1
    template:
      spec:
        parallelism: 1
        completions: 1
        backoffLimit: 0
        # podFailurePolicy which fails job immediately if job was not killed by SIGTERM (i.e., graceful node shutdown for maintenance events)
        podFailurePolicy:
          rules:
          - action: FailJob
            onExitCodes:
              containerName: main
              operator: NotIn
              values: [143] # SIGTERM = exit code 143
        template:
          spec:
            restartPolicy: Never
            containers:
            - name: main
              image: bash:latest
              image: docker.io/library/bash:5
              command: ["bash"]
              args:
              - -c
              - echo "Hello world! I'm going to exit with exit code 1 to simulate a software bug." && sleep 20 && exit 1
```

#### Story 2: RestartJobSet

As a user, I am running a distributed ML training workload using a JobSet. When any worker fails, I want all of the workers
to restart together. The JobSet should only restart some finite number of times before failing, so that if there is a bug
the JobSet does not restart indefinitely, hogging compute resources unnecessarily.

**Example Failure Policy Configuration for this use case**:

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: restart-jobset-example
spec:
  # Failure Policy configured to restart the JobSet upon any failure, up to 10 times.
  # Otherwise, restart up to 10 times. This rule applies to any failure type for all
  # replicated jobs.
  failurePolicy:
    rules:
    - action: RestartJobSet 
    maxRestarts: 10
  replicatedJobs:
  - name: buggy-job 
    replicas: 1
    template:
      spec:
        parallelism: 4
        completions: 4
        backoffLimit: 0
        template:
          spec:
            restartPolicy: Never
            containers:
            - name: main
              image: bash:latest
              image: python:3.8
              command: 
              - |
                python3 train.py
```

#### Story 3: RestartJobSetAndIgnoreMaxRestarts

As a user, in order to use my computing resources more efficiently, I want to 
configure my JobSet to restart unlimited times for child job failures due to host maintenance,
but restart a limited number of times for any other kind of error.

**Example Failure Policy Configuration for this use case**:

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: ignore-max-restarts-example
spec:
  # Failure Policy configured to ignore the failure (i.e., restart the JobSet without incrementing restart attempts)
  # if the failure was due to a host maintenance event (i.e., a SIGTERM sent as part of graceful node shutdown).
  failurePolicy:
    rules:
    - action: RestartJobSetAndIgnoreMaxRestarts
      onJobFailureReasons: 
      - PodFailurePolicy
    maxRestarts: 10
  replicatedJobs:
  - name: workers 
    replicas: 1
    template:
      spec:
        parallelism: 1
        completions: 1
        backoffLimit: 0
        # podFailurePolicy which fails job immediately if job was killed by SIGTERM (i.e., graceful node shutdown for maintenance events)
        podFailurePolicy:
          rules:
          - action: FailJob
            onExitCodes:
              containerName: main
              operator: In
              values: [143] # SIGTERM = exit code 143
        template:
          spec:
            restartPolicy: Never
            containers:
            - name: main
              image: bash:latest
              image: docker.io/library/bash:5
              command: ["bash"]
              args:
              - -c
              - echo "Hello world! I'm going to exit with exit code 143 (SIGTERM) to simulate host maintenance." && sleep 20 && exit 143
```

#### Story 4: Different failure policies for different replicated jobs

As a user, I am running a JobSet which contains 2 replicated jobs:
- `workers`: which runs a large scale distributed ML training job
- `parameter-server`: which runs a parameter server for the training job

When a worker in the `workers` replicated job fails, I want the entire JobSet to restart
and resume training from the latest checkpoint. The workers are running across 15k nodes,
so the probability of any 1 node in the cluster experiencing some kind of failure is relatively
high; mean time to failure (MTTF) is only a few hours - every few hours we can expect a node to fail
and the JobSet will need to restart once the node is recreated and healthy again. Since the JobSet
will be restarting numerous times along the road to completing this very long training job, the workers
should be able to restart an unlimited number of times.

When the parameter service in the `parameter-server` replicated job fails, I want the JobSet
to restart but only up to 3 times, as the job 

**Example Failure Policy Configuration for this use case**:

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: different-policies-for-different-replicated-jobs
spec:
  # Failure Policy configured to restart the JobSet upon any failure, up to 10 times.
  # Otherwise, restart up to 10 times. This rule applies to any failure type for all
  # replicated jobs.
  failurePolicy:
    rules:
    - action: RestartJobSetAndIgnoreMaxRestarts
      targetReplicatedJobs:
      - workers
    - action: RestartJobSet
      targetReplicatedJobs:
      - parameter-server
  replicatedJobs:
  - name: workers
    replicas: 5
    template:
      spec:
        parallelism: 3000
        completions: 3000
        backoffLimit: 0
        template:
          spec:
            restartPolicy: Never
            containers:
            - name: main
              image: bash:latest
              image: python:3.8
              command: 
              - |
                python3 train.py
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

### Proposed Failure Policy API

```go

// FailurePolicyAction defines the action the JobSet controller will take for
// a given FailurePolicyRule.
type FailurePolicyAction string

const (
  // Fail the JobSet immediately, regardless of maxRestarts.
  FailJobSet FailurePolicyAction = "FailJobSet"

  // Restart the JobSet if the number of restart attempts is less than MaxRestarts.
  // Otherwise, fail the JobSet.
  RestartJobSet FailurePolicyAction = "RestartJobSet"

  // Don't count the failure against maxRestarts.
  RestartJobSetAndIgnoreMaxRestarts FailurePolicyAction = "RestartJobSetAndIgnoreMaxRestarts"
)

// FailurePolicyRule defines a FailurePolicyAction to be executed if a child job
// fails due to a reason listed in OnJobFailureReasons.
type FailurePolicyRule struct {
  // The action to take if the rule is matched.
  // +kubebuilder:validation:Enum:=FailJobSet;RestartJobSetAndIgnoreMaxRestarts;FailJob
  Action FailurePolicyAction `json:"action"`
  // The requirement on the job failure reasons. The requirement
  // is satisfied if at least one reason matches the list.
  // The rules are evaluated in order, and the first matching
  // rule is executed.
  // An empty list applies the rule to any job failure reason.
  // +kubebuilder:validation:UniqueItems:true
  OnJobFailureReasons []string `json:"onJobFailureReasons"`
  // TargetReplicatedJobs are the names of the replicated jobs the operator applies to.
  // An empty list will apply to all replicatedJobs.
  // +optional
  // +listType=atomic
  // +kubebuilder:validation:UniqueItems
  TargetReplicatedJobs []string `json:"targetReplicatedJobs,omitempty"`
}

type FailurePolicy struct {
  // MaxRestarts defines the limit on the number of JobSet restarts.
  // A restart is achieved by recreating all active child jobs.
  MaxRestarts int32 `json:"maxRestarts,omitempty"`
  // List of failure policy rules for this JobSet.
  // For a given Job failure, the rules will be evaluated in order,
  // and only the first matching rule will be executed.
  // If no matching rule is found, the RestartJobSet action is applied.
  Rules []FailurePolicyRule `json:"rules,omitempty"`
}

```

### Constraints

- For a given Job failure, the rules will be evaluated in order, and only the first matching rule will be executed. This
follows an established pattern used by PodFailurePolicy in the Job controller.

- OnJobFailureReasons must be a valid Job failure reason as defined [here](https://github.com/kubernetes/kubernetes/blob/2d4100335e4c4ccc28f96fac78153f378212da4c/staging/src/k8s.io/api/batch/v1/types.go#L537-L554)
in the batchv1 Job API. At the time of writing this KEP, these include:
  - `PodFailurePolicy`
  - `BackoffLimitExceeded`
  - `DeadlineExceeded`
  - `MaxFailedIndexesExceeded`
  - `FailedIndexes`

### Implementation

The core part of the implementation will be defining what specific mechanisms the JobSet controller uses to implement
the behavior defined for each FailurePolicyAction type:

1) `FailJobSet`: To fail the JobSet immediately without restarting, the controller updates the JobSet status to failed.

2) `RestartJobSetAndIgnoreMaxRestarts`: To restart the JobSet without counting it against `MaxRestarts`, the controller
will add an annotation `jobset.sigs.k8s.io/restart` to mark Jobs which need to be restarted. On the subsequent reconciles,
the JobSet controller will delete any jobs with this annotation, allowing them to be recreated in as part of the normal
reconciliation process, without ever incrementing `jobset.sigs.k8s.io/restart-attempt` annotation.

### Test Plan

The testing plan will focus on integration tests. Specific test cases and scenarios are defined in the integration test
section below.

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

- `controllers`: `01/19/2024` - `30.2%`

#### Integration tests

Integration test cases will be added for:

- Failure policy targetting all replicated jobs, a job fails, ensure the failure policy is triggered.
  - Repeat this test for all 4 FailurePolicyAction types, checking the action was executed correctly.
- Failure policy targetting a specific replicated job, a child job of that replicated job fails,
ensure the failure policy is triggered.
- Failure policy targetting a specific replicated job, a child job of a different replicated job fails,
ensure we don't trigger the failure policy and instead fall back to default behavior.

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

- Prototyped proposal January 12th, 2024
- KEP published January 19th, 2024

## Drawbacks


## Alternatives

We also discussed 2 simpler APIs (see https://github.com/kubernetes-sigs/jobset/issues/262#issuecomment-1689097658).

1) The first one added a boolean `FollowPodFailurePolicy` field to the FailurePolicy API, allowing the user 
to configure custom pod failure policies for specific failure types, and JobSet would respect failures triggered
by those policies by failing the JobSet immediately, rather than restarting the JobSet up to `MaxRestarts` times. 
This allowed the user to use podFailurePolicies to define which errors they considered retriable and non-retriable,
and for JobSet to react accordingly. More configuration was required from the user, but this allowed more flexibility.

2) The second one added a boolean `FailNonRetriable` field, in which JobSet developers would decide for the user
what kind of errors are considered retriable and non-retriable, by defining podFailurePolicies to ignore failures
caused by SIGTERM (host maintenance), and failing the job immediately for failures caused by any other exit code.
This would require less configuration from the user, providing some smarter restart/failure semantics out of the
box, but lacks the flexibility we want to offer users.

## Future Work

There are some additional use cases which would require extending the Failure Policy API with more Action types,
as described below. However, there is still some unresolved ambiguity and in how we will track restarts of 
individual jobs, since we do not want to restart indefinitely, there needs to be some limit. In addition,
some of them have dependencies on upstream features that have been [proposed](https://github.com/kubernetes/kubernetes/issues/122972) but are still under review, and won't
be available to use for some time.

Additional actions we want to support in the future include:

1) `RestartReplicatedJob`: To restart the child jobs of a specific replicated job, the controller will delete the child
jobs of the target replicated job, **without incrementing the restart attempt annotation**. The jobs will then be 
recreated via the normal reconciliation process.

2) `FailJob`: To leave a particular child job in a failed state without restarting it or restarting the JobSet, the
controller will simply do nothing, taking no action on this job.

