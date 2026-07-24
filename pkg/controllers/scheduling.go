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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	workloadbuilder "k8s.io/component-helpers/scheduling/schedulingv1/workloadbuilder"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	jobsetutil "sigs.k8s.io/jobset/pkg/util"
)

const (
	// SchedulingGroupTemplateNameKey is the annotation set on child Jobs to map them
	// to their corresponding PodGroupTemplate in the parent Workload.
	SchedulingGroupTemplateNameKey = "scheduling.k8s.io/group-template-name"

	// SchedulingParentCompositePodGroupKey is the annotation set on child Jobs to
	// link them to the parent CompositePodGroup instance.
	SchedulingParentCompositePodGroupKey = "scheduling.k8s.io/parent-composite-podgroup"
)

// UseTopLevelGang returns true when the top-level scheduling policy is Gang
// (or defaults to Gang) and there are no per-ReplicatedJob policy overrides.
// In this mode a single PodGroup is created so that all pods across every
// ReplicatedJob are gang-scheduled together.
//
// Top-level gang is disabled when DependsOn or InOrder StartupPolicy is used,
// because those features create Jobs sequentially. A single PodGroup requiring
// all pods would deadlock since not all pods exist simultaneously.
func UseTopLevelGang(scheduling *jobset.JobSetScheduling) bool {
	if scheduling == nil {
		return false
	}
	// Any per-RJ overrides means we fall back to one PodGroup per RJ.
	if len(scheduling.ReplicatedJobPolicies) > 0 {
		return false
	}
	// If no explicit policy is set, the default is Gang.
	if scheduling.Policy == nil {
		return true
	}
	// Explicit Gang policy at the top level.
	return scheduling.Policy.Gang != nil
}

// HasSequencedStartup returns true when the JobSet uses DependsOn or an InOrder
// StartupPolicy. These features create Jobs sequentially, meaning not all pods
// exist at the same time. This affects how scheduling objects are compiled.
func HasSequencedStartup(js *jobset.JobSet) bool {
	// Check for InOrder startup policy.
	if js.Spec.StartupPolicy != nil && js.Spec.StartupPolicy.StartupPolicyOrder == jobset.InOrder {
		return true
	}
	// Check for DependsOn on any ReplicatedJob.
	for i := range js.Spec.ReplicatedJobs {
		if len(js.Spec.ReplicatedJobs[i].DependsOn) > 0 {
			return true
		}
	}
	return false
}

// totalMinCount computes the aggregate minCount across all ReplicatedJobs.
// If the top-level Gang policy specifies an explicit minCount, that value is
// used directly; otherwise the sum of parallelism*replicas for every
// ReplicatedJob is returned.
func totalMinCount(js *jobset.JobSet) int32 {
	if js.Spec.Scheduling != nil && js.Spec.Scheduling.Policy != nil &&
		js.Spec.Scheduling.Policy.Gang != nil && js.Spec.Scheduling.Policy.Gang.MinCount > 0 {
		return js.Spec.Scheduling.Policy.Gang.MinCount
	}
	return jobsetutil.TotalReplicatedJobPodCount(js.Spec.ReplicatedJobs)
}

// buildWorkload compiles a JobSet's scheduling configuration into a Workload resource
// using the workloadbuilder library.
func buildWorkload(js *jobset.JobSet) (*schedulingv1alpha3.Workload, error) {
	builders := newBuildersForJobSet(js)
	return buildWorkloadFromBuilders(builders)
}

// computeMinCount returns parallelism * replicas for a ReplicatedJob.
func computeMinCount(rjob *jobset.ReplicatedJob) int32 {
	return jobsetutil.ReplicatedJobPodCount(rjob)
}

// reconcileWorkload ensures the Workload resource for a JobSet exists and is up to date.
// When ElasticJobSet scaling changes parallelism, the Workload spec (which is
// immutable) may become stale. In that case we delete and recreate it.
func (r *JobSetReconciler) reconcileWorkload(ctx context.Context, js *jobset.JobSet) (bool, error) {
	log := ctrl.LoggerFrom(ctx)

	desired, err := buildWorkload(js)
	if err != nil {
		return false, fmt.Errorf("building Workload: %w", err)
	}

	// Set JobSet as the owner of the Workload. The workloadbuilder sets a
	// non-controller ownerRef; we upgrade it to a controller ref here.
	desired.OwnerReferences = nil
	if err := ctrl.SetControllerReference(js, desired, r.Scheme); err != nil {
		return false, fmt.Errorf("setting controller reference on Workload: %w", err)
	}

	// Try to get existing Workload.
	existing := &schedulingv1alpha3.Workload{}
	err = r.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if apierrors.IsNotFound(err) {
		log.V(2).Info("Creating Workload for JobSet", "workload", klog.KObj(desired))
		if err := r.Create(ctx, desired); apierrors.IsAlreadyExists(err) {
			// A previous Workload may still be terminating (e.g., finalizers);
			// let the next reconcile (triggered by the owned-object watch) retry.
			log.V(2).Info("Workload already exists, will retry on next reconcile", "workload", klog.KObj(desired))
			return false, nil
		} else if err != nil {
			return false, fmt.Errorf("creating Workload: %w", err)
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("getting Workload: %w", err)
	}

	// Refuse to act on a Workload with the same name that is not controlled by
	// this JobSet, to avoid conflicting with (or deleting) a Workload owned by
	// someone else.
	if !metav1.IsControlledBy(existing, js) {
		return false, fmt.Errorf("workload %s already exists and is not controlled by JobSet %s", klog.KObj(existing), klog.KObj(js))
	}

	// If the Workload spec has drifted (e.g., ElasticJobSet scaling changed
	// parallelism), the immutable Workload must be deleted and recreated.
	if workloadNeedsRecreation(existing, desired) {
		log.V(2).Info("Workload spec drifted, deleting stale scheduling objects", "workload", klog.KObj(existing))
		if err := r.deleteSchedulingObjects(ctx, js); err != nil {
			return false, fmt.Errorf("deleting stale scheduling objects: %w", err)
		}
		// Don't recreate the Workload immediately: deletion may not be finalized
		// yet (e.g., pending finalizers), which would race and return
		// AlreadyExists. The owned-object watch on Workload will trigger the
		// next reconcile once the old object is fully removed.
		return true, nil
	}

	// Workload exists and matches desired state.
	return false, nil
}

// workloadNeedsRecreation returns true if the existing Workload's
// PodGroupTemplates differ from the desired state. This happens when
// ElasticJobSet scaling changes the parallelism of a ReplicatedJob,
// causing the computed minCount to change.
func workloadNeedsRecreation(existing, desired *schedulingv1alpha3.Workload) bool {
	if len(existing.Spec.PodGroupTemplates) != len(desired.Spec.PodGroupTemplates) {
		return true
	}
	for i := range existing.Spec.PodGroupTemplates {
		e := &existing.Spec.PodGroupTemplates[i]
		d := &desired.Spec.PodGroupTemplates[i]
		if e.Name != d.Name {
			return true
		}
		// PodGroupTemplate fields are immutable. Compare the complete template so
		// changes to constraints, disruption, resource claims, priority, or policy
		// details cannot leave stale scheduling objects behind.
		if !apiequality.Semantic.DeepEqual(*e, *d) {
			return true
		}
	}
	return false
}

// reconcilePodGroups ensures PodGroup resources exist for the JobSet using the
// workloadbuilder's NewPodGroup to materialize PodGroups from the compiled Workload.
func (r *JobSetReconciler) reconcilePodGroups(ctx context.Context, js *jobset.JobSet) error {
	log := ctrl.LoggerFrom(ctx)

	// Build the Workload to get the compiled templates.
	builders := newBuildersForJobSet(js)
	workload, err := buildWorkloadFromBuilders(builders)
	if err != nil {
		return fmt.Errorf("building Workload for PodGroup materialization: %w", err)
	}

	// Create a builder from the existing compiled Workload to materialize PodGroups.
	existingBuilder := workloadbuilder.NewBuilderFromExistingWorkload(workload, buildOpts(js))

	// Materialize PodGroups from each template.
	for _, tmpl := range workload.Spec.PodGroupTemplates {
		pgName := schedulingPodGroupName(js, tmpl.Name)

		desired, err := existingBuilder.NewPodGroup(pgName, tmpl.Name)
		if err != nil {
			return fmt.Errorf("materializing PodGroup %s: %w", pgName, err)
		}

		// The workloadbuilder sets a non-controller ownerRef. Clear it and use
		// ctrl.SetControllerReference for proper garbage collection.
		desired.OwnerReferences = nil
		if err := ctrl.SetControllerReference(js, desired, r.Scheme); err != nil {
			return fmt.Errorf("setting controller reference on PodGroup %s: %w", pgName, err)
		}

		existing := &schedulingv1alpha3.PodGroup{}
		getErr := r.Get(ctx, client.ObjectKeyFromObject(desired), existing)
		if apierrors.IsNotFound(getErr) {
			log.V(2).Info("Creating PodGroup", "podGroup", pgName, "template", tmpl.Name)
			if err := r.Create(ctx, desired); apierrors.IsAlreadyExists(err) {
				// A terminating PodGroup may still occupy the name. The owned
				// object watch will trigger a retry after finalization.
				log.V(2).Info("PodGroup already exists, will retry on next reconcile", "podGroup", pgName)
			} else if err != nil {
				return fmt.Errorf("creating PodGroup %s: %w", pgName, err)
			}
			continue
		}
		if getErr != nil {
			return fmt.Errorf("getting PodGroup %s: %w", pgName, getErr)
		}
		if !metav1.IsControlledBy(existing, js) {
			return fmt.Errorf("podgroup %s already exists and is not controlled by JobSet %s", klog.KObj(existing), klog.KObj(js))
		}

		// PodGroup specs are immutable. Treat an owned stale PodGroup as a
		// recoverable inconsistency and recreate all scheduling objects from the
		// Workload source of truth.
		if !apiequality.Semantic.DeepEqual(existing.Spec, desired.Spec) {
			log.V(2).Info("PodGroup spec drifted, deleting stale scheduling objects", "podGroup", klog.KObj(existing))
			if err := r.deleteSchedulingObjects(ctx, js); err != nil {
				return fmt.Errorf("deleting stale scheduling objects: %w", err)
			}
			return nil
		}

		// PodGroup exists and is controlled by this JobSet with the desired spec.
	}

	return nil
}

// deleteSchedulingObjects removes the Workload and all PodGroups owned by the
// JobSet. This is called when a JobSet is suspended so that the scheduler
// releases all resource claims. The objects are recreated when the JobSet is
// resumed.
func (r *JobSetReconciler) deleteSchedulingObjects(ctx context.Context, js *jobset.JobSet) error {
	log := ctrl.LoggerFrom(ctx)

	// Delete PodGroups owned by this JobSet.
	var pgList schedulingv1alpha3.PodGroupList
	if err := r.List(ctx, &pgList, client.InNamespace(js.Namespace)); err != nil {
		return fmt.Errorf("listing PodGroups: %w", err)
	}
	for i := range pgList.Items {
		pg := &pgList.Items[i]
		if !metav1.IsControlledBy(pg, js) {
			continue
		}
		log.V(2).Info("Deleting PodGroup for suspended JobSet", "podGroup", klog.KObj(pg))
		if err := r.Delete(ctx, pg); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("deleting PodGroup %s: %w", pg.Name, err)
		}
	}

	// Delete the Workload, but only if it exists and is controlled by this
	// JobSet, to avoid deleting an unrelated Workload with the same name.
	workload := &schedulingv1alpha3.Workload{}
	err := r.Get(ctx, client.ObjectKey{Name: js.Name, Namespace: js.Namespace}, workload)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting Workload %s: %w", js.Name, err)
	}
	if !metav1.IsControlledBy(workload, js) {
		log.V(2).Info("Skipping deletion of Workload not controlled by this JobSet", "workload", klog.KObj(workload))
		return nil
	}
	log.V(2).Info("Deleting Workload for suspended JobSet", "workload", klog.KObj(workload))
	if err := r.Delete(ctx, workload); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("deleting Workload %s: %w", workload.Name, err)
	}

	return nil
}

const (
	maxPodGroupNameLength  = 63
	podGroupNameHashLength = 10
)

// podGroupName generates a deterministic PodGroup name from the JobSet and
// ReplicatedJob names. The readable form is retained when it fits in a DNS
// label; otherwise a truncated JobSet name and hash preserve uniqueness.
func schedulingPodGroupName(js *jobset.JobSet, templateName string) string {
	if UseTopLevelGang(js.Spec.Scheduling) && !HasSequencedStartup(js) {
		return js.Name
	}
	return podGroupName(js.Name, templateName)
}

func podGroupName(jobSetName, replicatedJobName string) string {
	name := fmt.Sprintf("%s-%s", jobSetName, replicatedJobName)
	if len(name) <= maxPodGroupNameLength {
		return name
	}

	hash := sha1Hash(fmt.Sprintf("%s/%s", jobSetName, replicatedJobName))[:podGroupNameHashLength]
	prefixLength := maxPodGroupNameLength - 1 - podGroupNameHashLength
	if len(jobSetName) > prefixLength {
		jobSetName = jobSetName[:prefixLength]
	}
	return fmt.Sprintf("%s-%s", jobSetName, hash)
}
