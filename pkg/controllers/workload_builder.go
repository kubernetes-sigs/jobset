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
	"errors"
	"fmt"
	"strings"

	schedulingv1alpha3 "k8s.io/api/scheduling/v1alpha3"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
	workloadbuilder "k8s.io/component-helpers/scheduling/schedulingv1/workloadbuilder"
	"k8s.io/utils/ptr"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	jobsetutil "sigs.k8s.io/jobset/pkg/util"
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

// SchedulingGroupName deterministically names the WorkloadItem/PodGroupTemplate
// formed by grouping one or more ReplicatedJobs under a single
// ReplicatedJobSchedulingPolicy's targetReplicatedJob list.
func SchedulingGroupName(targets []string) string {
	return strings.Join(targets, "-")
}

// duplicateSchedulingGroupNameError indicates that two different scheduling
// inputs (a ReplicatedJob's own name, a replicatedJobPolicies grouping, or a
// per-Job scheduling item) produced the same generated WorkloadItem name,
// which would compile into two identically named PodGroupTemplates.
type duplicateSchedulingGroupNameError struct {
	name string
}

func (e *duplicateSchedulingGroupNameError) Error() string {
	return fmt.Sprintf("generated scheduling group name %q is not unique: rename the colliding ReplicatedJob or replicatedJobPolicies targetReplicatedJob grouping so generated PodGroupTemplate names don't collide", e.name)
}

// buildTopLevelGangItem creates a single WorkloadItem for top-level gang scheduling
// where all ReplicatedJobs are ganged together in a single PodGroup.
func buildTopLevelGangItem(js *jobset.JobSet) *workloadbuilder.WorkloadItem {
	scheduling := js.Spec.Scheduling

	// mapPolicyInput propagates a persisted, explicit Gang.MinCount directly,
	// taking priority over the gangMinCountCallback fallback below. If the
	// webhook auto-derived that persisted value (rather than the user setting
	// it), strip it here so the callback's live-recomputed totalMin is used
	// instead; otherwise ElasticJobSet scaling would never update minCount.
	policy := scheduling.SchedulingPolicy
	if isGangMinCountAutoDefaulted(js) && policy != nil && policy.Gang != nil {
		policy = policy.DeepCopy()
		policy.Gang.MinCount = 0
	}

	input := workloadbuilder.WorkloadInput{
		Policy:      mapPolicyInput(policy, []string{"schedulingPolicy"}),
		Constraints: mapConstraintsInput(scheduling.SchedulingConstraints, []string{"schedulingConstraints"}),
	}
	if scheduling.DisruptionMode != nil {
		input.DisruptionMode = mapDisruptionInput(scheduling.DisruptionMode, []string{"disruptionMode"})
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

// buildPerRJItem creates a WorkloadItem for a single ReplicatedJob that has no
// dedicated replicatedJobPolicies entry, using the JobSet's global scheduling
// configuration.
func buildPerRJItem(rjob *jobset.ReplicatedJob, globalScheduling *jobset.JobSetScheduling, ignoreGlobalGangMinCount bool) *workloadbuilder.WorkloadItem {
	input := globalSchedulingInput(globalScheduling, ignoreGlobalGangMinCount)

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

// buildGroupItem creates a single WorkloadItem shared by every ReplicatedJob a
// ReplicatedJobSchedulingPolicy targets, so they are compiled into one PodGroup.
// members must be non-empty and contain only ReplicatedJobs that were resolved
// from leafPolicy.TargetReplicatedJob.
func buildGroupItem(js *jobset.JobSet, members []*jobset.ReplicatedJob, leafPolicy *jobset.ReplicatedJobSchedulingPolicy, globalScheduling *jobset.JobSetScheduling, ignoreGlobalGangMinCount bool) *workloadbuilder.WorkloadItem {
	groupName := SchedulingGroupName(leafPolicy.TargetReplicatedJob)

	// mapPolicyInput propagates a persisted, explicit Gang.MinCount directly,
	// taking priority over the gangMinCountCallback fallback below. If the
	// webhook auto-derived that persisted value (rather than the user setting
	// it), strip it here so the callback's live-recomputed totalMin is used
	// instead; otherwise ElasticJobSet scaling would never update minCount.
	leafSchedulingPolicy := leafPolicy.SchedulingPolicy
	if isReplicatedJobGroupMinCountAutoDefaulted(js, groupName) && leafSchedulingPolicy != nil && leafSchedulingPolicy.Gang != nil {
		leafSchedulingPolicy = leafSchedulingPolicy.DeepCopy()
		leafSchedulingPolicy.Gang.MinCount = 0
	}

	var input workloadbuilder.WorkloadInput
	if leafSchedulingPolicy != nil {
		input.Policy = mapPolicyInput(leafSchedulingPolicy, []string{"replicatedJobPolicies", groupName, "schedulingPolicy"})
	}
	if leafPolicy.SchedulingConstraints != nil {
		input.Constraints = mapConstraintsInput(leafPolicy.SchedulingConstraints, []string{"replicatedJobPolicies", groupName, "schedulingConstraints"})
	}
	if leafPolicy.DisruptionMode != nil {
		input.DisruptionMode = mapDisruptionInput(leafPolicy.DisruptionMode, []string{"replicatedJobPolicies", groupName, "disruptionMode"})
	}
	if len(leafPolicy.ResourceClaims) > 0 {
		input.ResourceClaims = mapResourceClaimsInput(leafPolicy.ResourceClaims, []string{"replicatedJobPolicies", groupName, "resourceClaims"})
	}

	// If the leaf policy didn't set policy/constraints/disruption, fall back to global.
	globalInput := globalSchedulingInput(globalScheduling, ignoreGlobalGangMinCount)
	if input.Policy.PodGroupData == nil {
		input.Policy = globalInput.Policy
	}
	if input.Constraints.PodGroupData == nil {
		input.Constraints = globalInput.Constraints
	}
	if input.DisruptionMode.PodGroupData == nil {
		input.DisruptionMode = globalInput.DisruptionMode
	}

	var totalMin int32
	for _, rjob := range members {
		totalMin += computeMinCount(rjob)
	}

	defaultCfg := defaultGangConfig()
	defaultCfg.PriorityClassName = members[0].Template.Spec.Template.Spec.PriorityClassName

	return &workloadbuilder.WorkloadItem{
		Name:          groupName,
		DefaultConfig: defaultCfg,
		Input:         input,
		Callbacks: []workloadbuilder.SchedulingConfigFunc{
			gangMinCountCallback(totalMin),
		},
	}
}

// buildJobItem creates a WorkloadItem for a single Job (replica) of a
// ReplicatedJob whose leaf policy configures jobSchedulingPolicy, so this
// one replica is compiled into its own independent PodGroup instead of
// sharing a single PodGroup with the ReplicatedJob's other replicas (the
// Gang-of-Gangs per-Job model). Callers must only invoke this for policies
// that have already been validated to target exactly one ReplicatedJob.
func buildJobItem(js *jobset.JobSet, rjob *jobset.ReplicatedJob, jobIdx int, jobPolicy *jobset.JobSchedulingPolicy, globalScheduling *jobset.JobSetScheduling) *workloadbuilder.WorkloadItem {
	itemName := jobSchedulingItemName(rjob.Name, jobIdx)

	// mapPolicyInput propagates a persisted, explicit Gang.MinCount directly,
	// taking priority over the gangMinCountCallback fallback below. If the
	// webhook auto-derived that persisted value (rather than the user setting
	// it), strip it here so the callback's live-recomputed minCount (this
	// Job's own parallelism) is used instead; otherwise ElasticJobSet scaling
	// would never update minCount.
	jobSchedulingPolicy := jobPolicy.SchedulingPolicy
	if isJobMinCountAutoDefaulted(js, rjob.Name) && jobSchedulingPolicy != nil && jobSchedulingPolicy.Gang != nil {
		jobSchedulingPolicy = jobSchedulingPolicy.DeepCopy()
		jobSchedulingPolicy.Gang.MinCount = 0
	}

	var input workloadbuilder.WorkloadInput
	if jobSchedulingPolicy != nil {
		input.Policy = mapPolicyInput(jobSchedulingPolicy, []string{"replicatedJobPolicies", rjob.Name, "jobSchedulingPolicy", "schedulingPolicy"})
	}
	if jobPolicy.SchedulingConstraints != nil {
		input.Constraints = mapConstraintsInput(jobPolicy.SchedulingConstraints, []string{"replicatedJobPolicies", rjob.Name, "jobSchedulingPolicy", "schedulingConstraints"})
	}
	if jobPolicy.DisruptionMode != nil {
		input.DisruptionMode = mapDisruptionInput(jobPolicy.DisruptionMode, []string{"replicatedJobPolicies", rjob.Name, "jobSchedulingPolicy", "disruptionMode"})
	}
	if len(jobPolicy.ResourceClaims) > 0 {
		input.ResourceClaims = mapResourceClaimsInput(jobPolicy.ResourceClaims, []string{"replicatedJobPolicies", rjob.Name, "jobSchedulingPolicy", "resourceClaims"})
	}

	// Fall back to the JobSet's global scheduling settings for any building
	// block jobSchedulingPolicy didn't set itself, matching the leaf-level
	// fallback behavior used for grouped/per-RJ PodGroups. The global Gang
	// minCount is always ignored (regardless of sequenced startup) because it
	// represents a JobSet- or ReplicatedJob-wide total, which can never be the
	// correct minCount for a single Job's own PodGroup; each per-Job
	// PodGroup instead computes its own default from this Job's parallelism
	// via gangMinCountCallback below, unless jobSchedulingPolicy sets an
	// explicit minCount itself.
	globalInput := globalSchedulingInput(globalScheduling, true)
	if input.Policy.PodGroupData == nil {
		input.Policy = globalInput.Policy
	}
	if input.Constraints.PodGroupData == nil {
		input.Constraints = globalInput.Constraints
	}
	if input.DisruptionMode.PodGroupData == nil {
		input.DisruptionMode = globalInput.DisruptionMode
	}

	// Each Job's own gang is sized to its own parallelism (the pods within
	// that single replica), not the ReplicatedJob's total pod count across
	// every replica.
	minCount := jobsetutil.JobParallelism(rjob)

	defaultCfg := defaultGangConfig()
	defaultCfg.PriorityClassName = rjob.Template.Spec.Template.Spec.PriorityClassName

	return &workloadbuilder.WorkloadItem{
		Name:          itemName,
		DefaultConfig: defaultCfg,
		Input:         input,
		Callbacks: []workloadbuilder.SchedulingConfigFunc{
			gangMinCountCallback(minCount),
		},
	}
}

// globalSchedulingInput maps the JobSet-level scheduling configuration to a
// WorkloadInput usable as a fallback by ReplicatedJobs or groups that don't set
// their own leaf-level policy/constraints/disruption.
func globalSchedulingInput(globalScheduling *jobset.JobSetScheduling, ignoreGlobalGangMinCount bool) workloadbuilder.WorkloadInput {
	var input workloadbuilder.WorkloadInput
	if globalScheduling == nil {
		return input
	}
	if globalScheduling.SchedulingPolicy != nil {
		policyInput := mapPolicyInput(globalScheduling.SchedulingPolicy, []string{"schedulingPolicy"})
		// When sequenced startup is active, ignore the explicit global gang minCount
		// because each group/RJ needs its own computed minCount.
		if ignoreGlobalGangMinCount && policyInput.PodGroupData != nil &&
			policyInput.PodGroupData.Gang != nil {
			policyInput.PodGroupData.Gang.MinCount = nil
		}
		input.Policy = policyInput
	}
	if globalScheduling.SchedulingConstraints != nil {
		input.Constraints = mapConstraintsInput(globalScheduling.SchedulingConstraints, []string{"schedulingConstraints"})
	}
	if globalScheduling.DisruptionMode != nil {
		input.DisruptionMode = mapDisruptionInput(globalScheduling.DisruptionMode, []string{"disruptionMode"})
	}
	return input
}

// newBuilderForJobSet creates a workloadbuilder.Builder for the given JobSet.
// For top-level gang mode (single PodGroup), it creates one item. For per-RJ mode,
// it creates one item per replicatedJobPolicies group (which may span more than
// one ReplicatedJob) plus one item for every ReplicatedJob not covered by a group.
// Since the workloadbuilder only supports single-item trees, in per-RJ mode we
// return multiple builders.
func newBuildersForJobSet(js *jobset.JobSet) ([]*workloadbuilder.Builder, error) {
	scheduling := js.Spec.Scheduling
	sequencedStartup := HasSequencedStartup(js)
	// Sequenced startup (DependsOn or InOrder StartupPolicy) means not all pods
	// exist simultaneously, so a single inherited minCount spanning every group
	// or ReplicatedJob would be wrong; each must compute its own. Outside of
	// sequenced startup, per-RJ/leaf items that don't set their own Gang policy
	// still inherit an explicit global minCount, matching top-level gang's
	// behavior of using it as-is rather than recomputing per group.
	ignoreGlobalGangMinCount := sequencedStartup
	opts := buildOpts(js)

	if UseTopLevelGang(scheduling) && !sequencedStartup {
		item := buildTopLevelGangItem(js)
		return []*workloadbuilder.Builder{workloadbuilder.NewBuilder(item, opts)}, nil
	}

	rjobsByName := make(map[string]*jobset.ReplicatedJob, len(js.Spec.ReplicatedJobs))
	for i := range js.Spec.ReplicatedJobs {
		rjobsByName[js.Spec.ReplicatedJobs[i].Name] = &js.Spec.ReplicatedJobs[i]
	}

	var builders []*workloadbuilder.Builder
	// seenNames tracks every WorkloadItem/PodGroupTemplate name generated so
	// far. Names are derived independently for grouped policies
	// (SchedulingGroupName), per-Job items (jobSchedulingItemName), and
	// ungrouped ReplicatedJobs (the RJ's own name), so two different inputs
	// can legitimately generate the same string (e.g. a group targeting
	// ["leader","worker"] joins to "leader-worker", colliding with a
	// ReplicatedJob literally named "leader-worker"). PodGroupTemplates must
	// be uniquely named, so catch that here rather than failing opaquely at
	// Workload creation.
	seenNames := sets.New[string]()
	addBuilder := func(item *workloadbuilder.WorkloadItem) error {
		if seenNames.Has(item.Name) {
			return &duplicateSchedulingGroupNameError{name: item.Name}
		}
		seenNames.Insert(item.Name)
		builders = append(builders, workloadbuilder.NewBuilder(item, opts))
		return nil
	}

	grouped := sets.New[string]()
	if scheduling != nil {
		for i := range scheduling.ReplicatedJobPolicies {
			policy := &scheduling.ReplicatedJobPolicies[i]
			var members []*jobset.ReplicatedJob
			for _, name := range policy.TargetReplicatedJob {
				rjob, ok := rjobsByName[name]
				if !ok {
					// Invalid target name; reported separately by webhook validation.
					continue
				}
				members = append(members, rjob)
				grouped.Insert(name)
			}
			if len(members) == 0 {
				continue
			}
			// jobSchedulingPolicy switches this entry to the Gang-of-Gangs
			// per-Job model: each replica (Job) of the targeted ReplicatedJob
			// compiles into its own independent WorkloadItem/PodGroup instead
			// of one PodGroup shared across every replica. Webhook validation
			// guarantees exactly one member in this case.
			if policy.JobSchedulingPolicy != nil {
				rjob := members[0]
				for jobIdx := 0; jobIdx < int(rjob.Replicas); jobIdx++ {
					item := buildJobItem(js, rjob, jobIdx, policy.JobSchedulingPolicy, scheduling)
					if err := addBuilder(item); err != nil {
						return nil, err
					}
				}
				continue
			}
			item := buildGroupItem(js, members, policy, scheduling, ignoreGlobalGangMinCount)
			if err := addBuilder(item); err != nil {
				return nil, err
			}
		}
	}

	for i := range js.Spec.ReplicatedJobs {
		rjob := &js.Spec.ReplicatedJobs[i]
		if grouped.Has(rjob.Name) {
			continue
		}
		item := buildPerRJItem(rjob, scheduling, ignoreGlobalGangMinCount)
		if err := addBuilder(item); err != nil {
			return nil, err
		}
	}
	return builders, nil
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
	builders, err := newBuildersForJobSet(js)
	if err != nil {
		var dupErr *duplicateSchedulingGroupNameError
		if errors.As(err, &dupErr) {
			return field.ErrorList{field.Invalid(rootPath.Child("replicatedJobPolicies"), dupErr.name, dupErr.Error())}
		}
		return field.ErrorList{field.InternalError(rootPath, err)}
	}
	var allErrs field.ErrorList
	for _, b := range builders {
		allErrs = append(allErrs, b.Validate(ctx, rootPath, workloadbuilder.ValidationInput{})...)
	}
	return allErrs
}
