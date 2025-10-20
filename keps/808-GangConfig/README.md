# KEP-1068: Gang Scheduling

<!--
Gang Scheduling of JobSets
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
  - [Test Plan](#test-plan)
      - [Prerequisite testing updates](#prerequisite-testing-updates)
    - [Unit Tests](#unit-tests)
    - [Integration tests](#integration-tests)
  - [Graduation Criteria](#graduation-criteria)
- [Implementation History](#implementation-history)
- [Drawbacks](#drawbacks)
- [Alternatives](#alternatives)
<!-- /toc -->

## Summary

This KEP adds gang scheduling support for JobSets in accordance with the gang scheduling [KEP](https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/4671-gang-scheduling) for kube-scheduler.

## Motivation

Gang scheduling allows a group of pods to be scheduled and bound to nodes in an all-or-nothing manner. The primary use case is HPC workloads with collective communication (MPI, PyTorch) that require all pods to start/be ready at once. Long delays between the first and last pods starting can cause the collective communication initialization to timeout.

The proposed `kube-scheduler` [changes](https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/4671-gang-scheduling) require JobSet controller changes to create [Workload](https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/4671-gang-scheduling#api) objects automatically when a user creates a JobSet. `kube-scheduler` will schedule identical PodGroups in a gang. The PodGroups are set on the Workload object and a reference to this Workload object is stored in a new field `workloadRef` on each pod spec which will be used by kube-scheduler for gang scheduling.

### Goals

1. The JobSet controller should create a Workload object automatically when a JobSet that needs gang scheduling support is created by the user. The Workload object may be user created manually in which case the JobSet controller should respect any gang scheduling specifications and not override the user created Workload.
2. The JobSet controller manages the lifecycle of controller created Workloads i.e. deletes them when the JobSet is deleted.
3. Gang scheduling should be supported at the JobSet, ReplicatedJob and Job replica levels.

### Non-Goals

1. Topology aware scheduling.

## Proposal

The JobSet API will support a new GangConfig field to specify if a JobSet, ReplicatedJob or Job replica should be gang scheduled. The user should set this field at the appropriate level (JobSet or ReplicatedJob) in the JobSet spec to indicate they require gang scheduling at that level. If the JobSet controller detects the GangConfig field in the JobSet spec, it will generate a single Workload object and associate the pod spec templates it generates with that Workload.

The Job controller should not create a new Workload or change the workloadRef in a pod spec if the JobSet controller has already set it. To differentiate the levels of gang scheduling (JobSet, ReplicatedJob or Job replica gangs), the JobSet controller will set different PodGroup and PodGroupReplicaIndex fields in the WorkloadReference spec per pod i.e.:
At the JobSet level, each pod spec is identical i.e. all pods have a workloadRef object reference with the same pod group name and PodGroupReplicaIndex is unset.
At the ReplicatedJob level, all pod specs across all Job replicas for that ReplicatedJob are identical i.e. pods have a workloadRef object reference with a different pod group name for pods across different ReplicatedJobs and PodGroupReplicaIndex is unset.
At the replica/Job level, pod specs for a single Job replica are identical i.e. pods have a workloadRef object reference with a different pod group name for pods across different ReplicatedJobs and PodGroupReplicaIndex is set to the Job replica index.

See [Associating Pods into PodGroups](https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/4671-gang-scheduling#associating-pod-into-podgroups) for the `WorkloadReference` definition.

### User Stories (Optional)

#### Story 1

As a user, I want to schedule all workers in my workload to start at once i.e. all pods in my JobSet should be scheduled at once or not at all.

The API below would fit my purpose:

```
apiVersion: jobset.x-k8s.io/v1
kind: JobSet
metadata:
  name: sample-jobset
spec:
  scheduleAtOnce: true
  replicatedJobs:
    - name: replicated-job-1
      replicas: 2
      template:
        spec:
          parallelism: 4
          completions: 4
          template:
            spec:
              containers:
                - name: initializer
                  image: busybox
                  command:
                    ...
    - name: replicated-job-2
      replicas: 2
      template:
      spec:
          parallelism: 4
          completions: 4
          template:
            spec:
              containers:
                - name: launcher
                  image: busybox
                  command:
                   ...
```

#### Story 2

As a user, I want to schedule all workers in a part of my workload (eg.: the driver) to start at once i.e. all pods in a ReplicatedJob should be scheduled at once or not at all.

The API below would fit my purpose:

```
apiVersion: jobset.x-k8s.io/v1
kind: JobSet
metadata:
  name: sample-jobset
spec:
    replicatedJobs:
    - name: replicated-job-1
      replicas: 2
      scheduleAtOnce: true
      template:
        spec:
          parallelism: 4
          completions: 4
          template:
            spec:
              containers:
                - name: initializer
                  image: busybox
                  command:
                    ...
    - name: replicated-job-2
      replicas: 1
      scheduleAtOnce: true
      dependsOn:
        - name: replicated-job-1
          status: Complete
      template:
        spec:
          parallelism: 3
          completions: 3
          template:
            spec:
              containers:
                - name: launcher
                  image: busybox
                  command:
                    ...
```

#### Story 3

As a user, I want to schedule groups of parallel workers in my workload to start at once i.e. all pods in a Job replica should be scheduled at once or not at all.

The API below would fit my purpose:

```
apiVersion: jobset.x-k8s.io/v1
kind: JobSet
metadata:
  name: sample-jobset
spec:
  replicatedJobs:
    - name: replicated-job-1
      replicas: 2
      scheduleAtOnce: true
      scheduleAtOncePerReplica: true
      template:
        spec:
          parallelism: 4
          completions: 4
          template:
            spec:
              containers:
                - name: initializer
                  image: busybox
                  command:
                    ...
    - name: replicated-job-2
      replicas: 3
      scheduleAtOnce: true
      scheduleAtOncePerReplica: true
      template:
        spec:
          parallelism: 3
          completions: 3
          template:
            spec:
              containers:
                - name: launcher
                  image: busybox
                  command:
                    ...
```

### Risks and Mitigations

## Design Details

### API proposal

The JobSetSpec and ReplicatedJob types will be extended to support a new GangConfig field:

```
type GangMode string

const (
    GangModeOff GangMode = "Off"  // no gang scheduling
    GangModeGang GangMode = "Gang" // top level gang scheduling
    GangModeReplicatedGang GangMode = "ReplicatedGang" // replica level gang scheduling
)

// May be extended to include timeouts or topology config in the future.
type GangConfig struct {
    GangMode GangMode
}
```
```
type JobSetSpec struct {
	ReplicatedJobs []ReplicatedJob json:"replicatedJobs,omitempty"
      ...

      // GangConfig should be specified if all ReplicatedJobs in this JobSet 
      // should be scheduled as a gang (i.e. all at once).
      GangConfig GangConfig json:"gangConfig,omitempty"
}
```
```
type ReplicatedJob struct {
	Name string json:"name"
       ....

      // GangConfig should be specified if the ReplicatedJob (or specific 
      // Replicas) should be scheduled as a gang (i.e. all at once).
      GangConfig GangConfig json:"gangConfig,omitempty"
}
```

API rules:

1. `jobSetSpec.GangConfig.GangMode` options:
- `GangModeOff`: no gang scheduling
- `GangModeGang`: all pods in the JobSet are in a gang
2. `replicatedJob.GangConfig.GangMode` options:
- `GangModeOff`: no gang scheduling
- `GangModeGang`: all pods in the ReplicatedJob are in a gang
- `GangModeReplicatedGang`: all pods in a Job replica are in a gang
3. For simplicity, both `jobSetSpec.GangConfig.GangMode` and `replicatedJob.GangConfig.GangMode` cannot be specified at once (except to `GangModeOff`).
4. If `GangConfig` is unset at both the JobSet and ReplicatedJob level, no Workload object will be generated i.e. the pods will not be gang scheduled.

Note that in v1, we will only support the API change for ReplicatedJob and not JobSetSpec. To use JobSet level gang scheduling i.e. schedule all JobSet pods together, we will rely on the user to create the Workload and set a reference in the ReplicatedJob spec. Eg.:

JobSet:

```
apiVersion: jobset.x-k8s.io/v1
kind: JobSet
metadata:
  name: sample-jobset
spec:
  gangConfig:
    gangMode: Gang
  replicatedJobs:
    - name: replicated-job-1
      replicas: 2
      template:
        spec:
          parallelism: 4
          completions: 4
          template:
            spec:
              workload:
   		 name: w-sample-jobset
   		 podGroup: pg-sample-jobset
              containers:
                - name: initializer
                  image: busybox
                  command:
                    ...
    - name: replicated-job-2
      replicas: 2
      template:
      spec:
          parallelism: 4
          completions: 4
          template:
            spec:
              workload:
   		 name: w-sample-jobset
   		 podGroup: pg-sample-jobset
              containers:
                - name: launcher
                  image: busybox
                  command:
                    ...
```

User-created Workload:

```
apiVersion: scheduling/v1alpha1  
kind: Workload
metadata:
  name: w-sample-jobset
spec:
  controllerRef:
    name: sample-jobset
    kind: JobSet
    apiGroup: jobset.x-k8s.io
  podGroups: 
    - name: "pg-sample-jobset"
      replicas: 1
      policy:
        kind: PodGroupPolicyKindGang
        gang:
          # minCount = num ReplicatedJobs X num replicas X parallelism
          minCount: 2 X 2 X 4
```

### Implementation

#### Workload Creation

The JobSet controller `Reconcile()` loop will be updated to generate a single Workload object per JobSet if the `GangConfig` field is specified on the JobSetSpec or ReplicatedJob.

The high level idea to create pod groups within the Workload spec is:
1. If `jobSetSpec.GangConfig.GangMode == GangModeGang`, all JobSet pods are one gang:
- Create a single `PodGroup` for the JobSet.
- Set `Replicas` to 1
- Set the `minCount` in the `Gang` to `#replicated jobs * #job replicas * #pods per Job (parallelism)`. (`minCount` is the number of pods that should be ready before the gang is scheduled).
2. If `replicatedJob.GangConfig.GangMode == GangModeGang` is set on any ReplicatedJob, all pods in that ReplicatedJob are one gang:
- Create a `PodGroup` per ReplicatedJob.
- Set `Replicas` to 1
- Set the `minCount` in the `Gang` to  `#job replicas * #pods per Job (parallelism)`.
3. If `replicatedJob.GangConfig.GangMode == GangModeReplicatedGang`, all pods in each Job replica are one gang:
- Create a `PodGroup` per ReplicatedJob.
- Set `Replicas` to #job replicas
- Set the `minCount` in the `Gang` to `#pods per Job (parallelism)`.

#### Pod Workload Reference Creation

The `constructJobTemplate()` method should set the `workloadRef` field on the generated pod spec to reference the new Workload object. This field is how the `kube-scheduler` knows which pods to schedule as a gang.
- If `jobSetSpec.GangConfig.GangMode == GangModeGang`,  all pods have a `workloadRef` object reference with the same pod group name and `PodGroupReplicaIndex` is unset.
- If `replicatedJob.GangConfig.GangMode == GangModeGang`, all pods across all Job replicas for that ReplicatedJob have a `workloadRef` object reference with a different pod group name per ReplicatedJob and `PodGroupReplicaIndex` is unset.
- If `replicatedJob.GangConfig.GangMode == GangModeReplicatedGang`, all pods for a single Job replica have a `workloadRef` object reference with a different pod group name per ReplicatedJob and `PodGroupReplicaIndex` is set to the Job replica index.

#### Workload Deletion

When the JobSet is deleted, the JobSet controller will also delete the Workload it created for that JobSet.

#### Defaulting/Validation

- `jobSetSpec.GangConfig.GangMode` and `replicatedJob.GangConfig.GangMode` cannot be specified at once (except to `GangModeOff`).
- `jobSetSpec.GangConfig.GangMode` and `replicatedJob.GangConfig.GangMode` are immutable.

### Translating JobSet Workloads to Workloads

#### JobSet Level Gang

JobSet:

```
apiVersion: jobset.x-k8s.io/v1
kind: JobSet
metadata:
  name: sample-jobset
spec:
  gangConfig:
    gangMode: Gang
  replicatedJobs:
    - name: replicated-job-1
      replicas: 2
      template:
        spec:
          parallelism: 4
          completions: 4
          template:
            spec:
              containers:
                - name: initializer
                  image: busybox
                  command:
                    ...
    - name: replicated-job-2
      replicas: 2
      template:
      spec:
          parallelism: 4
          completions: 4
          template:
            spec:
              containers:
                - name: launcher
                  image: busybox
                  command:
                   ...
```

Generated Workload:

```
apiVersion: scheduling/v1alpha1  
kind: Workload
metadata:
  name: w-sample-jobset
spec:
  controllerRef:
    name: sample-jobset
    kind: JobSet
    apiGroup: jobset.x-k8s.io
  podGroups: 
    - name: "pg-sample-jobset"
      replicas: 1
      policy:
        kind: PodGroupPolicyKindGang
        gang:
          # minCount = num ReplicatedJobs X num replicas X parallelism
          minCount: 2 X 2 X 4
```

Generated WorkloadRef in Pod Spec:

```
apiVersion: v1
kind: Pod
name:
  sample-jobset-job-1-abc123
spec:
  ...
  workload:
    name: w-sample-jobset
    podGroup: pg-sample-jobset
  ...
```

#### ReplicatedJob Level Gang

JobSet:

```
apiVersion: jobset.x-k8s.io/v1
kind: JobSet
metadata:
  name: sample-jobset
spec:
    replicatedJobs:
    - name: replicated-job-1
      replicas: 2
      gangConfig:
        gangMode: Gang
      template:
        spec:
          parallelism: 4
          completions: 4
          template:
            spec:
              containers:
                - name: initializer
                  image: busybox
                  command:
                    ...
    - name: replicated-job-2
      replicas: 1
      gangConfig:
        gangMode: Gang
      dependsOn:
        - name: replicated-job-1
          status: Complete
      template:
        spec:
          parallelism: 3
          completions: 3
          template:
            spec:
              containers:
                - name: launcher
                  image: busybox
                  command:
                    ...
```

Generated Workload:

```
apiVersion: scheduling/v1alpha1  
kind: Workload
metadata:
  name: w-sample-jobset
spec:
  controllerRef:
    name: sample-jobset
    kind: JobSet
    apiGroup: jobset.x-k8s.io
  podGroups: 
    - name: "pg-replicated-job-1"
      replicas: 1
      policy:
        kind: PodGroupPolicyKindGang
        gang:
          # minCount = num replicas X parallelism for for replicated-job-1
          minCount: 2 X 4
    - name: "pg-replicated-job-2"
      replicas: 1
      policy:
        kind: PodGroupPolicyKindGang
        gang:
          # minCount = num replicas X parallelism for replicated-job-2
          minCount: 1 X 3
```
Generated WorkloadRef in Pod Spec:

For replicated-job-1’s pods:
```
apiVersion: v1
kind: Pod
name:
  sample-jobset-job-1-abc123
spec:
  ...
  workload:
    name: w-sample-jobset
    podGroup: pg-replicated-job-1
  ...
```

For replicated-job-2’s pods:
```
apiVersion: v1
kind: Pod
name:
  sample-jobset-job-5-abc123
spec:
  ...
  workload:
    name: w-sample-jobset
    podGroup: pg-replicated-job-2
  ...
```

#### Replica Level Gang

JobSet:

```
apiVersion: jobset.x-k8s.io/v1
kind: JobSet
metadata:
  name: sample-jobset
spec:
  replicatedJobs:
    - name: replicated-job-1
      replicas: 2
      gangConfig:
        gangMode: ReplicatedGang
      template:
        spec:
          parallelism: 4
          completions: 4
          template:
            spec:
              containers:
                - name: initializer
                  image: busybox
                  command:
                    ...
    - name: replicated-job-2
      replicas: 3
      gangConfig:
        gangMode: ReplicatedGang
      template:
        spec:
          parallelism: 3
          completions: 3
          template:
            spec:
              containers:
                - name: launcher
                  image: busybox
                  command:
                    ...
```

Generated Workload:

```
apiVersion: scheduling/v1alpha1  
kind: Workload
metadata:
  name: w-sample-jobset
spec:
  controllerRef:
    name: sample-jobset
    kind: JobSet
    apiGroup: jobset.x-k8s.io
  podGroups: 
    - name: "pg-replicated-job-1"
      replicas: 2
      policy:
        kind: PodGroupPolicyKindGang
        gang:
          # minCount = parallelism for replicated-job-1 replica
          minCount: 4
    - name: "pg-replicated-job-2"
      replicas: 3
      policy:
        kind: PodGroupPolicyKindGang
        gang:
          # minCount =  parallelism for replicated-job-2 replica
          minCount: 3
```

Generated WorkloadRef in Pod Spec:

For replica 1 of replicated-job-1’s pods:
```
apiVersion: v1
kind: Pod
name:
  sample-jobset-job-1-abc123
spec:
  ...
  workload:
    name: w-sample-jobset
    podGroup: pg-replicated-job-1
    podGroupReplicaIndex: 1
  ...
```

For replica 2 of replicated-job-1’s pods:
```
apiVersion: v1
kind: Pod
name:
  sample-jobset-job-1-abc456
spec:
  ...
  workload:
    name: w-sample-jobset
    podGroup: pg-replicated-job-1
    podGroupReplicaIndex: 2
  ...
```

For replica 1 of replicated-job-2’s pods:
```
apiVersion: v1
kind: Pod
name:
  sample-jobset-job-5-abc123
spec:
  ...
  workload:
    name: w-sample-jobset
    podGroup: pg-replicated-job-2
    podGroupReplicaIndex: 1
  ...
```

For replica 2 of replicated-job-2’s pods:
```
apiVersion: v1
kind: Pod
name:
  sample-jobset-job-5-abc345
spec:
  ...
  workload:
    name: w-sample-jobset
    podGroup: pg-replicated-job-2
    podGroupReplicaIndex: 2
  ...
```

For replica 3 of replicated-job-2’s pods:
```
apiVersion: v1
kind: Pod
name:
  sample-jobset-job-5-abc678
spec:
  ...
  workload:
    name: w-sample-jobset
    podGroup: pg-replicated-job-2
    podGroupReplicaIndex: 3
  ...
```

### Test Plan

[x] I/we understand the owners of the involved components may require updates to
existing tests to make this code solid enough prior to committing the changes necessary
to implement this enhancement.

##### Prerequisite testing updates

#### Unit Tests
controllers: 01/19/2024 - 30.2%
Unit tests are low in this area as we do more of our testing at the integration level.

`jobset_controller.go` and `jobset_controller_test.go` will be extended to handle the new `GangConfig` fields, set the `workloadRef` in Pod specs & generate a Workload object.

#### Integration tests
1. Gang scheduling for a single or multiple ReplicatedJobs with JobSet controller created Workload objects.
2. Gang scheduling for a single or multiple Job replicas with JobSet controller created Workload objects.
3. Gang scheduling for the entire JobSet with JobSet controller created Workload objects (when this API is supported).
4. Gang scheduling at JobSet, ReplicatedJob and Job replica levels when the JobSet is being recreated.
5. Gang scheduling for ReplicatedJobs when DependsOn or StartupPolicy is configured.

In all tests, validate Workload spec values for `controllerRef` and `podGroups` and JobSet Pod spec values for `workloadRef`. 

Additionally, manually validate the cases for user created Workload objects.

### Graduation Criteria

- `kube-scheduler` changes and `Workload` API alpha are targeted to `1.35`.
- Support for `replicatedJob.GangConfig` will be alpha in 1.35 behind an alpha feature gate and graduated to stable when `Workload` API is stable.

## Implementation History

- Drafted October 15 2025

## Drawbacks

## Alternatives

#### Multiple Workload Objects per ReplicatedJob and Replica

Instead of a single Workload object per JobSet, it's possible to have a different Workload object for each ReplicatedJob or Job replica that requires gang scheduling. The `workloadRef` for that PodGroup would point to a unique Workload. However, this design increases complexity & impacts debuggability since JobSet controller would have to manage multiple Workloads per JobSet instance.
