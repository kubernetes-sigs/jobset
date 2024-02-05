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
    - [Story 2: Ignore](#story-2-ignore)
  - [Story 3: RestartReplicatedJob](#story-3-restartreplicatedjob)
  - [Story 4: FailJob and RestartJob](#story-4-failjob-and-restartjob)
  - [Notes/Constraints/Caveats (Optional)](#notesconstraintscaveats-optional)
  - [Risks and Mitigations](#risks-and-mitigations)
- [Design Details](#design-details)
  - [Proposed Failure Policy API](#proposed-failure-policy-api)
  - [Constraints](#constraints)
  - [Implmentation](#implmentation)
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

#### Story 2: Ignore

As a user, in order to use my computing resources more efficiently, I want to 
configure my JobSet to restart unlimited times for child job failures due to host maintenance,
but restart a limited number of times for any other kind of error.

**Example Failure Policy Configuration for this use case**:

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: ignore-example
spec:
  # Failure Policy configured to ignore the failure (i.e., restart the JobSet without incrementing restart attempts)
  # if the failure was due to a host maintenance event (i.e., a SIGTERM sent as part of graceful node shutdown).
  failurePolicy:
    rules:
    - action: Ignore
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

### Story 3: RestartReplicatedJob

As a user, I have a JobSet with 2 replicated jobs: one which runs distributed training processes across a pool of GPU
nodes, and one which runs the driver/coordinator on a CPU pool. If a child job of the GPU worker ReplicatedJob crashes, I just want to restart the GPU workers and not the driver, then resume training from the latest checkpoint. However, if
the driver crashes, I want to restart the entire JobSet, then resume training from the latest checkpoint.

**Example Failure Policy configuration for this use case**:

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: restart-replicated-job-example
  annotations:
    alpha.jobset.sigs.k8s.io/exclusive-topology: {{topologyDomain}} # 1:1 job replica to topology domain assignment
spec:
  # Failure Policy to restart the child jobs of the target ReplicatedJob (gpu-workers) if any fail, but fall
  # back to the default behavior of restarting the entire JobSet if the driver fails.
  failurePolicy:
    rules:
    - action: RestartReplicatedJob
      targetReplicatedJobs:
      - gpu-workers
    maxRestarts: 10
  replicatedJobs:
  - name: driver
    replicas: 1
    template:
      spec:
        parallelism: 1
        completions: 1
        backoffLimit: 0
        template:
          spec:
            restartPolicy: Never
            containers:
            - name: main
              image: python:3.10
              command: ["..."]
  - name: gpu-workers
    replicas: 4 # number of node pools
    template:
      spec:
        parallelism: 2
        completions: 2
        backoffLimit: 0
        template:
          spec:
            containers:
            - name: main
              image: pytorch:latest
              command: ["..."]
            resources:
              limits:
                nvidia.com/gpu: 1
```

### Story 4: FailJob and RestartJob

Dependency: https://github.com/kubernetes/kubernetes/issues/122972

As a user, I want to run a HPC simulation in which each child job runs a simulation with different random initial
parameters. When a simulation ends, the application will exit with one of two exit codes:

- Exit code 2, which indicates the simulation produced an invalid result due to bad starting parameters, and should
not be retried.
- Exit code 3, which indicates the simulation produced an invalid result but the intial parameters were reasonable,
so the simulation should be restarted.

When a Job fails due to a pod failing with exit code 2, I want the Job to stay in a failed state.
When a Job fails due to a pod failing with exit code 3, I want to restart the Job.

**Example Failure Policy configuration for this use case**:

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: restart-replicated-job-example
  annotations:
    alpha.jobset.sigs.k8s.io/exclusive-topology: {{topologyDomain}} # 1:1 job replica to topology domain assignment
spec:
  failurePolicy:
    rules:
    # If Job fails due to a pod failing with exit code 2, leave it in a failed state.
    - action: FailJob
      targetReplicatedJobs:
      - simulations
      onJobFailureReasons:
      - PodFailurePolicyExitCode2
    # If Job fails due to a pod failing with exit code 3, restart that Job.
    - action: RestartJob
      targetReplicatedJobs:
      - simulations
      onJobFailureReasons:
      - PodFailurePolicyExitCode3
    maxRestarts: 10
  replicatedJobs:
  - name: simulations
    replicas: 10
    template:
      spec:
        parallelism: 1
        completions: 1
        backoffLimit: 0
        # If a pod fails with exit code 2 or 3, fail the Job, using the user-defined reason.
        podFailurePolicy:
          rules:
          - action: FailJob
            onExitCodes:
              containerName: main
              operator: In
              values: [2]
            reason: "ExitCode2"
          - action: FailJob
            onExitCodes:
              containerName: main
              operator: In
              values: [3]
            reason: "ExitCode3"
        template:
          spec:
            restartPolicy: Never
            containers:
            - name: main
              image: python:3.10
              command: ["..."]
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
  // Fail JobSet immediately, regardless of maxRestarts.
  FailJobSet FailurePolicyAction = "FailJobSet"

  // Don't count the failure against maxRestarts.
  Ignore FailurePolicyAction = "Ignore"

  // Restart all the child jobs of the ReplicatedJob the failed job is a part of.
  RestartReplicatedJob FailurePolicyAction = "RestartReplicatedJob"
  
  // The failed child job is permanently failed and should not be
  // restarted, active child jobs will continue to run to completion.
  FailJob FailurePolicyAction = "FailJob"

  // Restart the failed child job only, not the whole JobSet.
  RestartJob FailurePolicyAction = "RestartJob"

)

// FailurePolicyRule defines a FailurePolicyAction to be executed if a child job
// fails due to a reason listed in OnJobFailureReasons.
type FailurePolicyRule struct {
  // The action to take if the rule is matched.
  // +kubebuilder:validation:Enum:=FailJobSet;Ignore;FailJob;RestartJob
  Action FailurePolicyAction `json:"action"`
  // The requirement on the job failure reasons. The requirement
  // is satisfied if at least one reason matches the list.
  // Each unique failure reason can only be associated with one FailurePolicyRule.
  // An empty list applies the rule to any job failure reason.
  // +kubebuilder:validation:UniqueItems:true
  OnJobFailureReasons []string `json:"onJobFailureReasons"`
  // TargetReplicatedJobs are the names of the replicated jobs the operator will apply to.
  // A null or empty list will apply to all replicatedJobs.
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

### Implmentation

The core part of the implementation will be defining what specific mechanisms the JobSet controller uses to implement
the behavior defined for each FailurePolicyAction type:

1) `FailJobSet`: To fail the JobSet immediately without restarting, the controller updates the JobSet status to failed.

2) `Ignore`: To "ignore" the failure (i.e., restart the JobSet without counting it against `MaxRestarts`), the controller
will delete all child jobs and allow the normal reconciliation process to recreate them. This is in contrast to how
we restart the JobSet **without** ignoring the failure, which is done by incrementing the counter on the JobSet annotation
`jobset.sigs.k8s.io/restart-attempt`, which on subsequent reconiliations will trigger job deletion and recreation for any
child job with a `jobset.sigs.k8s.io/restart-attempt` counter value less than that of the JobSet.

3) `RestartReplicatedJob`: To restart the child jobs of a specific replicated job, the controller will delete the child
jobs of the target replicated job, **without incrementing the restart attempt annotation**. The jobs will then be 
recreated via the normal reconciliation process.

4) `RestartJob`: To restart a single child job without restarting the entire JobSet, the controller will delete that 
particular child job, **without incrementing the restart attempt annotation**, and allow the normal reconciliation
process to recreate it.

5) `FailJob`: To leave a particular child job in a failed state without restarting it or restarting the JobSet, the
controller will simply do nothing, taking no action on this job.


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