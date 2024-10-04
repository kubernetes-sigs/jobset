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
    - [Story 3](#story-3)
  - [Risks and Mitigations](#risks-and-mitigations)
- [Design Details](#design-details)
  - [API Details](#api-details)
  - [Implementation](#implementation)
  - [Quota Management](#quota-management)
  - [Defaulting/Validation](#defaultingvalidation)
  - [Test Plan](#test-plan)
    - [Unit Tests](#unit-tests)
    - [Integration tests](#integration-tests)
  - [Graduation Criteria](#graduation-criteria)
- [Implementation History](#implementation-history)
- [Drawbacks](#drawbacks)
  - [Manage quota for Job sequence](#manage-quota-for-job-sequence)
  - [Support complex DAGs with JobSet](#support-complex-dags-with-jobset)
- [Alternatives](#alternatives)
  - [Add ExecutionPolicyRule parameter into the StartupPolicy API](#add-executionpolicyrule-parameter-into-the-startuppolicy-api)
  <!-- /toc -->

## Summary

This KEP outlines the proposal to add the ExecutionPolicy API into JobSet. The ExecutionPolicy API
allows to run sequence of ReplicatedJobs within a single JobSet.

## Motivation

Currently, JobSet supports the StartupPolicy API which allows to create Jobs in order after the
first Job is in ready status. This can be useful when driver should be ready before workers.
However, sometime high performance computing and machine learning users want to run sequence of
ReplicatedJobs within JobSet. For example, it is common to run pre-processing,
distributed fine-tuning, post-processing for LLM fine-tuning.

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
    rules:
      - targetReplicatedJobs:
          - initializer
        waitForReplicatedJobsStatus: Succeeded
  replicatedJobs:
    - name: initializer
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
    - name: trainer-node
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

As a user, I want to fine-tune my LLM using MPI and DeepSpeed. I have the first
ReplicatedJob for pre-trained model and dataset initialization, the second ReplicatedJob for MPI launch
and the third ReplicatedJob for distributed fine-tuning.

The example of JobSet looks as follows:

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: deepspeed-mpi
spec:
  executionPolicy:
    executionPolicyOrder: InOrder
    rules:
      - targetReplicatedJobs:
          - initializer
        waitForReplicatedJobsStatus: Succeeded
      - targetReplicatedJobs:
          - launcher
        waitForReplicatedJobsStatus: Ready
  replicatedJobs:
    - name: initializer
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
    - name: launcher
      template:
        spec:
          parallelism: 1
          completions: 1
          template:
            spec:
              containers:
                - name: launcher
                  image: docker.io/kubeflow/mpi-launcher
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
    - name: trainer-node
      template:
        spec:
          parallelism: 3
          completions: 3
          template:
            spec:
              containers:
                - name: trainer
                  image: docker.io/kubeflow/deepspeed-trainer
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

#### Story 3

As an HPC user, I want to run a set of simulations with MPI that complete successfully before
using the data for a next step analysis.

As an HPC user I want to scale up my simulation job only after a small set have completed
successfully. For that case, I could imagine essentially the same replicatedJob done twice,
just with a larger size. If the outcome of the first isn't success you wouldn't launch
the larger bulk of work.

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

	// After all ReplicatedJobs reach this status, the JobSet will create the next ReplicatedJobs.
	ExecutionPolicyRule []ExecutionPolicyRule `json:"rules"`
}

// ExecutionPolicyRule represents the execution policy rule for Job sequence.
type ExecutionPolicyRule struct {

	// Names of the replicated Jobs that applied the status.
	TargetReplicatedJobs []string `json:"targetReplicatedJobs"`

	// Status the target ReplicatedJobs must reach before subsequent ReplicatedJobs begin executing.
	WaitForReplicatedJobsStatus ReplicatedJobsStatusOption `json:"waitForReplicatedJobsStatus"`
}

type ReplicatedJobsStatusOption string

const (
	ReadyStatus ReplicatedJobsStatusOption = "Ready"

	SucceededStatus ReplicatedJobsStatusOption = "Succeeded"
)
```

### Implementation

The JobSet operator will control the creation of ReplicatedJobs based on their status. When
desired replicated Jobs reach the status defined in the waitForReplicatedJobsStatus, the
controller creates the next set of replicated Jobs.

If JobSet is suspended the all ReplicatedJobs will be suspended and the Job sequence starts again.

### Quota Management

In the initial implementation of the ExecutionPolicy the resource quota will be calculated as
sum of all ReplicatedJobs resources. Which means JobSet will be admitted by
[Kueue](https://github.com/kubernetes-sigs/kueue) only when all resources are available for
every ReplicatedJob within JobSet.

That allows us to leverage the existing integration between Kueue and JobSet while using the
ExecutionPolicy API.

In the future versions we will discuss potential partial admission of JobSet by Kueue. For example,
when compute resources are available for the first ReplicatedJob the JobSet can be dispatched by
Kueue.

### Defaulting/Validation

- ExecutionPolicy is immutable.
- ExecutionPolicyOrder of `AnyOrder` is default setting.
- StartupPolicy should be equal to `AnyOrder` when ExecutionPolicy is used.
  - The StartupPolicy API will be deprecated over the next few JobSet releases, since
    ExecutionPolicy can be used with ready status of ReplicatedJobs.

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

### Manage quota for Job sequence

It is hard to manage quota for sequence of Job within single JobSet. Usually, users don't want to
lock resources (e.g. GPUs/TPUs) when the first Job is running.

### Support complex DAGs with JobSet

If users want to create complex DAG workflows, they should not use JobSet for such purpose.

## Alternatives

### Add ExecutionPolicyRule parameter into the StartupPolicy API

Currently, when StartupPolicy is set to InOrder, the controller waits until replicatedJobs be in
Ready status before creating the next replicatedJobs.

We can re-use add the `ExecutionPolicyRule` parameter into `StartupPolicy` to give user an ability
to specify the required Job status before creating the next ReplicatedJobs.
