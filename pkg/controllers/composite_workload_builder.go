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
	"strconv"

	schedulingv1alpha3 "k8s.io/api/scheduling/v1alpha3"
	schedulingv1beta1 "k8s.io/api/scheduling/v1beta1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	workloadbuilder "k8s.io/component-helpers/scheduling/schedulingv1/workloadbuilder"
	"k8s.io/utils/ptr"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	jobsetutil "sigs.k8s.io/jobset/pkg/util"
)

// buildCompositeWorkload compiles a JobSet's scheduling configuration into a
// Workload whose templates form the uniform CompositePodGroup (gang of gangs)
// tree described in the Gang of Gangs Support section of KEP-969.
//
// It is used whenever the JobSetGangOfGangs feature gate is enabled and
// spec.scheduling is set, regardless of which levels the user configured. The
// flat single-PodGroup-per-template path (buildWorkload) is unchanged and still
// handles the gate-off models.
//
// The tree is:
//
//	root CompositePodGroup        level 1: top-level policy
//	  group CompositePodGroup     level 2: one per replicatedJobs entry, plus one
//	                              fallback group for untargeted ReplicatedJobs
//	    leaf PodGroup(s)          level 3: one shared leaf, or one per Job when
//	                              the entry sets job
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
	// fail the Workload create as Invalid on every reconcile. The reachable cases
	// are more than 8 level-2 groups under the root (8 entries plus the fallback
	// group) and an entry with job set on a ReplicatedJob with more than 8 replicas.
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

// compositeGroup is one level-2 group of the uniform tree: either a
// replicatedJobs entry or the implicit fallback group that collects every
// ReplicatedJob no entry targets.
type compositeGroup struct {
	// index is the entry's position in spec.scheduling.replicatedJobs. The
	// fallback group uses len(spec.scheduling.replicatedJobs), so it reads as an
	// implicit trailing entry.
	index int
	// targets are the ReplicatedJob names of the group, in entry order (or
	// spec.replicatedJobs order for the fallback group).
	targets []string
	// members are the resolved ReplicatedJobs of the group.
	members []*jobset.ReplicatedJob
	// entry is the replicatedJobs entry, or nil for the fallback group.
	entry *jobset.ReplicatedJobScheduling
}

// compositeGroups returns the level-2 groups of the uniform tree in a stable
// order: one per replicatedJobs entry that resolves at least one ReplicatedJob,
// then the fallback group if any ReplicatedJob is untargeted.
func compositeGroups(js *jobset.JobSet) []compositeGroup {
	scheduling := js.Spec.Scheduling
	rjobsByName := make(map[string]*jobset.ReplicatedJob, len(js.Spec.ReplicatedJobs))
	for i := range js.Spec.ReplicatedJobs {
		rjobsByName[js.Spec.ReplicatedJobs[i].Name] = &js.Spec.ReplicatedJobs[i]
	}

	var groups []compositeGroup
	targeted := make(map[string]bool, len(js.Spec.ReplicatedJobs))
	for i := range scheduling.ReplicatedJobs {
		entry := &scheduling.ReplicatedJobs[i]
		g := compositeGroup{index: i, entry: entry}
		for _, name := range entry.TargetReplicatedJobs {
			rjob, ok := rjobsByName[name]
			if !ok {
				// Invalid target name; reported separately by webhook validation.
				continue
			}
			g.targets = append(g.targets, name)
			g.members = append(g.members, rjob)
			targeted[name] = true
		}
		if len(g.members) > 0 {
			groups = append(groups, g)
		}
	}

	fallback := compositeGroup{index: len(scheduling.ReplicatedJobs)}
	for i := range js.Spec.ReplicatedJobs {
		rjob := &js.Spec.ReplicatedJobs[i]
		if targeted[rjob.Name] {
			continue
		}
		fallback.targets = append(fallback.targets, rjob.Name)
		fallback.members = append(fallback.members, rjob)
	}
	if len(fallback.members) > 0 {
		groups = append(groups, fallback)
	}
	return groups
}

// usesTopLevelScheduling reports whether any level-1 field is set.
func usesTopLevelScheduling(scheduling *jobset.JobSetScheduling) bool {
	return scheduling.SchedulingPolicy != nil || scheduling.SchedulingConstraints != nil || scheduling.DisruptionMode != nil
}

// compositeRootIsGang reports whether the root CompositePodGroup is Gang. An
// explicit policy wins. Otherwise a JobSet that only sets replicatedJobs gets a
// Basic root, which keeps the Alpha meaning of independent groups, and every
// other shape (including an empty scheduling block, the Alpha top-level gang)
// gets a Gang root.
func compositeRootIsGang(scheduling *jobset.JobSetScheduling) bool {
	if scheduling.SchedulingPolicy != nil {
		return scheduling.SchedulingPolicy.Gang != nil
	}
	return usesTopLevelScheduling(scheduling) || len(scheduling.ReplicatedJobs) == 0
}

// newCompositeRootItem builds the root composite WorkloadItem from the top-level
// policy, with one level-2 child per group.
func newCompositeRootItem(js *jobset.JobSet) *workloadbuilder.WorkloadItem {
	scheduling := js.Spec.Scheduling
	rootGang := compositeRootIsGang(scheduling)

	groups := compositeGroups(js)
	children := make([]*workloadbuilder.WorkloadItem, 0, len(groups))
	for i := range groups {
		children = append(children, newCompositeGroupItem(&groups[i], rootGang))
	}

	return &workloadbuilder.WorkloadItem{
		Name:          compositeRootTemplateName(js),
		Path:          schedulingRootPath,
		DefaultConfig: compositePolicyConfig(rootGang),
		Input:         compositeNodeInput(scheduling.SchedulingPolicy, scheduling.SchedulingConstraints, scheduling.DisruptionMode, nil),
		Callbacks:     []workloadbuilder.SchedulingConfigFunc{gangMinCountCallback(int32(len(children)))},
		Children:      children,
	}
}

// newCompositeGroupItem builds a level-2 CompositePodGroup for one group.
func newCompositeGroupItem(g *compositeGroup, rootGang bool) *workloadbuilder.WorkloadItem {
	if g.entry != nil && g.entry.Job != nil {
		return newCompositePerJobGroupItem(g)
	}
	return newCompositeSharedGroupItem(g, rootGang)
}

// newCompositePerJobGroupItem builds the level-2 CompositePodGroup for an entry
// that sets job: the entry-level fields configure the CompositePodGroup and each
// Job replica gets its own leaf from the job fields. Webhook validation
// guarantees exactly one member.
func newCompositePerJobGroupItem(g *compositeGroup) *workloadbuilder.WorkloadItem {
	entry := g.entry
	rjob := g.members[0]
	children := make([]*workloadbuilder.WorkloadItem, 0, rjob.Replicas)
	for jobIdx := 0; jobIdx < int(rjob.Replicas); jobIdx++ {
		children = append(children, newLeafItem(
			compositeJobLeafTemplateName(rjob.Name, jobIdx),
			jobsetutil.JobParallelism(rjob),
			rjob.Template.Spec.Template.Spec.PriorityClassName,
			jobLeafInput(entry.Job, g.index),
		))
	}
	return &workloadbuilder.WorkloadItem{
		Name:          compositeGroupTemplateName(g.index, g.targets),
		Path:          schedulingRootPath,
		DefaultConfig: compositePolicyConfig(true),
		Input:         compositeNodeInput(entry.SchedulingPolicy, entry.SchedulingConstraints, entry.DisruptionMode, entryPath(g.index)),
		Callbacks:     []workloadbuilder.SchedulingConfigFunc{gangMinCountCallback(int32(len(children)))},
		Children:      children,
	}
}

// newCompositeSharedGroupItem builds the level-2 CompositePodGroup for an entry
// without job, or for the fallback group, over one shared leaf PodGroup.
//
// The group's gang is a pod-count gang on the shared leaf (minCount = the
// group's pods, excluding DependsOn-deferred members). The CompositePodGroup
// above it has a single child, so it is Gang with minGroupCount 1 when the
// group is Gang and Basic otherwise; it carries the entry's
// schedulingConstraints and disruptionMode. The fallback group inherits the
// root policy and has no constraints of its own (the root holds them).
func newCompositeSharedGroupItem(g *compositeGroup, rootGang bool) *workloadbuilder.WorkloadItem {
	groupGang := rootGang
	var constraints *schedulingv1alpha3.WorkloadCompositePodGroupSchedulingConstraints
	var disruption *schedulingv1alpha3.WorkloadCompositePodGroupDisruptionMode
	var path []string
	if g.entry != nil {
		groupGang = g.entry.SchedulingPolicy == nil || g.entry.SchedulingPolicy.Gang != nil
		constraints = g.entry.SchedulingConstraints
		disruption = g.entry.DisruptionMode
		path = entryPath(g.index)
	}

	leaf := newLeafItem(
		compositeGroupLeafTemplateName(g.index, g.targets),
		sharedGroupMinCount(g.members),
		g.members[0].Template.Spec.Template.Spec.PriorityClassName,
		workloadbuilder.WorkloadInput{Policy: leafPolicyInput(groupGang)},
	)

	return &workloadbuilder.WorkloadItem{
		Name:          compositeGroupTemplateName(g.index, g.targets),
		Path:          schedulingRootPath,
		DefaultConfig: compositePolicyConfig(groupGang),
		Input:         compositeNodeInput(singleChildCompositePolicy(groupGang), constraints, disruption, path),
		Children:      []*workloadbuilder.WorkloadItem{leaf},
	}
}

// singleChildCompositePolicy is the policy of a CompositePodGroup with exactly
// one child: Gang with minGroupCount 1, or Basic.
func singleChildCompositePolicy(gang bool) *schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy {
	if gang {
		return &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{
			Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{MinGroupCount: ptr.To[int32](1)},
		}
	}
	return &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{
		Basic: &schedulingv1alpha3.WorkloadCompositePodGroupBasicSchedulingPolicy{},
	}
}

// leafPolicyInput returns an explicit Basic or Gang leaf policy. A Gang leaf's
// minCount is left unset so the leaf's gangMinCountCallback derives it.
func leafPolicyInput(gang bool) workloadbuilder.PolicyInput {
	policy := &schedulingv1alpha3.WorkloadPodGroupSchedulingPolicy{}
	if gang {
		policy.Gang = &schedulingv1alpha3.WorkloadPodGroupGangSchedulingPolicy{}
	} else {
		policy.Basic = &schedulingv1alpha3.WorkloadPodGroupBasicSchedulingPolicy{}
	}
	return workloadbuilder.PolicyInput{PodGroupData: policy}
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

// entryPath is the field path prefix of a replicatedJobs entry, used to report
// validation errors against the field the user set.
func entryPath(index int) []string {
	return []string{"replicatedJobs", strconv.Itoa(index)}
}

// compositeNodeInput maps a composite node's building blocks to a WorkloadInput
// that sets CompositePodGroupData (not the leaf PodGroupData). pathPrefix is
// prepended to each field's path element for error reporting.
func compositeNodeInput(
	policy *schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy,
	constraints *schedulingv1alpha3.WorkloadCompositePodGroupSchedulingConstraints,
	disruption *schedulingv1alpha3.WorkloadCompositePodGroupDisruptionMode,
	pathPrefix []string,
) workloadbuilder.WorkloadInput {
	path := func(name string) []string {
		return append(append([]string(nil), pathPrefix...), name)
	}
	var input workloadbuilder.WorkloadInput
	if policy != nil {
		input.Policy = workloadbuilder.PolicyInput{
			CompositePodGroupData: policy.DeepCopy(),
			PathElements:          path("schedulingPolicy"),
		}
	}
	if constraints != nil {
		input.Constraints = workloadbuilder.ConstraintsInput{
			CompositePodGroupData: constraints.DeepCopy(),
			PathElements:          path("schedulingConstraints"),
		}
	}
	if disruption != nil {
		input.DisruptionMode = workloadbuilder.DisruptionModeInput{
			CompositePodGroupData: disruption.DeepCopy(),
			PathElements:          path("disruptionMode"),
		}
	}
	return input
}

// jobLeafInput maps a JobScheduling (per-Job leaf) to a WorkloadInput.
func jobLeafInput(job *jobset.JobScheduling, entryIndex int) workloadbuilder.WorkloadInput {
	path := func(name string) []string {
		return append(entryPath(entryIndex), "job", name)
	}
	return workloadbuilder.WorkloadInput{
		Policy:         mapPodPolicyInput(job.SchedulingPolicy, path("schedulingPolicy")),
		Constraints:    mapPodConstraintsInput(job.SchedulingConstraints, path("schedulingConstraints")),
		DisruptionMode: mapPodDisruptionInput(job.DisruptionMode, path("disruptionMode")),
		ResourceClaims: mapResourceClaimsInput(job.ResourceClaims, path("resourceClaims")),
	}
}

// compositePolicyConfig returns the default config of a composite node: Gang or
// Basic. A Gang node's minGroupCount (carried in the IR as Gang.MinCount) is
// defaulted to full quorum by a callback when the user left it unset.
func compositePolicyConfig(gang bool) *workloadbuilder.SchedulingConfig {
	if gang {
		return defaultGangConfig()
	}
	return &workloadbuilder.SchedulingConfig{
		Policy: &workloadbuilder.SchedulingPolicy{Basic: &workloadbuilder.BasicSchedulingPolicy{}},
	}
}

// Naming (gate on). Template names follow the index-prefix model of the
// Kubeflow Trainer integration (KEP-3015), extended to the composite levels.
// Upstream requires template names to be unique across the whole tree, and the
// scheme below is collision-free by construction:
//
//   - root CompositePodGroupTemplate:   <jobset-name>-cpg
//   - level-2 CompositePodGroupTemplate: <index>-<joined-targets>-cpg
//   - shared leaf PodGroupTemplate:     <index>-<joined-targets>
//   - per-Job leaf PodGroupTemplate:    <replicatedjob-name>-<job-index>
//
// ReplicatedJob names cannot start with a digit, so index-prefixed names never
// collide with the root or per-Job names, and two entries never collide with
// each other even when ReplicatedJob names contain "-" ([a, b] is 0-a-b and
// [a-b] is 1-a-b). Per-Job names end in a digit and composite names end in
// -cpg, so those cannot collide either.

// compositeNameSuffix marks a CompositePodGroupTemplate name, distinct from the
// shared leaf PodGroupTemplate beneath it.
const compositeNameSuffix = "-cpg"

// compositeObjectSourceKind is the hash input kind of runtime CompositePodGroup
// and leaf PodGroup object names in the composite tree.
const compositeObjectSourceKind = "SchedulingTemplate"

// boundedTemplateName returns name if it fits a DNS label; otherwise it truncates
// it and appends a hash of the full name so the result stays deterministic and
// unique.
func boundedTemplateName(name string) string {
	if len(name) <= maxPodGroupNameLength {
		return name
	}
	hash := sha1Hash(name)[:podGroupNameHashLength]
	return fmt.Sprintf("%s-%s", name[:maxPodGroupNameLength-1-podGroupNameHashLength], hash)
}

// compositeNodeName appends the -cpg suffix to base, truncating and hashing base
// when the result would exceed the DNS label limit, so the suffix is always kept.
func compositeNodeName(base string) string {
	name := base + compositeNameSuffix
	if len(name) <= maxPodGroupNameLength {
		return name
	}
	hash := sha1Hash(base)[:podGroupNameHashLength]
	prefixLen := maxPodGroupNameLength - len(compositeNameSuffix) - 1 - podGroupNameHashLength
	return fmt.Sprintf("%s-%s%s", base[:prefixLen], hash, compositeNameSuffix)
}

// compositeRootTemplateName names the root CompositePodGroupTemplate.
func compositeRootTemplateName(js *jobset.JobSet) string {
	return compositeNodeName(js.Name)
}

// compositeGroupBaseName is the <index>-<joined-targets> stem of a level-2 group.
func compositeGroupBaseName(index int, targets []string) string {
	return fmt.Sprintf("%d-%s", index, SchedulingGroupName(targets))
}

// compositeGroupTemplateName names a level-2 CompositePodGroupTemplate.
func compositeGroupTemplateName(index int, targets []string) string {
	return compositeNodeName(compositeGroupBaseName(index, targets))
}

// compositeGroupLeafTemplateName names the shared leaf PodGroupTemplate of a
// level-2 group.
func compositeGroupLeafTemplateName(index int, targets []string) string {
	return boundedTemplateName(compositeGroupBaseName(index, targets))
}

// compositeJobLeafTemplateName names the per-Job leaf PodGroupTemplate of the
// jobIdx'th replica of a ReplicatedJob.
func compositeJobLeafTemplateName(rjobName string, jobIdx int) string {
	return boundedTemplateName(fmt.Sprintf("%s-%d", rjobName, jobIdx))
}

// compositeObjectName names the runtime CompositePodGroup or leaf PodGroup
// materialized from a template: <workload-name>-<template-name>-<hash>, with the
// redundant template segment dropped for the root, following KEP-5547 and
// KEP-3015. The prefix is truncated when needed; the hash over the JobSet and
// template identity keeps the name unique.
func compositeObjectName(js *jobset.JobSet, templateName string) string {
	prefix := workloadName(js)
	if templateName != compositeRootTemplateName(js) {
		prefix = fmt.Sprintf("%s-%s", prefix, templateName)
	}
	return hashedName(prefix, schedulingIdentityHash(compositeObjectSourceKind, js.Namespace, js.Name+"/"+templateName))
}

// compositeLeafTemplateName returns the leaf PodGroupTemplate that owns the pods
// of the jobIdx'th Job of rjobName in the composite tree.
func compositeLeafTemplateName(js *jobset.JobSet, rjobName string, jobIdx int) string {
	groups := compositeGroups(js)
	for i := range groups {
		g := &groups[i]
		for _, name := range g.targets {
			if name != rjobName {
				continue
			}
			if g.entry != nil && g.entry.Job != nil {
				return compositeJobLeafTemplateName(rjobName, jobIdx)
			}
			return compositeGroupLeafTemplateName(g.index, g.targets)
		}
	}
	// Unreachable for a ReplicatedJob of this JobSet: every ReplicatedJob is
	// either targeted by an entry or collected by the fallback group.
	return compositeGroupLeafTemplateName(len(js.Spec.Scheduling.ReplicatedJobs), []string{rjobName})
}

// CompositeGroupCount returns the number of level-2 groups under the root of the
// JobSet's CompositePodGroup tree: one per replicatedJobs entry that resolves a
// ReplicatedJob, plus the fallback group if any ReplicatedJob is untargeted.
func CompositeGroupCount(js *jobset.JobSet) int {
	return len(compositeGroups(js))
}
