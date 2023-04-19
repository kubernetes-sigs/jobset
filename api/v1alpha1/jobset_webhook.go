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
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	batchv1 "k8s.io/api/batch/v1"
)

// log is for logging in this package.
var jobsetlog = logf.Log.WithName("jobset-resource")

func (r *JobSet) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

//+kubebuilder:webhook:path=/mutate-batch-x-k8s-io-v1alpha1-jobset,mutating=true,failurePolicy=fail,sideEffects=None,groups=batch.x-k8s.io,resources=jobsets,verbs=create;update,versions=v1alpha1,name=mjobset.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &JobSet{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *JobSet) Default() {
	jobsetlog.Info("default", "name", r.Name)

	for _, rjob := range r.Spec.Jobs {
		if rjob.Template.Spec.CompletionMode == nil {
			rjob.Template.Spec.CompletionMode = completionModePtr(batchv1.IndexedCompletion)
		}
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-batch-x-k8s-io-v1alpha1-jobset,mutating=false,failurePolicy=fail,sideEffects=None,groups=batch.x-k8s.io,resources=jobsets,verbs=create;update,versions=v1alpha1,name=vjobset.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &JobSet{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *JobSet) ValidateCreate() error {
	jobsetlog.Info("validate create", "name", r.Name)

	// TODO(user): fill in your validation logic upon object creation.
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *JobSet) ValidateUpdate(old runtime.Object) error {
	jobsetlog.Info("validate update", "name", r.Name)

	// TODO(user): fill in your validation logic upon object update.
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *JobSet) ValidateDelete() error {
	jobsetlog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil
}

func completionModePtr(mode batchv1.CompletionMode) *batchv1.CompletionMode {
	return &mode
}
