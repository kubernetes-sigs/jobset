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

package webhooks

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"sigs.k8s.io/jobset/pkg/util/collections"
	"sigs.k8s.io/jobset/pkg/util/placement"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// maximum lnegth of the value of the managedBy field
const maxManagedByLength = 63

const (
	// This is the error message returned by IsDNS1035Label when the given input
	// is longer than 63 characters.
	dns1035MaxLengthExceededErrorMsg = "must be no more than 63 characters"

	// Error message returned by JobSet validation if the generated child jobs
	// will be longer than 63 characters.
	jobNameTooLongErrorMsg = "JobSet name is too long, job names generated for this JobSet will exceed 63 characters"

	// Error message returned by JobSet validation if the generated pod names
	// will be longer than 63 characters.
	podNameTooLongErrorMsg = "JobSet name is too long, pod names generated for this JobSet will exceed 63 characters"

	// Error message returned by JobSet validation if the network subdomain
	// will be longer than 63 characters.
	subdomainTooLongErrMsg = ".spec.network.subdomain is too long, must be less than 63 characters"
)

//+kubebuilder:webhook:path=/mutate-jobset-x-k8s-io-v1alpha2-jobset,mutating=true,failurePolicy=fail,sideEffects=None,groups=jobset.x-k8s.io,resources=jobsets,verbs=create;update,versions=v1alpha2,name=mjobset.kb.io,admissionReviewVersions=v1

// jobSetWebhook for defaulting and admission.
type jobSetWebhook struct {
	client  client.Client
	decoder *admission.Decoder
}

func NewJobSetWebhook(mgrClient client.Client) (*jobSetWebhook, error) {
	return &jobSetWebhook{client: mgrClient}, nil
}

// InjectDecoder injects the decoder into the jobSetWebhook.
func (j *jobSetWebhook) InjectDecoder(d *admission.Decoder) error {
	j.decoder = d
	return nil
}

func (j *jobSetWebhook) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&jobset.JobSet{}).
		WithDefaulter(j).
		WithValidator(j).
		Complete()
}

func (j *jobSetWebhook) Default(ctx context.Context, obj runtime.Object) error {
	js, ok := obj.(*jobset.JobSet)
	if !ok {
		return nil
	}
	// Default success policy to operator "All" targeting all replicatedJobs.
	if js.Spec.SuccessPolicy == nil {
		js.Spec.SuccessPolicy = &jobset.SuccessPolicy{Operator: jobset.OperatorAll}
	}
	if js.Spec.StartupPolicy == nil {
		js.Spec.StartupPolicy = &jobset.StartupPolicy{StartupPolicyOrder: jobset.AnyOrder}
	}
	for i := range js.Spec.ReplicatedJobs {
		// Default job completion mode to indexed.
		if js.Spec.ReplicatedJobs[i].Template.Spec.CompletionMode == nil {
			js.Spec.ReplicatedJobs[i].Template.Spec.CompletionMode = completionModePtr(batchv1.IndexedCompletion)
		}
		// Default pod restart policy to OnFailure.
		if js.Spec.ReplicatedJobs[i].Template.Spec.Template.Spec.RestartPolicy == "" {
			js.Spec.ReplicatedJobs[i].Template.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyOnFailure
		}
	}

	// Enable DNS hostnames by default.
	if js.Spec.Network == nil {
		js.Spec.Network = &jobset.Network{}
	}
	if js.Spec.Network.EnableDNSHostnames == nil {
		js.Spec.Network.EnableDNSHostnames = ptr.To(true)
	}
	if js.Spec.Network.PublishNotReadyAddresses == nil {
		js.Spec.Network.PublishNotReadyAddresses = ptr.To(true)
	}

	return nil
}

//+kubebuilder:webhook:path=/validate-jobset-x-k8s-io-v1alpha2-jobset,mutating=false,failurePolicy=fail,sideEffects=None,groups=jobset.x-k8s.io,resources=jobsets,verbs=create;update,versions=v1alpha2,name=vjobset.kb.io,admissionReviewVersions=v1

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (j *jobSetWebhook) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	js, ok := obj.(*jobset.JobSet)
	if !ok {
		return nil, fmt.Errorf("expected a JobSet but got a %T", obj)
	}

	var allErrs []error
	// Validate that replicatedJobs listed in success policy are part of this JobSet.
	validReplicatedJobs := replicatedJobNamesFromSpec(js)

	// Ensure that a provided subdomain is a valid DNS name
	if js.Spec.Network != nil && js.Spec.Network.Subdomain != "" {

		// This can return 1 or 2 errors, validating max length and format
		for _, errMessage := range validation.IsDNS1123Subdomain(js.Spec.Network.Subdomain) {
			allErrs = append(allErrs, fmt.Errorf(errMessage))
		}

		// Since subdomain name is also used as service name, it must adhere to RFC 1035 as well.
		for _, errMessage := range validation.IsDNS1035Label(js.Spec.Network.Subdomain) {
			if strings.Contains(errMessage, dns1035MaxLengthExceededErrorMsg) {
				errMessage = subdomainTooLongErrMsg
			}
			allErrs = append(allErrs, fmt.Errorf(errMessage))
		}
	}

	// Validate the managedBy field used for multi-kueue support.
	if js.Spec.ManagedBy != nil {
		manager := *js.Spec.ManagedBy
		fieldPath := field.NewPath("spec", "managedBy")
		for _, err := range validation.IsDomainPrefixedPath(fieldPath, manager) {
			allErrs = append(allErrs, err)
		}
		if len(manager) > maxManagedByLength {
			allErrs = append(allErrs, field.TooLongMaxLength(fieldPath, manager, maxManagedByLength))
		}
	}

	// Validate each replicatedJob.
	for _, rjob := range js.Spec.ReplicatedJobs {
		var parallelism int32 = 1
		if rjob.Template.Spec.Parallelism != nil {
			parallelism = *rjob.Template.Spec.Parallelism
		}
		if int64(parallelism)*int64(rjob.Replicas) > math.MaxInt32 {
			allErrs = append(allErrs, fmt.Errorf("the product of replicas and parallelism must not exceed %d for replicatedJob '%s'", math.MaxInt32, rjob.Name))
		}

		// Check that the generated job names for this replicated job will be DNS 1035 compliant.
		// Use the largest job index as it will have the longest name.
		longestJobName := placement.GenJobName(js.Name, rjob.Name, int(rjob.Replicas-1))
		for _, errMessage := range validation.IsDNS1035Label(longestJobName) {
			if strings.Contains(errMessage, dns1035MaxLengthExceededErrorMsg) {
				errMessage = jobNameTooLongErrorMsg
			}
			allErrs = append(allErrs, fmt.Errorf(errMessage))
		}
		// Check that the generated pod names for the replicated job is DNS 1035 compliant.
		isIndexedJob := rjob.Template.Spec.CompletionMode != nil && *rjob.Template.Spec.CompletionMode == batchv1.IndexedCompletion
		if isIndexedJob && rjob.Template.Spec.Completions != nil {
			maxJobIndex := strconv.Itoa(int(rjob.Replicas - 1))
			maxPodIndex := strconv.Itoa(int(*rjob.Template.Spec.Completions - 1))
			// Add 5 char suffix to the deterministic part of the pod name to validate the full pod name is compliant.
			longestPodName := placement.GenPodName(js.Name, rjob.Name, maxJobIndex, maxPodIndex) + "-abcde"
			for _, errMessage := range validation.IsDNS1035Label(longestPodName) {
				if strings.Contains(errMessage, dns1035MaxLengthExceededErrorMsg) {
					errMessage = podNameTooLongErrorMsg
				}
				allErrs = append(allErrs, fmt.Errorf(errMessage))
			}
		}
	}

	// Validate the success policy's target replicated jobs are valid.
	for _, rjobName := range js.Spec.SuccessPolicy.TargetReplicatedJobs {
		if !collections.Contains(validReplicatedJobs, rjobName) {
			allErrs = append(allErrs, fmt.Errorf("invalid replicatedJob name '%s' does not appear in .spec.ReplicatedJobs", rjobName))
		}
	}
	return nil, errors.Join(allErrs...)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (j *jobSetWebhook) ValidateUpdate(ctx context.Context, old, newObj runtime.Object) (admission.Warnings, error) {
	js, ok := newObj.(*jobset.JobSet)
	if !ok {
		return nil, fmt.Errorf("expected a JobSet but got a %T", newObj)
	}
	oldJS, ok := old.(*jobset.JobSet)
	if !ok {
		return nil, fmt.Errorf("expected a JobSet from old object but got a %T", old)
	}
	mungedSpec := js.Spec.DeepCopy()
	if ptr.Deref(oldJS.Spec.Suspend, false) {
		for index := range js.Spec.ReplicatedJobs {
			mungedSpec.ReplicatedJobs[index].Template.Spec.Template.Spec.NodeSelector = oldJS.Spec.ReplicatedJobs[index].Template.Spec.Template.Spec.NodeSelector
		}
	}
	// Note that SucccessPolicy and failurePolicy are made immutable via CEL.
	errs := apivalidation.ValidateImmutableField(mungedSpec.ReplicatedJobs, oldJS.Spec.ReplicatedJobs, field.NewPath("spec").Child("replicatedJobs"))
	errs = append(errs, apivalidation.ValidateImmutableField(mungedSpec.ManagedBy, oldJS.Spec.ManagedBy, field.NewPath("spec").Child("labels").Key("managedBy"))...)
	return nil, errs.ToAggregate()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (j *jobSetWebhook) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func completionModePtr(mode batchv1.CompletionMode) *batchv1.CompletionMode {
	return &mode
}

func replicatedJobNamesFromSpec(js *jobset.JobSet) []string {
	names := []string{}
	for _, rjob := range js.Spec.ReplicatedJobs {
		names = append(names, rjob.Name)
	}
	return names
}
