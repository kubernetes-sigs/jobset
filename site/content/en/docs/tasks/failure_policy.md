---
title: "Failure Policy"
linktitle: "Failure Policy"
weight: 10
date: 2025-07-23
description: >
    Configuring jobset failure policies
no_list: true
---

JobSet provides failure policy API to control how your workload behaves in response to child Job failures.

The `failurePolicy` is defined by a set of `rules`. For any job failure, the rules are evaluated in order, and the first matching rule's action is executed. If no rule matches, the default action is `RestartJobSet`, which counts towards the `maxRestarts` limit.

## Failure Policy Actions

### `FailJobSet`
This action immediately marks the entire JobSet as failed.

In [this example](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/failure-policy/failjobset-action.yaml), the JobSet is configured to fail immediately if the `leader` job fails.

{{< include file="/examples/failure-policy/failjobset-action.yaml" lang="yaml" >}}

### `RestartJobSet`
This action restarts the entire JobSet by recreating its child jobs. The number of restarts before failure is limited by `failurePolicy.maxRestarts`. This is the default action if no other rules match a failure.

In [this example](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/failure-policy/restartjobset-action.yaml), the JobSet will restart up to 3 times if the `leader` job fails.

{{< include file="/examples/failure-policy/restartjobset-action.yaml" lang="yaml" >}}

### `RestartJobSetAndIgnoreMaxRestarts`
This action is similar to `RestartJobSet`, but the restart does not count towards the `maxRestarts` limit. 

In [this example](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/failure-policy/restartjobsetandignoremaxrestarts-action.yaml), the JobSet will restart an unlimited number of times if the `leader` job fails. However, `workers` jobs will be covered by the default `RestartJobSet` action and only restart up to `maxRestarts` times.

{{< include file="/examples/failure-policy/restartjobsetandignoremaxrestarts-action.yaml" lang="yaml" >}}

## Targeting Specific Failures

You can make your failure policy rules more granular by using `targetReplicatedJobs` and `onJobFailureReasons`.

### Targeting Replicated Jobs
The `targetReplicatedJobs` field allows you to apply a rule only to failures originating from specific replicated jobs. All the examples above use this field.

### Targeting Job Failure Reasons
The `onJobFailureReasons` field allows you to trigger a rule based on the reason for the job failure. This is powerful for distinguishing between different kinds of errors. Valid reasons include `BackoffLimitExceeded`, `DeadlineExceeded`, and `PodFailurePolicy`.

#### Example: Handling `BackoffLimitExceeded`
[This example](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/failure-policy/onjobfailurereasons-present.yaml) configures the JobSet to perform unlimited restarts only when the `leader` job fails because its `backoffLimit` was exceeded.

{{< include file="/examples/failure-policy/onjobfailurereasons-present.yaml" lang="yaml" >}}

#### Example: Handling `PodFailurePolicy`
[This example](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/failure-policy/onjobfailurereasons-present-podfailurepolicy.yaml) shows how to use Kubernetes' `podFailurePolicy` in conjunction with JobSet's `failurePolicy`. The JobSet will restart an unlimited number of times only if the `leader` job fails due to its pod failure policy being triggered (in this case, a container exiting with code 1).

{{< include file="/examples/failure-policy/onjobfailurereasons-present-podfailurepolicy.yaml" lang="yaml" >}}

#### Example: Handling Host Maintenance Events
[This example](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/failure-policy/host-maintenance-event-model.yaml) models how to handle a host maintenance event. A pod eviction due to maintenance triggers a `DisruptionTarget` pod condition, which in turn triggers the job's `podFailurePolicy`. The JobSet's failure policy rule matches on the `PodFailurePolicy` reason and restarts the JobSet without counting it against `maxRestarts`. Any other type of failure in the `leader` job will trigger a normal restart that counts against `maxRestarts` (which is 0 in this case, meaning any other failure will cause the JobSet to fail immediately).

{{< include file="/examples/failure-policy/host-maintenance-event-model.yaml" lang="yaml" >}}