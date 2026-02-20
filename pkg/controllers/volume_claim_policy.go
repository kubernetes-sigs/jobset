package controllers

import (
	"context"
	"errors"
	"fmt"
	"maps"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
)

// reconcileVolumeClaimPolicies creates PVCs using volumeClaimPolicies.
func (r *JobSetReconciler) reconcileVolumeClaimPolicies(ctx context.Context, js *jobset.JobSet) error {
	var allErrors []error
	// Create PVC for each volume claim policy.
	for _, volumeClaimPolicy := range js.Spec.VolumeClaimPolicies {
		if err := r.createPVCsIfNecessary(ctx, js, volumeClaimPolicy); err != nil {
			allErrors = append(allErrors, err)
		}
	}
	return errors.Join(allErrors...)
}

// createPVCsIfNecessary creates PVCs if it doesn't exist.
func (r *JobSetReconciler) createPVCsIfNecessary(ctx context.Context, js *jobset.JobSet, volumeClaimPolicy jobset.VolumeClaimPolicy) error {
	log := ctrl.LoggerFrom(ctx)

	for _, template := range volumeClaimPolicy.Templates {
		// Add JobSet name label.
		labels := make(map[string]string)
		maps.Copy(labels, template.Labels)
		labels[jobset.JobSetNameKey] = js.Name

		// Create new PVC based on template.
		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:        GeneratePVCName(js.Name, template.Name),
				Namespace:   js.Namespace,
				Labels:      labels,
				Annotations: template.Annotations,
			},
			Spec: template.Spec,
		}

		// Set PVC owner reference if retention policy is Delete.
		if volumeClaimPolicy.RetentionPolicy != nil && *volumeClaimPolicy.RetentionPolicy.WhenDeleted == jobset.RetentionPolicyDelete {
			if err := ctrl.SetControllerReference(js, &pvc, r.Scheme); err != nil {
				return err
			}
		}

		// Create PVC if it doesn't exist.
		if err := r.Create(ctx, &pvc); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				r.Record.Eventf(js, nil, corev1.EventTypeWarning, constants.PVCCreationFailedReason, "Reconciling", err.Error())
				return err
			}
		} else {
			log.V(2).Info("successfully created PVC", "pvc", klog.KObj(&pvc))
		}
	}

	return nil
}

// GeneratePVCName creates a name for the PVC in this format: <pvc-template-name>-<jobset-name>
func GeneratePVCName(jobSetName, pvcTemplateName string) string {
	return fmt.Sprintf("%s-%s", pvcTemplateName, jobSetName)
}

// addVolumes adds volumes and volumeMounts to the Job spec.
func addVolumes(job *batchv1.Job, js *jobset.JobSet) {
	for _, policy := range js.Spec.VolumeClaimPolicies {
		for _, template := range policy.Templates {
			// Verify that Job spec has a corresponding volumeMount.
			if !hasVolumeMount(job, template.Name) {
				continue
			}

			// Add volume to the Job spec.
			job.Spec.Template.Spec.Volumes = append(job.Spec.Template.Spec.Volumes,
				corev1.Volume{
					Name: template.Name,
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: GeneratePVCName(js.Name, template.Name),
						},
					},
				},
			)
		}
	}
}

// hasVolumeMount checks whether Job spec has desired volume mount in container or InitContainer.
func hasVolumeMount(job *batchv1.Job, volumeMountName string) bool {
	for _, container := range job.Spec.Template.Spec.Containers {
		for _, volumeMount := range container.VolumeMounts {
			if volumeMount.Name == volumeMountName {
				return true
			}
		}
	}

	for _, container := range job.Spec.Template.Spec.InitContainers {
		for _, volumeMount := range container.VolumeMounts {
			if volumeMount.Name == volumeMountName {
				return true
			}
		}
	}
	return false
}
