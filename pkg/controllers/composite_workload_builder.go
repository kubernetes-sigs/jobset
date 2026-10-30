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
	schedulingv1beta1 "k8s.io/api/scheduling/v1beta1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	workloadbuilder "k8s.io/component-helpers/scheduling/schedulingv1/workloadbuilder"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	jobsetutil "sigs.k8s.io/jobset/pkg/util"
)

// buildCompositeWorkload compiles a JobSet's scheduling configuration into a
// Workload whose PodGroupTemplates are arranged as a CompositePodGroup (gang of
// gangs) tree, as described in the Gang of Gangs Support section of KEP-969.
//
// It is only used when the JobSetGangOfGangs feature gate is enabled and the
// JobSet combines a top-level policy with per-ReplicatedJob entries. The flat
// single-PodGroup-per-template path (buildWorkload) is unchanged and still
// handles the gate-off models.
//
// The tree is:
//
//	root CompositePodGroup           (top-level policy)
//	  child CompositePodGroup        (one per replicatedJobs entry)
//	    leaf PodGroup(s)             (one shared, or one per Job when job is set)
//	  leaf PodGroup                  (untargeted ReplicatedJob, falls back to root)
func buildCompositeWorkload(js *jobset.JobSet) (*schedulingv1beta1.Workload, error) {
	root := newCompositeRootItem(js)
	return workloadbuilder.NewBuilder(root, buildOpts(js)).BuildWorkload()
}

// validateCompositeWorkload runs the workloadbuilder's declarative and
// cross-field validation over the composite tree, plus the per-node template cap
// (see checkCompositeFanout) that the structural schema would otherwise only
// catch at Workload creation time.
func validateCompositeWorkload(ctx context.Context, js *jobset.JobSet, rootPath *field.Path) field.ErrorList {
	root := newCompositeRootItem(js)
	// The upstream Workload API caps both podGroupTemplates and
	// compositePodGroupTemplates at WorkloadMaxPodGroupTemplates per node (not a
	// single total across the tree). Reject here rather than let the apiserver
	// fail the Workload create as Invalid on every reconcile. The common reachable
	// case is an entry with job set on a ReplicatedJob whose replicas exceed the
	// cap, producing too many per-Job leaves under one child.
	if err := checkCompositeFanout(root, rootPath.Child("replicatedJobs")); err != nil {
		return field.ErrorList{err}
	}
	return workloadbuilder.NewBuilder(root, buildOpts(js)).Validate(ctx, workloadbuilder.ValidationInput{})
}

// checkCompositeFanout verifies that no node in the tree exceeds the per-list
// maxItems cap: at most WorkloadMaxPodGroupTemplates composite children and at
// most WorkloadMaxPodGroupTemplates leaf children.
func checkCompositeFanout(item *workloadbuilder.WorkloadItem, path *field.Path) *field.Error {
	var composites, leaves int
	for _, c := range item.Children {
		if len(c.Children) > 0 {
			composites++
		} else {
			leaves++
		}
	}
	if composites > schedulingv1beta1.WorkloadMaxPodGroupTemplates {
		return field.TooMany(path, composites, schedulingv1beta1.WorkloadMaxPodGroupTemplates)
	}
	if leaves > schedulingv1beta1.WorkloadMaxPodGroupTemplates {
		return field.TooMany(path, leaves, schedulingv1beta1.WorkloadMaxPodGroupTemplates)
	}
	for _, c := range item.Children {
		if err := checkCompositeFanout(c, path); err != nil {
			return err
		}
	}
	return nil
}

// newCompositeRootItem builds the root composite WorkloadItem from the top-level
// policy, with one child composite per replicatedJobs entry and one leaf per
// untargeted ReplicatedJob attached directly to the root.
func newCompositeRootItem(js *jobset.JobSet) *workloadbuilder.WorkloadItem {
	scheduling := js.Spec.Scheduling

	rjobsByName := make(map[string]*jobset.ReplicatedJob, len(js.Spec.ReplicatedJobs))
	for i := range js.Spec.ReplicatedJobs {
		rjobsByName[js.Spec.ReplicatedJobs[i].Name] = &js.Spec.ReplicatedJobs[i]
	}

	var children []*workloadbuilder.WorkloadItem
	grouped := make(map[string]bool, len(js.Spec.ReplicatedJobs))

	for i := range scheduling.ReplicatedJobs {
		entry := &scheduling.ReplicatedJobs[i]
		var members []*jobset.ReplicatedJob
		for _, name := range entry.TargetReplicatedJobs {
			rjob, ok := rjobsByName[name]
			if !ok {
				// Invalid target name; reported separately by webhook validation.
				continue
			}
			members = append(members, rjob)
			grouped[name] = true
		}
		if len(members) == 0 {
			continue
		}
		children = append(children, newCompositeChildItem(js, members, entry))
	}

	// Untargeted ReplicatedJobs attach their leaf PodGroup directly to the root
	// and schedule under the top-level policy.
	for i := range js.Spec.ReplicatedJobs {
		rjob := &js.Spec.ReplicatedJobs[i]
		if grouped[rjob.Name] {
			continue
		}
		children = append(children, newLeafItem(perRJTemplateName(js, rjob.Name), computeMinCount(rjob), rjob.Template.Spec.Template.Spec.PriorityClassName, workloadbuilder.WorkloadInput{}))
	}

	return &workloadbuilder.WorkloadItem{
		Name:          compositeNodeName(topLevelTemplateName(js)),
		Path:          schedulingRootPath,
		DefaultConfig: defaultCompositeConfig(),
		Input:         compositeNodeInput(scheduling.SchedulingPolicy, scheduling.SchedulingConstraints, scheduling.DisruptionMode, nil),
		Callbacks:     []workloadbuilder.SchedulingConfigFunc{gangMinCountCallback(int32(len(children)))},
		Children:      children,
	}
}

// compositeNameSuffix marks a CompositePodGroupTemplate name as an internal
// composite node, distinct from the shared leaf PodGroupTemplate beneath it.
const compositeNameSuffix = "-cpg"

// compositeNodeName derives a CompositePodGroupTemplate name that is distinct
// from any leaf PodGroupTemplate name. Leaf names stay Job-facing (child Jobs
// map to them via the scheduling group annotation); composite nodes are internal
// grouping objects, so they carry a "-cpg" suffix to avoid colliding with the
// shared leaf beneath them.
func compositeNodeName(base string) string {
	name := base + compositeNameSuffix
	if len(name) <= maxPodGroupNameLength {
		return name
	}
	// CompositePodGroupTemplate.Name must be a DNS label (<= 63 chars). base already
	// carries a source-identity hash and may itself be at the limit, so appending the
	// suffix can overflow. Re-derive a bounded, deterministic name: truncate the
	// human-readable prefix and keep a fresh hash plus the -cpg marker.
	hash := sha1Hash(base)[:podGroupNameHashLength]
	prefixLen := maxPodGroupNameLength - len(compositeNameSuffix) - 1 - podGroupNameHashLength
	prefix := base
	if len(prefix) > prefixLen {
		prefix = prefix[:prefixLen]
	}
	return fmt.Sprintf("%s-%s%s", prefix, hash, compositeNameSuffix)
}

// newCompositeChildItem builds a child composite WorkloadItem for a single
// replicatedJobs entry. Its children are either one leaf per Job replica (when
// job is set) or a single shared leaf across the targeted ReplicatedJobs.
func newCompositeChildItem(js *jobset.JobSet, members []*jobset.ReplicatedJob, entry *jobset.ReplicatedJobScheduling) *workloadbuilder.WorkloadItem {
	var children []*workloadbuilder.WorkloadItem

	if entry.Job != nil {
		// Per-Job model: webhook guarantees exactly one member.
		rjob := members[0]
		for jobIdx := 0; jobIdx < int(rjob.Replicas); jobIdx++ {
			children = append(children, newLeafItem(
				perJobTemplateName(js, rjob.Name, jobIdx),
				jobsetutil.JobParallelism(rjob),
				rjob.Template.Spec.Template.Spec.PriorityClassName,
				jobLeafInput(entry.Job),
			))
		}
	} else {
		children = append(children, newLeafItem(
			groupTemplateName(js, entry.TargetReplicatedJobs),
			sharedGroupMinCount(members),
			members[0].Template.Spec.Template.Spec.PriorityClassName,
			workloadbuilder.WorkloadInput{},
		))
	}

	return &workloadbuilder.WorkloadItem{
		Name:          compositeNodeName(groupTemplateName(js, entry.TargetReplicatedJobs)),
		Path:          schedulingRootPath,
		DefaultConfig: defaultCompositeConfig(),
		Input:         compositeNodeInput(entry.SchedulingPolicy, entry.SchedulingConstraints, entry.DisruptionMode, nil),
		Callbacks:     []workloadbuilder.SchedulingConfigFunc{gangMinCountCallback(int32(len(children)))},
		Children:      children,
	}
}

// newLeafItem builds a leaf WorkloadItem (compiled into a single PodGroupTemplate)
// with a gang minCount default of its represented pod count.
func newLeafItem(name string, minCount int32, priorityClassName string, input workloadbuilder.WorkloadInput) *workloadbuilder.WorkloadItem {
	cfg := defaultGangConfig()
	cfg.PriorityClassName = priorityClassName
	return &workloadbuilder.WorkloadItem{
		Name:          name,
		Path:          schedulingRootPath,
		DefaultConfig: cfg,
		Input:         input,
		Callbacks:     []workloadbuilder.SchedulingConfigFunc{gangMinCountCallback(minCount)},
	}
}

// compositeNodeInput maps a composite node's building blocks to a WorkloadInput
// that sets CompositePodGroupData (not the leaf PodGroupData).
func compositeNodeInput(
	policy *schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy,
	constraints *schedulingv1alpha3.WorkloadCompositePodGroupSchedulingConstraints,
	disruption *schedulingv1alpha3.WorkloadCompositePodGroupDisruptionMode,
	_ any,
) workloadbuilder.WorkloadInput {
	var input workloadbuilder.WorkloadInput
	if policy != nil {
		input.Policy = workloadbuilder.PolicyInput{
			CompositePodGroupData: policy.DeepCopy(),
			PathElements:          []string{"schedulingPolicy"},
		}
	}
	if constraints != nil {
		input.Constraints = workloadbuilder.ConstraintsInput{
			CompositePodGroupData: constraints.DeepCopy(),
			PathElements:          []string{"schedulingConstraints"},
		}
	}
	if disruption != nil {
		input.DisruptionMode = workloadbuilder.DisruptionModeInput{
			CompositePodGroupData: disruption.DeepCopy(),
			PathElements:          []string{"disruptionMode"},
		}
	}
	return input
}

// jobLeafInput maps a JobScheduling (per-Job leaf) to a WorkloadInput.
func jobLeafInput(job *jobset.JobScheduling) workloadbuilder.WorkloadInput {
	return workloadbuilder.WorkloadInput{
		Policy:         mapPodPolicyInput(job.SchedulingPolicy, []string{"job", "schedulingPolicy"}),
		Constraints:    mapPodConstraintsInput(job.SchedulingConstraints, []string{"job", "schedulingConstraints"}),
		DisruptionMode: mapPodDisruptionInput(job.DisruptionMode, []string{"job", "disruptionMode"}),
		ResourceClaims: mapResourceClaimsInput(job.ResourceClaims, []string{"job", "resourceClaims"}),
	}
}

// defaultCompositeConfig defaults a composite node to Gang. The gang's
// minGroupCount (carried in the IR as Gang.MinCount) is defaulted by a callback
// to the node's child count when the user left it unset.
func defaultCompositeConfig() *workloadbuilder.SchedulingConfig {
	return &workloadbuilder.SchedulingConfig{
		Policy: &workloadbuilder.SchedulingPolicy{
			Gang: &workloadbuilder.GangSchedulingPolicy{},
		},
	}
}
