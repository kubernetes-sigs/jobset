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
	"regexp"
	"slices"
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

// validOnJobFailureReasons stores supported values of the reason field of the condition of
// a failed job. See https://github.com/kubernetes/api/blob/2676848ed8201866119a94759a2d525ffc7396c0/batch/v1/types.go#L632
// for more details.
var validOnJobFailureReasons = []string{
	batchv1.JobReasonBackoffLimitExceeded,
	batchv1.JobReasonDeadlineExceeded,
	batchv1.JobReasonFailedIndexes,
	batchv1.JobReasonMaxFailedIndexesExceeded,
	batchv1.JobReasonPodFailurePolicy,
}

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

const defaultRuleNameFmt = "failurePolicyRule%v"

// Default performs defaulting of jobset values as defined in the JobSet API.
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

	// Apply the default failure policy rule name policy.
	if js.Spec.FailurePolicy != nil {
		for i := range js.Spec.FailurePolicy.Rules {
			rule := &js.Spec.FailurePolicy.Rules[i]
			if len(rule.Name) == 0 {
				rule.Name = fmt.Sprintf(defaultRuleNameFmt, i)
			}
		}
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
			allErrs = append(allErrs, errors.New(errMessage))
		}

		// Since subdomain name is also used as service name, it must adhere to RFC 1035 as well.
		for _, errMessage := range validation.IsDNS1035Label(js.Spec.Network.Subdomain) {
			if strings.Contains(errMessage, dns1035MaxLengthExceededErrorMsg) {
				errMessage = subdomainTooLongErrMsg
			}
			allErrs = append(allErrs, errors.New(errMessage))
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

	// Map where key is ReplicatedJob name.
	replicatedJobNames := map[string]bool{}

	// Validate each replicatedJob.
	for _, rJob := range js.Spec.ReplicatedJobs {
		var parallelism int32 = 1
		if rJob.Template.Spec.Parallelism != nil {
			parallelism = *rJob.Template.Spec.Parallelism
		}
		if int64(parallelism)*int64(rJob.Replicas) > math.MaxInt32 {
			allErrs = append(allErrs, fmt.Errorf("the product of replicas and parallelism must not exceed %d for replicatedJob '%s'", math.MaxInt32, rJob.Name))
		}

		// Check that the generated job names for this replicated job will be DNS 1035 compliant.
		// Use the largest job index as it will have the longest name.
		longestJobName := placement.GenJobName(js.Name, rJob.Name, int(rJob.Replicas-1))
		for _, errMessage := range validation.IsDNS1035Label(longestJobName) {
			if strings.Contains(errMessage, dns1035MaxLengthExceededErrorMsg) {
				errMessage = jobNameTooLongErrorMsg
			}
			allErrs = append(allErrs, errors.New(errMessage))
		}
		// Check that the generated pod names for the replicated job is DNS 1035 compliant.
		isIndexedJob := rJob.Template.Spec.CompletionMode != nil && *rJob.Template.Spec.CompletionMode == batchv1.IndexedCompletion
		if isIndexedJob && rJob.Template.Spec.Completions != nil {
			maxJobIndex := strconv.Itoa(int(rJob.Replicas - 1))
			maxPodIndex := strconv.Itoa(int(*rJob.Template.Spec.Completions - 1))
			// Add 5 char suffix to the deterministic part of the pod name to validate the full pod name is compliant.
			longestPodName := placement.GenPodName(js.Name, rJob.Name, maxJobIndex, maxPodIndex) + "-abcde"
			for _, errMessage := range validation.IsDNS1035Label(longestPodName) {
				if strings.Contains(errMessage, dns1035MaxLengthExceededErrorMsg) {
					errMessage = podNameTooLongErrorMsg
				}
				allErrs = append(allErrs, errors.New(errMessage))
			}
		}
		replicatedJobNames[rJob.Name] = true
		// Check that DependsOn references the previous ReplicatedJob.
		if rJob.DependsOn != nil {
			_, ok := replicatedJobNames[rJob.DependsOn[0].Name]
			if !ok {
				allErrs = append(allErrs, fmt.Errorf("replicatedJob: %s cannot depend on replicatedJob: %s", rJob.Name, rJob.DependsOn[0].Name))
			}
		}
	}

	// Validate the success policy's target replicated jobs are valid.
	for _, rjobName := range js.Spec.SuccessPolicy.TargetReplicatedJobs {
		if !slices.Contains(validReplicatedJobs, rjobName) {
			allErrs = append(allErrs, fmt.Errorf("invalid replicatedJob name '%s' does not appear in .spec.ReplicatedJobs", rjobName))
		}
	}

	// Validate failure policy
	if js.Spec.FailurePolicy != nil {
		failurePolicyErrors := validateFailurePolicy(js.Spec.FailurePolicy, validReplicatedJobs)
		allErrs = append(allErrs, failurePolicyErrors...)
	}

	// Validate coordinator, if set.
	if js.Spec.Coordinator != nil {
		allErrs = append(allErrs, validateCoordinator(js))
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

	// Allow pod template to be mutated for suspended JobSets, or JobSets getting suspended.
	// This is needed for integration with Kueue/DWS.
	if ptr.Deref(oldJS.Spec.Suspend, false) || ptr.Deref(js.Spec.Suspend, false) {
		for index := range js.Spec.ReplicatedJobs {
			// Pod values which must be mutable for Kueue are defined here: https://github.com/kubernetes-sigs/kueue/blob/a50d395c36a2cb3965be5232162cf1fded1bdb08/apis/kueue/v1beta1/workload_types.go#L256-L260
			mungedSpec.ReplicatedJobs[index].Template.Spec.Template.Annotations = oldJS.Spec.ReplicatedJobs[index].Template.Spec.Template.Annotations
			mungedSpec.ReplicatedJobs[index].Template.Spec.Template.Labels = oldJS.Spec.ReplicatedJobs[index].Template.Spec.Template.Labels
			mungedSpec.ReplicatedJobs[index].Template.Spec.Template.Spec.NodeSelector = oldJS.Spec.ReplicatedJobs[index].Template.Spec.Template.Spec.NodeSelector
			mungedSpec.ReplicatedJobs[index].Template.Spec.Template.Spec.Tolerations = oldJS.Spec.ReplicatedJobs[index].Template.Spec.Template.Spec.Tolerations

			// Pod Scheduling Gates can be updated for batch/v1 Job: https://github.com/kubernetes/kubernetes/blob/ceb58a4dbc671b9d0a2de6d73a1616bc0c299863/pkg/apis/batch/validation/validation.go#L662
			mungedSpec.ReplicatedJobs[index].Template.Spec.Template.Spec.SchedulingGates = oldJS.Spec.ReplicatedJobs[index].Template.Spec.Template.Spec.SchedulingGates
		}
	}

	// Note that SucccessPolicy and failurePolicy are made immutable via CEL.
	errs := apivalidation.ValidateImmutableField(mungedSpec.ReplicatedJobs, oldJS.Spec.ReplicatedJobs, field.NewPath("spec").Child("replicatedJobs"))
	errs = append(errs, apivalidation.ValidateImmutableField(mungedSpec.ManagedBy, oldJS.Spec.ManagedBy, field.NewPath("spec").Child("managedBy"))...)
	return nil, errs.ToAggregate()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (j *jobSetWebhook) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// Failure policy constants.
const (
	minRuleNameLength = 1
	maxRuleNameLength = 128
	ruleNameFmt       = "^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$"
)

// ruleNameRegexp is the regular expression that failure policy rules must match.
var ruleNameRegexp = regexp.MustCompile(ruleNameFmt)

// validateFailurePolicy performs validation for jobset failure policies and returns all errors detected.
func validateFailurePolicy(failurePolicy *jobset.FailurePolicy, validReplicatedJobs []string) []error {
	var allErrs []error
	if failurePolicy == nil {
		return allErrs
	}

	// ruleNameToRulesWithName is used to verify that rule names are unique
	ruleNameToRulesWithName := make(map[string][]int)
	for index, rule := range failurePolicy.Rules {
		// Check that the rule name meets the minimum length
		nameLen := len(rule.Name)
		if !(minRuleNameLength <= nameLen && nameLen <= maxRuleNameLength) {
			err := fmt.Errorf("invalid failure policy rule name of length %v, the rule name must be at least %v characters long and at most %v characters long", nameLen, minRuleNameLength, maxRuleNameLength)
			allErrs = append(allErrs, err)
		}

		ruleNameToRulesWithName[rule.Name] = append(ruleNameToRulesWithName[rule.Name], index)

		if !ruleNameRegexp.MatchString(rule.Name) {
			err := fmt.Errorf("invalid failure policy rule name '%v', a failure policy rule name must start with an alphabetic character, optionally followed by a string of alphanumeric characters or '_,:', and must end with an alphanumeric character or '_'", rule.Name)
			allErrs = append(allErrs, err)
		}

		// Validate the rules target replicated jobs are valid
		for _, rjobName := range rule.TargetReplicatedJobs {
			if !slices.Contains(validReplicatedJobs, rjobName) {
				allErrs = append(allErrs, fmt.Errorf("invalid replicatedJob name '%s' in failure policy does not appear in .spec.ReplicatedJobs", rjobName))
			}
		}

		// Validate the rules on job failure reasons are valid
		for _, failureReason := range rule.OnJobFailureReasons {
			if !slices.Contains(validOnJobFailureReasons, failureReason) {
				allErrs = append(allErrs, fmt.Errorf("invalid job failure reason '%s' in failure policy is not a recognized job failure reason", failureReason))
			}
		}
	}

	// Checking that rule names are unique
	for ruleName, rulesWithName := range ruleNameToRulesWithName {
		if len(rulesWithName) > 1 {
			err := fmt.Errorf("rule names are not unique, rules with indices %v all have the same name '%v'", rulesWithName, ruleName)
			allErrs = append(allErrs, err)
		}
	}

	return allErrs
}

// validateCoordinator validates the following:
// 1. coordinator replicatedJob is a valid replicatedJob in the JobSet spec.
// 2. coordinator jobIndex is a valid index for the replicatedJob.
// 3. coordinator podIndex is a valid pod index for the job.
func validateCoordinator(js *jobset.JobSet) error {
	// Validate replicatedJob.
	replicatedJob := replicatedJobByName(js, js.Spec.Coordinator.ReplicatedJob)
	if replicatedJob == nil {
		return fmt.Errorf("coordinator replicatedJob %s does not exist", js.Spec.Coordinator.ReplicatedJob)
	}

	// Validate Job index.
	if js.Spec.Coordinator.JobIndex < 0 || js.Spec.Coordinator.JobIndex >= int(replicatedJob.Replicas) {
		return fmt.Errorf("coordinator job index %d is invalid for replicatedJob %s", js.Spec.Coordinator.JobIndex, replicatedJob.Name)
	}

	// Validate job is using indexed completion mode.
	if replicatedJob.Template.Spec.CompletionMode == nil || *replicatedJob.Template.Spec.CompletionMode != batchv1.IndexedCompletion {
		return fmt.Errorf("job for coordinator pod must be indexed completion mode")
	}

	// Validate Pod index.
	if js.Spec.Coordinator.PodIndex < 0 || js.Spec.Coordinator.PodIndex >= int(*replicatedJob.Template.Spec.Completions) {
		return fmt.Errorf("coordinator pod index %d is invalid for replicatedJob %s job index %d", js.Spec.Coordinator.PodIndex, js.Spec.Coordinator.ReplicatedJob, js.Spec.Coordinator.JobIndex)
	}
	return nil
}

// replicatedJobByName fetches the replicatedJob spec from the JobSet by name.
// Returns nil if no replicatedJob with the given name exists.
func replicatedJobByName(js *jobset.JobSet, replicatedJob string) *jobset.ReplicatedJob {
	for _, rjob := range js.Spec.ReplicatedJobs {
		if rjob.Name == replicatedJob {
			return &rjob
		}
	}
	return nil
}

// replicatedJobNamesFromSpec parses the JobSet spec and returns a list of
// the replicatedJob names.
func replicatedJobNamesFromSpec(js *jobset.JobSet) []string {
	names := []string{}
	for _, rjob := range js.Spec.ReplicatedJobs {
		names = append(names, rjob.Name)
	}
	return names
}

func completionModePtr(mode batchv1.CompletionMode) *batchv1.CompletionMode {
	return &mode
}
