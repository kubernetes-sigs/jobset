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
	"slices"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
	"sigs.k8s.io/jobset/pkg/metrics"
)

// actionFunctionMap relates jobset failure policy action names to the appropriate behavior during jobset reconciliation.
var actionFunctionMap = map[jobset.FailurePolicyAction]failurePolicyActionApplier{
	jobset.FailJobSet:                        failJobSetActionApplier,
	jobset.RestartJobSet:                     restartJobSetActionApplier,
	jobset.RestartJobSetAndIgnoreMaxRestarts: restartJobSetAndIgnoreMaxRestartsActionApplier,
}

// The source of truth for the definition of defaultFailurePolicyRuleAction is the Configurable Failure Policy KEP.
const defaultFailurePolicyRuleAction = jobset.RestartJobSet

// executeFailurePolicy applies the Failure Policy of a JobSet when a failed child Job is found.
// This function is run only when a failed child job has already been found.
func executeFailurePolicy(ctx context.Context, js *jobset.JobSet, ownedJobs *childJobs, updateStatusOpts *statusUpdateOpts) error {
	log := ctrl.LoggerFrom(ctx)

	// If no failure policy is defined, mark the JobSet as failed.
	if js.Spec.FailurePolicy == nil {
		// firstFailedJob is only computed if necessary since it is expensive to compute
		// for JobSets with many child jobs. This is why we don't unconditionally compute
		// it once at the beginning of the function and share the results between the different
		// possible code paths here.
		firstFailedJob := findFirstFailedJob(ownedJobs.failed)
		msg := messageWithFirstFailedJob(constants.FailedJobsMessage, firstFailedJob.Name)
		setJobSetFailedCondition(js, constants.FailedJobsReason, msg, updateStatusOpts)
		return nil
	}

	// Check for matching Failure Policy Rule
	rules := js.Spec.FailurePolicy.Rules
	matchingFailurePolicyRule, matchingFailedJob := findFirstFailedPolicyRuleAndJob(ctx, rules, ownedJobs.failed)

	var failurePolicyRuleAction jobset.FailurePolicyAction
	if matchingFailurePolicyRule == nil {
		failurePolicyRuleAction = defaultFailurePolicyRuleAction
		matchingFailedJob = findFirstFailedJob(ownedJobs.failed)
	} else {
		failurePolicyRuleAction = matchingFailurePolicyRule.Action
	}

	if err := applyFailurePolicyRuleAction(ctx, js, matchingFailedJob, updateStatusOpts, failurePolicyRuleAction); err != nil {
		log.Error(err, "applying FailurePolicyRuleAction %v", failurePolicyRuleAction)
		return err
	}

	return nil
}

// findFirstFailedPolicyRuleAndJob returns the first failure policy rule matching a failed child job.
// The function also returns the first child job matching the failure policy rule returned.
// If there does not exist a matching failure policy rule, then the function returns nil for all values.
func findFirstFailedPolicyRuleAndJob(ctx context.Context, rules []jobset.FailurePolicyRule, failedOwnedJobs []*batchv1.Job) (*jobset.FailurePolicyRule, *batchv1.Job) {
	log := ctrl.LoggerFrom(ctx)

	for index, rule := range rules {
		var matchedFailedJob *batchv1.Job
		var matchedFailureTime *metav1.Time
		for _, failedJob := range failedOwnedJobs {
			jobFailureCondition := findJobFailureCondition(failedJob)
			// This means that the Job has not failed.
			if jobFailureCondition == nil {
				continue
			}

			jobFailureTime, jobFailureReason := ptr.To(jobFailureCondition.LastTransitionTime), jobFailureCondition.Reason
			jobFailedEarlier := matchedFailedJob == nil || jobFailureTime.Before(matchedFailureTime)
			if ruleIsApplicable(ctx, rule, failedJob, jobFailureReason) && jobFailedEarlier {
				matchedFailedJob = failedJob
				matchedFailureTime = jobFailureTime
			}
		}

		if matchedFailedJob != nil {
			log.V(2).Info(fmt.Sprintf("found a failed job matching failure policy rule with index %v and name %v", index, rule.Name))
			return &rule, matchedFailedJob
		}
		log.V(2).Info(fmt.Sprintf("did not find a failed job matching failure policy rule with index %v and name %v", index, rule.Name))
	}

	log.V(2).Info("never found a matched failure policy rule.")
	return nil, nil
}

// applyFailurePolicyRuleAction applies the supplied FailurePolicyRuleAction.
func applyFailurePolicyRuleAction(ctx context.Context, js *jobset.JobSet, matchingFailedJob *batchv1.Job, updateStatusOps *statusUpdateOpts, failurePolicyRuleAction jobset.FailurePolicyAction) error {
	log := ctrl.LoggerFrom(ctx)

	applier, ok := actionFunctionMap[failurePolicyRuleAction]
	if !ok {
		err := fmt.Errorf("failed to find a corresponding action for the FailurePolicyRuleAction %v", failurePolicyRuleAction)
		log.Error(err, "retrieving information for FailurePolicyRuleAction")
		return err
	}

	if err := applier(ctx, js, matchingFailedJob, updateStatusOps); err != nil {
		log.Error(err, "error applying the FailurePolicyRuleAction: %v", failurePolicyRuleAction)
		return err
	}

	return nil
}

// ruleIsApplicable returns true if the failed job and job failure reason match the failure policy rule.
// The function returns false otherwise.
func ruleIsApplicable(ctx context.Context, rule jobset.FailurePolicyRule, failedJob *batchv1.Job, jobFailureReason string) bool {
	log := ctrl.LoggerFrom(ctx)

	ruleAppliesToJobFailureReason := len(rule.OnJobFailureReasons) == 0 || slices.Contains(rule.OnJobFailureReasons, jobFailureReason)
	if !ruleAppliesToJobFailureReason {
		return false
	}

	parentReplicatedJob, exists := parentReplicatedJobName(failedJob)
	if !exists {
		// If we cannot find the parent ReplicatedJob, we assume the rule does not apply.
		log.V(2).Info(fmt.Sprintf("The failed job %v does not appear to have a parent replicatedJob.", failedJob.Name))
		return false
	}

	ruleAppliesToParentReplicatedJob := len(rule.TargetReplicatedJobs) == 0 || slices.Contains(rule.TargetReplicatedJobs, parentReplicatedJob)
	return ruleAppliesToParentReplicatedJob
}

// failurePolicyRecreateAll triggers a JobSet restart for the next reconcillation loop.
func failurePolicyRecreateAll(ctx context.Context, js *jobset.JobSet, shouldCountTowardsMax bool, updateStatusOpts *statusUpdateOpts, event *eventParams) {
	log := ctrl.LoggerFrom(ctx)

	if updateStatusOpts == nil {
		updateStatusOpts = &statusUpdateOpts{}
	}

	// Increment JobSet restarts. This will trigger reconciliation and result in deletions
	// of old jobs not part of the current jobSet run.
	js.Status.Restarts += 1

	if shouldCountTowardsMax {
		js.Status.RestartsCountTowardsMax += 1
	}

	updateStatusOpts.shouldUpdate = true

	// Emit event for each JobSet restarts for observability and debugability.
	enqueueEvent(updateStatusOpts, event)
	log.V(2).Info("attempting restart", "restart attempt", js.Status.Restarts)
}

// The type failurePolicyActionApplier applies a FailurePolicyAction and returns nil if the FailurePolicyAction was successfully applied.
// The function returns an error otherwise.
type failurePolicyActionApplier = func(ctx context.Context, js *jobset.JobSet, matchingFailedJob *batchv1.Job, updateStatusOpts *statusUpdateOpts) error

// failJobSetActionApplier applies the FailJobSet FailurePolicyAction
var failJobSetActionApplier failurePolicyActionApplier = func(ctx context.Context, js *jobset.JobSet, matchingFailedJob *batchv1.Job, updateStatusOpts *statusUpdateOpts) error {
	failureBaseMessage := constants.FailJobSetActionMessage
	failureMessage := messageWithFirstFailedJob(failureBaseMessage, matchingFailedJob.Name)

	failureReason := constants.FailJobSetActionReason
	setJobSetFailedCondition(js, failureReason, failureMessage, updateStatusOpts)
	return nil
}

// restartJobSetActionApplier applies the RestartJobSet FailurePolicyAction
var restartJobSetActionApplier failurePolicyActionApplier = func(ctx context.Context, js *jobset.JobSet, matchingFailedJob *batchv1.Job, updateStatusOpts *statusUpdateOpts) error {
	if js.Status.RestartsCountTowardsMax >= js.Spec.FailurePolicy.MaxRestarts {
		failureBaseMessage := constants.ReachedMaxRestartsMessage
		failureMessage := messageWithFirstFailedJob(failureBaseMessage, matchingFailedJob.Name)

		failureReason := constants.ReachedMaxRestartsReason
		setJobSetFailedCondition(js, failureReason, failureMessage, updateStatusOpts)
		return nil
	}

	baseMessage := constants.RestartJobSetActionMessage
	eventMessage := messageWithFirstFailedJob(baseMessage, matchingFailedJob.Name)
	event := &eventParams{
		object:       js,
		eventType:    corev1.EventTypeWarning,
		eventReason:  constants.RestartJobSetActionReason,
		eventMessage: eventMessage,
	}

	shouldCountTowardsMax := true
	failurePolicyRecreateAll(ctx, js, shouldCountTowardsMax, updateStatusOpts, event)
	return nil
}

// restartJobSetAndIgnoreMaxRestartsActionApplier applies the RestartJobSetAndIgnoreMaxRestarts FailurePolicyAction
var restartJobSetAndIgnoreMaxRestartsActionApplier failurePolicyActionApplier = func(ctx context.Context, js *jobset.JobSet, matchingFailedJob *batchv1.Job, updateStatusOpts *statusUpdateOpts) error {
	baseMessage := constants.RestartJobSetAndIgnoreMaxRestartsActionMessage
	eventMessage := messageWithFirstFailedJob(baseMessage, matchingFailedJob.Name)
	event := &eventParams{
		object:       js,
		eventType:    corev1.EventTypeWarning,
		eventReason:  constants.RestartJobSetAndIgnoreMaxRestartsActionReason,
		eventMessage: eventMessage,
	}

	shouldCountTowardsMax := false
	failurePolicyRecreateAll(ctx, js, shouldCountTowardsMax, updateStatusOpts, event)
	return nil
}

// parentReplicatedJobName returns the name of the parent
// ReplicatedJob and true if it is able to retrieve the parent.
// The empty string and false are returned otherwise.
func parentReplicatedJobName(job *batchv1.Job) (string, bool) {
	if job == nil {
		return "", false
	}

	replicatedJobName, ok := job.Labels[jobset.ReplicatedJobNameKey]
	replicatedJobNameIsUnset := !ok || replicatedJobName == ""
	return replicatedJobName, !replicatedJobNameIsUnset
}

// makeFailedConditionOpts returns the options we use to generate the JobSet failed condition.
func makeFailedConditionOpts(reason, msg string) *conditionOpts {
	return &conditionOpts{
		condition: &metav1.Condition{
			Type:    string(jobset.JobSetFailed),
			Status:  metav1.ConditionTrue,
			Reason:  reason,
			Message: msg,
		},
		eventType: corev1.EventTypeWarning,
	}
}

// setJobSetFailedCondition sets a condition and terminal state on the JobSet status indicating it has failed.
func setJobSetFailedCondition(js *jobset.JobSet, reason, msg string, updateStatusOpts *statusUpdateOpts) {
	setCondition(js, makeFailedConditionOpts(reason, msg), updateStatusOpts)
	js.Status.TerminalState = string(jobset.JobSetFailed)
	// Update the metrics
	metrics.JobSetFailed(js.Name, js.Namespace)
}

// findJobFailureTimeAndReason is a helper function which extracts the Job failure condition from a Job,
// if the JobFailed condition exists and is true.
func findJobFailureCondition(job *batchv1.Job) *batchv1.JobCondition {
	if job == nil {
		return nil
	}
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return &c
		}
	}
	return nil
}

// findJobFailureTime is a helper function which extracts the Job failure time from a Job,
// if the JobFailed condition exists and is true.
func findJobFailureTime(job *batchv1.Job) *metav1.Time {
	failureCondition := findJobFailureCondition(job)
	if failureCondition == nil {
		return nil
	}
	return &failureCondition.LastTransitionTime
}

// findFirstFailedJob accepts a slice of failed Jobs and returns the Job which has a JobFailed condition
// with the oldest transition time.
func findFirstFailedJob(failedJobs []*batchv1.Job) *batchv1.Job {
	var (
		firstFailedJob   *batchv1.Job
		firstFailureTime *metav1.Time
	)
	for _, job := range failedJobs {
		failureTime := findJobFailureTime(job)
		// If job has actually failed and it is the first (or only) failure we've seen,
		// store the job for output.
		if failureTime != nil && (firstFailedJob == nil || failureTime.Before(firstFailureTime)) {
			firstFailedJob = job
			firstFailureTime = failureTime
		}
	}
	return firstFailedJob
}

// messageWithFirstFailedJob appends the first failed job to the original event message in human readable way.
func messageWithFirstFailedJob(msg, firstFailedJobName string) string {
	return fmt.Sprintf("%s (first failed job: %s)", msg, firstFailedJobName)
}
