# KEP-672: Execution Policy

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
  - [API Details](#api-details)
  - [Implementation](#implementation)
  - [Defaulting/Validation](#defaultingvalidation)
  - [Test Plan](#test-plan)
    - [Unit Tests](#unit-tests)
    - [Integration tests](#integration-tests)
  - [Graduation Criteria](#graduation-criteria)
- [Implementation History](#implementation-history)
- [Drawbacks](#drawbacks)
- [Alternatives](#alternatives)
<!-- /toc -->

## Summary

This KEP outlines the proposal to add the ExecutionPolicy API into JobSet. The ExecutionPolicy API
allows to run sequence of ReplicatedJobs within a single JobSet.

## Motivation

Currently, JobSet supports the StartupPolicy API which allows to create Jobs in order after the
first Job is in ready status. This can be useful when driver should be ready before workers.
However, sometime high performance computing and machine learning users want to run sequence of
succeeded ReplicatedJobs within JobSet. For example, it is common to run pre-processing,
distributed fine-tuning, post-processing for LLM fine-tuning use-cases.

### Goals

- Add the ExecutionPolicy API for JobSet

### Non-Goals

- Support workflow management like DAGs outside of Job sequence.
  - Users should consider to use Argo Workflows or Tekton Pipelines for such use-cases.
- Allowing for a percentage of Jobs in a ReplicatedJob to be ready to consider the
  whole ReplicatedJob to ready.

## Proposal

### User Stories (Optional)

#### Story 1

As a user, I want to fine-tune LLM using multi-node training with JobSet. I have the first
ReplicatedJob for pre-trained model and dataset initialization, and the second ReplicatedJob for
distributed fine-tuning.

The example of JobSet looks as follows:

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: fine-tune-llm
spec:
  executionPolicy:
    executionPolicyOrder: InOrder
    replicatedJobsStatus: Succeeded
  replicatedJobs:
    - name: Initializer
      template:
        spec:
          template:
            spec:
              containers:
                - name: model-initializer
                  image: docker.io/kubeflow/model-initializer
                  volumeMounts:
                    - mountPath: /workspace/pre-trained-model
                      name: model-initializer
                - name: dataset-initializer
                  image: docker.io/kubeflow/dataset-initializer
                  volumeMounts:
                    - mountPath: /workspace/dataset
                      name: dataset-initializer
              volumes:
                - name: dataset-initializer
                  persistentVolumeClaim:
                    claimName: dataset-initializer
                - name: model-initializer
                  persistentVolumeClaim:
                    claimName: model-initializer
    - name: Node
      template:
        spec:
          parallelism: 3
          completions: 3
          template:
            spec:
              containers:
                - name: trainer
                  image: docker.io/kubeflow/trainer
                  resources:
                    limits:
                      nvidia.com/gpu: 5
                  volumeMounts:
                    - mountPath: /workspace/dataset
                      name: dataset-initializer
                    - mountPath: /workspace/pre-trained-model
                      name: model-initializer
              volumes:
                - name: dataset-initializer
                  persistentVolumeClaim:
                    claimName: dataset-initializer
                - name: model-initializer
                  persistentVolumeClaim:
                    claimName: model-initializer
```

#### Story 2

TODO: Add HPC use-case with Job sequence

### Risks and Mitigations

This API will not allow to describe DAGs to avoid workflow manager features in JobSet.
The goal is to only focus on Job sequence to cover model training/HPC use-cases.

## Design Details

### API Details

```golang
type JobSetSpec struct {
	ExecutionPolicy *ExecutionPolicy `json:"executionPolicy,omitempty"`
}


type ExecutionPolicyOption string

const (
  // This is the default settings.
  // AnyOrder means that Jobs will be started in any order.
	AnyOrder ExecutionPolicyOption = "AnyOrder"

  // InOrder starts the ReplicatedJobs in order that they are listed. Jobs within a ReplicatedJob
  // will still start in any order.
	InOrder ExecutionPolicyOption = "InOrder"
)

type ExecutionPolicy struct {
	// Order in which Jobs will be created.
	ExecutionPolicyOrder ExecutionPolicyOption `json:"executionPolicyOrder"`

	// After all replicated Jobs reach this status, the JobSet will create the next replicated Jobs.
	ReplicatedJobsStatus ReplicatedJobsStatusOption `json:"replicatedJobsStatus"`
}

type ReplicatedJobsStatusOption string

// For Ready status the startupPolicy API can be used.
// TODO: What statuses do we want to support in the first version ?
const (
	ReadyStatus ReplicatedJobsStatusOption = "Succeeded"

	FailedStatus ReplicatedJobsStatusOption = "Failed"

	ActiveStatus ReplicatedJobsStatusOption = "Active"
)
```

### Implementation

Open Questions:

1. How we should calculate quota for JobSet with execution policy ?
2. Integration with Kueue. Related KEP to support Argo Workflow in Kueue: https://github.com/kubernetes-sigs/kueue/pull/2976

The JobSet operator will control the creation of replicated Jobs based on their status.

### Defaulting/Validation

- ExecutionPolicy is immutable.
- ExecutionPolicyOrder of `AnyOrder` is default setting.
- StartupPolicy should be empty if ExecutionPolicy is set

### Test Plan

[x] I/we understand the owners of the involved components may require updates to
existing tests to make this code solid enough prior to committing the changes necessary
to implement this enhancement.

#### Unit Tests

- `controllers`: `01/19/2024` - `30.2%`

We will create a new file called execution_policy for the functionality.

#### Integration tests

- Having 2 Jobs running in sequence with Succeeded condition.
- Suspend JobSet with execution policy being set.
- Restart the failed Job when execution policy is set in JobSet.

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

- Draft KEP: September 25th 2024

## Drawbacks

<!--
Why should this KEP _not_ be implemented?
-->

## Alternatives

<!--
What other approaches did you consider, and why did you rule them out? These do
not need to be as detailed as the proposal, but should include enough
information to express the idea and why it was not acceptable.
-->
