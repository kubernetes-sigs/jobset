/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"

	schedulingv1alpha3 "k8s.io/api/scheduling/v1alpha3"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	workloadbuilder "k8s.io/component-helpers/scheduling/schedulingv1/workloadbuilder"
	"k8s.io/utils/ptr"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// jobSetOwnerRef returns a non-controller OwnerReference for the JobSet,
// suitable for use with the workloadbuilder. The actual controller reference
// is set via ctrl.SetControllerReference in the reconciler.
func jobSetOwnerRef(js *jobset.JobSet) *metav1.OwnerReference {
	return &metav1.OwnerReference{
		APIVersion: jobset.GroupVersion.String(),
		Kind:       "JobSet",
		Name:       js.Name,
		UID:        js.UID,
	}
}

// buildOpts returns the BuildOptions common to all builder invocations for a JobSet.
func buildOpts(js *jobset.JobSet) workloadbuilder.BuildOptions {
	return workloadbuilder.BuildOptions{
		Name:      js.Name,
		Namespace: js.Namespace,
		Owner:     jobSetOwnerRef(js),
		AllowedPolicies: []workloadbuilder.SchedulingPolicyOption{
			workloadbuilder.BasicPolicy,
			workloadbuilder.GangPolicy,
		},
		AllowedDisruptionModes: []workloadbuilder.DisruptionModeOption{
			workloadbuilder.SingleMode,
			workloadbuilder.AllMode,
		},
		// JobSet is an out-of-tree controller, so we let the workloadbuilder
		// run declarative validation on the building blocks for us.
		DisableDeclarativeValidation: false,
	}
}

// mapPolicyInput maps the JobSet's scheduling policy to the workloadbuilder PolicyInput.
func mapPolicyInput(policy *schedulingv1alpha3.PodGroupSchedulingPolicy, pathElements []string) workloadbuilder.PolicyInput {
	if policy == nil {
		return workloadbuilder.PolicyInput{}
	}
	wlPolicy := &schedulingv1alpha3.WorkloadPodGroupSchedulingPolicy{}
	if policy.Basic != nil {
		wlPolicy.Basic = &schedulingv1alpha3.WorkloadPodGroupBasicSchedulingPolicy{}
	}
	if policy.Gang != nil {
		wlPolicy.Gang = &schedulingv1alpha3.WorkloadPodGroupGangSchedulingPolicy{}
		if policy.Gang.MinCount > 0 {
			wlPolicy.Gang.MinCount = ptr.To(policy.Gang.MinCount)
		}
	}
	return workloadbuilder.PolicyInput{
		PodGroupData: wlPolicy,
		PathElements: pathElements,
	}
}

// mapConstraintsInput maps PodGroupSchedulingConstraints to the workloadbuilder ConstraintsInput.
func mapConstraintsInput(constraints *schedulingv1alpha3.PodGroupSchedulingConstraints, pathElements []string) workloadbuilder.ConstraintsInput {
	if constraints == nil {
		return workloadbuilder.ConstraintsInput{}
	}
	return workloadbuilder.ConstraintsInput{
		PodGroupData: &schedulingv1alpha3.WorkloadPodGroupSchedulingConstraints{
			Topology: constraints.Topology,
		},
		PathElements: pathElements,
	}
}

// mapDisruptionInput maps a DisruptionMode to the workloadbuilder DisruptionModeInput.
func mapDisruptionInput(disruption *schedulingv1alpha3.DisruptionMode, pathElements []string) workloadbuilder.DisruptionModeInput {
	if disruption == nil {
		return workloadbuilder.DisruptionModeInput{}
	}
	wlDisruption := &schedulingv1alpha3.WorkloadPodGroupDisruptionMode{}
	if disruption.Single != nil {
		wlDisruption.Single = &schedulingv1alpha3.WorkloadPodGroupSingleDisruptionMode{}
	}
	if disruption.All != nil {
		wlDisruption.All = &schedulingv1alpha3.WorkloadPodGroupAllDisruptionMode{}
	}
	return workloadbuilder.DisruptionModeInput{
		PodGroupData: wlDisruption,
		PathElements: pathElements,
	}
}

// mapResourceClaimsInput maps PodGroupResourceClaim slice to the workloadbuilder ResourceClaimsInput.
func mapResourceClaimsInput(claims []schedulingv1alpha3.PodGroupResourceClaim, pathElements []string) workloadbuilder.ResourceClaimsInput {
	if len(claims) == 0 {
		return workloadbuilder.ResourceClaimsInput{}
	}
	wlClaims := make([]schedulingv1alpha3.WorkloadPodGroupResourceClaim, len(claims))
	for i := range claims {
		wlClaims[i] = schedulingv1alpha3.WorkloadPodGroupResourceClaim{
			Name:                      claims[i].Name,
			ResourceClaimName:         claims[i].ResourceClaimName,
			ResourceClaimTemplateName: claims[i].ResourceClaimTemplateName,
		}
	}
	return workloadbuilder.ResourceClaimsInput{
		PodGroupData: wlClaims,
		PathElements: pathElements,
	}
}

// gangMinCountCallback returns a SchedulingConfigFunc that defaults the gang
// minCount to the given value when the user left it unset. This is used to
// default minCount to parallelism * replicas for a ReplicatedJob.
func gangMinCountCallback(minCount int32) workloadbuilder.SchedulingConfigFunc {
	return func(cfg *workloadbuilder.SchedulingConfig) {
		if cfg.Policy != nil && cfg.Policy.Gang != nil && cfg.Policy.Gang.MinCount == nil {
			cfg.Policy.Gang.MinCount = ptr.To(minCount)
		}
	}
}

// defaultGangConfig returns a SchedulingConfig that defaults to Gang scheduling.
func defaultGangConfig() *workloadbuilder.SchedulingConfig {
	return &workloadbuilder.SchedulingConfig{
		Policy: &workloadbuilder.SchedulingPolicy{
			Gang: &workloadbuilder.GangSchedulingPolicy{},
		},
	}
}

// buildTopLevelGangItem creates a single WorkloadItem for top-level gang scheduling
// where all ReplicatedJobs are ganged together in a single PodGroup.
func buildTopLevelGangItem(js *jobset.JobSet) *workloadbuilder.WorkloadItem {
	scheduling := js.Spec.Scheduling

	input := workloadbuilder.WorkloadInput{
		Policy:      mapPolicyInput(scheduling.Policy, []string{"policy"}),
		Constraints: mapConstraintsInput(scheduling.Constraints, []string{"constraints"}),
	}
	if scheduling.Disruption != nil {
		input.DisruptionMode = mapDisruptionInput(scheduling.Disruption, []string{"disruption"})
	}

	// For top-level gang, compute total minCount across all ReplicatedJobs.
	totalMin := totalMinCount(js)

	// Propagate priorityClassName from the first ReplicatedJob.
	defaultCfg := defaultGangConfig()
	if len(js.Spec.ReplicatedJobs) > 0 {
		defaultCfg.PriorityClassName = js.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec.PriorityClassName
	}

	return &workloadbuilder.WorkloadItem{
		Name:          js.Name,
		DefaultConfig: defaultCfg,
		Input:         input,
		Callbacks: []workloadbuilder.SchedulingConfigFunc{
			gangMinCountCallback(totalMin),
		},
	}
}

// buildPerRJItem creates a WorkloadItem for a single ReplicatedJob.
func buildPerRJItem(rjob *jobset.ReplicatedJob, leafPolicy *jobset.ReplicatedJobSchedulingPolicy, globalScheduling *jobset.JobSetScheduling, ignoreGlobalGangMinCount bool) *workloadbuilder.WorkloadItem {
	var input workloadbuilder.WorkloadInput

	// Determine which policy/constraints/disruption to use (leaf > global).
	if leafPolicy != nil {
		if leafPolicy.Policy != nil {
			input.Policy = mapPolicyInput(leafPolicy.Policy, []string{"replicatedJobPolicies", rjob.Name, "policy"})
		}
		if leafPolicy.Constraints != nil {
			input.Constraints = mapConstraintsInput(leafPolicy.Constraints, []string{"replicatedJobPolicies", rjob.Name, "constraints"})
		}
		if leafPolicy.Disruption != nil {
			input.DisruptionMode = mapDisruptionInput(leafPolicy.Disruption, []string{"replicatedJobPolicies", rjob.Name, "disruption"})
		}
		if len(leafPolicy.ResourceClaims) > 0 {
			input.ResourceClaims = mapResourceClaimsInput(leafPolicy.ResourceClaims, []string{"replicatedJobPolicies", rjob.Name, "resourceClaims"})
		}
	}

	// If leaf didn't set policy/constraints/disruption, use global.
	if input.Policy.PodGroupData == nil && globalScheduling != nil && globalScheduling.Policy != nil {
		policyInput := mapPolicyInput(globalScheduling.Policy, []string{"policy"})
		// When sequenced startup is active, ignore the explicit global gang minCount
		// because each RJ needs its own computed minCount.
		if ignoreGlobalGangMinCount && policyInput.PodGroupData != nil &&
			policyInput.PodGroupData.Gang != nil {
			policyInput.PodGroupData.Gang.MinCount = nil
		}
		input.Policy = policyInput
	}
	if input.Constraints.PodGroupData == nil && globalScheduling != nil && globalScheduling.Constraints != nil {
		input.Constraints = mapConstraintsInput(globalScheduling.Constraints, []string{"constraints"})
	}
	if input.DisruptionMode.PodGroupData == nil && globalScheduling != nil && globalScheduling.Disruption != nil {
		input.DisruptionMode = mapDisruptionInput(globalScheduling.Disruption, []string{"disruption"})
	}

	minCount := computeMinCount(rjob)

	defaultCfg := defaultGangConfig()
	defaultCfg.PriorityClassName = rjob.Template.Spec.Template.Spec.PriorityClassName

	return &workloadbuilder.WorkloadItem{
		Name:          rjob.Name,
		DefaultConfig: defaultCfg,
		Input:         input,
		Callbacks: []workloadbuilder.SchedulingConfigFunc{
			gangMinCountCallback(minCount),
		},
	}
}

// newBuilderForJobSet creates a workloadbuilder.Builder for the given JobSet.
// For top-level gang mode (single PodGroup), it creates one item. For per-RJ mode,
// it creates one item per ReplicatedJob. Since the workloadbuilder only supports
// single-item trees, in per-RJ mode we return multiple builders.
func newBuildersForJobSet(js *jobset.JobSet) []*workloadbuilder.Builder {
	scheduling := js.Spec.Scheduling
	sequencedStartup := HasSequencedStartup(js)
	opts := buildOpts(js)

	if UseTopLevelGang(scheduling) && !sequencedStartup {
		item := buildTopLevelGangItem(js)
		return []*workloadbuilder.Builder{workloadbuilder.NewBuilder(item, opts)}
	}

	// Per-ReplicatedJob mode: one builder per RJ.
	leafPolicies := make(map[string]*jobset.ReplicatedJobSchedulingPolicy)
	if scheduling != nil {
		for i := range scheduling.ReplicatedJobPolicies {
			p := &scheduling.ReplicatedJobPolicies[i]
			leafPolicies[p.TargetReplicatedJob] = p
		}
	}

	var builders []*workloadbuilder.Builder
	for i := range js.Spec.ReplicatedJobs {
		rjob := &js.Spec.ReplicatedJobs[i]
		item := buildPerRJItem(rjob, leafPolicies[rjob.Name], scheduling, !UseTopLevelGang(scheduling) || sequencedStartup)
		builders = append(builders, workloadbuilder.NewBuilder(item, opts))
	}
	return builders
}

// buildWorkloadFromBuilders compiles the Workload from the builders. Since each builder
// produces a Workload with a single PodGroupTemplate (the library's current limitation),
// we merge all templates into a single Workload when there are multiple builders.
func buildWorkloadFromBuilders(builders []*workloadbuilder.Builder) (*schedulingv1alpha3.Workload, error) {
	if len(builders) == 1 {
		return builders[0].BuildWorkload()
	}

	// Multiple builders (per-RJ mode): build each and merge templates.
	var allTemplates []schedulingv1alpha3.PodGroupTemplate
	base, err := builders[0].BuildWorkload()
	if err != nil {
		return nil, err
	}
	for i, b := range builders {
		wl, err := b.BuildWorkload()
		if err != nil {
			return nil, err
		}
		if i > 0 && !apiequality.Semantic.DeepEqual(wl.Spec.ControllerRef, base.Spec.ControllerRef) {
			return nil, fmt.Errorf("cannot merge Workloads with different controller references")
		}
		allTemplates = append(allTemplates, wl.Spec.PodGroupTemplates...)
	}

	// The builders intentionally differ only in their PodGroupTemplates. Keep
	// the common Workload-level fields from the first builder and fail loudly if
	// that assumption stops being true for controller references.
	base.Spec.PodGroupTemplates = allTemplates
	return base, nil
}

// ValidateSchedulingWithBuilder validates the scheduling configuration of a JobSet
// using the workloadbuilder's Validate method. This runs declarative validation on
// the building blocks and the complex cross-field controller-policy checks.
func ValidateSchedulingWithBuilder(ctx context.Context, js *jobset.JobSet, rootPath *field.Path) field.ErrorList {
	builders := newBuildersForJobSet(js)
	var allErrs field.ErrorList
	for _, b := range builders {
		allErrs = append(allErrs, b.Validate(ctx, rootPath, workloadbuilder.ValidationInput{})...)
	}
	return allErrs
}
