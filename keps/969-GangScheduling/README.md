# KEP-969: Gang Scheduling Support for JobSet

<!-- toc -->
- [Summary](#summary)
- [Motivation](#motivation)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Proposal](#proposal)
  - [User Stories](#user-stories)
    - [Story 1: All-or-Nothing Scheduling for Distributed Training](#story-1-all-or-nothing-scheduling-for-distributed-training)
    - [Story 2: Per-ReplicatedJob Gang Scheduling](#story-2-per-replicatedjob-gang-scheduling)
    - [Story 3: Advanced Workload Template Control](#story-3-advanced-workload-template-control)
  - [Risks and Mitigations](#risks-and-mitigations)
- [Design Details](#design-details)
  - [API Proposal](#api-proposal)
  - [Implementation](#implementation)
  - [Integration with Kubernetes Workload API](#integration-with-kubernetes-workload-api)
  - [Defaulting/Validation](#defaultingvalidation)
  - [User Experience](#user-experience)
  - [Test Plan](#test-plan)
      - [Prerequisite testing updates](#prerequisite-testing-updates)
    - [Unit Tests](#unit-tests)
    - [Integration tests](#integration-tests)
  - [Graduation Criteria](#graduation-criteria)
- [Implementation History](#implementation-history)
- [Drawbacks](#drawbacks)
- [Alternatives](#alternatives)
  - [Using Annotations Instead of API Field](#using-annotations-instead-of-api-field)
  - [Hard-coding Gang Policy Behavior](#hard-coding-gang-policy-behavior)
<!-- /toc -->

## Summary

This KEP introduces gang scheduling support for JobSets through a new `gangPolicy` field in the JobSet API.
Gang scheduling ensures that groups of pods are scheduled atomically - either all pods in the gang are scheduled
together, or none are scheduled. This is critical for distributed AI/ML training workloads and HPC applications
where partial scheduling leads to resource deadlock and wasted cluster capacity.

The proposal introduces three gang scheduling modes:
- **JobSetAsGang**: The entire JobSet is treated as a single gang (all-or-nothing scheduling)
- **JobSetGangPerReplicatedJob**: Each ReplicatedJob within the JobSet is treated as a separate gang
- **JobSetWorkloadTemplate**: Advanced mode allowing users to provide custom Workload specifications for fine-grained control

## Motivation

Distributed AI/ML training workloads (PyTorch, JAX, TensorFlow) and HPC applications (MPI) typically require
all worker pods to be running before any useful work can begin. Without gang scheduling, partial pod scheduling
can lead to:

1. **Resource Deadlock**: Partially scheduled JobSets hold resources while waiting for remaining pods that may never schedule due to resource constraints
2. **Wasted Resources**: Scheduled pods consume cluster resources while waiting indefinitely for their peers
3. **Reduced Cluster Utilization**: Queue head-of-line blocking where smaller jobs cannot run because large jobs are partially scheduled
4. **Extended Training Time**: Jobs take longer to complete due to repeated failures and restarts

Current workarounds include:
- Manual coordination with cluster administrators to reserve capacity
- Pessimistic resource quotas that reduce cluster efficiency
- External batch schedulers that add operational complexity

The Kubernetes Workload API (scheduling/v1alpha1) provides a standard mechanism for gang scheduling,
but currently lacks first-class integration with JobSet. This KEP bridges that gap.

### Goals

- Provide declarative gang scheduling configuration for JobSets
- Support common gang scheduling patterns (whole JobSet, per-ReplicatedJob)
- Integrate with the Kubernetes Workload API for scheduler interoperability
- Enable advanced use cases via custom Workload template specifications
- Maintain backward compatibility with existing JobSet workloads

### Non-Goals

- Implementing a new scheduler or scheduling algorithm (we rely on existing schedulers like Kueue, Volcano, etc.)
- Supporting arbitrary DAG-based scheduling dependencies
- Replacing or competing with dedicated batch schedulers
- Gang scheduling across multiple JobSets
- Dynamic gang membership changes after JobSet creation

## Proposal

### User Stories

#### Story 1: All-or-Nothing Scheduling for Distributed Training

As a ML engineer running distributed PyTorch training with 64 GPUs across 8 nodes, I want all 64 pods to be
scheduled together or not at all. Partial scheduling wastes expensive GPU resources and blocks other jobs.

With this KEP, I can specify:

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: pytorch-training
spec:
  gangPolicy:
    gangPolicyOption: JobSetAsGang
  replicatedJobs:
  - name: master
    replicas: 1
    template:
      spec:
        completions: 1
        parallelism: 1
        template:
          spec:
            containers:
            - name: pytorch
              image: pytorch/pytorch:latest
              resources:
                limits:
                  nvidia.com/gpu: 8
  - name: worker
    replicas: 7
    template:
      spec:
        completions: 1
        parallelism: 1
        template:
          spec:
            containers:
            - name: pytorch
              image: pytorch/pytorch:latest
              resources:
                limits:
                  nvidia.com/gpu: 8
```

The scheduler will only schedule this JobSet when resources are available for all 64 GPUs simultaneously.

#### Story 2: Per-ReplicatedJob Gang Scheduling

As a HPC user running a multi-tier application with separate parameter servers and workers, I want each tier
to be gang-scheduled independently. Parameter servers can start as soon as their gang is ready, while workers
wait for their separate gang to be satisfied.

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: multi-tier-training
spec:
  gangPolicy:
    gangPolicyOption: JobSetGangPerReplicatedJob
  replicatedJobs:
  - name: ps
    replicas: 4
    template:
      spec:
        completions: 1
        parallelism: 1
        template:
          spec:
            containers:
            - name: ps
              image: tensorflow/tensorflow:latest
  - name: worker
    replicas: 16
    template:
      spec:
        completions: 1
        parallelism: 1
        template:
          spec:
            containers:
            - name: worker
              image: tensorflow/tensorflow:latest
```

Parameter server pods (4) will be gang-scheduled together, and worker pods (16) will be gang-scheduled as a separate group.

#### Story 3: Advanced Workload Template Control

As a ML engineer running distributed training, I have an initialization phase that must complete before training starts,
but I want the leader and worker pods to be gang-scheduled together as a single unit (not separately). The simple gang
policies don't support this pattern where I need custom grouping across ReplicatedJobs.

With the WorkloadTemplate option, I can define custom pod sets that group the leader and worker together while keeping
the initializer separate:

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: multi-phase-training
spec:
  gangPolicy:
    gangPolicyOption: JobSetWorkloadTemplate
    workload:
      metadata:
        labels:
          workload-type: multi-phase-training
      spec:
        podSets:
        # First gang: initializer runs independently
        - name: initializer
          count: 1
          template:
            spec:
              containers:
              - name: init
                image: my-app:latest
        # Second gang: leader and workers gang-scheduled together
        - name: training
          count: 9  # 1 leader + 8 workers
          template:
            spec:
              containers:
              - name: training
                image: my-app:latest
        priorityClassName: high-priority
  replicatedJobs:
  - name: initializer
    replicas: 1
    template:
      spec:
        completions: 1
        parallelism: 1
        template:
          spec:
            containers:
            - name: init
              image: my-app:latest
              command: ["python", "initialize.py"]
  - name: leader
    replicas: 1
    template:
      spec:
        completions: 1
        parallelism: 1
        template:
          spec:
            containers:
            - name: leader
              image: my-app:latest
              command: ["python", "train.py", "--role=leader"]
  - name: worker
    replicas: 1
    template:
      spec:
        completions: 8
        parallelism: 8
        template:
          spec:
            containers:
            - name: worker
              image: my-app:latest
              command: ["python", "train.py", "--role=worker"]
```

This creates two gangs: the initializer (1 pod) can be scheduled independently, while the training gang
(1 leader + 8 workers = 9 pods) must all be scheduled together atomically. This pattern is useful when
initialization can proceed opportunistically, but training requires all resources to be available.

### Risks and Mitigations

**Risk: Scheduler Compatibility**
Not all Kubernetes clusters have schedulers that support the Workload API (scheduling/v1alpha1).

*Mitigation*: GangPolicy is optional and defaults to disabled. Documentation will clearly state scheduler requirements.
Integration tests will validate behavior with Kueue and other common schedulers.

**Risk: Increased API Complexity**
Adding gang scheduling configuration increases the JobSet API surface area.

*Mitigation*: The API is designed with sensible defaults and progressive disclosure. Simple use cases require minimal configuration.
The `gangPolicyOption` field makes the intent clear and self-documenting.

**Risk: Workload API Evolution**
The Kubernetes Workload API is alpha (v1alpha1) and may change.

*Mitigation*: The GangPolicy field is marked as immutable and will be versioned alongside the Workload API.
We'll follow Kubernetes API deprecation policies when the Workload API evolves.

**Risk: Interaction with Existing Features**
Gang scheduling may interact unexpectedly with suspend, startup policies, and failure policies.

*Mitigation*: Comprehensive integration testing covering combinations of features. Clear documentation of interaction semantics.

## Design Details

### API Proposal

```go
type GangPolicyOptions string

const (
	// JobSetAsGang means that the entire JobSet is considered to be a gang.
	// The scheduler will admit all pods in the JobSet for scheduling together as a gang.
	JobSetAsGang GangPolicyOptions = "JobSetAsGang"

	// JobSetGangPerReplicatedJob means that each ReplicatedJob will be a separate gang.
	// Each ReplicatedJob's pods will be gang-scheduled independently.
	JobSetGangPerReplicatedJob GangPolicyOptions = "JobSetGangPerReplicatedJob"

	// JobSetWorkloadTemplate means that the JobSet will create the Workload from the provided template.
	// This is an advanced option that provides fine-grained control over pod groups
	// and scheduling logic for a JobSet.
	JobSetWorkloadTemplate GangPolicyOptions = "JobSetWorkloadTemplate"
)

type GangPolicy struct {
	// gangPolicyOption determines the gang scheduling policy for JobSet.
	// Defaults to nil (no gang scheduling).
	// +kubebuilder:validation:Enum=JobSetAsGang;JobSetGangPerReplicatedJob;JobSetWorkloadTemplate
	// +optional
	GangPolicyOption *GangPolicyOptions `json:"gangPolicyOption,omitempty"`

	// workload provides a Workload template to create on JobSet creation.
	// This field is only valid when gangPolicyOption is JobSetWorkloadTemplate.
	// When specified, the JobSet controller will create this Workload object
	// and link it to the JobSet for gang scheduling.
	// +optional
	Workload *schedulingv1alpha1.Workload `json:"workload,omitempty"`
}

// In JobSetSpec:
type JobSetSpec struct {
	// ... existing fields ...

	// gangPolicy configures gang scheduling policy for this JobSet.
	// Gang scheduling ensures that all pods (or groups of pods) are scheduled
	// atomically, preventing resource deadlock in distributed workloads.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable"
	// +optional
	GangPolicy *GangPolicy `json:"gangPolicy,omitempty"`

	// ... existing fields ...
}
```

### Implementation

The JobSet controller will implement gang scheduling as follows:

1. **Workload Creation**:
   - When a JobSet with `gangPolicy` is created, the controller creates a corresponding Workload object
   - For `JobSetAsGang`: Create a single Workload with one PodSet containing all pods
   - For `JobSetGangPerReplicatedJob`: Create a single Workload with multiple PodSets (one per ReplicatedJob)
   - For `JobSetWorkloadTemplate`: Create the user-provided Workload template

2. **Ownership and Lifecycle**:
   - The Workload is owned by the JobSet (via OwnerReference) and shares its lifecycle
   - Deleting the JobSet cascades deletion to the Workload
   - The Workload name follows the pattern: `<jobset-name>`

3. **Job Creation Coordination**:
   - Jobs are created with appropriate labels/annotations to link them to the Workload's PodSets
   - The scheduler (e.g., Kueue) uses these labels to enforce gang scheduling constraints

4. **Integration with Existing Features**:
   - **Suspend**: When JobSet is suspended, the Workload is also suspended (if supported by scheduler)
   - **Failure Policy**: Gang scheduling is respected during failure recovery and restarts
   - **StartupPolicy**: Gang admission happens before startup sequencing begins

### Integration with Kubernetes Workload API

The Workload API (k8s.io/api/scheduling/v1alpha1) provides the abstraction for gang scheduling:

```go
// Example generated Workload for JobSetAsGang mode
apiVersion: scheduling.x-k8s.io/v1alpha1
kind: Workload
metadata:
  name: pytorch-training-workload
  namespace: default
  ownerReferences:
  - apiVersion: jobset.x-k8s.io/v1alpha2
    kind: JobSet
    name: pytorch-training
    uid: ...
spec:
  podSets:
  - name: pytorch-training-all
    count: 64  # Total pods across all ReplicatedJobs
    template:
      spec:
        # Template extracted from JobSet
```

The JobSet controller will:
- Watch for Workload admission events
- Update JobSet conditions based on Workload status
- Coordinate Job creation with Workload pod set membership

### Defaulting/Validation

**Defaulting**:
- `gangPolicy` defaults to nil (no gang scheduling)
- `gangPolicyOption` has no default when `gangPolicy` is set (user must be explicit)

**Validation**:
- `gangPolicy` is immutable after creation (enforced via CEL validation rule)
- When `gangPolicyOption` is `JobSetWorkloadTemplate`, `workload` field must be provided
- When `gangPolicyOption` is `JobSetAsGang` or `JobSetGangPerReplicatedJob`, `workload` field must be nil
- Workload template validation follows the Kubernetes Workload API schema

**Webhook Validation**:
```go
// Validating webhook checks:
// 1. Workload field is only set when using JobSetWorkloadTemplate mode
// 2. PodSet counts in Workload template match ReplicatedJob specs
// 3. Workload API is available in the cluster
```

### User Experience

Events will be emitted for key gang scheduling milestones:
- `WorkloadCreated`: When the Workload object is created
- `WorkloadDeleted`: When the Workload object is created

### Test Plan

[x] I/we understand the owners of the involved components may require updates to
existing tests to make this code solid enough prior to committing the changes necessary
to implement this enhancement.

##### Prerequisite testing updates

- Add test utilities for creating mock Workload API objects
- Add test scheduler that implements basic gang scheduling semantics
- Extend existing test framework to validate Workload creation and lifecycle

#### Unit Tests

Test coverage will include:

- `pkg/controllers/jobset_controller.go`: Gang policy handling in reconciliation loop
  - Workload creation for each gang policy option
  - Workload update and deletion
  - Status synchronization between JobSet and Workload

- `pkg/webhooks/jobset_webhook.go`: Validation webhook
  - GangPolicy immutability enforcement
  - Workload template validation
  - Cross-field validation (gangPolicyOption vs workload field)

- `pkg/util/workload/`: Workload utility functions
  - Workload name generation
  - PodSet construction from ReplicatedJobs
  - Label/annotation management

Target coverage: >80% for new code

#### Integration tests

Integration tests will cover:

1. **JobSetAsGang Mode**:
   - Create JobSet with multiple ReplicatedJobs
   - Verify single Workload created with correct PodSet
   - Verify all Jobs created only after Workload admission
   - Verify JobSet deletion cascades to Workload

2. **JobSetGangPerReplicatedJob Mode**:
   - Create JobSet with multiple ReplicatedJobs
   - Verify Workload created with multiple PodSets
   - Verify Jobs for each ReplicatedJob gang-scheduled independently

3. **JobSetWorkloadTemplate Mode**:
   - Create JobSet with custom Workload template
   - Verify Workload created matches template
   - Verify Jobs respect custom PodSet definitions

4. **Feature Interactions**:
   - Gang scheduling + Suspend: Verify suspension behavior
   - Gang scheduling + Failure Policy: Verify restart creates new Workload
   - Gang scheduling + StartupPolicy: Verify gang admission precedes startup sequence

5. **Edge Cases**:
   - Orphaned Workload cleanup
   - Invalid Workload template rejection
   - Workload API not available in cluster

### Graduation Criteria

**Alpha** (v0.11.0):
- API field added with validation
- Basic implementation for JobSetAsGang and JobSetGangPerReplicatedJob modes
- Integration with Kueue scheduler
- Unit and integration test coverage
- Documentation for basic usage

**Beta** (TBD):
- Workload API is stable and present in all supported k8s versions

## Implementation History

- 2025-12-21: KEP drafted
- TBD: POC implementation on poc-gang branch
- TBD: Alpha release
- TBD: Beta release
- TBD: GA release

## Drawbacks

**Increased Complexity**: Gang scheduling adds complexity to the JobSet controller and requires understanding of the Workload API.
Users must understand gang scheduling concepts and scheduler capabilities.

**Scheduler Dependency**: This feature requires a scheduler that implements the Workload API (e.g., Kueue, Volcano).
Not all Kubernetes clusters have such schedulers installed.

**Resource Efficiency Trade-offs**: While gang scheduling prevents resource deadlock, it may reduce cluster utilization
in some scenarios by forcing jobs to wait for full resource availability rather than partial scheduling.

**Additional API Objects**: Each JobSet with gang scheduling creates an additional Workload object,
increasing API server load and etcd storage requirements.

## Alternatives

### Using Annotations Instead of API Field

We could use annotations (e.g., `jobset.x-k8s.io/gang-policy: "JobSetAsGang"`) instead of a proper API field.

**Rejected because**:
- Annotations don't have schema validation
- Poor discoverability and API ergonomics
- Doesn't follow Kubernetes API design guidelines for structured configuration
- Makes programmatic access more difficult

### Hard-coding Gang Policy Behavior

We could hard-code gang scheduling behavior without making it configurable.

**Rejected because**:
- Different workloads have different gang scheduling needs
- Removes flexibility for users with specific requirements
- Prevents integration with different schedulers that may have different capabilities
- Not all workloads require gang scheduling

