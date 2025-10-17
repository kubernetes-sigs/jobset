/*
Copyright 2023 The Kubernetes Authors.
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

	corev1 "k8s.io/api/core/v1"
	schedulingv1alpha1 "k8s.io/api/scheduling/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
	"sigs.k8s.io/jobset/pkg/workload"
)

// reconcileGangPolicy handles the creation and management of Workload resources for gang scheduling.
func (r *JobSetReconciler) reconcileGangPolicy(ctx context.Context, js *jobset.JobSet) error {
	// If no gang policy is set or workloadTemplate is nil, ensure any existing workload is cleaned up
	if js.Spec.GangPolicy == nil || js.Spec.GangPolicy.WorkloadTemplate == nil {
		return r.deleteWorkloadIfExists(ctx, js)
	}

	// If JobSet is suspended, delete the workload if it exists.
	// Suspended JobSets should not have workloads since they are not actively scheduling.
	if jobSetSuspended(js) {
		return r.deleteWorkloadIfExists(ctx, js)
	}

	// Check if workload already exists
	workloadName := workload.GenWorkloadName(js)
	var existingWorkload schedulingv1alpha1.Workload
	err := r.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &existingWorkload)

	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		// Workload doesn't exist, create it
		return r.createWorkload(ctx, js)
	}

	// Workload exists, update if necessary
	return r.updateWorkloadIfNeeded(ctx, &existingWorkload)
}

// createWorkload creates a new Workload resource based on the gang policy configuration.
func (r *JobSetReconciler) createWorkload(ctx context.Context, js *jobset.JobSet) error {
	log := ctrl.LoggerFrom(ctx)

	if js.Spec.GangPolicy.WorkloadTemplate == nil {
		return fmt.Errorf("gangPolicy.workloadTemplate must be set")
	}

	wl := workload.ConstructWorkloadFromTemplate(js)

	// Set JobSet as owner for garbage collection
	if err := ctrl.SetControllerReference(js, wl, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, wl); err != nil {
		r.Record.Eventf(js, corev1.EventTypeWarning, constants.WorkloadCreationFailedReason, "Failed to create workload: %v", err)
		return err
	}

	log.V(2).Info("successfully created workload", "workload", klog.KObj(wl))
	r.Record.Eventf(js, corev1.EventTypeNormal, constants.WorkloadCreatedReason, "Created workload %s for gang scheduling", wl.Name)
	return nil
}

// updateWorkloadIfNeeded updates the workload if the JobSet spec has changed.
func (r *JobSetReconciler) updateWorkloadIfNeeded(ctx context.Context, existing *schedulingv1alpha1.Workload) error {
	log := ctrl.LoggerFrom(ctx)

	// For now, workload spec is immutable
	// If users need to change gang policy, they should recreate the JobSet
	// This is consistent with other immutable fields in JobSet like FailurePolicy and SuccessPolicy

	log.V(2).Info("workload is up to date", "workload", klog.KObj(existing))
	return nil
}

// deleteWorkloadIfExists deletes the workload if it exists.
func (r *JobSetReconciler) deleteWorkloadIfExists(ctx context.Context, js *jobset.JobSet) error {
	log := ctrl.LoggerFrom(ctx)

	workloadName := workload.GenWorkloadName(js)
	var wl schedulingv1alpha1.Workload
	err := r.Get(ctx, types.NamespacedName{Name: workloadName, Namespace: js.Namespace}, &wl)

	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	// Delete the workload
	if err := r.Delete(ctx, &wl); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	log.V(2).Info("deleted workload", "workload", klog.KObj(&wl))
	return nil
}
