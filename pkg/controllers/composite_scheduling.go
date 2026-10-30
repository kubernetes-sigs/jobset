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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	workloadbuilder "k8s.io/component-helpers/scheduling/schedulingv1/workloadbuilder"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// patchCompositeTreeMinCounts updates every mutable gang min count in the
// existing composite tree to match desired, returning whether anything changed.
func patchCompositeTreeMinCounts(existing, desired []schedulingv1beta1.CompositePodGroupTemplate) bool {
	changed := false
	for i := range existing {
		e, d := &existing[i], &desired[i]
		if e.SchedulingPolicy.Gang != nil && d.SchedulingPolicy.Gang != nil &&
			e.SchedulingPolicy.Gang.MinGroupCount != d.SchedulingPolicy.Gang.MinGroupCount {
			e.SchedulingPolicy.Gang.MinGroupCount = d.SchedulingPolicy.Gang.MinGroupCount
			changed = true
		}
		for j := range e.PodGroupTemplates {
			le, ld := &e.PodGroupTemplates[j], &d.PodGroupTemplates[j]
			if le.SchedulingPolicy.Gang != nil && ld.SchedulingPolicy.Gang != nil &&
				le.SchedulingPolicy.Gang.MinCount != ld.SchedulingPolicy.Gang.MinCount {
				le.SchedulingPolicy.Gang.MinCount = ld.SchedulingPolicy.Gang.MinCount
				changed = true
			}
		}
		if patchCompositeTreeMinCounts(e.CompositePodGroupTemplates, d.CompositePodGroupTemplates) {
			changed = true
		}
	}
	return changed
}

// materializeCompositeTree creates the CompositePodGroup objects (root and
// children) and the leaf PodGroup objects for a gang-of-gangs Workload, linking
// each child CompositePodGroup and leaf PodGroup to its parent via
// spec.parentCompositePodGroupName.
//
// The upstream workloadbuilder can materialize a CompositePodGroup from a
// template but has no public API to materialize the nested leaf PodGroups, so
// the leaves are built directly from their (same-version) templates.
func (r *JobSetReconciler) materializeCompositeTree(ctx context.Context, js *jobset.JobSet, workload *schedulingv1beta1.Workload) error {
	builder := workloadbuilder.NewBuilderFromExistingWorkload(workload, buildOpts(js))
	for i := range workload.Spec.CompositePodGroupTemplates {
		if err := r.materializeCompositeNode(ctx, js, builder, workload, &workload.Spec.CompositePodGroupTemplates[i], ""); err != nil {
			return err
		}
	}
	return nil
}

// materializeCompositeNode materializes one CompositePodGroupTemplate as a
// CompositePodGroup object, then recurses into its child CompositePodGroups and
// materializes its leaf PodGroups, linking each to this node.
func (r *JobSetReconciler) materializeCompositeNode(ctx context.Context, js *jobset.JobSet, builder *workloadbuilder.Builder, workload *schedulingv1beta1.Workload, tmpl *schedulingv1beta1.CompositePodGroupTemplate, parentName string) error {
	cpgName := schedulingPodGroupName(js, tmpl.Name)

	cpg, err := builder.NewCompositePodGroup(cpgName, tmpl.Name)
	if err != nil {
		return fmt.Errorf("materializing CompositePodGroup %s: %w", cpgName, err)
	}
	if parentName != "" {
		cpg.Spec.ParentCompositePodGroupName = ptr.To(parentName)
	}
	cpg.OwnerReferences = nil
	if err := r.ensureCompositePodGroup(ctx, js, cpg); err != nil {
		return err
	}

	for i := range tmpl.CompositePodGroupTemplates {
		if err := r.materializeCompositeNode(ctx, js, builder, workload, &tmpl.CompositePodGroupTemplates[i], cpgName); err != nil {
			return err
		}
	}
	for i := range tmpl.PodGroupTemplates {
		leaf := &tmpl.PodGroupTemplates[i]
		pg := leafPodGroupFromTemplate(workload, leaf, schedulingPodGroupName(js, leaf.Name))
		pg.Spec.ParentCompositePodGroupName = ptr.To(cpgName)
		if err := r.ensureLeafPodGroup(ctx, js, pg); err != nil {
			return err
		}
	}
	return nil
}

// leafPodGroupFromTemplate builds a leaf PodGroup object from its template
// (both are scheduling.k8s.io/v1beta1, so this is a direct field copy).
func leafPodGroupFromTemplate(workload *schedulingv1beta1.Workload, tmpl *schedulingv1beta1.PodGroupTemplate, name string) *schedulingv1beta1.PodGroup {
	spec := schedulingv1beta1.PodGroupSpec{
		WorkloadRef: &schedulingv1beta1.WorkloadReference{
			WorkloadName: workload.Name,
			TemplateName: tmpl.Name,
		},
		SchedulingPolicy:      *tmpl.SchedulingPolicy.DeepCopy(),
		SchedulingConstraints: tmpl.SchedulingConstraints.DeepCopy(),
		DisruptionMode:        tmpl.DisruptionMode.DeepCopy(),
		PriorityClassName:     tmpl.PriorityClassName,
	}
	if len(tmpl.ResourceClaims) > 0 {
		spec.ResourceClaims = make([]schedulingv1beta1.PodGroupResourceClaim, len(tmpl.ResourceClaims))
		for i := range tmpl.ResourceClaims {
			tmpl.ResourceClaims[i].DeepCopyInto(&spec.ResourceClaims[i])
		}
	}
	return &schedulingv1beta1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: workload.Namespace},
		Spec:       spec,
	}
}

func (r *JobSetReconciler) ensureCompositePodGroup(ctx context.Context, js *jobset.JobSet, cpg *schedulingv1alpha3.CompositePodGroup) error {
	log := ctrl.LoggerFrom(ctx)
	if err := ctrl.SetControllerReference(js, cpg, r.Scheme); err != nil {
		return fmt.Errorf("setting controller reference on CompositePodGroup %s: %w", cpg.Name, err)
	}
	if cpg.Labels == nil {
		cpg.Labels = make(map[string]string)
	}
	cpg.Labels[jobset.JobSetNameKey] = js.Name

	existing := &schedulingv1alpha3.CompositePodGroup{}
	err := r.Get(ctx, client.ObjectKeyFromObject(cpg), existing)
	if apierrors.IsNotFound(err) {
		log.V(2).Info("Creating CompositePodGroup", "compositePodGroup", cpg.Name)
		if err := r.Create(ctx, cpg); client.IgnoreAlreadyExists(err) != nil {
			return fmt.Errorf("creating CompositePodGroup %s: %w", cpg.Name, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting CompositePodGroup %s: %w", cpg.Name, err)
	}
	if !metav1.IsControlledBy(existing, js) {
		return fmt.Errorf("compositePodGroup %s already exists and is not controlled by JobSet %s", klog.KObj(existing), klog.KObj(js))
	}
	// Gang minGroupCount is the one mutable field; patch it in place when scaling
	// changed it. All other spec drift is handled by the Workload-level
	// delete-and-recreate path.
	if existing.Spec.SchedulingPolicy.Gang != nil && cpg.Spec.SchedulingPolicy.Gang != nil &&
		existing.Spec.SchedulingPolicy.Gang.MinGroupCount != cpg.Spec.SchedulingPolicy.Gang.MinGroupCount {
		patched := existing.DeepCopy()
		patched.Spec.SchedulingPolicy.Gang.MinGroupCount = cpg.Spec.SchedulingPolicy.Gang.MinGroupCount
		if err := r.Update(ctx, patched); err != nil {
			return fmt.Errorf("patching CompositePodGroup %s minGroupCount: %w", cpg.Name, err)
		}
	}
	return nil
}

func (r *JobSetReconciler) ensureLeafPodGroup(ctx context.Context, js *jobset.JobSet, pg *schedulingv1beta1.PodGroup) error {
	log := ctrl.LoggerFrom(ctx)
	if err := ctrl.SetControllerReference(js, pg, r.Scheme); err != nil {
		return fmt.Errorf("setting controller reference on PodGroup %s: %w", pg.Name, err)
	}
	if pg.Labels == nil {
		pg.Labels = make(map[string]string)
	}
	pg.Labels[jobset.JobSetNameKey] = js.Name

	existing := &schedulingv1beta1.PodGroup{}
	err := r.Get(ctx, client.ObjectKeyFromObject(pg), existing)
	if apierrors.IsNotFound(err) {
		log.V(2).Info("Creating leaf PodGroup", "podGroup", pg.Name)
		if err := r.Create(ctx, pg); client.IgnoreAlreadyExists(err) != nil {
			return fmt.Errorf("creating PodGroup %s: %w", pg.Name, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting PodGroup %s: %w", pg.Name, err)
	}
	if !metav1.IsControlledBy(existing, js) {
		return fmt.Errorf("podgroup %s already exists and is not controlled by JobSet %s", klog.KObj(existing), klog.KObj(js))
	}
	// Gang minCount is mutable; patch it in place when scaling changed it, matching
	// the flat PodGroup reconcile path.
	if existing.Spec.SchedulingPolicy.Gang != nil && pg.Spec.SchedulingPolicy.Gang != nil &&
		existing.Spec.SchedulingPolicy.Gang.MinCount != pg.Spec.SchedulingPolicy.Gang.MinCount {
		patched := existing.DeepCopy()
		patched.Spec.SchedulingPolicy.Gang.MinCount = pg.Spec.SchedulingPolicy.Gang.MinCount
		if err := r.Update(ctx, patched); err != nil {
			return fmt.Errorf("patching PodGroup %s minCount: %w", pg.Name, err)
		}
	}
	return nil
}

// deleteCompositePodGroups deletes every CompositePodGroup owned by this JobSet,
// selected by the owning-JobSet label. Leaf PodGroups are deleted by
// deleteSchedulingObjects via the same label.
func (r *JobSetReconciler) deleteCompositePodGroups(ctx context.Context, js *jobset.JobSet) error {
	log := ctrl.LoggerFrom(ctx)
	var cpgList schedulingv1alpha3.CompositePodGroupList
	if err := r.List(ctx, &cpgList, client.InNamespace(js.Namespace),
		client.MatchingLabels{jobset.JobSetNameKey: js.Name}); err != nil {
		return fmt.Errorf("listing CompositePodGroups: %w", err)
	}
	for i := range cpgList.Items {
		cpg := &cpgList.Items[i]
		if !metav1.IsControlledBy(cpg, js) {
			continue
		}
		log.V(2).Info("Deleting CompositePodGroup", "compositePodGroup", klog.KObj(cpg))
		if err := r.Delete(ctx, cpg); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("deleting CompositePodGroup %s: %w", cpg.Name, err)
		}
	}
	return nil
}

// compositeObjectsReady reports whether every CompositePodGroup and leaf
// PodGroup in the composite Workload exists and is not terminating.
func (r *JobSetReconciler) compositeObjectsReady(ctx context.Context, js *jobset.JobSet, workload *schedulingv1beta1.Workload) (bool, error) {
	ready := true
	var checkErr error
	visit := func(get func() (metav1.Object, error)) bool {
		obj, err := get()
		if apierrors.IsNotFound(err) {
			ready = false
			return false
		}
		if err != nil {
			checkErr = err
			return false
		}
		if !obj.GetDeletionTimestamp().IsZero() {
			ready = false
			return false
		}
		return true
	}

	var walk func(tmpls []schedulingv1beta1.CompositePodGroupTemplate) bool
	walk = func(tmpls []schedulingv1beta1.CompositePodGroupTemplate) bool {
		for i := range tmpls {
			cpgName := schedulingPodGroupName(js, tmpls[i].Name)
			if !visit(func() (metav1.Object, error) {
				o := &schedulingv1alpha3.CompositePodGroup{}
				return o, r.Get(ctx, client.ObjectKey{Name: cpgName, Namespace: js.Namespace}, o)
			}) {
				return false
			}
			for j := range tmpls[i].PodGroupTemplates {
				pgName := schedulingPodGroupName(js, tmpls[i].PodGroupTemplates[j].Name)
				if !visit(func() (metav1.Object, error) {
					o := &schedulingv1beta1.PodGroup{}
					return o, r.Get(ctx, client.ObjectKey{Name: pgName, Namespace: js.Namespace}, o)
				}) {
					return false
				}
			}
			if !walk(tmpls[i].CompositePodGroupTemplates) {
				return false
			}
		}
		return true
	}
	walk(workload.Spec.CompositePodGroupTemplates)
	return ready, checkErr
}
