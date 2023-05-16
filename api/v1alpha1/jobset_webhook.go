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

package v1alpha1

import (
	"errors"
	"fmt"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

func (r *JobSet) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

//+kubebuilder:webhook:path=/mutate-jobset-x-k8s-io-v1alpha1-jobset,mutating=true,failurePolicy=fail,sideEffects=None,groups=jobset.x-k8s.io,resources=jobsets,verbs=create;update,versions=v1alpha1,name=mjobset.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &JobSet{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *JobSet) Default() {
	for i, _ := range r.Spec.ReplicatedJobs {
		// Default job completion mode to indexed.
		if r.Spec.ReplicatedJobs[i].Template.Spec.CompletionMode == nil {
			r.Spec.ReplicatedJobs[i].Template.Spec.CompletionMode = completionModePtr(batchv1.IndexedCompletion)
		}
		// Enable DNS hostnames by default.
		if r.Spec.ReplicatedJobs[i].Network == nil {
			r.Spec.ReplicatedJobs[i].Network = &Network{}
		}
		if r.Spec.ReplicatedJobs[i].Network.EnableDNSHostnames == nil {
			r.Spec.ReplicatedJobs[i].Network.EnableDNSHostnames = pointer.Bool(true)
		}
		// Default pod restart policy to OnFailure.
		if r.Spec.ReplicatedJobs[i].Template.Spec.Template.Spec.RestartPolicy == "" {
			r.Spec.ReplicatedJobs[i].Template.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyOnFailure
		}
	}

}

//+kubebuilder:webhook:path=/validate-jobset-x-k8s-io-v1alpha1-jobset,mutating=false,failurePolicy=fail,sideEffects=None,groups=jobset.x-k8s.io,resources=jobsets,verbs=create;update,versions=v1alpha1,name=vjobset.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &JobSet{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *JobSet) ValidateCreate() error {
	for i := range r.Spec.ReplicatedJobs {
		enableDNSHostnames := r.Spec.ReplicatedJobs[i].Network != nil &&
			r.Spec.ReplicatedJobs[i].Network.EnableDNSHostnames != nil && *r.Spec.ReplicatedJobs[i].Network.EnableDNSHostnames
		indexed := r.Spec.ReplicatedJobs[i].Template.Spec.CompletionMode != nil &&
			*r.Spec.ReplicatedJobs[i].Template.Spec.CompletionMode == batchv1.IndexedCompletion

		if enableDNSHostnames && !indexed {
			return fmt.Errorf("EnableDNSHostnames requires job to be in indexed completion mode")
		}
	}
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *JobSet) ValidateUpdate(old runtime.Object) error {
	oldObj := old.(*JobSet)
	var allErr []error
	for index := range r.Spec.ReplicatedJobs {
		oldObj.Spec.ReplicatedJobs[index].Template.Spec.Template.Spec.NodeSelector = r.Spec.ReplicatedJobs[index].Template.Spec.Template.Spec.NodeSelector
		if !reflect.DeepEqual(oldObj.Spec, r.Spec) {
			allErr = append(allErr, fmt.Errorf("the spec.ReplicatedJobs value is imutable excepting Spec.ReplicatedJobs[index].Template.Spec.Template.Spec.NodeSelector"))
		}
	}
	return errors.Join(allErr...)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *JobSet) ValidateDelete() error {
	return nil
}

func completionModePtr(mode batchv1.CompletionMode) *batchv1.CompletionMode {
	return &mode
}
