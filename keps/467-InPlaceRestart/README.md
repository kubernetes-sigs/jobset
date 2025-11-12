# KEP-467: Fast failure recovery with in place restarts

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
    - [Story 1: Fast Recovery for Large Scale ML Training](#story-1-fast-recovery-for-large-scale-ml-training)
    - [Story 2: Combining In Place Restart With The Failure API for Unrecoverable Errors](#story-2-combining-in-place-restart-with-the-failure-api-for-unrecoverable-errors)
  - [Notes/Constraints/Caveats (Optional)](#notesconstraintscaveats-optional)
  - [Risks and Mitigations](#risks-and-mitigations)
- [Design Details](#design-details)
  - [API](#api)
  - [Implementation - Documentation](#implementation---documentation)
  - [Implementation - Webhook validation](#implementation---webhook-validation)
  - [Implementation - Agent sidecar](#implementation---agent-sidecar)
  - [Implementation - Permissions for the agent sidecar](#implementation---permissions-for-the-agent-sidecar)
  - [Implementation - JobSet controller](#implementation---jobset-controller)
  - [Test Plan](#test-plan)
      - [Prerequisite testing updates](#prerequisite-testing-updates)
    - [Unit Tests](#unit-tests)
    - [Integration tests](#integration-tests)
    - [e2e tests](#e2e-tests)
  - [Graduation Criteria](#graduation-criteria)
    - [Alpha](#alpha)
    - [Beta](#beta)
    - [GA](#ga)
- [Implementation History](#implementation-history)
- [Drawbacks](#drawbacks)
- [Alternatives](#alternatives)
  - [Use the JobSet controller to directly restart the Pods in place instead of using the agent sidecar](#use-the-jobset-controller-to-directly-restart-the-pods-in-place-instead-of-using-the-agent-sidecar)
  - [Use gRPC instead of the API server to establish communication between the JobSet controller and the agent sidecars](#use-grpc-instead-of-the-api-server-to-establish-communication-between-the-jobset-controller-and-the-agent-sidecars)
  - [Use the annotation <code>jobset.sigs.k8s.io/restart-attempt</code> instead of <code>jobset.sigs.k8s.io/epoch</code>](#use-the-annotation--instead-of-)
  - [Make the barrier optional](#make-the-barrier-optional)
  - [Restart only specified Pods instead of all of them](#restart-only-specified-pods-instead-of-all-of-them)
- [Future work](#future-work)
  - [Inject the agent sidecars to simplify the JobSet spec](#inject-the-agent-sidecars-to-simplify-the-jobset-spec)
  - [Recreate only failed Jobs](#recreate-only-failed-jobs)
  - [Speed up failure detection](#speed-up-failure-detection)
  - [Restart only specified containers to skip the overhead of recreating all watches](#restart-only-specified-containers-to-skip-the-overhead-of-recreating-all-watches)
<!-- /toc -->

## Summary

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

This KEP proposes the new restart strategy `InPlaceRestart` for JobSet. The objective is to speed up the restart time of distributed ML model training, which is traditionally done by recreating all Pods. This is especially important for large scales, where frequent failures that take longer to recover can cost millions. The proposal leverages the incoming `RestartPod` action to enable in place restart of Pods for JobSet workloads, which avoids the overhead of recreating Pods and significantly reduces recovery time. The solution involves adding an agent sidecar to each worker Pod to allow Pods to be restarted in place on demand and updating the JobSet controller to orchestrate group restarts by restarting healthy Pods in place while allowing the Job controller to recreate failed Pods.

## Motivation

<!--
This section is for explicitly listing the motivation, goals, and non-goals of
this KEP.  Describe why the change is important and the benefits to users. The
motivation section can optionally provide links to [experience reports] to
demonstrate the interest in a KEP within the wider Kubernetes community.

[experience reports]: https://github.com/golang/go/wiki/ExperienceReports
-->

A common pattern for training ML models in distributed systems is creating one worker process per Node and having them coordinate to run the workload. If any process fails in this setting, the workers lose sync and all of them must be restarted to restore coordination and resume the training from the last checkpoint.

This pattern can be achieved in JobSet by using `jobSet.failurePolicy.restartStrategy = Recreate` (default) along with `jobSet.failurePolicy.rules: []` (default) and `jobTemplate.spec.backoffLimit = 0`. The following manifest shows an example of a ML workload that implements this pattern in GKE with TPUs.

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: jobset-example-recreate-all-pods
  annotations:
    # Force 1 Job per Node pool
    alpha.jobset.sigs.k8s.io/exclusive-topology: cloud.google.com/gke-nodepool
spec:
  failurePolicy:
    maxRestarts: ... # Maximum number of full restarts
    restartStrategy: Recreate # (Default) Restart the JobSet by recreating the Jobs
    rules: [] # (Default) Trigger a JobSet restart if any Job fails
  replicatedJobs:
  - name: workers
    replicas: ... # Number of Node pools
    template:
      spec:
        completions: ... # Number of Nodes per Node pool
        parallelism: ... # Number of Nodes per Node pool
        backoffLimit: 0 # Must be zero to fail the Job if any of its child Pods fail
        template:
          spec:
            # Force 1 worker Pod per Node
            resources:
              requests:
                google.com/tpu: 4 # Use all TPU chips in the Node
            containers:
            - name: worker
            ...
```

In this case, when a failure happens like a worker container exiting non-zero or a Node failing, the associated Pod will fail. Since `backoffLimit = 0`, the parent Job will fail. Because `rules = []`, the default action `RestartJobSet` will be triggered, which restarts the JobSet. As `restartStrategy = Recreate`, the JobSet will be restarted by recreating all Jobs. That is, the JobSet controller will recreate all child Jobs, which causes all child Pods to be recreated.

While failure recovery can be done by recreating all child Pods, it introduces significant overhead for large workloads (>= 1,000 Nodes). The process of terminating, rescheduling, and re-initializing thousands of Pods can take several minutes per restart. At large scales, failures are common, with a mean time to failure (MTTF) measured in hours. Consequently, these time-consuming restarts occur frequently, leading to a substantial loss of productive compute time. This downtime is especially costly for workloads that rely on expensive hardware accelerators like GPUs or TPUs. For a 10,000 Node cluster, each minute of restart overhead can translate to [$1 million per month in wasted resources](https://docs.google.com/document/d/16zexVooHKPc80F4dVtUjDYK9DOpkVPRNfSv0zRtfFpk/edit?tab=t.0#heading=h.xhhuoh80qqw).

An alternative is to skip the Pod recreation and instead restart only the containers while keeping the Pods running. We refer to this process as "in place restart". Currently, this can be achieved by setting `restartPolicy = OnFailure` in the Pod spec, but it is limited to restarting only the failed container and can be triggered by only the failed container.

A more comprehensive API is the `RestartPod` action which will extend the new `restartPolicyRules` API (released in version 1.34) and is planned to be released soon (ETA version 1.35). The `RestartPod` action can be triggered by any container exiting a specified exit code and causes all containers to terminate and start in order. In other words, instead of deleting a Pod and creating a new one to replace it, an in place restart can be achieved by forcing one of the containers to exit with a specified exit code to trigger `RestartPod` and cause all containers to be restarted while the Pod is still running in the same Node.

The only missing piece for achieving efficient restarts is native support from JobSet controller for restarting JobSet objects by restarting the Pods in place instead of recreating them. This is what we aim to fix in this proposal.

### Goals

<!--
List the specific goals of the KEP. What is it trying to achieve? How will we
know that this has succeeded?
-->

- Introduce the new restart strategy `InPlaceRestart` to the JobSet API to significantly reduce recovery time from failures at large scales. This will be achieved by restarting the healthy Pods in place instead of recreating them
- Ensure that in place restart is compatible with all sources of failures currently supported (container failure, Pod failure, Node failure, Job failure, etc)
- Ensure that in place restart is compatible with and can be configured alongside the existing Failure Policy API

### Non-Goals

<!--
What is out of scope for this KEP? Listing non-goals helps to focus discussion
and make progress.
-->

- Improve performance of JobSet restarts by default and for all scales
- Implement partial restarts. [Restarting single Jobs is an ongoing discussion in JobSet](https://github.com/kubernetes-sigs/jobset/issues/976) and can be incorporated later to in place restart

## Proposal

<!--
This is where we get down to the specifics of what the proposal actually is.
This should have enough detail that reviewers can understand exactly what
you're proposing, but should not include things like API designs or
implementation. What is the desired outcome and how do we measure success?.
The "Design Details" section below is for the real
nitty-gritty.
-->

We propose a new restart strategy to JobSet called `InPlaceRestart`. When enabled, this strategy will recover from failures by restarting the healthy Pods in place and recreating the Failed Pods (expected to be none or only a few in the case of failed Nodes). 

When enabled, in place restarts are achieved with 3 main changes:

- Forcing `backoffLimit = 2147483647` (`MaxInt32`) so that Pod failures lead to individual Pod recreations, while the other Pods are restarted in place
- Adding a new "agent" sidecar to be used only with `InPlaceRestart`. Its objective is to restart its Pod in place on demand by exiting specified exit codes to trigger the `RestartPod` action
- Changing the JobSet controller to support the `InPlaceRestart` strategy. For instance, the JobSet controller needs to broadcast restart signals to the agent sidecars during group restarts

### User Stories (Optional)

<!--
Detail the things that people will be able to do if this KEP is implemented.
Include as much detail as possible so that people can understand the "how" of
the system. The goal here is to make this feel real for users without getting
bogged down.
-->

#### Story 1: Fast Recovery for Large Scale ML Training

As a user running large scale ML training workloads on thousands of expensive accelerator Nodes, I face frequent recoverable failures. Each failure triggers a full JobSet restart, which takes several minutes to recreate all Pods,  leading to significant downtime and wasted cost. I want to configure my JobSet to recover from these failures in seconds, not minutes, by restarting the worker Pods in place.

**Example JobSet Configuration for this use case**:

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: jobset-example-in-place-restart
  annotations:
    # Force 1 Job per Node pool
    alpha.jobset.sigs.k8s.io/exclusive-topology: cloud.google.com/gke-nodepool
spec:
  failurePolicy:
    maxRestarts: ... # Maximum number of full restarts
    restartStrategy: InPlaceRestart # Enable in place restart
  replicatedJobs:
  - name: workers
    replicas: ... # Number of Node pools
    template:
      spec:
        completions: ... # Number of Nodes per Node pool
        parallelism: ... # Number of Nodes per Node pool
        backoffLimit: 2147483647 # MaxInt32. Avoid full recreation by not failing Jobs due to failed Pods and containers
        podReplacementPolicy: Failed # Make sure the replacement Pod is created only after the original has fully failed
        template:
          spec:
            initContainers:
            # Agent sidecar
            - name: agent
              image: ... # Should be buildable from new code in the JobSet repo
              # Restart Pod in place if agent exits with exit code X
              # Otherwise, fail Pod
              restartPolicy: Always # Necessary for sidecar
              restartPolicyRules:
              - action: RestartPod
                exitCodes:
                  operator: In
                  values: [X]
              - action: Terminate
                exitCodes:
                  operator: NoIn
                  values: [X]
              # Barrier
              # Allow worker container to start only when the agent sidecar creates this endpoint
              startupProbe:
                httpGet:
                  path: /barrier-is-lifted
                  port: 8080
                failureThreshold: ...
                periodSeconds: 1
              env:
                # Required env variables for agent sidecar
                - name: NAMESPACE
                  valueFrom:
                    fieldRef:
                      fieldPath: metadata.namespace
                - name: POD_NAME
                  valueFrom:
                    fieldRef:
                      fieldPath: metadata.name
                - name: JOBSET_NAME
                  valueFrom:
                    fieldRef:
                      fieldPath: metadata.annotations['jobset.sigs.k8s.io/jobset-name']
                # Optional env variables for agent sidecar
                # Customize the exit code of agent sidecar for triggering RestartPod
                - name: RESTART_POD_IN_PLACE_EXIT_CODE
                  value: "X"
            containers:
            # Worker container
            - name: worker
              # Restart Pod in place if worker exits non-zero exit code
              # Otherwise succeed the Pod
              restartPolicy: Never
              restartPolicyRules:
              - action: RestartPod
                exitCodes:
                  operator: NotIn
                  values: [0]
            # Force 1 worker Pod per Node
            resources:
              requests:
                google.com/tpu: 4 # Use all TPU chips in the Node
```

#### Story 2: Combining In Place Restart With The Failure API for Unrecoverable Errors

As a user, my training workload can fail for two reasons: recoverable issues and fatal unrecoverable errors (e.g., a misconfigured dataset path) that causes the worker to exit with exit code `Y`. I want to use fast in place restarts for transient issues but fail the entire JobSet immediately if the unrecoverable errors occur to avoid wasting resources on pointless restarts.

**Example JobSet Configuration for this use case**:

Summary of how exit codes are handled:

* If agent sidecar exits `X`: restart Pod in place (on demand in place restart)
* If agent sidecar exits anything but `X`: fail the Pod without failing the parent Job (failure in agent, recreate the Pod individually)
* If worker container exits `0`: succeed the Pod to complete the workload (workload finished successfully)
* If worker container exits `Y`: fail the JobSet no matter the number of restarts so far (unrecoverable failure)
* If worker container exits `Z`: fail the Pod without failing the parent Job (worker failure that is recoverable with Pod recreation but not Pod in place restart)

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: jobset-example-in-place-restart-failure-policy
  annotations:
    # Force 1 Job per Node pool
    alpha.jobset.sigs.k8s.io/exclusive-topology: cloud.google.com/gke-nodepool
Spec:
  failurePolicy:
    maxRestarts: ... # Maximum number of full restarts
    restartStrategy: InPlaceRestart # Enable in place restart
    # Fail JobSet if Job fails with reason PodFailurePolicy
    # Equivalently, fail JobSet if a worker container exits with exit code Y
    rules:
    - action: FailJobSet
      onJobFailureReasons:
      - PodFailurePolicy
  replicatedJobs:
  - name: workers
    replicas: ... # Number of Node pools
    template:
      spec:
        completions: ... # Number of Nodes per Node pool
        parallelism: ... # Number of Nodes per Node pool
        backoffLimit: 2147483647 # MaxInt32. Required to not fail Job due to failed Pods and intentional container restarts
        podReplacementPolicy: Failed # Make sure the replacement Pod is created only after the original has fully failed
        # Fail Job with reason PodFailurePolicy if a worker container exits with exit code Y
        podFailurePolicy:
          rules:
          - action: FailJob
            onExitCodes:
              containerName: worker
              operator: In
              values: [Y]
        template:
          spec:
            initContainers:
            # Agent sidecar
            - name: agent
              image: ... # Should be buildable from new code in the JobSet repo
              # Restart Pod in place if agent exits with exit code X
              # Otherwise, fail Pod
              restartPolicy: Always # Necessary for sidecar
              restartPolicyRules:
              - action: RestartPod
                exitCodes:
                  operator: In
                  values: [X]
              - action: Terminate
                exitCodes:
                  operator: NoIn
                  values: [X]
              # Allow worker container to start only when the agent container creates this endpoint
              startupProbe:
                httpGet:
                  path: /barrier-is-lifted
                  port: 8080
                failureThreshold: ...
                periodSeconds: 1
              env:
                # Required env variables for agent sidecar
                - name: NAMESPACE
                  valueFrom:
                    fieldRef:
                      fieldPath: metadata.namespace
                - name: POD_NAME
                  valueFrom:
                    fieldRef:
                      fieldPath: metadata.name
                - name: JOBSET_NAME
                  valueFrom:
                    fieldRef:
                      fieldPath: metadata.annotations['jobset.sigs.k8s.io/jobset-name']
                # Optional env variables for agent sidecar
                # Customize the exit code of agent sidecar for triggering RestartPod
                - name: RESTART_POD_IN_PLACE_EXIT_CODE
                  value: "X"
            containers:
            # Worker container
            - name: worker
              # Complete Pod if worker exits 0
              # Fail Pod if worker exits with exit code Y or Z
              # Otherwise, restart Pod in place
              restartPolicy: Never
              restartPolicyRules:
              - action: Terminate
                exitCodes:
                  operator: In
                  values: [Y, Z]
              - action: RestartPod
                exitCodes:
                  operator: NotIn
                  values: [0, Y, Z]
              ...
            # Force 1 worker Pod per Node
            resources:
              requests:
                google.com/tpu: 4 # Use all TPU chips in the Node
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

<!--
This section should contain enough information that the specifics of your
change are understandable. This may include API specs (though not always
required) or even code snippets. If there's any ambiguity about HOW your
proposal will be implemented, this is the place to discuss them.
-->

Refer to the [design doc](https://docs.google.com/document/d/16zexVooHKPc80F4dVtUjDYK9DOpkVPRNfSv0zRtfFpk/edit?usp=sharing) for more details.

### API

Changes to the JobSet spec.

```go
// README: No change. Only here for reference.
type FailurePolicy struct {
  // Limit on the number of JobSet restarts.
  MaxRestarts int32 `json:"maxRestarts,omitempty"`

  // Strategy to use when restarting the JobSet.
  //
  // +optional
  // +kubebuilder:default=Recreate
  RestartStrategy JobSetRestartStrategy `json:"restartStrategy,omitempty"`

  // List of failure policy rules for the JobSet.
  //
  // For a given Job failure, the rules will be evaluated in order,
  // and only the first matching rule will be executed.
  //
  // If no matching rule is found, the RestartJobSet action is applied.
  Rules []FailurePolicyRule `json:"rules,omitempty"`
}

// README: Add `InPlaceRestart` to valid enum values in validation.
//
// +kubebuilder:validation:Enum=Recreate;BlockingRecreate;InPlaceRestart
type JobSetRestartStrategy string

// README: Add new value `InPlaceRestart`.
const (
  // Restart the JobSet by recreating all Jobs.
  //
  // Each Job is recreated as soon as its previous iteration (and its Pods) is deleted.
  Recreate JobSetRestartStrategy = "Recreate"

  // Restart the JobSet by recreating all Jobs.
  // 
  // Ensures that all Jobs (and their Pods) from the previous iteration are deleted 
  // before creating new Jobs.
  BlockingRecreate JobSetRestartStrategy = "BlockingRecreate"

  // When no Job has failed, restart the JobSet by restarting healthy Pods in place 
  // and recreating failed Pods.
  //
  // When a Job has failed, fall back to action `Recreate` and execute the matching failure policy rule.
  InPlaceRestart JobSetRestartStrategy = "InPlaceRestart"
)
```

Changes to the JobSet status.

```go
// README: Add new fields `deprecatedEpoch` and `syncedEpoch`.
type JobSetStatus struct {
  // ... Other fields

  // The most recent deprecated epoch of the JobSet.
  //
  // Healthy Pods that have an epoch smaller than or equal to this value should be restarted in place.
  //
  // This is written by the JobSet controller and read by the agent sidecars.
  //
  // +optional
  // +kubebuilder:default=0
  DeprecatedEpoch *int32 `json:"deprecatedEpoch,omitempty"`

  // The most recent synced epoch of the JobSet.
  //
  // Pods that have an epoch equal to this value should lift their barrier to allow the worker containers to start running.
  //
  // This is written by the JobSet controller and read by the agent sidecars.
  //
  // +optional
  // +kubebuilder:default=0
  SyncedEpoch *int32 `json:"syncedEpoch,omitempty"`
}
```

Changes to the reserved annotations.

```go
const (
  // ... Other reserved labels and annotations
  
  // Meant to be used as a Pod annotation for worker Pods.
  //
  // Its value is the epoch of the worker Pod and should be treated as int32.
  //
  // This is written by the agent sidecar and read by the JobSet controller.
  //
  // If the epoch of any worker Pod exceeds jobSet.spec.failurePolicy.maxRestarts, fail the JobSet.
  //
  // If the epoch of the worker Pod is smaller than or equal to `deprecatedEpoch`, restart the Pod in place.
  //
  // If the epoch of the worker Pod is equal to `syncedEpoch`, lift the Pod barrier to allow the worker container to start running.
  EpochKey string = "jobset.sigs.k8s.io/epoch"
)
```

### Implementation - Documentation

In place restart is a complex feature with a complex set up. Therefore, we propose to add documentation to the JobSet website to explain the feature, its requirements and set up.

### Implementation - Webhook validation

When in place restart is enabled (i.e., the field `jobSet.spec.failurePolicy.restartStrategy` is set to `InPlaceRestart`), the following should be validated by the JobSet webhook:

* `jobSet.spec.replicatedJobs[].template.spec.backOffLimit` should be set to `MaxInt32` (i.e., `2147483647`) to avoid unnecessarily failing Jobs if a child Pod fails individually or a Pod is restarted in place  
* `jobSet.spec.replicatedJobs[].template.spec.podReplacementPolicy` should be set to `Failed` to make sure the replacement Pod is created only after the original Pod has fully failed  
* `jobSet.spec.replicatedJobs[].template.spec.template.spec.initContainers` should contain the agent sidecar to handle in place restart operations at the worker Pod level for the JobSet controller. This is partially verified by checking for the existence of a sidecar that contains the `RestartPod` rule

### Implementation - Agent sidecar

The only way to trigger the `RestartPod` action on demand is by making one container exit with a specified exit code. Therefore, we propose to add a new container image that will be buildable from new code in the JobSet repo. The container should be added by the user to the JobSet manifest as the “agent sidecar”. The agent sidecar is responsible for handling in place restart operations at the worker Pod level for the JobSet controller.

The high level structure of the agent sidecar is the following.

```python
# Initialize
parentJobSet = getJobSet(env.namespace, env.jobSetName)
mostRecentSyncedEpoch = jobset.status.syncedEpoch
epoch = mostRecentSyncedEpoch + 1
patch = {"metadata" : {"annotations" : {"jobset.sigs.k8s.io/epoch" : epoch}}}
patchPod(env.namespace, env.podName, patch)

# Watch
for event in watchJobSet(env.namespace, env.jobSetName):
  mostRecentDeprecatedEpoch = event.jobSet.status.deprecatedEpoch
  mostRecentSyncedEpoch = event.jobSet.status.syncedEpoch

  # Check if Pod must be restarted in place because its epoch has been deprecated
  # If so, exit specified exit code to trigger RestartPod action
  if epoch <= mostRecentDeprecatedEpoch:
    exit(env.restartInPlaceExitCode)

  # Check if Pod barrier must be lifted because its epoch has been marked as synced
  # If so, create endpoint "/barrier-is-lifted" to succeed startup probe 
  if epoch == mostRecentSyncedEpoch:
    createEndpoint("/barrier-is-lifted") # Idempotent
```

The highlights are:

- Calculate the Pod epoch at start up as `jobSet.status.syncedEpoch + 1`. This makes sure the worker container will start running only when the JobSet controller updates `jobSet.status.syncedEpoch`. This is done only when all worker Pods are at the same epoch
- Restart the Pod in place if its epoch has been deprecated by `checking epoch <= jobSet.status.deprecatedEpoch`. This is done only when a group restart is necessary
- Lift the Pod barrier if its epoch has been marked as synced by checking `epoch == mostRecentSyncedEpoch`. This is done only when all worker Pods are at the same epoch

When `RestartPod` is used, the barrier lift can be done by setting up a startup probe for the agent sidecar and making sure the agent sidecar creates the specified endpoint when the barrier must be lifted. If `restartPolicy = OnFailure` is used instead, the barrier lift can be done starting the worker process command when the barrier must be lifted.

The image of the agent sidecar will be buildable from code in the JobSet repo (e.g., available in a new folder `agent/`). New versions of this image will be released in the JobSet releases.

### Implementation - Permissions for the agent sidecar

The agent sidecar requires permissions to watch its parent JobSet. It also requires permissions to update its Pod epoch, which can be integrated with a validating admission policy to make sure only the epoch annotation can be modified. The following manifest shows an example of how a service account can be set up for the worker Pod. It will be part of the documentation of in place restart since it's up to the user to integrate these extra permissions to any existing service account.

```yaml
# Service account
apiVersion: v1
kind: ServiceAccount
metadata:
  name: in-place-restart-sa
---
# JobSet watcher role
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: jobset-watcher
  namespace: default
rules:
- apiGroups: ["jobset.x-k8s.io"]
  resources: ["jobsets"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: bind-in-place-restart-sa-to-jobset-watcher
  namespace: default
subjects:
- kind: ServiceAccount
  name: in-place-restart-sa
  namespace: default
roleRef:
  kind: Role
  name: jobset-watcher
  apiGroup: rbac.authorization.k8s.io
---
# Pod patcher role
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: pod-patcher-role
  namespace: default
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: bind-in-place-restart-sa-to-pod-patcher-role
  namespace: default
subjects:
- kind: ServiceAccount
  name: in-place-restart-sa
  namespace: default
roleRef:
  kind: Role
  name: pod-patcher-role
  apiGroup: rbac.authorization.k8s.io
---
# Allow only the pod annotation "jobset.sigs.k8s.io/epoch" to be updated
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: update-only-epoch-pod-annotation
spec:
  failurePolicy: Fail
  matchConstraints:
    resourceRules:
    - apiGroups:   [""]
      apiVersions: ["v1"]
      operations:  ["UPDATE"]
      resources:   ["pods"]
  validations:
  - expression: >
      request.userInfo.username != 'system:serviceaccount:default:in-place-restart-sa' ||
      (
        oldObject.spec == object.spec &&
        oldObject.metadata.labels == object.metadata.labels &&
        oldObject.metadata.annotations.all(key, key == 'jobset.sigs.k8s.io/epoch' || (key in object.metadata.annotations && oldObject.metadata.annotations[key] == object.metadata.annotations[key])) &&
        object.metadata.annotations.all(key, key == 'jobset.sigs.k8s.io/epoch' || (key in oldObject.metadata.annotations && oldObject.metadata.annotations[key] == object.metadata.annotations[key]))
      )
    message: "ServiceAccount 'in-place-restart-sa' can only update the Pod annotation 'jobset.sigs.k8s.io/epoch'."
---
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicyBinding
metadata:
  name: update-only-epoch-pod-annotation-binding
spec:
  policyName: update-only-epoch-pod-annotation
  validationActions: [Deny]
```

### Implementation - JobSet controller

The changes to the JobSet controller are responsible for orchestrating the group restarts of JobSet workloads when using in place restart. This boils down to updating the `jobSet.status.deprecatedEpoch` and `jobSet.status.syncedEpoch` fields based on the epochs of the worker Pods. 

The high level structure of the changes to the JobSet reconciliation are the following.

```python
# Reconcile JobSet
def reconcile(jobSet):
  # Current code
  # ...

  # New code
  if isInPlaceRestartEnabled(jobSet):
    reconcileEpochs(jobSet)

# Check if in place restart is enabled
def isInPlaceRestartEnabled(jobSet):
  return jobSet.spec.failurePolicy.restartStrategy == "InPlaceRestart"

# Reconcile only in place restart fields
def reconcileEpochs(jobSet):
  childPods = listChildPods(jobSet.metadata.namespace, jobSet.metadata.name)
  epochs = extractEpochs(childPods)
  expectedEpochLength = countExpectedChildPods(jobSet)

  # Check if all worker Pods are at the same epoch
  # If so, make sure syncedEpoch is equal to this common value
  # (represented here by `epochs[0]`)
  # This makes sure the Pod barriers are lifted
  if len(epochs) == expectedEpochsLength and allEqual(epochs):
    jobSet.status.syncedEpoch = epochs[0] # Idempotent

  # Otherwise, it means that the worker Pods are not in sync
  # If so, make sure deprecatedEpoch is equal to max(epochs) - 1
  # This makes sure all Pods that are not at the last epoch will be restarted in place
  else:
    jobSet.status.deprecatedEpoch = max(epochs) - 1 # Idempotent

# Extract values of jobset.sigs.k8s.io/epoch annotations
def extractEpochs(pods):
  epochs = []
  for pod in pods:
    rawEpoch = pod.metadata.annotations["jobset.sigs.k8s.io/epoch"]
    epoch = int(rawEpoch)
    epochs.append(epoch)

  return epochs

# Count expected number of child Pods
def countExpectedChildPods(jobSet):
  count = 0
  for replicatedJob in jobSet.spec.replicatedJobs:
    jobTemplate = replicatedJob.template
    count += replicatedJob.replicas * jobTemplate.spec.parallelism

  return count
```

The highlights are:

* Only run in place restart logic for JobSet objects that have in place restart enabled (i.e., the field `jobSet.spec.failurePolicy.restartStrategy` is set to `InPlaceRestart`)  
* If all child Pods exist and have the same epoch, it means they are in sync and should have their barriers lifted, so set `jobSet.status.syncedEpoch = epochs[0]` (equivalent to `jobSet.status.syncedEpoch += 1`). The agent sidecars will get this new synced epoch value and lift their barriers  
* If the Pods are still not in sync (there is a mismatch in their epochs), make sure to deprecate all epochs that are not the most recent with `jobSet.status.deprecatedEpoch = max(epochs) - 1` (equivalent to `deprecatedEpoch = syncedEpoch`). This makes sure all agent sidecars that are not at the most recent epoch will restart in place to reach the new epoch

Besides the mentioned changes to the reconciliation loop, we also require to:

* Set up a feature flag
* Change the JobSet controller to watch child Pods for reconciliation  
* Change the JobSet controller to index child Pods for efficient listing  
* If the worker epochs ever exceed `jobset.spec.failurePolicy.maxRestarts`, fail the JobSet

### Test Plan

<!--
**Note:** *Not required until targeted at a release.*
The goal is to ensure that we don't accept enhancements with inadequate testing.

All code is expected to have adequate tests (eventually with coverage
expectations). Please adhere to the [Kubernetes testing guidelines][testing-guidelines]
when drafting this test plan.

[testing-guidelines]: https://git.k8s.io/community/contributors/devel/sig-testing/testing.md
-->

[X] I/we understand the owners of the involved components may require updates to
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

- `controllers`: `11/03/2025` - `55.6%`

Unit tests will be added to both the JobSet controller and the new agent image. For the JobSet controller, the objective is to test the core logic of how the JobSet controller should react given the child Pod epochs. For the agent, the objective is to test the core logic of how the agent should react given the parent JobSet status.

#### Integration tests

<!--
Describe what tests will be added to ensure proper quality of the enhancement.

After the implementation PR is merged, add the names of the tests here.
-->

Make sure the following cases are covered:

- All Pods in sync: JobSet controller should not update the JobSet status
- All Pods just became in sync: JobSet controller should update `syncedEpoch`
- All Pods were in sync but one restarted into a new epoch: JobSet controller should update `deprecatedEpoch`
- Pods just lost sync and a second Pod restarted into the new epoch but not all yet: JobSet controller should not update the JobSet status

#### e2e tests

The e2e test should make sure a group restart succeeds when in place restart is enabled. The test should work as following:

- Create a JobSet that has 2 worker Pods. Each worker Pod should have the agent sidecar and a worker container. The worker container should exit non-zero on demand. This could be done by checking for the existence of a file, k8s resource or an HTTP request
- Once all worker processes are running, force one of them to exit non zero
- Check the Pod and JobSet manifests to make sure the group restart succeeded. If so, succeed the test. Otherwise, after a timeout period has passed, fail the test

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

#### Alpha

- Add the agent code and image to the JobSet repo to support Pods being restarted in place on demand
- Change the JobSet controller to orchestrate in place restarts. Implemented behing a feature flag
- Add extra validation to the JobSet webhook to validate JobSet manifests that have in place restart enabled
- Add tests for in place restart
- Add documentation for in place restart

#### Beta

- Address reviews and bug reports from alpha users
- Add a way to automatically build and release the agent sidecar image
- Add a way to add the agent sidecar automatically

#### GA

- Address reviews and bug reports from beta users
- Feature flag enabled by default

## Implementation History

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

- Draft KEP: November 3rd 2025

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

### Use the JobSet controller to directly restart the Pods in place instead of using the agent sidecar

Currently, there is no way to do that. The `RestartPod` action can only be triggered by a container exiting with a specified exit code. Alternatively, a new ideal upstream API could be created but the `RestartPod` action is the path of least resistance.

### Use gRPC instead of the API server to establish communication between the JobSet controller and the agent sidecars

Using the API server with a declarative Pod annotation for the epoch is a better practice from the [Kubernetes API conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status).

### Use the annotation `jobset.sigs.k8s.io/restart-attempt` instead of `jobset.sigs.k8s.io/epoch`

The annotation `jobset.sigs.k8s.io/restart-attempt` is technically written by the JobSet controller through the Pod template, so changing it with the agent sidecar would be an anti-pattern. Besides, the annotation `jobset.sigs.k8s.io/restart-attempt` in Pods is already heavily used for observability and changing its semantics would also be problematic.

### Make the barrier optional

This is technically possible, but it adds little overhead while allowing the design to make less assumptions over the worker containers.

### Restart only specified Pods instead of all of them

Currently JobSet only supports restarting the whole workload. Partial restart like restarting only a single Job or a group of ReplicatedJobs is desirable but it is still being [discussed](https://github.com/kubernetes-sigs/jobset/issues/918). 

## Future work

### Inject the agent sidecars to simplify the JobSet spec

The addition of agent sidecar makes the JobSet spec long and complex. It could be added by default and have a toggle to disable this behavior.

### Recreate only failed Jobs

When the JobSet failure policy is updated to support the recreation of only failed Jobs, in place restart should be updated to support it.

### Speed up failure detection

In the proposed design, the JobSet controller can only detect that a failure occurred when the agent sidecar of the failed worker is started again and updates the epoch annotation. A best effort optimization can be done by triggering a group restart when any worker container of the most recent epoch terminates. This optimization saves the time between the worker container failing and its agent sidecar starting again.

### Restart only specified containers to skip the overhead of recreating all watches

The proposed [RestartPod action](https://docs.google.com/document/d/1UmJHJzdmMA1hWwkoP1f3rG9nS0oZ2cRcOx8rO8MsExA/edit?usp=sharing&resourcekey=0-OuKspBji_1KJlj2JbnZkgQ) can only support restarting all containers, but a future expansion to the `restartPolicyRules` API can be made to allow only a few specified containers to be restarted. This allows for the agent sidecar to avoid being terminated in an on demand in place restart, which skips the overhead of recreating all watches.
