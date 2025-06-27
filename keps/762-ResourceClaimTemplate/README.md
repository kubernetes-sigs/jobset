# KEP-762: Support ResourceClaimTemplate

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
  - [Design Details](#design-details)
    - [API Changes](#api-changes)
    - [Behavior Changes](#behavior-changes)
  - [User Stories](#user-stories)
    - [IMEX User Story](#imex-user-story)
    - [TPU Slice User Story](#tpu-slice-user-story)
    - [Kubeflow User Story](#kubeflow-user-story)
  - [Notes/Constraints/Caveats](#notesconstraintscaveats)
  - [Risks and Mitigations](#risks-and-mitigations)
  - [Test Plan](#test-plan)
      - [Prerequisite testing updates](#prerequisite-testing-updates)
    - [Unit Tests](#unit-tests)
    - [Integration tests](#integration-tests)
  - [Graduation Criteria](#graduation-criteria)
- [Alternatives](#alternatives)
  - [Manual ResourceClaim management](#manual-resourceclaim-management)
  - [Modifying CoreJob API](#modifying-corejob-api)
  - [PodSpec Patching](#podspec-patching)
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

This KEP proposes adding a `resourceClaimTemplates` field to the JobSet CRD
(specifically within `spec.replicatedJobs[].resourceClaimTemplates`) to allow users to define
ResourceClaimTemplates that the JobSet controller will use to dynamically provision shared ResourceClaims
for each Job created by the JobSet. All Pods within a given Job will be
configured to use the same set of created ResourceClaims, enabling access
to shared resources like multi-node accelerator slices.

## Motivation

<!--
This section is for explicitly listing the motivation, goals, and non-goals of
this KEP.  Describe why the change is important and the benefits to users. The
motivation section can optionally provide links to [experience reports] to
demonstrate the interest in a KEP within the wider Kubernetes community.

[experience reports]: https://github.com/golang/go/wiki/ExperienceReports
-->

Currently, Kubernetes' Dynamic Resource Allocation (DRA) primarily focuses on ResourceClaims
that are tightly coupled to the lifecycle of individual Pods (via `ResourceClaimTemplate` in the Pod spec).
This model works well for per-pod resources like GPUs. However, some distributed workloads
require resources that are shared across all Pods within a Job, such as a multi-node accelerator slice
(e.g., IMEX channels within an NVLink domain, or multi-node TPU slices). The existing `ResourceClaimTemplate`
mechanism within the Pod spec doesn't support this shared resource allocation
pattern for Jobs within a JobSet. This enhancement will fill that gap.

### Goals

<!--
List the specific goals of the KEP. What is it trying to achieve? How will we
know that this has succeeded?
-->

* Enable `JobSet` controller to create and manage shared `ResourceClaims` (using `ResourceClaimTemplates`)
* Allow users to define `ResourceClaimTemplates` at the JobSet level.
* Enable the JobSet controller to create a `ResourceClaim` (based on the template) for each Job created.
* Allow optional targeting of specific containers within the Pod to receive access to the shared ResourceClaim.
* Manage the lifecycle of the Job-scoped ResourceClaims, ensuring they are
created when the Job starts and deleted when the Job completes or is deleted.

### Non-Goals

<!--
What is out of scope for this KEP? Listing non-goals helps to focus discussion
and make progress.
-->

* Modifying the core Kubernetes Job/ PodSpec API.
* Supporting retroactive application of new or modified `resourceClaimTemplates` to already running Jobs.

## Proposal

<!--
This is where we get down to the specifics of what the proposal actually is.
This should have enough detail that reviewers can understand exactly what
you're proposing, but should not include things like API designs or
implementation. What is the desired outcome and how do we measure success?.
The "Design Details" section below is for the real
nitty-gritty.
-->

Introduce a new field `resourceClaimTemplates` within the `JobSet.spec`. This
field will be a list of `ResourceClaimTemplate` objects, similar to how they are defined
within a Pod's spec, but at the JobSet level.


### Design Details

#### API Changes

<!--
This section should contain enough information that the specifics of your
change are understandable. This may include API specs (though not always
required) or even code snippets. If there's any ambiguity about HOW your
proposal will be implemented, this is the place to discuss them.
-->

```yaml

# Example Pod-level ResourceClaimTemplate to contrasnt with per Job
# ResourceCLaimTemplate below.
apiVersion: resource.k8s.io/v1beta1
kind: ResourceClaimTemplate
metadata:
  name: gpu
spec:
  devices:
    requests:
    - name: gpu
      deviceClassName: gpu.nvidia.com
---
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: shared-resource-jobset
spec:
  replicatedJobs:
    - name: worker-group
      replicas: 3
      resourceClaimTemplates:  # NEW: JobSet-level ResourceClaimTemplates (at ReplicatedJob level)
        - metadata:
            name: imex-channel
          spec:
            devices:
              requests:
              - name: imex-channel
                deviceClassName: imex.nvidia.com
          containers:
            - worker # Only the 'worker' container gets the 'imex-channel' claim reference
        - metadata:
            name: shared-data
          spec:
            devices:
              requests:
              - name: shared-data
                deviceClassName: shared-data-resource
      template: # This is the JobTemplateSpec
        spec: # This is the JobSpec
          parallelism: 1 # JobSpec fields
          completions: 1 # JobSpec fields
          template: # This is the PodTemplateSpec
            spec: # This is the PodSpec
              containers:
                - name: worker
                  image: your-training-image:latest
                  command: ["python", "train.py"]
                  resources:
                    requests:
                      cpu: "2"
                      memory: "4Gi"
                  resourceClaims: # Pod-level DRA
                    - name: gpu-claim
                      source:
                        resourceClaimTemplateName: pod-gpu-template
                - name: helper
                  image: helper-image:latest
                  command: ["sleep", "infinity"]
              restartPolicy: OnFailure
```
The following changes will be made to the JobSet Go types:

```go
// In JobSetSpec, inside ReplicatedJobSpec:
type ReplicatedJobSpec struct {
    // ... existing fields ...
    ResourceClaimTemplates []ResourceClaimTemplate `json:"resourceClaimTemplates,omitempty"`
}

// ResourceClaimTemplate is a local type that matches the structure of resource.k8s.io ResourceClaimTemplate,
// possibly with an additional Containers field for targeting.
type ResourceClaimTemplate struct {
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec             ResourceClaimTemplateSpec `json:"spec"`
    // Optional: restrict claim injection to specific containers
    Containers       []string                  `json:"containers,omitempty"`
}

// ResourceClaimTemplateSpec matches resource.k8s.io ResourceClaimTemplateSpec
type ResourceClaimTemplateSpec struct {
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec             ResourceClaimSpec `json:"spec"`
}

// ResourceClaimSpec matches resource.k8s.io ResourceClaimSpec
type ResourceClaimSpec struct {
    Devices *DeviceClaim `json:"devices,omitempty"`
    // ... other fields as needed ...
}
```

#### Behavior Changes
1. For each `Job` created from the `replicatedJobs`, the controller creates a `ResourceClaim` per
`resourceClaimTemplates` entry (e.g., `gpu-training-jobset-job-0-imex-channel`).

2. An `ownerReference` is added from the `ResourceClaim` to the `Job`.

3. The controller modifies the Job's Pod spec. For the imex-channel claim,
only the worker container gets a reference. For the shared-data claim, all containers get a reference.

4. `ResourceClaim`s are deleted when the `Job` completes (default policy).

### User Stories

<!--
Detail the things that people will be able to do if this KEP is implemented.
Include as much detail as possible so that people can understand the "how" of
the system. The goal here is to make this feel real for users without getting
bogged down.
-->

#### IMEX User Story

As an engineer running simulations that require an IMEX channel within an NVLink domain,
I want the JobSet to create a ResourceClaim representing that channel, and have all Pods
within the Job configured to use that channel.

#### TPU Slice User Story

As a researcher running distributed training on a multi-node TPU slice, I want
to define a JobSet that automatically provisions a TPU slice ResourceClaim
shared by all Pods of a Job, ensuring my training job has the necessary multi-node resources.

#### Kubeflow User Story

As a Kubeflow user leveraging JobSet through common interfaces like the [Kubeflow Trainer](https://www.kubeflow.org/docs/components/trainer/overview/#who-is-this-for),
I should be able to request per-job shared resources in addition to my per-pod resources in a declarative manner,
so that the platform can manage resources for complex multi-node, multi-pod jobs without manual intervention.


### Notes/Constraints/Caveats

<!--
What are the caveats to the proposal?
What are some important details that didn't come across above?
Go in to as much detail as necessary here.
This might be a good place to talk about core concepts and how they relate.
-->

* If `spec.replicatedJobs[].template.spec.resourceClaimTemplates[].spec.containers` is omitted, the controller should still create the ResourceClaim for the Job and associate it with the Pod (e.g., by adding it to pod.spec.resourceClaims), but it should not automatically add a reference to it in spec.containers[].resourceClaims for any container.

* Pod-level `resourceClaim`s with the same name as a JobSet-templated
claim will override the JobSet-level claim for that container, and a warning will be emitted. See Appendix for a webhook based approach too.

* If there is a name conflict between resource claims for different containers,
the JobSet creation will fail.

### Risks and Mitigations

<!--
What are the risks of this proposal, and how do we mitigate? Think broadly.
For example, consider both security and how this will impact the larger
Kubernetes ecosystem.

How will security be reviewed, and by whom?

How will UX be reviewed, and by whom?

Consider including folks who also work outside the SIG or subproject.
-->
* Risk: Increased complexity in the JobSet controller due to managing ResourceClaim lifecycles.
  * Mitigation: Thorough testing, clear separation of concerns within the controller code,
  and leveraging existing Kubernetes garbage collection mechanisms (via owner references from
  ResourceClaim to Job).
* Risk: User confusion regarding the interaction between JobSet-level and Pod-level ResourceClaims.
  * Mitigation: Clear documentation, warning events for overrides, and illustrative examples

### Test Plan

<!--
**Note:** *Not required until targeted at a release.*
The goal is to ensure that we don't accept enhancements with inadequate testing.

All code is expected to have adequate tests (eventually with coverage
expectations). Please adhere to the [Kubernetes testing guidelines][testing-guidelines]
when drafting this test plan.

[testing-guidelines]: https://git.k8s.io/community/contributors/devel/sig-testing/testing.md
-->

[ ] I/we understand the owners of the involved components may require updates to
existing tests to make this code solid enough prior to committing the changes necessary
to implement this enhancement.

##### Prerequisite testing updates

N/ A

<!--
Based on reviewers feedback describe what additional tests need to be added prior
implementing this enhancement to ensure the enhancements have also solid foundations.
-->

#### Unit Tests

* Test parsing of resourceClaimTemplates from JobSet spec.
* Test generation of ResourceClaim names.
* Test creation of ResourceClaim specs based on templates.
* Test deletion policy handling.


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

- `<package>`: `<date>` - `<test coverage>`

#### Integration tests

* Create a JobSet with resourceClaimTemplates and verify that the correct 
ResourceClaims are created and associated with the Jobs. Verify that Pods receive the
correct ResourceClaim references based on the containers field.
* Verify that ResourceClaims are cleaned up when Jobs complete.
* Test different deletion policies.

<!--
Describe what tests will be added to ensure proper quality of the enhancement.

After the implementation PR is merged, add the names of the tests here.
-->

### Graduation Criteria

#### Feature Gate
The feature will be controlled with a `JobSetResourceClaimTemplates` feature gate:
- **Initial**: `JobSetResourceClaimTemplates=false` (disabled by default)
- **Community Validation**: `JobSetResourceClaimTemplates=true` (enabled by default)
- **Community Usage**: Feature gate removed (always enabled)

This approach allows for community-driven development and validation while maintaining backward compatibility during the initial phase.

## Alternatives

<!--
What other approaches did you consider, and why did you rule them out? These do
not need to be as detailed as the proposal, but should include enough
information to express the idea and why it was not acceptable.
-->

### Manual ResourceClaim management

Users could manually create the shared ResourceClaim and then reference it in their
Pod specs. This requires manual cleanup, and doesn't integrate well with the
JobSet's lifecycle management.

### Modifying CoreJob API

We could add a similar feature to the core Kubernetes Job API. However, this has a
much higher barrier to entry, wider impact, and longer development cycle compared to
enhancing an out-of-tree controller like JobSet.

### PodSpec Patching

Instead of the `JobSet` controller injecting resourceClaims entries into the Pod
spec based on `resourceClaimTemplates[].spec.containers`, we can consider an alternative approach.

```yaml
# Pod Template Spec Example
containers:
  - name: worker
    resourceClaims:
      - name: shared-claim-ref # Name for the reference within the container
        source:
          # Uses the name from JobSet's resourceClaimTemplates.metadata.name
          resourceClaimName: imex-channel # Placeholder
```

1. Users would define a `resourceClaim`s entry with the container spec in the `Job`'s
Pod template. However, the `source.resourceClaimName` field would contain the name of the JobSet-level `ResourceClaimTemplate` as a **placeholder**.

2. When creating the Job's Pods, the JobSet controller would first create the
actual `ResourceClaim` (e.g., my-jobset-job-0-imex-channel).

3. Then, the controller would patch the Pod spec before creation, replacing the
placeholder name (`imex-channel`) in `source.resourceClaimName` with the actual
generated `ResourceClaim` name (`my-jobset-job-0-imex-channel`).

Potential Advantages: This might feel more declarative within the Pod spec, as the
user explicitly links the container to the template name. It could also remove the
need for the `resourceClaimTemplates.spec.containers` field, as the targeting is implicit in where the user places the placeholder reference.

---

## Appendix: Webhook Validation for ResourceClaim Name Collisions

As an additional safeguard, a validation webhook can be implemented to prevent name collisions between JobSet-level `resourceClaimTemplates` and Pod-level `resourceClaims` within the JobSet's Pod template. This webhook would *WARN* (or reject) JobSets where a Pod template defines a `resourceClaim` with the same name as a JobSet-level `resourceClaimTemplate`, ensuring users receive immediate feedback and resolve the conflict.

This approach will be implemented as an add-on to the core feature. While it provides a stronger guarantee of correctness and a better user experience, it introduces some potential pitfalls:

* **Operational complexity:** The webhook adds logic and maintenance overhead to the JobSet admission path.
* **Availability:** If the webhook is unavailable (e.g., during upgrades), JobSet creation and updates may be blocked.
* **Strictness:** Overly strict validation could block valid use cases if not carefully designed.
