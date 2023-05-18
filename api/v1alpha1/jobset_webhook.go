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
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	util "sigs.k8s.io/jobset/pkg/util/collections"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

func (js *JobSet) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(js).
		Complete()
}

//+kubebuilder:webhook:path=/mutate-jobset-x-k8s-io-v1alpha1-jobset,mutating=true,failurePolicy=fail,sideEffects=None,groups=jobset.x-k8s.io,resources=jobsets,verbs=create;update,versions=v1alpha1,name=mjobset.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &JobSet{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (js *JobSet) Default() {
	// Default success policy to operator "All" targeting all replicatedJobs.
	if js.Spec.SuccessPolicy == nil {
		js.Spec.SuccessPolicy = &SuccessPolicy{Operator: OperatorAll}
	}
	for i, _ := range js.Spec.ReplicatedJobs {
		// Default job completion mode to indexed.
		if js.Spec.ReplicatedJobs[i].Template.Spec.CompletionMode == nil {
			js.Spec.ReplicatedJobs[i].Template.Spec.CompletionMode = completionModePtr(batchv1.IndexedCompletion)
		}
		// Enable DNS hostnames by default.
		if js.Spec.ReplicatedJobs[i].Network == nil {
			js.Spec.ReplicatedJobs[i].Network = &Network{}
		}
		if js.Spec.ReplicatedJobs[i].Network.EnableDNSHostnames == nil {
			js.Spec.ReplicatedJobs[i].Network.EnableDNSHostnames = pointer.Bool(true)
		}
		// Default pod restart policy to OnFailure.
		if js.Spec.ReplicatedJobs[i].Template.Spec.Template.Spec.RestartPolicy == "" {
			js.Spec.ReplicatedJobs[i].Template.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyOnFailure
		}
	}
}

//+kubebuilder:webhook:path=/validate-jobset-x-k8s-io-v1alpha1-jobset,mutating=false,failurePolicy=fail,sideEffects=None,groups=jobset.x-k8s.io,resources=jobsets,verbs=create;update,versions=v1alpha1,name=vjobset.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &JobSet{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (js *JobSet) ValidateCreate() error {
	var allErrs []error
	// Validate that replicatedJobs listed in success policy are part of this JobSet.
	validReplicatedJobs := replicatedJobNamesFromSpec(js)
	for _, rjobName := range js.Spec.SuccessPolicy.TargetReplicatedJobs {
		if !util.Contains(validReplicatedJobs, rjobName) {
			allErrs = append(allErrs, fmt.Errorf("invalid replicatedJob name '%s' does not appear in .spec.ReplicatedJobs", rjobName))
		}
	}
	return errors.Join(allErrs...)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (js *JobSet) ValidateUpdate(old runtime.Object) error {
	oldObjCopy := old.DeepCopyObject().(*JobSet)
	var allErr []error
	var updatableFields = []string{
		"`template.spec.template.spec.nodeSelector`",
	}
	for index := range js.Spec.ReplicatedJobs {
		oldObjCopy.Spec.ReplicatedJobs[index].Template.Spec.Template.Spec.NodeSelector = js.Spec.ReplicatedJobs[index].Template.Spec.Template.Spec.NodeSelector
		if !reflect.DeepEqual(oldObjCopy.Spec.ReplicatedJobs, js.Spec.ReplicatedJobs) {
			allErr = append(allErr, fmt.Errorf(
				fmt.Sprintf("%s update validation: spec.ReplicatedJobs updates may not change fields other than %s", r.Spec.ReplicatedJobs[index].Name, strings.Join(updatableFields, ", ")),
			))
		}
	}
	return errors.Join(allErr...)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (js *JobSet) ValidateDelete() error {
	return nil
}

func completionModePtr(mode batchv1.CompletionMode) *batchv1.CompletionMode {
	return &mode
}

func replicatedJobNamesFromSpec(js *JobSet) []string {
	names := []string{}
	for _, rjob := range js.Spec.ReplicatedJobs {
		names = append(names, rjob.Name)
	}
	return names
}
