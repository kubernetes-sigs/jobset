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

package webhooks

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	schedulingv1alpha3 "k8s.io/api/scheduling/v1alpha3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
	"sigs.k8s.io/jobset/pkg/controllers"
	"sigs.k8s.io/jobset/pkg/features"
	jobsetutil "sigs.k8s.io/jobset/pkg/util"
	"sigs.k8s.io/jobset/pkg/util/placement"
)

// maximum length of the value of the managedBy field
const maxManagedByLength = 63
const maxVolumeClaimLength = 63

const (
	// This is the error message returned by IsDNS1035Label when the given input
	// is longer than 63 characters.
	dns1035MaxLengthExceededErrorMsg = "must be no more than 63 characters"

	// Error message returned by JobSet validation if the group name
	// will be longer than 63 characters.
	groupNameTooLongErrorMsg = ".spec.replicatedJob[].groupName is too long, must be less than 63 characters"

	// Error message returned by JobSet validation if the generated child jobs
	// will be longer than 63 characters.
	jobNameTooLongErrorMsg = "JobSet name is too long, job names generated for this JobSet will exceed 63 characters"

	// Error message returned by JobSet validation if the generated pod names
	// will be longer than 63 characters.
	podNameTooLongErrorMsg = "JobSet name is too long, pod names generated for this JobSet will exceed 63 characters"

	// Error message returned by JobSet validation if the network subdomain
	// will be longer than 63 characters.
	subdomainTooLongErrMsg = ".spec.network.subdomain is too long, must be less than 63 characters"

	// maxReplicasPerReplicatedJob limits the number of replicas when using the RestartJob action.
	// The limit is based on the 1024 MaxItems of the JobRestarts field in ReplicatedJobStatus.
	// See api/jobset/v1alpha2/jobset_types.go for more details.
	maxReplicasPerReplicatedJob = 1024

	// Default rule name for FailurePolicy
	defaultRuleNameFmt = "failurePolicyRule%v"
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

//+kubebuilder:webhook:path=/mutate-jobset-x-k8s-io-v1alpha2-jobset,mutating=true,failurePolicy=fail,sideEffects=None,groups=jobset.x-k8s.io,resources=jobsets,verbs=create,versions=v1alpha2,name=mjobset.kb.io,admissionReviewVersions=v1

// jobSetWebhook for defaulting and admission of JobSet.
type jobSetWebhook struct {
	client client.Client
}

var _ admission.Defaulter[*jobset.JobSet] = (*jobSetWebhook)(nil)
var _ admission.Validator[*jobset.JobSet] = (*jobSetWebhook)(nil)

func setupWebhookForJobSet(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &jobset.JobSet{}).
		WithDefaulter(&jobSetWebhook{client: mgr.GetClient()}).
		WithValidator(&jobSetWebhook{client: mgr.GetClient()}).
		Complete()
}

// Default performs defaulting of jobset values as defined in the JobSet API.
func (j *jobSetWebhook) Default(ctx context.Context, js *jobset.JobSet) error {
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
			js.Spec.ReplicatedJobs[i].Template.Spec.CompletionMode = ptr.To(batchv1.IndexedCompletion)
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

	// Apply the default retention policy for the VolumeClaimPolicies.
	for i, policy := range js.Spec.VolumeClaimPolicies {
		if policy.RetentionPolicy == nil {
			js.Spec.VolumeClaimPolicies[i].RetentionPolicy = &jobset.VolumeRetentionPolicy{
				WhenDeleted: ptr.To(jobset.RetentionPolicyDelete),
			}
		}
	}

	// Scheduling defaulting is gated behind JobSetWorkloadAwareSchedulingAPI so
	// that a JobSet with spec.scheduling set (which ValidateCreate rejects when
	// the gate is disabled) is not otherwise mutated by this webhook.
	if features.Enabled(features.JobSetWorkloadAwareSchedulingAPI) {
		// Default scheduling policy to Gang when scheduling block is present but
		// policy is nil. Sequenced startup uses one PodGroup per ReplicatedJob, so
		// it intentionally leaves the composite policy unset and lets the builder
		// apply the per-ReplicatedJob Gang defaults.
		if js.Spec.Scheduling != nil && js.Spec.Scheduling.SchedulingPolicy == nil && !controllers.HasSequencedStartup(js) {
			js.Spec.Scheduling.SchedulingPolicy = &schedulingv1alpha3.PodGroupSchedulingPolicy{
				Gang: &schedulingv1alpha3.GangSchedulingPolicy{},
			}
		}
		defaultSchedulingGangMinCounts(js)
	}

	return nil
}

//+kubebuilder:webhook:path=/validate-jobset-x-k8s-io-v1alpha2-jobset,mutating=false,failurePolicy=fail,sideEffects=None,groups=jobset.x-k8s.io,resources=jobsets,verbs=create;update,versions=v1alpha2,name=vjobset.kb.io,admissionReviewVersions=v1

// defaultSchedulingGangMinCounts fills required gang minCount fields with the
// number of pods represented by the corresponding JobSet or ReplicatedJob.
//
// Each defaulted value is recorded via an annotation (see
// constants.GangMinCountAutoDefaultedKey and friends) marking it as derived
// rather than user-specified. The mutating webhook only runs on Create, so
// without that marker a value defaulted here would go stale once
// ElasticJobSet scaling later changes parallelism/completions; the
// controller uses the marker to keep recomputing it from the live
// ReplicatedJobs on every reconcile instead.
func defaultSchedulingGangMinCounts(js *jobset.JobSet) {
	scheduling := js.Spec.Scheduling
	if scheduling == nil {
		return
	}

	if scheduling.SchedulingPolicy != nil && scheduling.SchedulingPolicy.Gang != nil && scheduling.SchedulingPolicy.Gang.MinCount == 0 &&
		(!controllers.HasSequencedStartup(js) || len(scheduling.ReplicatedJobPolicies) > 0) {
		scheduling.SchedulingPolicy.Gang.MinCount = jobsetutil.TotalReplicatedJobPodCount(js.Spec.ReplicatedJobs)
		setAnnotation(js, constants.GangMinCountAutoDefaultedKey, "true")
	}

	rjobsByName := make(map[string]*jobset.ReplicatedJob, len(js.Spec.ReplicatedJobs))
	for i := range js.Spec.ReplicatedJobs {
		rjobsByName[js.Spec.ReplicatedJobs[i].Name] = &js.Spec.ReplicatedJobs[i]
	}

	for i := range scheduling.ReplicatedJobPolicies {
		policy := &scheduling.ReplicatedJobPolicies[i]

		// jobSchedulingPolicy (Gang-of-Gangs per-Job model) sizes its Gang
		// minCount to the single targeted ReplicatedJob's own per-Job
		// parallelism, not the summed pod count across every replica.
		if policy.JobSchedulingPolicy != nil {
			jsp := policy.JobSchedulingPolicy
			if jsp.SchedulingPolicy != nil && jsp.SchedulingPolicy.Gang != nil && jsp.SchedulingPolicy.Gang.MinCount == 0 &&
				len(policy.TargetReplicatedJob) == 1 {
				if rjob, ok := rjobsByName[policy.TargetReplicatedJob[0]]; ok {
					jsp.SchedulingPolicy.Gang.MinCount = jobsetutil.JobParallelism(rjob)
					appendAutoDefaultedName(js, constants.JobGangMinCountAutoDefaultedKey, rjob.Name)
				}
			}
			continue
		}

		if policy.SchedulingPolicy == nil || policy.SchedulingPolicy.Gang == nil || policy.SchedulingPolicy.Gang.MinCount != 0 {
			continue
		}
		var minCount int32
		for _, name := range policy.TargetReplicatedJob {
			if rjob, ok := rjobsByName[name]; ok {
				minCount += replicatedJobPodCount(rjob)
			}
		}
		policy.SchedulingPolicy.Gang.MinCount = minCount
		appendAutoDefaultedName(js, constants.ReplicatedJobGangMinCountAutoDefaultedKey, controllers.SchedulingGroupName(policy.TargetReplicatedJob))
	}
}

// setAnnotation sets a single annotation value on the JobSet, initializing
// the annotations map if necessary.
func setAnnotation(js *jobset.JobSet, key, value string) {
	if js.Annotations == nil {
		js.Annotations = make(map[string]string)
	}
	js.Annotations[key] = value
}

// appendAutoDefaultedName adds name to the comma-separated list stored under
// the given annotation key, used to track which replicatedJobPolicies
// group/ReplicatedJob names had their leaf Gang.MinCount auto-derived (see
// defaultSchedulingGangMinCounts). Defaulting only runs on Create, so this
// only needs to guard against duplicate names within a single pass.
func appendAutoDefaultedName(js *jobset.JobSet, key, name string) {
	existing := js.Annotations[key]
	if existing == "" {
		setAnnotation(js, key, name)
		return
	}
	if slices.Contains(strings.Split(existing, ","), name) {
		return
	}
	setAnnotation(js, key, existing+","+name)
}

func replicatedJobPodCount(rjob *jobset.ReplicatedJob) int32 {
	return jobsetutil.ReplicatedJobPodCount(rjob)
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (j *jobSetWebhook) ValidateCreate(ctx context.Context, js *jobset.JobSet) (admission.Warnings, error) {
	var allErrs []error
	jobSetNameForValidation := getJobSetNameForValidation(js)

	// Validate InPlaceRestart feature gate.
	// The in-place restart API should be used only when the feature gate is enabled.
	if !features.Enabled(features.InPlaceRestart) {
		if js.Spec.FailurePolicy != nil && js.Spec.FailurePolicy.RestartStrategy == jobset.InPlaceRestart {
			allErrs = append(allErrs, fmt.Errorf("InPlaceRestart restart strategy cannot be set when InPlaceRestart feature gate is disabled"))
		}
	}

	// Validate that depends On can't be set for the first replicated job.
	if len(js.Spec.ReplicatedJobs) > 0 && js.Spec.ReplicatedJobs[0].DependsOn != nil {
		allErrs = append(allErrs, fmt.Errorf("DependsOn can't be set for the first ReplicatedJob"))
	}

	// Ensure that a provided subdomain is a valid DNS name
	if js.Spec.Network != nil && js.Spec.Network.Subdomain != "" {
		fieldPath := field.NewPath("spec", "network", "subdomain")
		// This can return 1 or 2 errors, validating max length and format
		for _, errMessage := range validation.IsDNS1123Subdomain(js.Spec.Network.Subdomain) {
			allErrs = append(allErrs, field.Invalid(fieldPath, js.Spec.Network.Subdomain, errMessage))
		}

		// Since subdomain name is also used as service name, it must adhere to RFC 1035 as well.
		for _, errMessage := range validation.IsDNS1035Label(js.Spec.Network.Subdomain) {
			if strings.Contains(errMessage, dns1035MaxLengthExceededErrorMsg) {
				errMessage = subdomainTooLongErrMsg
			}

			allErrs = append(allErrs, field.Invalid(fieldPath, js.Spec.Network.Subdomain, errMessage))
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

	rJobNames := sets.New[string]()

	// Validate each replicatedJob.
	for rJobIdx, rJob := range js.Spec.ReplicatedJobs {
		fieldPath := field.NewPath("spec", "replicatedJobs").Index(rJobIdx)
		rJobNames.Insert(rJob.Name)

		var parallelism int32 = 1
		if rJob.Template.Spec.Parallelism != nil {
			parallelism = *rJob.Template.Spec.Parallelism
		}
		if int64(parallelism)*int64(rJob.Replicas) > math.MaxInt32 {
			allErrs = append(allErrs, fmt.Errorf("the product of replicas and parallelism must not exceed %d for replicatedJob '%s'", math.MaxInt32, rJob.Name))
		}

		// Check that the group name is DNS 1035 compliant.
		for _, errMessage := range validation.IsDNS1035Label(rJob.GroupName) {
			if strings.Contains(errMessage, dns1035MaxLengthExceededErrorMsg) {
				errMessage = groupNameTooLongErrorMsg
			}
			allErrs = append(allErrs, field.Invalid(fieldPath.Child("groupName"), rJob.GroupName, errMessage))
		}
		// Check that the generated job names for this replicated job will be DNS 1035 compliant.
		// Use the largest job index as it will have the longest name.
		longestJobName := placement.GenJobName(jobSetNameForValidation, rJob.Name, int(rJob.Replicas-1))
		for _, errMessage := range validation.IsDNS1035Label(longestJobName) {
			if strings.Contains(errMessage, dns1035MaxLengthExceededErrorMsg) {
				errMessage = jobNameTooLongErrorMsg
			}
			allErrs = append(allErrs, field.Invalid(fieldPath.Child("name"), longestJobName, errMessage))
		}
		// Check that the generated pod names for the replicated job is DNS 1035 compliant.
		isIndexedJob := rJob.Template.Spec.CompletionMode != nil && *rJob.Template.Spec.CompletionMode == batchv1.IndexedCompletion
		if isIndexedJob && rJob.Template.Spec.Completions != nil {
			maxJobIndex := strconv.Itoa(int(rJob.Replicas - 1))
			maxPodIndex := strconv.Itoa(int(*rJob.Template.Spec.Completions - 1))
			// Add 5 char suffix to the deterministic part of the pod name to validate the full pod name is compliant.
			longestPodName := placement.GenPodName(jobSetNameForValidation, rJob.Name, maxJobIndex, maxPodIndex) + "-abcde"
			for _, errMessage := range validation.IsDNS1035Label(longestPodName) {
				if strings.Contains(errMessage, dns1035MaxLengthExceededErrorMsg) {
					errMessage = podNameTooLongErrorMsg
				}
				allErrs = append(allErrs, field.Invalid(fieldPath.Child("name"), longestJobName, errMessage))
			}
		}

		// Check that DependsOn references the previous ReplicatedJob.
		for _, dependOnItem := range rJob.DependsOn {
			if !rJobNames.Has(dependOnItem.Name) {
				allErrs = append(allErrs, fmt.Errorf("replicatedJob: %s cannot depend on replicatedJob: %s", rJob.Name, dependOnItem.Name))
			}
		}

		// Validate in-place restart.
		if features.Enabled(features.InPlaceRestart) && js.Spec.FailurePolicy != nil && js.Spec.FailurePolicy.RestartStrategy == jobset.InPlaceRestart {
			// Validate that the backoff limit is set to max int32.
			if rJob.Template.Spec.BackoffLimit == nil || *rJob.Template.Spec.BackoffLimit != math.MaxInt32 {
				allErrs = append(allErrs, field.Invalid(fieldPath.Child("template", "spec", "backoffLimit"), rJob.Template.Spec.BackoffLimit, fmt.Sprintf("replicatedJob %s: must be set to %d (MaxInt32) when in-place restart is enabled", rJob.Name, math.MaxInt32)))
			}

			// Validate that the pod replacement policy is set to Failed.
			if rJob.Template.Spec.PodReplacementPolicy == nil || *rJob.Template.Spec.PodReplacementPolicy != batchv1.Failed {
				allErrs = append(allErrs, field.Invalid(fieldPath.Child("template", "spec", "podReplacementPolicy"), rJob.Template.Spec.PodReplacementPolicy, fmt.Sprintf("replicatedJob %s: must be set to %s when in-place restart is enabled", rJob.Name, batchv1.Failed)))
			}

			// Validate that completions is equal to parallelism.
			if rJob.Template.Spec.Completions == nil || rJob.Template.Spec.Parallelism == nil || *rJob.Template.Spec.Completions != *rJob.Template.Spec.Parallelism {
				allErrs = append(allErrs, field.Invalid(fieldPath.Child("template", "spec", "completions"), rJob.Template.Spec.Completions, fmt.Sprintf("replicatedJob %s: completions and parallelism must be set and equal to each other when in-place restart is enabled", rJob.Name)))
			}
		}
	}

	// Validate the success policy's target replicated jobs are valid.
	for _, rJobName := range js.Spec.SuccessPolicy.TargetReplicatedJobs {
		if !rJobNames.Has(rJobName) {
			allErrs = append(allErrs, fmt.Errorf("invalid replicatedJob name '%s' does not appear in .spec.ReplicatedJobs", rJobName))
		}
	}

	// Validate failure policy
	if js.Spec.FailurePolicy != nil {
		failurePolicyErrors := validateFailurePolicy(js, rJobNames)
		allErrs = append(allErrs, failurePolicyErrors...)
	}

	// Validate coordinator, if set.
	if js.Spec.Coordinator != nil {
		allErrs = append(allErrs, validateCoordinator(js))
		allErrs = append(allErrs, validateCoordinatorLabelValue(js, jobSetNameForValidation))
	}

	// Validate VolumeClaimPolicies, if set.
	if len(js.Spec.VolumeClaimPolicies) > 0 {
		allErrs = append(allErrs, j.validateVolumeClaimPolicies(ctx, js, jobSetNameForValidation, js.Spec.VolumeClaimPolicies)...)
	}

	// Validate scheduling configuration.
	allErrs = append(allErrs, validateScheduling(ctx, js, rJobNames)...)

	return nil, invalidError(js.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (j *jobSetWebhook) ValidateUpdate(ctx context.Context, oldJs, newJs *jobset.JobSet) (admission.Warnings, error) {
	mungedSpec := newJs.Spec.DeepCopy()
	var errs field.ErrorList

	// Create a map of old ReplicatedJobs by name for safe lookup
	oldJobsMap := make(map[string]*jobset.ReplicatedJob)
	for i := range oldJs.Spec.ReplicatedJobs {
		rjob := &oldJs.Spec.ReplicatedJobs[i]
		oldJobsMap[rjob.Name] = rjob
	}

	// Elastic JobSet Validation
	if features.Enabled(features.ElasticJobSet) {
		// Check if the JobSet is in a terminal state (Completed or Failed)
		isTerminal := meta.IsStatusConditionTrue(oldJs.Status.Conditions, string(jobset.JobSetCompleted)) ||
			meta.IsStatusConditionTrue(oldJs.Status.Conditions, string(jobset.JobSetFailed))

		for index := range newJs.Spec.ReplicatedJobs {
			newRJob := &newJs.Spec.ReplicatedJobs[index]

			// Safely grab the old replicated job by Name
			oldRJob, exists := oldJobsMap[newRJob.Name]
			if !exists {
				continue // Skip if this is a new job
			}

			rJobPath := field.NewPath("spec", "replicatedJobs").Index(index).Child("template", "spec")

			// Only allow elastic scaling for Indexed jobs.
			isIndexedJob := newRJob.Template.Spec.CompletionMode != nil && *newRJob.Template.Spec.CompletionMode == batchv1.IndexedCompletion

			if isIndexedJob {
				// Check if parallelism and completions have changed
				parallelismChanged := !ptr.Equal(newRJob.Template.Spec.Parallelism, oldRJob.Template.Spec.Parallelism)
				completionsChanged := !ptr.Equal(newRJob.Template.Spec.Completions, oldRJob.Template.Spec.Completions)

				if parallelismChanged || completionsChanged {
					if isTerminal {
						errs = append(errs, field.Forbidden(rJobPath, "Cannot mutate parallelism or completions when JobSet is in a terminal state (Completed or Failed)"))
					} else {
						if parallelismChanged && newRJob.Template.Spec.Parallelism != nil && *newRJob.Template.Spec.Parallelism < 1 {
							errs = append(errs, field.Invalid(rJobPath.Child("parallelism"), *newRJob.Template.Spec.Parallelism, "parallelism must be >= 1"))
						}
						if completionsChanged && newRJob.Template.Spec.Completions != nil && *newRJob.Template.Spec.Completions < 1 {
							errs = append(errs, field.Invalid(rJobPath.Child("completions"), *newRJob.Template.Spec.Completions, "completions must be >= 1"))
						}
						if newRJob.Template.Spec.Parallelism != nil && newRJob.Template.Spec.Completions != nil && *newRJob.Template.Spec.Parallelism != *newRJob.Template.Spec.Completions {
							errs = append(errs, field.Invalid(rJobPath.Child("completions"), *newRJob.Template.Spec.Completions, "completions must be equal to parallelism for Elastic Indexed Jobs"))
						}
					}
				}

				// Mask parallelism and completions in mungedSpec to bypass the strict immutability check
				mungedSpec.ReplicatedJobs[index].Template.Spec.Parallelism = oldRJob.Template.Spec.Parallelism
				mungedSpec.ReplicatedJobs[index].Template.Spec.Completions = oldRJob.Template.Spec.Completions
			}
		}

		// spec.scheduling is immutable, but the pod count it represents isn't:
		// ElasticJobSet scaling can shrink parallelism/completions below an
		// already-configured Gang minCount. Re-check that invariant on every
		// update; the minCount itself can't be lowered to compensate since
		// spec.scheduling is immutable, so the JobSet must be scaled back up
		// (or not scaled down further) instead.
		errs = append(errs, validateSchedulingGangMinCounts(newJs)...)
	}

	// Allow pod template to be mutated for suspended JobSets, or JobSets getting suspended.
	// This is needed for integration with Kueue/DWS.
	if ptr.Deref(oldJs.Spec.Suspend, false) || ptr.Deref(newJs.Spec.Suspend, false) {
		for index := range newJs.Spec.ReplicatedJobs {
			newRJob := &newJs.Spec.ReplicatedJobs[index]

			// Safely grab the old replicated job by Name to prevent panics on length mismatch
			oldRJob, exists := oldJobsMap[newRJob.Name]
			if !exists {
				continue
			}

			// Pod values which must be mutable for Kueue are defined here: https://github.com/kubernetes-sigs/kueue/blob/a50d395c36a2cb3965be5232162cf1fded1bdb08/apis/kueue/v1beta1/workload_types.go#L256-L260
			mungedSpec.ReplicatedJobs[index].Template.Spec.Template.Annotations = oldRJob.Template.Spec.Template.Annotations
			mungedSpec.ReplicatedJobs[index].Template.Spec.Template.Labels = oldRJob.Template.Spec.Template.Labels
			mungedSpec.ReplicatedJobs[index].Template.Spec.Template.Spec.NodeSelector = oldRJob.Template.Spec.Template.Spec.NodeSelector
			mungedSpec.ReplicatedJobs[index].Template.Spec.Template.Spec.Tolerations = oldRJob.Template.Spec.Template.Spec.Tolerations

			// Pod Scheduling Gates can be updated for batch/v1 Job: https://github.com/kubernetes/kubernetes/blob/ceb58a4dbc671b9d0a2de6d73a1616bc0c299863/pkg/apis/batch/validation/validation.go#L662
			mungedSpec.ReplicatedJobs[index].Template.Spec.Template.Spec.SchedulingGates = oldRJob.Template.Spec.Template.Spec.SchedulingGates
		}
	}

	// Note that SucccessPolicy and failurePolicy are made immutable via CEL.
	errs = append(errs, apivalidation.ValidateImmutableField(mungedSpec.ReplicatedJobs, oldJs.Spec.ReplicatedJobs, field.NewPath("spec").Child("replicatedJobs"))...)
	errs = append(errs, apivalidation.ValidateImmutableField(newJs.Spec.ManagedBy, oldJs.Spec.ManagedBy, field.NewPath("spec").Child("managedBy"))...)
	errs = append(errs, apivalidation.ValidateImmutableField(newJs.Spec.Scheduling, oldJs.Spec.Scheduling, field.NewPath("spec").Child("scheduling"))...)

	if len(errs) == 0 {
		return nil, nil
	}

	return nil, apierrors.NewInvalid(
		schema.GroupKind{Group: "jobset.x-k8s.io", Kind: "JobSet"},
		newJs.Name,
		errs,
	)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (j *jobSetWebhook) ValidateDelete(ctx context.Context, js *jobset.JobSet) (admission.Warnings, error) {
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
func validateFailurePolicy(js *jobset.JobSet, rJobNames sets.Set[string]) []error {
	var allErrs []error
	failurePolicy := js.Spec.FailurePolicy
	if failurePolicy == nil {
		return allErrs
	}

	// Check if any rule has RestartJob action and validate that no replicated job has replicas > maxReplicasPerReplicatedJob.
	hasRestartJob := false
	for _, rule := range failurePolicy.Rules {
		if rule.Action == jobset.RestartJob || rule.Action == jobset.RestartJobAndIgnoreMaxRestarts {
			hasRestartJob = true
			break
		}
	}
	if hasRestartJob {
		if !features.Enabled(features.RestartJob) {
			allErrs = append(allErrs, fmt.Errorf("RestartJob and RestartJobAndIgnoreMaxRestarts failure policy actions are not allowed when RestartJob feature gate is disabled"))
			return allErrs // early return for critical error (missing feature gate)
		}
		for _, rJob := range js.Spec.ReplicatedJobs {
			if rJob.Replicas > maxReplicasPerReplicatedJob {
				allErrs = append(allErrs, fmt.Errorf("JobSet cannot have a failure policy rule with RestartJob or RestartJobAndIgnoreMaxRestarts action and a replicated job with replicas > %d", maxReplicasPerReplicatedJob))
				break
			}
		}
	}

	// ruleNameToRulesWithName is used to verify that rule names are unique
	ruleNameToRulesWithName := make(map[string][]int)
	for index, rule := range failurePolicy.Rules {
		// Check that the rule name meets the minimum length
		nameLen := len(rule.Name)
		if nameLen < minRuleNameLength || nameLen > maxRuleNameLength {
			err := fmt.Errorf("invalid failure policy rule name of length %v, the rule name must be at least %v characters long and at most %v characters long", nameLen, minRuleNameLength, maxRuleNameLength)
			allErrs = append(allErrs, err)
		}

		ruleNameToRulesWithName[rule.Name] = append(ruleNameToRulesWithName[rule.Name], index)

		if !ruleNameRegexp.MatchString(rule.Name) {
			err := fmt.Errorf("invalid failure policy rule name '%v', a failure policy rule name must start with an alphabetic character, optionally followed by a string of alphanumeric characters or '_,:', and must end with an alphanumeric character or '_'", rule.Name)
			allErrs = append(allErrs, err)
		}

		// Validate the rules target replicated jobs are valid
		for _, rJobName := range rule.TargetReplicatedJobs {
			if !rJobNames.Has(rJobName) {
				allErrs = append(allErrs, fmt.Errorf("invalid replicatedJob name '%s' in failure policy does not appear in .spec.ReplicatedJobs", rJobName))
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

	// Validate job is using indexed completion mode and completions number is set.
	if replicatedJob.Template.Spec.CompletionMode == nil || replicatedJob.Template.Spec.Completions == nil || *replicatedJob.Template.Spec.CompletionMode != batchv1.IndexedCompletion {
		return fmt.Errorf("job for coordinator pod must be indexed completion mode, and completions number must be set")
	}

	// Validate Pod index.
	if js.Spec.Coordinator.PodIndex < 0 || js.Spec.Coordinator.PodIndex >= int(*replicatedJob.Template.Spec.Completions) {
		return fmt.Errorf("coordinator pod index %d is invalid for replicatedJob %s job index %d", js.Spec.Coordinator.PodIndex, js.Spec.Coordinator.ReplicatedJob, js.Spec.Coordinator.JobIndex)
	}
	return nil
}

// If spec will lead to invalid coordinator label value, return error
// This usually happens when the JobSet name is too long
func validateCoordinatorLabelValue(js *jobset.JobSet, jobSetName string) error {
	jsForValidation := js.DeepCopy()
	jsForValidation.Name = jobSetName

	labelValue := controllers.CoordinatorEndpoint(jsForValidation)
	errs := validation.IsValidLabelValue(labelValue)
	if len(errs) > 0 {
		return fmt.Errorf("spec will lead to invalid label value %q for coordinator label %q (long JobSet / ReplicatedJob / SubDomain name?): %s", labelValue, jobset.CoordinatorKey, strings.Join(errs, ", "))
	}
	return nil
}

// validateVolumeClaimPolicies validates the volume claim policies for the JobSet.
func (j *jobSetWebhook) validateVolumeClaimPolicies(ctx context.Context, js *jobset.JobSet, jobSetName string, volumeClaimPolicies []jobset.VolumeClaimPolicy) []error {
	var allErrs []error
	claimNames := sets.New[string]()

	// Collect all claim names from templates.
	for policyIdx, policy := range volumeClaimPolicies {
		fieldPath := field.NewPath("spec", "volumeClaimPolicies").Index(policyIdx)
		for templateIdx, template := range policy.Templates {
			templateFieldPath := fieldPath.Child("template").Index(templateIdx)

			// Validate claim name uniqueness.
			if claimNames.Has(template.Name) {
				allErrs = append(allErrs, field.Invalid(
					templateFieldPath.Child("name"),
					template.Name,
					"names must be unique for VolumeClaimPolicies template",
				))
			}

			// Validate DNS-1123 subdomain name
			for _, err := range validation.IsDNS1123Subdomain(template.Name) {
				allErrs = append(allErrs, field.Invalid(templateFieldPath.Child("name"), template.Name, err))
			}

			// Validate PVC name length limits
			pvcName := controllers.GeneratePVCName(jobSetName, template.Name)
			if len(pvcName) > maxVolumeClaimLength {
				allErrs = append(allErrs, field.Invalid(
					templateFieldPath.Child("name"),
					template.Name,
					"VolumeClaimPolicies template name is too long"))
			}

			// Validate that template has corresponding volumeMount in at least one container
			if err := validateReplicatedJobsVolumeClaims(js.Spec.ReplicatedJobs, template.Name); err != nil {
				allErrs = append(allErrs, err...)
			}

			// Validate template if PVC with the same name exists.
			existingPVC := &corev1.PersistentVolumeClaim{}
			err := j.client.Get(ctx, types.NamespacedName{
				Name:      pvcName,
				Namespace: js.Namespace,
			}, existingPVC)
			if err == nil {
				// PVC specs must be the same.
				if !reflect.DeepEqual(existingPVC.Spec, template.Spec) {
					allErrs = append(allErrs, field.Invalid(
						templateFieldPath.Child("spec"),
						template.Spec,
						fmt.Sprintf("spec does not match existing PVC %s in namespace %s", pvcName, js.Namespace),
					))
				}
				// Retention policy must be retain for the existing PVC.
				if policy.RetentionPolicy != nil && *policy.RetentionPolicy.WhenDeleted != jobset.RetentionPolicyRetain {
					allErrs = append(allErrs, field.Invalid(
						fieldPath.Child("retentionPolicy").Child("whenDeleted"),
						policy.RetentionPolicy.WhenDeleted,
						"retentionPolicy must be retain when PVC exists",
					))
				}
			} else if !apierrors.IsNotFound(err) {
				// Error other than NotFound occurred
				allErrs = append(allErrs, field.InternalError(
					templateFieldPath,
					fmt.Errorf("failed to check for existing PVC %s: %w", pvcName, err),
				))
			}

			claimNames.Insert(template.Name)
		}
	}
	return allErrs
}

func validateReplicatedJobsVolumeClaims(rJobs []jobset.ReplicatedJob, volumeClaimName string) []error {
	var allErrs []error
	hasMatchingMount := false

	for rJobIdx, rJob := range rJobs {
		rJobFieldPath := field.NewPath("spec", "replicatedJobs").Index(rJobIdx)

		// Check that ReplicatedJob doesn't have volume with the same name.
		for volIdx, volume := range rJob.Template.Spec.Template.Spec.Volumes {
			if volume.Name == volumeClaimName {
				allErrs = append(allErrs, field.Invalid(
					rJobFieldPath.Child("template", "spec", "template", "spec", "volumes").Index(volIdx).Child("name"),
					volume.Name,
					fmt.Sprintf("volume name conflicts with VolumeClaimPolicy template name: %s", volumeClaimName),
				))
			}
		}

		// Check whether ReplicatedJob initContainers or containers have the desired volume mount.
		if !hasMatchingMount {
			for _, container := range rJob.Template.Spec.Template.Spec.InitContainers {
				if hasVolumeMount(container.VolumeMounts, volumeClaimName) {
					hasMatchingMount = true
					break
				}
			}

			for _, container := range rJob.Template.Spec.Template.Spec.Containers {
				if hasVolumeMount(container.VolumeMounts, volumeClaimName) {
					hasMatchingMount = true
					break
				}
			}
		}
	}

	if !hasMatchingMount {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "replicatedJobs"),
			rJobs,
			fmt.Sprintf("replicatedJob containers don't have a matching volumeMount: %s from VolumeClaimPolicies", volumeClaimName),
		))
	}
	return allErrs
}

// hasVolumeMount checks if volumeMounts have mount with the provided name.
func hasVolumeMount(volumeMounts []corev1.VolumeMount, volumeMountName string) bool {
	for _, volumeMount := range volumeMounts {
		if volumeMount.Name == volumeMountName {
			return true
		}
	}
	return false
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

func getJobSetNameForValidation(js *jobset.JobSet) string {
	if js.Name != "" {
		return js.Name
	}
	if js.GenerateName != "" {
		return names.SimpleNameGenerator.GenerateName(js.GenerateName)
	}
	return ""
}

// toFieldErrorList converts a slice of errors (which may contain a mix of
// *field.Error and plain errors) into a field.ErrorList. Existing *field.Error
// values are preserved as-is; plain errors are wrapped as field.InternalError.
func toFieldErrorList(errs []error) field.ErrorList {
	var fieldErrs field.ErrorList
	for _, err := range errs {
		if err == nil {
			continue
		}
		var fe *field.Error
		if errors.As(err, &fe) {
			fieldErrs = append(fieldErrs, fe)
		} else {
			fieldErrs = append(fieldErrs, field.InternalError(nil, err))
		}
	}
	return fieldErrs
}

func usesSingleTopLevelPodGroup(js *jobset.JobSet) bool {
	return controllers.UseTopLevelGang(js.Spec.Scheduling) && !controllers.HasSequencedStartup(js)
}

// validateScheduling validates the scheduling configuration of a JobSet.
// It uses the workloadbuilder library for declarative validation and complex
// cross-field policy checks, and adds JobSet-specific validations on top.
func validateScheduling(ctx context.Context, js *jobset.JobSet, rJobNames sets.Set[string]) []error {
	var allErrs []error

	// If feature gate is disabled, reject any scheduling config.
	if !features.Enabled(features.JobSetWorkloadAwareSchedulingAPI) {
		if js.Spec.Scheduling != nil {
			allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "scheduling"), "cannot be set when JobSetWorkloadAwareSchedulingAPI feature gate is disabled"))
		}
		return allErrs
	}

	if js.Spec.Scheduling == nil {
		return nil
	}
	if len(js.Spec.ReplicatedJobs) == 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "replicatedJobs"), js.Spec.ReplicatedJobs, "must contain at least one replicatedJob when scheduling is configured"))
		return allErrs
	}

	// The JobSet controller manages each pod's schedulingGroup itself, mapping it
	// to the PodGroup compiled from spec.scheduling. Setting it directly in a Job
	// template (the "Template Delegation Model") would conflict with that and is
	// rejected, matching the KEP's decision to centralize scheduling configuration
	// in spec.scheduling instead of the embedded Job template.
	for i := range js.Spec.ReplicatedJobs {
		if js.Spec.ReplicatedJobs[i].Template.Spec.Template.Spec.SchedulingGroup != nil {
			allErrs = append(allErrs, field.Forbidden(
				field.NewPath("spec", "replicatedJobs").Index(i).Child("template", "spec", "template", "spec", "schedulingGroup"),
				"cannot be set directly when spec.scheduling is configured; the JobSet controller manages this field",
			))
		}
	}

	scheduling := js.Spec.Scheduling

	// Composite (Gang-of-Gangs) PodGroup hierarchies are not implemented in
	// alpha, so a top-level schedulingPolicy/schedulingConstraints/disruptionMode
	// combined with replicatedJobPolicies does not create a parent PodGroup
	// linking the leaf PodGroups. It is still allowed: any ReplicatedJob not
	// targeted by a replicatedJobPolicies entry falls back to the top-level
	// settings (see globalSchedulingInput), and a targeted ReplicatedJob's own
	// leaf settings take priority field-by-field over the top-level ones. This
	// lets a JobSet, e.g., set a top-level Gang policy as the default for most
	// ReplicatedJobs while overriding just one to Basic.

	// Run workloadbuilder validation for declarative checks and cross-field policy rules.
	schedulingPath := field.NewPath("spec", "scheduling")
	builderErrs := controllers.ValidateSchedulingWithBuilder(ctx, js, schedulingPath)
	for _, fe := range builderErrs {
		allErrs = append(allErrs, fe)
	}

	// JobSet-specific validations that the workloadbuilder doesn't cover.

	// Sequenced startup with explicit top-level gang minCount is not allowed.
	if controllers.HasSequencedStartup(js) && len(scheduling.ReplicatedJobPolicies) == 0 &&
		scheduling.SchedulingPolicy != nil && scheduling.SchedulingPolicy.Gang != nil && scheduling.SchedulingPolicy.Gang.MinCount > 0 {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "scheduling", "schedulingPolicy", "gang", "minCount"),
			scheduling.SchedulingPolicy.Gang.MinCount,
			"cannot be set when DependsOn or InOrder StartupPolicy is used; use per-ReplicatedJob gang policies instead",
		))
	}

	// All ReplicatedJobs must have the same priorityClassName when using a single top-level PodGroup.
	if usesSingleTopLevelPodGroup(js) && len(js.Spec.ReplicatedJobs) > 1 {
		priorityPath := field.NewPath("spec", "replicatedJobs")
		expected := js.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec.PriorityClassName
		for i := 1; i < len(js.Spec.ReplicatedJobs); i++ {
			actual := js.Spec.ReplicatedJobs[i].Template.Spec.Template.Spec.PriorityClassName
			if actual != expected {
				allErrs = append(allErrs, field.Invalid(
					priorityPath.Index(i).Child("template", "spec", "template", "spec", "priorityClassName"),
					actual,
					fmt.Sprintf("must match %q when top-level gang scheduling is used", expected),
				))
			}
		}
	}

	// Validate replicatedJobPolicies target valid, unique ReplicatedJob names.
	// Duplicate targetReplicatedJob entries within one entry's list are rejected by
	// the API server via +listType=set, but overlap across different entries is not,
	// so it must be checked here.
	targeted := sets.New[string]()
	for i, rjPolicy := range scheduling.ReplicatedJobPolicies {
		fieldPath := field.NewPath("spec", "scheduling", "replicatedJobPolicies").Index(i)
		targetPath := fieldPath.Child("targetReplicatedJob")

		var validTargets []string
		for j, name := range rjPolicy.TargetReplicatedJob {
			if !rJobNames.Has(name) {
				allErrs = append(allErrs, field.Invalid(targetPath.Index(j), name, "does not reference a valid replicatedJob name"))
				continue
			}
			if targeted.Has(name) {
				allErrs = append(allErrs, field.Invalid(targetPath.Index(j), name, "is targeted by more than one replicatedJobPolicies entry"))
				continue
			}
			targeted.Insert(name)
			validTargets = append(validTargets, name)
		}
		if len(validTargets) == 0 {
			continue
		}

		// jobSchedulingPolicy switches this entry to the Gang-of-Gangs per-Job
		// model, where each replica of the targeted ReplicatedJob gets its own
		// independent PodGroup. It is therefore restricted to exactly one
		// target and is mutually exclusive with the leaf-level fields that
		// configure a single PodGroup shared across the targeted ReplicatedJobs.
		if rjPolicy.JobSchedulingPolicy != nil {
			if len(rjPolicy.TargetReplicatedJob) != 1 {
				allErrs = append(allErrs, field.Invalid(
					targetPath, rjPolicy.TargetReplicatedJob,
					"must target exactly one replicatedJob when jobSchedulingPolicy is set",
				))
			}
			if rjPolicy.SchedulingPolicy != nil || rjPolicy.SchedulingConstraints != nil || rjPolicy.DisruptionMode != nil || len(rjPolicy.ResourceClaims) > 0 {
				allErrs = append(allErrs, field.Invalid(
					fieldPath.Child("jobSchedulingPolicy"), true,
					"cannot be set together with schedulingPolicy, schedulingConstraints, disruptionMode, or resourceClaims on the same replicatedJobPolicies entry; jobSchedulingPolicy replaces the single shared PodGroup those fields configure with one PodGroup per Job",
				))
			}
			// Per-Job PodGroups are independent of one another, so the
			// shared-PodGroup priorityClassName check below does not apply.
			continue
		}

		// ReplicatedJobs sharing a single PodGroup must share one priorityClassName.
		if len(validTargets) > 1 {
			expected := replicatedJobPriorityClassName(js, validTargets[0])
			for j, name := range validTargets[1:] {
				if actual := replicatedJobPriorityClassName(js, name); actual != expected {
					allErrs = append(allErrs, field.Invalid(
						targetPath.Index(j+1),
						name,
						fmt.Sprintf("priorityClassName %q must match %q of the other ReplicatedJobs sharing this PodGroup", actual, expected),
					))
				}
			}
		}
	}

	for _, fe := range validateSchedulingGangMinCounts(js) {
		allErrs = append(allErrs, fe)
	}

	return allErrs
}

// validateSchedulingGangMinCounts checks that any configured Gang minCount
// (top-level or per-replicatedJobPolicies entry) does not exceed the number of
// pods currently represented by its target(s). It is used both at creation and
// on update: spec.scheduling and the identity of spec.replicatedJobs are
// immutable, but ElasticJobSet scaling can change parallelism/completions and
// shrink the represented pod count below an already-configured minCount, which
// must be re-checked on every update.
func validateSchedulingGangMinCounts(js *jobset.JobSet) field.ErrorList {
	scheduling := js.Spec.Scheduling
	if scheduling == nil {
		return nil
	}

	var allErrs field.ErrorList
	for i := range scheduling.ReplicatedJobPolicies {
		rjPolicy := &scheduling.ReplicatedJobPolicies[i]

		// jobSchedulingPolicy sizes each Job's own PodGroup to that Job's
		// per-Job pod count (parallelism), not the ReplicatedJob's total
		// pod count across every replica.
		if rjPolicy.JobSchedulingPolicy != nil {
			jsp := rjPolicy.JobSchedulingPolicy
			if jsp.SchedulingPolicy == nil || jsp.SchedulingPolicy.Gang == nil || len(rjPolicy.TargetReplicatedJob) != 1 {
				continue
			}
			rjob := rjobByName(js, rjPolicy.TargetReplicatedJob[0])
			if rjob == nil {
				continue
			}
			maxCount := jobsetutil.JobParallelism(rjob)
			if jsp.SchedulingPolicy.Gang.MinCount > maxCount {
				allErrs = append(allErrs, field.Invalid(
					field.NewPath("spec", "scheduling", "replicatedJobPolicies").Index(i).Child("jobSchedulingPolicy", "schedulingPolicy", "gang", "minCount"),
					jsp.SchedulingPolicy.Gang.MinCount,
					fmt.Sprintf("cannot exceed the per-Job pod count (parallelism) of replicatedJob %q (%d)", rjPolicy.TargetReplicatedJob[0], maxCount),
				))
			}
			continue
		}

		if rjPolicy.SchedulingPolicy == nil || rjPolicy.SchedulingPolicy.Gang == nil {
			continue
		}
		var maxCount int32
		for _, name := range rjPolicy.TargetReplicatedJob {
			if rjob := rjobByName(js, name); rjob != nil {
				maxCount += replicatedJobPodCount(rjob)
			}
		}
		if rjPolicy.SchedulingPolicy.Gang.MinCount > maxCount {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec", "scheduling", "replicatedJobPolicies").Index(i).Child("schedulingPolicy", "gang", "minCount"),
				rjPolicy.SchedulingPolicy.Gang.MinCount,
				fmt.Sprintf("cannot exceed the number of pods across the targeted ReplicatedJobs %v (%d)", rjPolicy.TargetReplicatedJob, maxCount),
			))
		}
	}

	if controllers.UseTopLevelGang(scheduling) && !controllers.HasSequencedStartup(js) &&
		scheduling.SchedulingPolicy != nil && scheduling.SchedulingPolicy.Gang != nil {
		maxCount := jobsetutil.TotalReplicatedJobPodCount(js.Spec.ReplicatedJobs)
		if scheduling.SchedulingPolicy.Gang.MinCount > maxCount {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec", "scheduling", "schedulingPolicy", "gang", "minCount"),
				scheduling.SchedulingPolicy.Gang.MinCount,
				fmt.Sprintf("cannot exceed the total number of JobSet pods (%d)", maxCount),
			))
		}
	}

	return allErrs
}

// rjobByName returns the ReplicatedJob with the given name. Callers must only
// pass names already validated to exist in js.Spec.ReplicatedJobs.
func rjobByName(js *jobset.JobSet, name string) *jobset.ReplicatedJob {
	for i := range js.Spec.ReplicatedJobs {
		if js.Spec.ReplicatedJobs[i].Name == name {
			return &js.Spec.ReplicatedJobs[i]
		}
	}
	return nil
}

// replicatedJobPriorityClassName returns the priorityClassName of the named
// ReplicatedJob's pod template.
func replicatedJobPriorityClassName(js *jobset.JobSet, name string) string {
	rjob := rjobByName(js, name)
	if rjob == nil {
		return ""
	}
	return rjob.Template.Spec.Template.Spec.PriorityClassName
}

// invalidError converts a list of validation errors into an
// *apierrors.StatusError with HTTP 422 (Unprocessable Entity) and reason
// "Invalid". Returns nil if there are no errors.
func invalidError(name string, errs []error) error {
	fieldErrs := toFieldErrorList(errs)
	if len(fieldErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(
		schema.GroupKind{Group: "jobset.x-k8s.io", Kind: "JobSet"},
		name,
		fieldErrs,
	)
}
