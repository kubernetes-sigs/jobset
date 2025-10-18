# KEP-572: VolumeClaimPolicies API for Stateful JobSet

<!-- toc -->

- [Summary](#summary)
- [Motivation](#motivation)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Proposal](#proposal)
  - [User Stories](#user-stories)
    - [Story 1: Distributed ML Training with Per-Worker Checkpoints](#story-1-distributed-ml-training-with-per-worker-checkpoints)
    - [Story 2: Shared model and dataset across nodes.](#story-2-shared-model-and-dataset-across-nodes)
    - [Story 3: Hybrid Storage Pattern for Complex Pipelines](#story-3-hybrid-storage-pattern-for-complex-pipelines)
    - [Story 4: HPC Workloads with Node-Local Storage](#story-4-hpc-workloads-with-node-local-storage)
  - [Risks and Mitigations](#risks-and-mitigations)
    - [Storage Resource Exhaustion](#storage-resource-exhaustion)
    - [PVC Naming Conflicts](#pvc-naming-conflicts)
    - [Storage Class Compatibility](#storage-class-compatibility)
- [Design Details](#design-details)
  - [API Details](#api-details)
  - [Implementation](#implementation)
    - [1. Controller Enhancement](#1-controller-enhancement)
    - [2. PVC Creation Logic](#2-pvc-creation-logic)
    - [3. Job Template Enhancement](#3-job-template-enhancement)
    - [4. Naming Conventions](#4-naming-conventions)
    - [5. Retention Policy Enforcement](#5-retention-policy-enforcement)
  - [Defaulting/Validation](#defaultingvalidation)
  - [Test Plan](#test-plan)
    - [Unit Tests](#unit-tests)
    - [Integration Tests](#integration-tests)
  - [Graduation Criteria](#graduation-criteria)
- [Implementation History](#implementation-history)
- [Drawbacks](#drawbacks)
  - [Complexity of Volume Management](#complexity-of-volume-management)
  - [Storage Dependencies](#storage-dependencies)
  - [Resource Management Overhead](#resource-management-overhead)
- [Alternatives](#alternatives)
  - [Alternative 1: Job-level VolumeClaimTemplates](#alternative-1-job-level-volumeclaimtemplates)
  - [Alternative 2: External Volume Management](#alternative-2-external-volume-management)
  - [Alternative 3: Pre-created PVC References](#alternative-3-pre-created-pvc-references)
  <!-- /toc -->

## Summary

This KEP proposes adding the VolumeClaimPolicies API to support stateful JobSets, enabling automated creation
and management of shared PersistentVolumeClaims (PVCs) across multiple ReplicatedJobs within a JobSet.
This enhancement bridges the gap between stateless batch processing and stateful storage requirements
for modern distributed computing workloads, particularly in AI training and HPC domains.

## Motivation

The current JobSet implementation provides no native volume management capabilities, requiring users to
manually create and manage PVCs for stateful workloads. This limitation creates significant
operational overhead and prevents efficient resource utilization for distributed workloads that
require persistent storage for checkpoints, datasets, models, or intermediate results.

### Goals

- Automate shared PVC creation for ReplicatedJobs within a single JobSet.
- Design configurable retention policy for created PVCs.

### Non-Goals

- Storage performance tuning and optimization features are not covered.
- Support per Job index volume claim templates.
- Support per-job persistent storage (similar to StatefulSet VolumeClaimTemplates). This KEP can
  be extended to support such use-cases in the future.
- Cross-JobSet volume sharing. Sharing volumes between different JobSet instances is out of scope.
- Orchestration for PersistentVolumes. JobSet users should rely on StorageClasses to provision
  and delete PVs.

## Proposal

### User Stories

#### Story 1: Shared model and dataset across nodes.

As an AI practitioner, I want to initialize a model and dataset before distributed fine-tuning,
using shared ReadWriteMany volumes.

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: trainjob-qwen2.5
spec:
  volumeClaimPolicies:
    - templates:
        - metadata:
            name: initializer
            labels:
              content-type: model
          spec:
            accessModes: ["ReadWriteMany"]
            resources:
              requests:
                storage: 50Gi
            storageClassName: nfs-storage
      retentionPolicy:
        whenDeleted: Delete
  replicatedJobs:
    - name: dataset-initializer
      replicas: 1
      template:
        spec:
          template:
            spec:
              containers:
                - name: dataset-initializer
                  image: ghcr.io/kubeflow/trainer/dataset-initializer
                  env:
                    - name: STORAGE_URI
                      value: hf://tatsu-lab/alpaca
                  volumeMounts:
                    - mountPath: /workspace
                      name: initializer
    - name: model-initializer
      replicas: 1
      template:
        spec:
          template:
            spec:
              containers:
                - name: model-initializer
                  image: ghcr.io/kubeflow/trainer/model-initializer
                  env:
                    - name: STORAGE_URI
                      value: hf://Qwen/Qwen2.5-1.5B-Instruct
                  volumeMounts:
                    - mountPath: /workspace
                      name: initializer
    - name: node
      dependsOn:
        - name: dataset-initializer
          status: Complete
        - name: model-initializer
          status: Complete
      replicas: 1
      template:
        spec:
          template:
            spec:
              containers:
                - name: node
                  image: ghcr.io/kubeflow/trainer/torchtune-trainer
                  resources:
                    limits:
                      nvidia.com/gpu: 2
                  volumeMounts:
                    - mountPath: /workspace
                      name: initializer
```

**Expected Behavior**: Creates one shared PVC `initializer-trainjob-qwen2.5` mounted to all pods
in dataset-initializer, model-initializer, and node ReplicatedJob.

#### Story 2: HPC Workloads with Node-Local Storage

As an HPC researcher, I want to utilize node-local storage for high-performance simulation
workloads while maintaining data persistence across pod restarts.

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: hpc-simulation
  namespace: hpc
spec:
  volumeClaimPolicies:
    - templates:
        - metadata:
            name: simulation-data
          spec:
            accessModes: ["ReadWriteOnce"]
            resources:
              requests:
                storage: 200Gi
            storageClassName: local-nvme
      retentionPolicy:
        whenDeleted: Retain
  replicatedJobs:
    - name: coordinator
      replicas: 1
      template:
        spec:
          template:
            spec:
              containers:
                - name: coordinator
                  image: coordinator:v1.0
    - name: compute-node
      replicas: 16
      template:
        spec:
          template:
            spec:
              containers:
                - name: simulator
                  image: hpc-simulator:v3.0
                  resources:
                    requests:
                      cpu: "8"
                      memory: "32Gi"
                    limits:
                      cpu: "16"
                      memory: "64Gi"
                  volumeMounts:
                    - name: simulation-data
                      mountPath: /simulation
```

**Expected Behavior**: Creates one shared PVC `simulation-data-hpc-simulation` mounted to all pods
in compute-node ReplicatedJob.

### Risks and Mitigations

#### Storage Resource Exhaustion

If automatically created PVCs won't get proper cleanup, it might lead to quota exhaustion and
increased costs. We mitigate this by defaulting retention policies to safe values
(`Delete` as a default) while providing clear documentation on cleanup strategies and cost
implications.

#### PVC Naming Conflicts

Naming conflicts could occur with deterministic naming patterns, but we mitigate this through
comprehensive validation and consistent naming conventions that include JobSet name,
ReplicatedJob name, replica indices, and pod indices.

Additionally, we can implement validation webhook that checks PVC naming conflicts before JobSet
creation.

#### Storage Class Compatibility

Issues may arise when templates specify unsupported access modes or features of StorageClass.
We mitigate this through clear documentation which explains that platform admins are responsible
for setting up correct StorageClasses.

## Design Details

### API Details

The API extends JobSetSpec with a new `VolumeClaimPolicies` field that contains
volume management configuration:

```go
type JobSetSpec struct {

    // VolumeClaimPolicies defines volume claim templates and lifecycle management policies.
    // Each policy targets specific ReplicatedJobs and defines templates and retention rules.
    VolumeClaimPolicies []VolumeClaimPolicy `json:"volumeClaimPolicies,omitempty"`
}

// VolumeClaimPolicy defines volume claim templates and lifecycle management for shared PVCs.
type VolumeClaimPolicy struct {
    // Templates is a list of shared PVC claims that ReplicatedJobs are allowed to reference.
    // The JobSet controller is responsible for creating shared PVCs that can be mounted by
    // multiple ReplicatedJobs. Every claim in this list must have a matching (by name)
    // volumeMount in one container in at least one ReplicatedJob template. ReplicatedJob template
    // must not have volumes with the same name as defined in this template.
    //
    // Generated PVC naming convention: <claim-name>-<jobset-name>
    //
    // Example:
    // - "model-cache-training" (shared volume across all ReplicatedJobs)
    Templates []corev1.PersistentVolumeClaim `json:"templates,omitempty"`

    // RetentionPolicy defines the retention policy for PVCs created from this policy's templates.
    RetentionPolicy *VolumeRetentionPolicy `json:"retentionPolicy,omitempty"`
}

// VolumeRetentionPolicy defines the retention policy for PVCs created by the JobSet.
type VolumeRetentionPolicy struct {
    // WhenDeleted specifies what happens to PVCs when the JobSet is deleted.
    // Defaults to "Delete".
    WhenDeleted *RetentionPolicyType `json:"whenDeleted,omitempty"`
}

// RetentionPolicyType defines the retention policy for PVCs.
type RetentionPolicyType string

const (
    // RetentionPolicyDelete indicates that PVCs should be deleted.
    RetentionPolicyDelete RetentionPolicyType = "Delete"

    // RetentionPolicyRetain indicates that PVCs should be retained.
    RetentionPolicyRetain RetentionPolicyType = "Retain"
)
```

### Implementation

The implementation consists of several key components:

#### 1. Controller Enhancement

The JobSet controller is enhanced with volume claim management capabilities:

```go
func (r *JobSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    js := &jobset.JobSet{}
    if err := r.Get(ctx, req.NamespacedName, js); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // Reconcile volume claims before jobs
    if len(js.Spec.VolumeClaimPolicies) > 0 {
        if err := r.reconcileVolumeClaimPolicies(ctx, js); err != nil {
            return ctrl.Result{}, err
        }
    }

    // Existing job reconciliation logic...
    result, err := r.reconcileJobs(ctx, js)
    if err != nil {
        return result, err
    }

    return result, nil
}

func (r *JobSetReconciler) reconcileVolumeClaimPolicies(ctx context.Context, js *jobset.JobSet) error {
    var allErrors []error

    // Process each volume claim policy
    for _, policy := range js.Spec.VolumeClaimPolicies {
        // Process each PVC template in the policy
        for _, template := range policy.Templates {
            // Create single shared PVC
            pvcName := generateSharedPVCName(template.Name, js.Name)
            if err := r.ensurePVCExists(ctx, js, pvcName, &template, &policy); err != nil {
                allErrors = append(allErrors, err)
            }
        }
    }

    return errors.Join(allErrors...)
}
```

#### 2. PVC Creation Logic

```go
func (r *JobSetReconciler) ensurePVCExists(ctx context.Context, js *jobset.JobSet, pvcName string, template *corev1.PersistentVolumeClaim, policy *jobset.VolumeClaimPolicy) error {
    // Check if PVC already exists
    existingPVC := &corev1.PersistentVolumeClaim{}
    err := r.Get(ctx, types.NamespacedName{
        Name:      pvcName,
        Namespace: js.Namespace,
    }, existingPVC)

    if err == nil {
        // PVC already exists, nothing to do
        return nil
    }

    if !apierrors.IsNotFound(err) {
        return err
    }

    // Create labels starting with template labels
    labels := make(map[string]string)
    maps.Copy(labels, template.Labels)
    labels["jobset.sigs.k8s.io/jobset-name"] = js.Name

    // Create new PVC based on template
    pvc := &corev1.PersistentVolumeClaim{
        ObjectMeta: metav1.ObjectMeta{
            Name:        pvcName,
            Namespace:   js.Namespace,
            Labels:      labels,
            Annotations: template.Annotations,
        },
        Spec: template.Spec,
    }

    // Set owner reference before creation if whenDeleted is Delete
    if policy.RetentionPolicy != nil && policy.RetentionPolicy.WhenDeleted != nil &&
       *policy.RetentionPolicy.WhenDeleted == jobset.RetentionPolicyDelete {
        pvc.OwnerReferences = []metav1.OwnerReference{
            *metav1.NewControllerRef(js, jobset.GroupVersion.WithKind("JobSet")),
        }
    }

    if err := r.Create(ctx, pvc); err != nil {
        r.Record.Eventf(js, corev1.EventTypeWarning, "FailedCreatePVC",
            "Failed to create PVC %s: %v", pvcName, err)
        return err
    }

    r.Record.Eventf(js, corev1.EventTypeNormal, "SuccessfulCreatePVC",
        "Created PVC %s", pvcName)
    return nil
}
```

#### 3. Job Template Enhancement

The `constructJob` function is enhanced to inject volume references:

```go
func constructJobWithVolumes(js *jobset.JobSet, rjob *jobset.ReplicatedJob, replicaIdx, podIdx int) *batchv1.Job {
    job := constructJob(js, rjob, replicaIdx, podIdx) // Existing logic

    if len(js.Spec.VolumeClaimPolicies) > 0 {
        addVolumes(job, js, rjob, replicaIdx, podIdx)
    }

    return job
}

func addVolumes(job *batchv1.Job, js *jobset.JobSet, rjob *jobset.ReplicatedJob, replicaIdx, podIdx int) {
    for _, policy := range js.Spec.VolumeClaimPolicies {
        for _, template := range policy.Templates {
            // Verify that the ReplicatedJob has a corresponding volumeMount
            if !hasVolumeMount(rjob, template.Name) {
                continue // Skip this template if no matching volumeMount found
            }

            // Shared volume across all ReplicatedJobs
            pvcName := generateSharedPVCName(template.Name, js.Name)

            // Add volume to job template
            volume := corev1.Volume{
                Name: template.Name,
                VolumeSource: corev1.VolumeSource{
                    PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
                        ClaimName: pvcName,
                    },
                },
            }
            job.Spec.Template.Spec.Volumes = append(job.Spec.Template.Spec.Volumes, volume)
        }
    }
}

func hasVolumeMount(rjob *jobset.ReplicatedJob, volumeName string) bool {
    // Check all containers in the ReplicatedJob template
    for _, container := range rjob.Template.Spec.Template.Spec.Containers {
        for _, volumeMount := range container.VolumeMounts {
            if volumeMount.Name == volumeName {
                return true
            }
        }
    }

    // Check all init containers
    for _, container := range rjob.Template.Spec.Template.Spec.InitContainers {
        for _, volumeMount := range container.VolumeMounts {
            if volumeMount.Name == volumeName {
                return true
            }
        }
    }

    return false
}
```

#### 4. Naming Conventions

```go
// generateSharedPVCName creates a name for shared PVCs
// Format: <claim-name>-<jobset-name>
func generateSharedPVCName(claimName, jobsetName string) string {
    return fmt.Sprintf("%s-%s", claimName, jobsetName)
}
```

#### 5. Retention Policy Enforcement

Retention policy is enforced through Kubernetes garbage collection by setting owner references on PVCs
when `whenDeleted: Delete` is configured. When the JobSet is deleted, Kubernetes automatically deletes any PVCs that have the JobSet as their owner.

```go
func (r *JobSetReconciler) ensurePVCExists(ctx context.Context, js *jobset.JobSet, pvcName string, template *corev1.PersistentVolumeClaim, policy *jobset.VolumeClaimPolicy) error {
    // ... existing code ...

    // Set owner reference if whenDeleted is Delete
    if policy.RetentionPolicy != nil && policy.RetentionPolicy.WhenDeleted != nil &&
       *policy.RetentionPolicy.WhenDeleted == jobset.RetentionPolicyDelete {
        pvc.OwnerReferences = []metav1.OwnerReference{
            *metav1.NewControllerRef(js, jobset.GroupVersion.WithKind("JobSet")),
        }
    }

    if err := r.Create(ctx, pvc); err != nil {
        r.Record.Eventf(js, corev1.EventTypeWarning, "FailedCreatePVC",
            "Failed to create PVC %s: %v", pvcName, err)
        return err
    }

    r.Record.Eventf(js, corev1.EventTypeNormal, "SuccessfulCreatePVC",
        "Created PVC %s", pvcName)
    return nil
}
```

### Defaulting/Validation

- `VolumeClaimPolicies` API is immutable.
- Each PVC template must have a corresponding `volumeMount` in at least one container.
- Template namespace must not be set.
- Template names must be unique within each policy
- Template names must be valid DNS-1123 subdomain names
- Generated PVC names must not exceed Kubernetes name length limits
- Retention policy values must be valid (`Delete` or `Retain`)
- Default value is `Delete` when not specified
- If `retentionPolicy` API is omitted, the default value for `whenDeleted` will be `Delete`.

The webhook is enhanced with comprehensive validation for VolumeClaimPolicies:

```go
func (w *JobSetWebhook) validateVolumeClaimPolicies(js *jobset.JobSet) error {
    if len(js.Spec.VolumeClaimPolicies) == 0 {
        return nil
    }

    var allErrors []error

    // Validate each volume claim policy
    for _, policy := range js.Spec.VolumeClaimPolicies {
        // Validate PVC templates
        if err := w.validatePVCTemplates(js, policy.Templates); err != nil {
            allErrors = append(allErrors, err)
        }

        // Validate retention policy
        if err := w.validateRetentionPolicy(policy.RetentionPolicy); err != nil {
            allErrors = append(allErrors, err)
        }
    }

    return errors.Join(allErrors...)
}

func (w *JobSetWebhook) validatePVCTemplates(js *jobset.JobSet, templates []corev1.PersistentVolumeClaim) error {
    var allErrors []error

    templateNames := make(map[string]bool)
    for _, template := range templates {

        // Validate template namespace
        if template.Namespace != nil {
           allErrors = append(allErrors, fmt.Errorf("namespace cannot be set for volumeClaimTemplate %v", template))
        }

        // Validate template name uniqueness within policy
        if templateNames[template.Name] {
            allErrors = append(allErrors, fmt.Errorf("duplicate template name %q", template.Name))
        }
        templateNames[template.Name] = true

        // Validate DNS-1123 subdomain name
        if errs := validation.IsDNS1123Subdomain(template.Name); len(errs) > 0 {
            allErrors = append(allErrors, fmt.Errorf("invalid template name %q: %v", template.Name, errs))
        }

        // Validate PVC name length limits
        maxNameLength := 63 // Kubernetes name limit
        estimatedNameLength := len(template.Name) + len(js.Name) + 50 // Buffer for indices and separators
        if estimatedNameLength > maxNameLength {
            allErrors = append(allErrors, fmt.Errorf("generated PVC name may exceed length limit"))
        }

        // Validate that template has corresponding volumeMount in at least one container
        if err := w.validateVolumeMountExists(js, template.Name); err != nil {
            allErrors = append(allErrors, err)
        }
    }

    return errors.Join(allErrors...)
}

func (w *JobSetWebhook) validateVolumeMountExists(js *jobset.JobSet, templateName string) error {
    // Check if any ReplicatedJob has a volumeMount with this name
    for _, rjob := range js.Spec.ReplicatedJobs {
        for _, container := range rjob.Template.Spec.Template.Spec.Containers {
            for _, vm := range container.VolumeMounts {
                if vm.Name == templateName {
                    return nil // Found matching volumeMount
                }
            }
        }
    }

    return fmt.Errorf("template %q has no corresponding volumeMount in any container", templateName)
}
```

### Test Plan

#### Unit Tests

Unit tests will be added to the following packages with target coverage:

- `pkg/controllers/volume_claim_policy_test.go`: VolumeClaimPolicy reconciliation logic.
- `pkg/webhooks/jobset_webhook_test.go`: VolumeClaimPolicy validation.
- `api/jobset/v1alpha2/jobset_types_test.go`: API type validation and defaults.

#### Integration Tests

Integration tests will be added to verify end-to-end functionality:

1. **Shared Volume Test**: Create JobSet with shared volumes, verify single PVC is mounted to
   multiple jobs.
2. **Retention Policy Test**: Verify PVC cleanup behavior with different retention policies.

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

- Draft KEP: October 1st 2025

## Drawbacks

### Complexity of Volume Management

Adding volume management increases JobSet controller complexity and surface area for bugs. It
adds additional overhead to troubleshoot volume-related issues.

### Storage Dependencies

Stateful JobSet becomes dependent on underlying storage infrastructure and provisioners

### Resource Management Overhead

Additional resource tracking and quota management complexity.

## Alternatives

### Alternative 1: Job-level VolumeClaimTemplates

Instead of managing volumes at the JobSet level, extend the Kubernetes core Job API to support
VolumeClaimTemplates, similar to how StatefulSets work.

This is a solid long-term approach, but it requires changes to Kubernetes core APIs, which implies
a longer development timeline. Furthermore, it does not solve the need for a shared PVC across all
ReplicatedJobs - an essential requirement for distributed AI workloads.

With the stateful JobSet, we aim to demonstrate user demand for this feature, which could drive
Kubernetes core changes to the Job controller if necessary.

### Alternative 2: External Volume Management

Delegate volume lifecycle management to external operators or tools (such as the Persistent Volume
Operator). In this model, JobSet metadata could include labels or annotations that signal the
external operator to create PVCs.

This option avoids changes to the JobSet API and preserves a clear separation of concerns.
However, it introduces operational overhead: platform administrators must manage PVC
lifecycles outside of the JobSet API, increasing complexity.

### Alternative 3: Pre-created PVC References

Require users to pre-create PVCs and reference them directly in JobSet specifications.
This avoids changes to the JobSet API and removes the need for new labels or annotations.

However, this approach becomes impractical as the number of replicas in ReplicatedJobs grows.
Managing unique PVC names and ensuring correct references in JobSet manifests quickly becomes
cumbersome and error-prone.
