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
	schedulingv1beta1 "k8s.io/api/scheduling/v1beta1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
	workloadbuilder "k8s.io/component-helpers/scheduling/schedulingv1/workloadbuilder"
	"k8s.io/utils/ptr"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	jobsetutil "sigs.k8s.io/jobset/pkg/util"
)

// schedulingRootPath is the field path at which every WorkloadItem's
// scheduling building blocks are embedded in the JobSet API, used to
// generate field-path-accurate validation errors.
var schedulingRootPath = field.NewPath("spec", "scheduling")

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
		Name:      workloadName(js),
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
// ReplicatedJobScheduling's targetReplicatedJobs list.
func SchedulingGroupName(targets []string) string {
	return strings.Join(targets, "-")
}

// duplicateSchedulingGroupNameError indicates that two different scheduling
// inputs (a ReplicatedJob's own name, a replicatedJobs grouping, or a
// per-Job scheduling item) produced the same generated WorkloadItem name,
// which would compile into two identically named PodGroupTemplates.
type duplicateSchedulingGroupNameError struct {
	name string
}

func (e *duplicateSchedulingGroupNameError) Error() string {
	return fmt.Sprintf("generated scheduling group name %q is not unique: rename the colliding ReplicatedJob or replicatedJobs targetReplicatedJobs grouping so generated PodGroupTemplate names don't collide", e.name)
}

// buildTopLevelGangItem creates a single WorkloadItem for top-level gang scheduling
// where all ReplicatedJobs are ganged together in a single PodGroup.
func buildTopLevelGangItem(js *jobset.JobSet) *workloadbuilder.WorkloadItem {
	scheduling := js.Spec.Scheduling

	// mapPolicyInput propagates an explicit, user-set Gang.MinCount directly,
	// taking priority over the gangMinCountCallback fallback below. The webhook
	// never persists a derived minCount into spec (see totalMinCount), so a
	// non-zero value here is always the user's fixed quorum; an unset (0) value
	// falls through to the callback's live-recomputed totalMin, so ElasticJobSet
	// scaling keeps minCount in sync.
	policy := scheduling.SchedulingPolicy

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
		Name:          topLevelTemplateName(js),
		Path:          schedulingRootPath,
		DefaultConfig: defaultCfg,
		Input:         input,
		Callbacks: []workloadbuilder.SchedulingConfigFunc{
			gangMinCountCallback(totalMin),
		},
	}
}

// buildPerRJItem creates a WorkloadItem for a single ReplicatedJob that has no
// dedicated replicatedJobs entry, using the JobSet's global scheduling
// configuration.
func buildPerRJItem(js *jobset.JobSet, rjob *jobset.ReplicatedJob, globalScheduling *jobset.JobSetScheduling, ignoreGlobalGangMinCount bool) *workloadbuilder.WorkloadItem {
	input := globalSchedulingInput(globalScheduling, ignoreGlobalGangMinCount)

	minCount := computeMinCount(rjob)

	defaultCfg := defaultGangConfig()
	defaultCfg.PriorityClassName = rjob.Template.Spec.Template.Spec.PriorityClassName

	return &workloadbuilder.WorkloadItem{
		Name:          perRJTemplateName(js, rjob.Name),
		Path:          schedulingRootPath,
		DefaultConfig: defaultCfg,
		Input:         input,
		Callbacks: []workloadbuilder.SchedulingConfigFunc{
			gangMinCountCallback(minCount),
		},
	}
}

// buildGroupItem creates a single WorkloadItem shared by every ReplicatedJob a
// ReplicatedJobScheduling targets, so they are compiled into one PodGroup.
// members must be non-empty and contain only ReplicatedJobs that were resolved
// from leafPolicy.TargetReplicatedJobs.
func buildGroupItem(js *jobset.JobSet, members []*jobset.ReplicatedJob, leafPolicy *jobset.ReplicatedJobScheduling, globalScheduling *jobset.JobSetScheduling, ignoreGlobalGangMinCount bool) *workloadbuilder.WorkloadItem {
	groupName := SchedulingGroupName(leafPolicy.TargetReplicatedJobs)

	// A non-zero leaf Gang.MinCount is always the user's explicit quorum; the
	// webhook never persists a derived value into spec. An unset (0) value falls
	// through to the gangMinCountCallback below, which recomputes totalMin from
	// the live members so ElasticJobSet scaling keeps minCount in sync.
	leafSchedulingPolicy := leafPolicy.SchedulingPolicy

	var input workloadbuilder.WorkloadInput
	if leafSchedulingPolicy != nil {
		input.Policy = mapPolicyInput(leafSchedulingPolicy, []string{"replicatedJobs", groupName, "schedulingPolicy"})
	}
	if leafPolicy.SchedulingConstraints != nil {
		input.Constraints = mapConstraintsInput(leafPolicy.SchedulingConstraints, []string{"replicatedJobs", groupName, "schedulingConstraints"})
	}
	if leafPolicy.DisruptionMode != nil {
		input.DisruptionMode = mapDisruptionInput(leafPolicy.DisruptionMode, []string{"replicatedJobs", groupName, "disruptionMode"})
	}
	if len(leafPolicy.ResourceClaims) > 0 {
		input.ResourceClaims = mapResourceClaimsInput(leafPolicy.ResourceClaims, []string{"replicatedJobs", groupName, "resourceClaims"})
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
		Name:          groupTemplateName(js, leafPolicy.TargetReplicatedJobs),
		Path:          schedulingRootPath,
		DefaultConfig: defaultCfg,
		Input:         input,
		Callbacks: []workloadbuilder.SchedulingConfigFunc{
			gangMinCountCallback(totalMin),
		},
	}
}

// buildJobItem creates a WorkloadItem for a single Job (replica) of a
// ReplicatedJob whose leaf policy configures job, so this
// one replica is compiled into its own independent PodGroup instead of
// sharing a single PodGroup with the ReplicatedJob's other replicas (the
// Gang-of-Gangs per-Job model). Callers must only invoke this for policies
// that have already been validated to target exactly one ReplicatedJob.
func buildJobItem(js *jobset.JobSet, rjob *jobset.ReplicatedJob, jobIdx int, jobPolicy *jobset.JobScheduling, globalScheduling *jobset.JobSetScheduling) *workloadbuilder.WorkloadItem {
	itemName := perJobTemplateName(js, rjob.Name, jobIdx)

	// A non-zero Gang.MinCount is always the user's explicit quorum; the webhook
	// never persists a derived value into spec. An unset (0) value falls through
	// to the gangMinCountCallback below, which recomputes this Job's own
	// parallelism so ElasticJobSet scaling keeps minCount in sync.
	policy := jobPolicy.SchedulingPolicy

	var input workloadbuilder.WorkloadInput
	if policy != nil {
		input.Policy = mapPolicyInput(policy, []string{"replicatedJobs", rjob.Name, "job", "schedulingPolicy"})
	}
	if jobPolicy.SchedulingConstraints != nil {
		input.Constraints = mapConstraintsInput(jobPolicy.SchedulingConstraints, []string{"replicatedJobs", rjob.Name, "job", "schedulingConstraints"})
	}
	if jobPolicy.DisruptionMode != nil {
		input.DisruptionMode = mapDisruptionInput(jobPolicy.DisruptionMode, []string{"replicatedJobs", rjob.Name, "job", "disruptionMode"})
	}
	if len(jobPolicy.ResourceClaims) > 0 {
		input.ResourceClaims = mapResourceClaimsInput(jobPolicy.ResourceClaims, []string{"replicatedJobs", rjob.Name, "job", "resourceClaims"})
	}

	// Fall back to the JobSet's global scheduling settings for any building
	// block job didn't set itself, matching the leaf-level
	// fallback behavior used for grouped/per-RJ PodGroups. The global Gang
	// minCount is always ignored (regardless of sequenced startup) because it
	// represents a JobSet- or ReplicatedJob-wide total, which can never be the
	// correct minCount for a single Job's own PodGroup; each per-Job
	// PodGroup instead computes its own default from this Job's parallelism
	// via gangMinCountCallback below, unless job sets an
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
		Path:          schedulingRootPath,
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
// it creates one item per replicatedJobs group (which may span more than
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
	// (groupTemplateName), per-Job items (perJobTemplateName), and ungrouped
	// ReplicatedJobs (perRJTemplateName). Each name carries a hash over the
	// source object's kind/namespace/identity, so a human-readable prefix
	// collision (e.g. a group targeting ["leader","worker"] joins to
	// "leader-worker", matching a ReplicatedJob literally named "leader-worker")
	// no longer produces the same generated name. This guard remains as a
	// safety net so PodGroupTemplates are never silently overwritten, failing
	// loudly here rather than opaquely at Workload creation.
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
		for i := range scheduling.ReplicatedJobs {
			policy := &scheduling.ReplicatedJobs[i]
			var members []*jobset.ReplicatedJob
			for _, name := range policy.TargetReplicatedJobs {
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
			// job switches this entry to the Gang-of-Gangs
			// per-Job model: each replica (Job) of the targeted ReplicatedJob
			// compiles into its own independent WorkloadItem/PodGroup instead
			// of one PodGroup shared across every replica. Webhook validation
			// guarantees exactly one member in this case.
			if policy.Job != nil {
				rjob := members[0]
				for jobIdx := 0; jobIdx < int(rjob.Replicas); jobIdx++ {
					item := buildJobItem(js, rjob, jobIdx, policy.Job, scheduling)
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
		item := buildPerRJItem(js, rjob, scheduling, ignoreGlobalGangMinCount)
		if err := addBuilder(item); err != nil {
			return nil, err
		}
	}
	return builders, nil
}

// buildWorkloadFromBuilders compiles the Workload from the builders. Since each builder
// produces a Workload with a single PodGroupTemplate (the library's current limitation),
// we merge all templates into a single Workload when there are multiple builders.
func buildWorkloadFromBuilders(builders []*workloadbuilder.Builder) (*schedulingv1beta1.Workload, error) {
	if len(builders) == 1 {
		return builders[0].BuildWorkload()
	}

	// Multiple builders (per-RJ mode): build each and merge templates.
	var allTemplates []schedulingv1beta1.PodGroupTemplate
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
			return field.ErrorList{field.Invalid(rootPath.Child("replicatedJobs"), dupErr.name, dupErr.Error())}
		}
		return field.ErrorList{field.InternalError(rootPath, err)}
	}
	var allErrs field.ErrorList
	for _, b := range builders {
		allErrs = append(allErrs, b.Validate(ctx, workloadbuilder.ValidationInput{})...)
	}
	return allErrs
}
