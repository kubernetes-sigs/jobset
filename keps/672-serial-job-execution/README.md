# KEP-672: Serial Job Execution with StartupPolicy API

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
  - [User Experience](#user-experience)
  - [Test Plan](#test-plan)
    - [Unit Tests](#unit-tests)
    - [Integration tests](#integration-tests)
  - [Graduation Criteria](#graduation-criteria)
- [Implementation History](#implementation-history)
- [Drawbacks](#drawbacks)
  - [Manage quota for Job sequence](#manage-quota-for-job-sequence)
  - [Support complex DAGs with JobSet](#support-complex-dags-with-jobset)
- [Alternatives](#alternatives)
  - [Using workflow engine to execute sequence of jobs](#using-workflow-engine-to-execute-sequence-of-jobs)
  - [Add the WaitForStatus API under ReplicatedJob](#add-the-waitforstatus-api-under-replicatedjob)
  <!-- /toc -->

## Summary

This KEP outlines the proposal to expand the scope of the StartupPolicy API for JobSet. The
StartupPolicy API should support to run sequence of ReplicatedJobs after they reach Ready or
Succeeded status.

## Motivation

Currently, JobSet supports the StartupPolicy API which allows to create Jobs in order after the
first Job is in ready status. This can be useful when driver should be ready before workers.
However, sometimes high performance computing and machine learning users want to run sequence of
ReplicatedJobs within JobSet. For example, it is common to run pre-processing,
distributed fine-tuning, post-processing for LLM fine-tuning.

### Goals

- Add support for serial Job execution for the StartupPolicy API.

### Non-Goals

- Support workflow management like DAGs outside of Job sequence.
  - Users should consider to use Argo Workflows or Tekton Pipelines for such use-cases.
- Allowing for a percentage of Jobs in a ReplicatedJob to be ready to consider the
  whole ReplicatedJob to ready.
- Support any other JobSet status other than Succeeded and Ready.

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
  startupPolicy:
    startupPolicyOrder: InOrder
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
  startupPolicy:
    startupPolicyOrder: InOrder
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

As described in the [quota management](#quota-management) section, currently Kueue will enqueue the
whole JobSet even when `startupPolicy: InOrder` is set. Thus, it will lock all ReplicatedJobs
resources (GPU, CPU, TPU) even if they are executed in the sequence. We can mitigate that risk
by enqueue each group of ReplicatedJobs separately with Kueue.

## Design Details

### API Details

```golang
type JobSetSpec struct {
 StartupPolicy *StartupPolicy `json:"startupPolicy,omitempty"`
}

type StartupPolicyOrderOption string

const (
 // AnyOrder means that Jobs will be started in any order.
 AnyOrder WaitForReplicatedJobsStatusOption = "AnyOrder"

 // InOrder starts the ReplicatedJobs in order that they are listed. Jobs within a ReplicatedJob
 // will still start in any order.
 InOrder WaitForReplicatedJobsStatusOption = "InOrder"
)

type StartupPolicy struct {
 // Order in which Jobs will be created.
 // Defaults to AnyOrder.
 StartupPolicyOrder StartupPolicyOrderOption `json:"startupPolicyOrder"`

 // After all ReplicatedJobs reach this status, the JobSet will create the next ReplicatedJobs.
 StartupPolicyRule []StartupPolicyRule `json:"rules"`
}

type ReplicatedJobsStatusOption string

const (
 // Ready status means the Ready counter equals the number of child Jobs.
 // .spec.replicatedJobs["name==<JOB_NAME>"].replicas == .status.replicatedJobsStatus.name["name==<JOB_NAME>"].ready
 ReadyStatus ReplicatedJobsStatusOption = "Ready"

 // Succeeded status means the Succeeded counter equals the number of child Jobs.
 // .spec.replicatedJobs["name==<JOB_NAME>"].replicas == .status.replicatedJobsStatus.name["name==<JOB_NAME>"].succeeded
 SucceededStatus ReplicatedJobsStatusOption = "Succeeded"
)

// StartupPolicyRule represents the startup policy rule for Job sequence.
type StartupPolicyRule struct {

 // Names of the replicated Jobs that applied the status.
 TargetReplicatedJobs []string `json:"targetReplicatedJobs"`

 // Status the target ReplicatedJobs must reach before subsequent ReplicatedJobs begin executing.
 // Defaults to Ready.
 WaitForReplicatedJobsStatus WaitForReplicatedJobsStatusOption `json:"waitForReplicatedJobsStatus"`
}

```

### Implementation

The JobSet operator will control the creation of ReplicatedJobs based on their status. When
desired replicated Jobs reach the status defined in the waitForReplicatedJobsStatus, the
controller creates the next set of replicated Jobs.

If JobSet is suspended the all ReplicatedJobs will be suspended and the Job sequence starts again.

When the JobSet is restarted after failure, the Job sequence starts again. User controls how many
times Job can be restarted via backOffLimit parameter.

### Quota Management

In the initial implementation of the StartupPolicy the resource quota will be calculated as
sum of all ReplicatedJobs resources. Which means JobSet will be admitted by
[Kueue](https://github.com/kubernetes-sigs/kueue) only when all resources are available for
every ReplicatedJob within JobSet.

That allows us to leverage the existing integration between Kueue and JobSet while using the
expanded version of StartupPolicy API.

In the future versions we will discuss how Kueue can enqueue group of ReplicatedJobs separately
and admit them. For example, when compute resources are available for the first ReplicatedJob
the JobSet can be dispatched by Kueue.

### Defaulting/Validation

- StartupPolicy is immutable.
- StartupPolicyOrderOption of `AnyOrder` is the default setting.
- For backward compatibility the default value for ReplicatedJobsStatusOption is Ready when
  StartupPolicy API is used.
- All ReplicatedJob names except the last one must present in the targetReplicatedJobs,
  and their names must be unique.

Since the default value for status is Ready, the default list for targetReplicatedJobs looks as
follows:

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: startup-policy
spec:
  startupPolicy:
    startupPolicyOrder: InOrder
    rules:
      - targetReplicatedJobs:
          - job-1
          - job-2
        waitForReplicatedJobsStatus: Ready
  replicatedJobs:
    - name: job-1
      ...
    - name: job-2
      ...
    - name: job-3
      ...
```

### User Experience

We will keep the existing conditions when `InOrder` StartupPolicy is used.

The following condition will be added to JobSet when ReplicatedJobs are being created:

```golang
metav1.Condition{
   Type:    "StartupPolicyInProgress",
   Status:  metav1.ConditionStatus(corev1.ConditionFalse),
   Reason:  "InOrderStartupPolicyInProgress",
   Message: "in order startup policy is in progress",
  })
```

The following condition will be added to JobSet when all ReplicatedJobs have started:

```golang
metav1.Condition{
   Type:    "StartupPolicyCompleted",
   Status:  metav1.ConditionStatus(corev1.ConditionFalse),
   Reason:  "InOrderStartupPolicyCompleted",
   Message: "in order startup policy has completed",
  })
```

### Test Plan

[x] I/we understand the owners of the involved components may require updates to
existing tests to make this code solid enough prior to committing the changes necessary
to implement this enhancement.

#### Unit Tests

- `controllers`: `01/19/2024` - `30.2%`

We will add this functionality to the startup_policy API for the functionality.

#### Integration tests

- Using "ready" ReplicatedJobs status for JobSet with 2+ ReplicatedJobs
- Using "succeeded" ReplicatedJobs status for JobSet with 2+ ReplicatedJobs
- Validate that if the first ReplicatedJob fails, the second ReplicatedJob does not execute.
- Ensure that when StartupPolicy and FailurePolicy sets together, the JobSet will restart
  the job sequence from the beginning in case of failure.
- Validate the JobSet controller restart in the middle of the ReplicatedJobs execution when
  StartupPolicy is set.

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

### Using workflow engine to execute sequence of jobs

Instead of supported Succeeded condition in StartupPolicy, users can leverage the existing workflow
engines like Argo Workflow or Tekton Pipeline to execute it.

While workflow engines are a good option for orchestrating Directed Acyclic Graphs (DAGs),
users may want to include the initialization phase as part of their training jobs. This is
particularly important in LLMs fine-tuning use cases, where every training job involves downloading
and distributing pre-trained models and datasets across all training nodes before starting
distributed fine-tuning.

For such use-cases users can consider the pre-trained model and dataset download, along with
distributed fine-tuning as integral parts of the ML training step within the broader
AI/ML lifecycle, which contains data preparation, training, tuning, evaluation, and serving stages.

Thus, providing users the option to initialize ML assets as part of a single JobSet make sense.
However, for complex data preparation tasks, such as those using engines like Spark, users should
consider to decouple them from the JobSet used for training/fine-tuning.

### Add the WaitForStatus API under ReplicatedJob

Since every ReplicatedJob name must be present in the TargetReplicatedJobs list, alternatively
we can add the WaitForReplicatedJobsStatus option under the ReplicatedJob API:

```golang
type JobSetSpec struct {
  StartupPolicy *StartupPolicy `json:"startupPolicy,omitempty"`

  ReplicatedJobs []ReplicatedJob `json:"replicatedJobs,omitempty"`
}

type StartupPolicy struct {
  // Order in which Jobs will be created.
  StartupPolicyOrder StartupPolicyOrderOption `json:"startupPolicyOrder"`
}


type ReplicatedJob struct {
  // Name is the name of the entry and will be used as a suffix.
  Name string `json:"name"`

  // Status the target ReplicatedJobs must reach before subsequent ReplicatedJobs begin executing.
  WaitForStatus WaitForStatusOption `json:"waitForStatus,omitempty"`
}
```

However, it makes the StartupPolicy API inconsistent with the FailurePolicy and SuccessPolicy APIs.
