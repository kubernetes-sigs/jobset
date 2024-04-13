# KEP-104: StartupPolicy

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
- [Summary](#summary)
- [Motivation](#motivation)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Proposal](#proposal)
  - [User Stories (Optional)](#user-stories-optional)
    - [Story 1](#story-1)
    - [Story 2](#story-2)
  - [Risks and Mitigations](#risks-and-mitigations)
- [Design Details](#design-details)
  - [API Proposal](#api-proposal)
  - [Implementation](#implementation)
  - [Defaulting/Validation](#defaultingvalidation)
  - [User Experience](#user-experience)
  - [Test Plan](#test-plan)
      - [Prerequisite testing updates](#prerequisite-testing-updates)
    - [Unit Tests](#unit-tests)
    - [Integration tests](#integration-tests)
  - [Graduation Criteria](#graduation-criteria)
- [Implementation History](#implementation-history)
- [Drawbacks](#drawbacks)
  - [Poor Person Workflow Engine](#poor-person-workflow-engine)
  - [Increase number of patches to JobSet](#increase-number-of-patches-to-jobset)
- [Alternatives](#alternatives)
  - [Operate on Conditions](#operate-on-conditions)
  - [Allow for both ready and succeeded](#allow-for-both-ready-and-succeeded)
<!-- /toc -->

## Summary

This KEP adds a StartupPolicy for JobSets.  A StartupPolicy allows for some flexibility on when to start ReplicatedJobs in a JobSet.  
<!--
This section is incredibly important for producing high-quality, user-focused
documentation such as release notes or a development roadmap. It should be
possible to collect this information before implementation begins, in order to
avoid requiring implementors to split their attention between writing release
notes and implementing the feature itself. KEP editors and SIG Docs
should help to ensure that the tone and content of the `Summary` section is
useful for a wide audience.

A good summary is probably at least a paragraph in length.

Both in this section and below, follow the guidelines of the [documentation
style guide]. In particular, wrap lines to a reasonable length, to make it
easier for reviewers to cite specific portions, and to minimize diff churn on
updates.

[documentation style guide]: https://github.com/kubernetes/community/blob/master/contributors/guide/style-guide.md
-->

## Motivation

High Performance Computing / Machine Learning users usually want some control over how jobs are being created.  
A Startup policy would allow a user to specify which status a replicated job should have before starting other replicated jobs in a JobSet.

### Goals

- Add a startup policy for JobSet

### Non-Goals

- Comprehensive workflow specifications
  - This means that we will support simple dependecies rather than processing a full DAG.
  - Projects like argo-workflows are a better use case for this.
- Allowing for a percentage of jobs in a ReplicatedJob to be ready to consider the whole ReplicatedJob to ready

## Proposal

### User Stories (Optional)

#### Story 1

As a user, I have one ReplicatedJob that functions as the driver and a series of worker ReplicatedJobs.  
I want the driver to be ready before I start the worker replicated jobs.  
This is useful for HPC/AI/ML workloads as some frameworks have a driver pod that must be ready before the workers are started.  

The API below would fit my purpose.

```yaml
apiVersion: jobset.x-k8s.io/v1
kind: JobSet
metadata:
  name: driver-ready-worker-start
spec:
  startupPolicy:
     startupPolicyOrder: InOrder
  replicatedJobs:
  - name: driver
    replicas: 1
    template:
      spec:
        # Set backoff limit to 0 so job will immediately fail if any pod fails.
        backoffLimit: 0 
        completions: 1
        parallelism: 1
        template:
          spec:
            containers:
            - name: driver
              image: bash:latest
              command:
              - bash
              - -xc
              - |
                sleep 10000
  - name: workers
    replicas: 1
    template:
      spec:
        backoffLimit: 0 
        completions: 2
        parallelism: 2
        template:
          spec:
            containers:
            - name: worker
              image: bash:latest
              command:
              - bash
              - -xc
              - |
                sleep 10
```

#### Story 2

As a user, I want my replicated jobs to start in order.

One area that this could be useful is if someone has a message queue replicatedJob (e.g., Redis), followed by a driver replicatedJob, followed by a worker replicated job.

In this case, I want message queue to start first, then the driver, and then finally the worker.  

```yaml
apiVersion: jobset.x-k8s.io/v1
kind: JobSet
metadata:
  name: messagequeue-driver-worker
spec:
  startupPolicy:
     startupPolicyOrder: InOrder
  replicatedJobs:
  - name: messagequeue
    replicas: 1
    template:
      spec:
        # Set backoff limit to 0 so job will immediately fail if any pod fails.
        backoffLimit: 0 
        completions: 1
        parallelism: 1
        template:
          spec:
            containers:
            - name: messagequeue
              image: bash:latest
              command:
              - bash
              - -xc
              - |
                sleep 100
  - name: driver
    replicas: 2
    template:
      spec:
        backoffLimit: 0 
        completions: 2
        parallelism: 2
        template:
          spec:
            containers:
            - name: driver
              image: bash:latest
              command:
              - bash
              - -xc
              - |
                sleep 10
  - name: worker
    replicas: 2
    template:
      spec:
        backoffLimit: 0 
        completions: 2
        parallelism: 2
        template:
          spec:
            containers:
            - name: worker
              image: bash:latest
              command:
              - bash
              - -xc
              - |
                sleep 10
```

### Risks and Mitigations

To avoid the JobSet become a half-baked workflow manager, the API will not allow describing DAGs. 
The goal is to focus on supporting startup sequence that HPC and ML training jobs typically require.

## Design Details

### API Proposal
  
```golang

type StartupPolicyOptions string

const (
 // This is the default setting
 // AnyOrder means that we will start jobs without any specific order.
 AnyOrder StartupPolicyOptions = "AnyOrder"
 // InOrder starts the ReplicatedJobs in order that they are listed. Jobs within a ReplicatedJob will 
 // still start in any order.
 InOrder StartupPolicyOptions = "InOrder"
)

type StartupPolicy struct {
 // StartupPolicyOrder is an enum of different options for StartupPolicy
 // AnyOrder means to start replicated jobs in any order.
 // InOrder means to start them as they are listed in the JobSet.
 // +kubebuilder:validation:Enum=AnyOrder;InOrder
 StartupPolicyOrder StartupPolicyOptions `json:"startupPolicyOrder"`
}
```

### Implementation

We have three cases where startupPolicy applies:

- when the jobset is first created
- when the jobset is restarted after a failure (with restart or recreate all failure policy)
- when the jobset is resumed after suspension

In cases 1 and 2, the operator controls the order of creating the jobs, and so it creates the replicatedJob only when its turn comes in the startup sequence.

In case 3, the operator controls the order of unsuspending the jobs, and so it unsuspends the replicatedJob only when its turn comes in the startup sequence.

On initial creation or recreation, we will apply the startup policy before creating the jobs in a replicated job.
We will retrieve the `ReplicatedJobStatus` before each call to `createJobs` to verify which jobs have been created.

JobSet views the jobs that it owns and updates the `ReplicatedJobStatus` on each reconcile. For suspended jobs, we will view the `ReplicatedJobStatus` and resume the jobs as they are listed in the `startupPolicyOrder`.
Suspended is also tracked by `ReplicatedJobStatus`.

One area of contention is if Jobs finish fast and are deleted (ie `ttlAfterFinished` is short). 
We created [issue](https://github.com/kubernetes-sigs/jobset/issues/360) to address this.

### Defaulting/Validation

- StartupPolicy is immutable.
- StartupPolicyOrder of `AnyOrder` is default setting.

### User Experience

If Jobs are taking a long time to go to ready, we should have a clear way to check that the JobSet is behaving as expected.

We will add a condition for StartupPolicy to achieve this.

```golang
metav1.Condition{
   Type:    "JobSetStartupPolicyCompleted",
   Status:  metav1.ConditionStatus(corev1.ConditionFalse),
   Reason:  "StartupPolicyInOrder",
   Message: "replicated job %s is starting",
  })
```

A condition of false means that startup policy is running.

A condition of true means that all jobs at least ready. StartupPolicy does not care about succeeded or failed.

Reason will display the type of startupPolicy (`StartupPolicyInOrder`) for now.
Message will display the replicated job that is starting.
Once the StartupPolicy is complete, this will read `startup policy successful`.

If `StartupPolicyAnyOrder` (same as no startup policy), then this condition will not be set in the JobSet.

<!--
This section should contain enough information that the specifics of your
change are understandable. This may include API specs (though not always
required) or even code snippets. If there's any ambiguity about HOW your
proposal will be implemented, this is the place to discuss them.
-->

### Test Plan

<!--
**Note:** *Not required until targeted at a release.*
The goal is to ensure that we don't accept enhancements with inadequate testing.

All code is expected to have adequate tests (eventually with coverage
expectations). Please adhere to the [Kubernetes testing guidelines][testing-guidelines]
when drafting this test plan.

[testing-guidelines]: https://git.k8s.io/community/contributors/devel/sig-testing/testing.md
-->

[x] I/we understand the owners of the involved components may require updates to
existing tests to make this code solid enough prior to committing the changes necessary
to implement this enhancement.

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

- `controller`: `Aug 7th 2023` - `31.6%`

Unit tests are low in this area as we do more of our testing at the integration level.

We will add a new file called startup_policy to apply our actions.

#### Integration tests

- If Suspend is specified, startupPolicy resume is ignored.
- A JobSet is suspended while a ReplicatedJob is still starting
- [Story 1](#story-1)
- [Story 2](#story-2)

<!--
Describe what tests will be added to ensure proper quality of the enhancement.

After the implementation PR is merged, add the names of the tests here.
-->

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

- Drafted August 2 2023
- Updated API December 18th 2023
<!--
Major milestones in the lifecycle of a KEP should be tracked in this section.
Major milestones might include:
- the `Summary` and `Motivation` sections being merged, signaling SIG acceptance
- the `Proposal` section being merged, signaling agreement on a proposed design
- the date implementation started
- the first Kubernetes release where an initial version of the KEP was available
- the version of Kubernetes where the KEP graduated to general availability
- when the KEP was retired or superseded
-->

## Drawbacks

### Poor Person Workflow Engine

This startup policy can function as a poor person workflow syntax.  And there may be requests to make this more than what it is.  

We ideally want to utilize other workflows engines to provide more complicated dags but I could see simple ML workflows being used for JobSet.  

### Increase number of patches to JobSet

Since we will start all jobs in a suspended state, we will have to patch the spec of all the jobs eventually.  

## Alternatives

### Operate on Conditions

We originally wanted to operate based on Conditions for Jobs.  But it turns out that there is no Ready condition.  
This means that for some cases we would support statuses and others for conditions.  
We decided to go forward with operating on status fields for both cases.

### Allow for both ready and succeeded

To avoid JobSets becoming a bad workflow engine.  We want to support startup sequence where once Jobs are ready, then go onto the next job.  

We did not add support for succeeded at this time as this is more in align with a pipeline or workflow.

<!--
What other approaches did you consider, and why did you rule them out? These do
not need to be as detailed as the proposal, but should include enough
information to express the idea and why it was not acceptable.
-->
