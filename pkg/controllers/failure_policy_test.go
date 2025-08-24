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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
	testutils "sigs.k8s.io/jobset/pkg/util/testing"
)

func TestFindFirstFailedJob(t *testing.T) {
	testCases := []struct {
		name       string
		failedJobs []*batchv1.Job
		expected   *batchv1.Job
	}{
		{
			name:       "No failed jobs",
			failedJobs: []*batchv1.Job{},
			expected:   nil,
		},
		{
			name: "Single failed job",
			failedJobs: []*batchv1.Job{
				jobWithFailedCondition("job1", time.Now().Add(-1*time.Hour)),
			},
			expected: jobWithFailedCondition("job1", time.Now().Add(-1*time.Hour)),
		},
		{
			name: "Multiple failed jobs, earliest first",
			failedJobs: []*batchv1.Job{
				jobWithFailedCondition("job1", time.Now().Add(-3*time.Hour)),
				jobWithFailedCondition("job2", time.Now().Add(-5*time.Hour)),
			},
			expected: jobWithFailedCondition("job2", time.Now().Add(-5*time.Hour)),
		},
		{
			name: "Jobs without failed condition",
			failedJobs: []*batchv1.Job{
				{ObjectMeta: metav1.ObjectMeta{Name: "job1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "job2"}},
			},
			expected: nil,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			result := findFirstFailedJob(tc.failedJobs)
			if result != nil && tc.expected != nil {
				assert.Equal(t, result.Name, tc.expected.Name)
			} else if result != nil && tc.expected == nil || result == nil && tc.expected != nil {
				t.Errorf("Expected: %v, got: %v)", result, tc.expected)
			}
		})
	}
}

func TestFailurePolicyRuleIsApplicable(t *testing.T) {
	var (
		replicatedJobName1 = "test-replicated-job-1"
		replicatedJobName2 = "test-replicated-job-2"

		jobFailureReason1 = batchv1.JobReasonBackoffLimitExceeded
		jobFailureReason2 = batchv1.JobReasonDeadlineExceeded

		jobName = "test-job"
		ns      = "default"
	)

	tests := []struct {
		name             string
		rule             jobset.FailurePolicyRule
		failedJob        *batchv1.Job
		jobFailureReason string
		expected         bool
	}{
		{
			name: "a job has failed and the failure policy rule matches all possible parent replicated jobs and matches all possible job failure reasons",
			rule: jobset.FailurePolicyRule{
				OnJobFailureReasons:  []string{},
				TargetReplicatedJobs: []string{},
			},
			failedJob: testutils.MakeJob(jobName, ns).JobLabels(
				map[string]string{jobset.ReplicatedJobNameKey: replicatedJobName1},
			).Obj(),
			jobFailureReason: jobFailureReason1,
			expected:         true,
		},
		{
			name: "a job has failed and the failure policy rule matches all possible parent replicated jobs and matches the job failure reason",
			rule: jobset.FailurePolicyRule{
				OnJobFailureReasons:  []string{jobFailureReason1},
				TargetReplicatedJobs: []string{},
			},
			failedJob: testutils.MakeJob(jobName, ns).JobLabels(
				map[string]string{jobset.ReplicatedJobNameKey: replicatedJobName1},
			).Obj(),
			jobFailureReason: jobFailureReason1,
			expected:         true,
		},
		{
			name: "a job has failed and the failure policy rule matches all possible parent replicated jobs and does not match the job failure reason",
			rule: jobset.FailurePolicyRule{
				OnJobFailureReasons:  []string{jobFailureReason1},
				TargetReplicatedJobs: []string{},
			},
			failedJob: testutils.MakeJob(jobName, ns).JobLabels(
				map[string]string{jobset.ReplicatedJobNameKey: replicatedJobName1},
			).Obj(),
			jobFailureReason: jobFailureReason2,
			expected:         false,
		},
		{
			name: "a job has failed and the failure policy rule matches the parent replicatedJob and matches all possible job failure reasons",
			rule: jobset.FailurePolicyRule{
				OnJobFailureReasons:  []string{},
				TargetReplicatedJobs: []string{replicatedJobName1},
			},
			failedJob: testutils.MakeJob(jobName, ns).JobLabels(
				map[string]string{jobset.ReplicatedJobNameKey: replicatedJobName1},
			).Obj(),
			jobFailureReason: jobFailureReason1,
			expected:         true,
		},
		{
			name: "a job has failed and the failure policy rule matches the parent replicatedJob and matches the job failure reason",
			rule: jobset.FailurePolicyRule{
				OnJobFailureReasons:  []string{jobFailureReason1},
				TargetReplicatedJobs: []string{replicatedJobName1},
			},
			failedJob: testutils.MakeJob(jobName, ns).JobLabels(
				map[string]string{jobset.ReplicatedJobNameKey: replicatedJobName1},
			).Obj(),
			jobFailureReason: jobFailureReason1,
			expected:         true,
		},
		{
			name: "a job has failed and the failure policy rule matches the parent replicatedJob and does not match the job failure reason",
			rule: jobset.FailurePolicyRule{
				OnJobFailureReasons:  []string{jobFailureReason1},
				TargetReplicatedJobs: []string{replicatedJobName1},
			},
			failedJob: testutils.MakeJob(jobName, ns).JobLabels(
				map[string]string{jobset.ReplicatedJobNameKey: replicatedJobName1},
			).Obj(),
			jobFailureReason: jobFailureReason2,
			expected:         false,
		},
		{
			name: "a job has failed and the failure policy rule does not match the parent replicatedJob and matches all possible job failure reasons",
			rule: jobset.FailurePolicyRule{
				OnJobFailureReasons:  []string{},
				TargetReplicatedJobs: []string{replicatedJobName1},
			},
			failedJob: testutils.MakeJob(jobName, ns).JobLabels(
				map[string]string{jobset.ReplicatedJobNameKey: replicatedJobName2},
			).Obj(),
			jobFailureReason: jobFailureReason1,
			expected:         false,
		},
		{
			name: "a job has failed and the failure policy rule does not match the parent replicatedJob and matches the job failure reason",
			rule: jobset.FailurePolicyRule{
				OnJobFailureReasons:  []string{jobFailureReason1},
				TargetReplicatedJobs: []string{replicatedJobName1},
			},
			failedJob: testutils.MakeJob(jobName, ns).JobLabels(
				map[string]string{jobset.ReplicatedJobNameKey: replicatedJobName2},
			).Obj(),
			jobFailureReason: jobFailureReason1,
			expected:         false,
		},
		{
			name: "a job has failed and the failure policy rule does not match the parent replicatedJob and does not match the job failure reason",
			rule: jobset.FailurePolicyRule{
				OnJobFailureReasons:  []string{jobFailureReason1},
				TargetReplicatedJobs: []string{replicatedJobName1},
			},
			failedJob: testutils.MakeJob(jobName, ns).JobLabels(
				map[string]string{jobset.ReplicatedJobNameKey: replicatedJobName2},
			).Obj(),
			jobFailureReason: jobFailureReason2,
			expected:         false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := ruleIsApplicable(context.TODO(), tc.rule, tc.failedJob, tc.jobFailureReason)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("unexpected finished value (+got/-want): %s", diff)
			}
		})
	}
}

func TestFindFirstFailedPolicyRuleAndJob(t *testing.T) {
	var (
		replicatedJobName = "test-replicatedJob"

		failureReason1 = batchv1.JobReasonBackoffLimitExceeded
		failureReason2 = batchv1.JobReasonDeadlineExceeded
		failureReason3 = batchv1.JobReasonFailedIndexes

		failedJob1 = jobWithFailedConditionAndOpts("job1", time.Now().Add(-6*time.Hour),
			&failJobOptions{
				reason:                  ptr.To(failureReason1),
				parentReplicatedJobName: ptr.To(replicatedJobName),
			},
		)
		failedJob1DupEarlierFailure = jobWithFailedConditionAndOpts("job1DupEarlierFailure", time.Now().Add(-12*time.Hour),
			&failJobOptions{
				reason:                  ptr.To(failureReason1),
				parentReplicatedJobName: ptr.To(replicatedJobName),
			},
		)
		failedJob2 = jobWithFailedConditionAndOpts("job2", time.Now().Add(-3*time.Hour),
			&failJobOptions{
				reason:                  ptr.To(failureReason2),
				parentReplicatedJobName: ptr.To(replicatedJobName),
			},
		)
		failedJob3 = jobWithFailedConditionAndOpts("job3", time.Now().Add(-1*time.Hour),
			&failJobOptions{
				reason:                  ptr.To(failureReason3),
				parentReplicatedJobName: ptr.To(replicatedJobName),
			},
		)

		// failurePolicyRuleN matches failedJobN
		failurePolicyRule1 = jobset.FailurePolicyRule{
			Action:              jobset.RestartJobSet,
			OnJobFailureReasons: []string{failureReason1},
		}
		failurePolicyRule2 = jobset.FailurePolicyRule{
			Action:              jobset.RestartJobSet,
			OnJobFailureReasons: []string{failureReason2},
		}
		failurePolicyRule3 = jobset.FailurePolicyRule{
			Action:              jobset.RestartJobSet,
			OnJobFailureReasons: []string{failureReason3},
		}

		fakeReplicatedJobName      = "fakeReplicatedJobName"
		unmatchedFailurePolicyRule = jobset.FailurePolicyRule{
			Action:               jobset.RestartJobSet,
			TargetReplicatedJobs: []string{fakeReplicatedJobName},
		}
	)
	tests := []struct {
		name            string
		rules           []jobset.FailurePolicyRule
		failedOwnedJobs []*batchv1.Job

		expectedFailurePolicyRule *jobset.FailurePolicyRule
		expectedJob               *batchv1.Job
	}{
		{
			name:            "There are 0 failed jobs and there are 0 failure policy rules, therefore nil is returned for both the rule and job.",
			rules:           []jobset.FailurePolicyRule{},
			failedOwnedJobs: []*batchv1.Job{},

			expectedFailurePolicyRule: nil,
			expectedJob:               nil,
		},
		{
			name:            "There is 1 failed job and there are 0 failure policy rules, therefore nil is returned for both the rule and job.",
			rules:           []jobset.FailurePolicyRule{},
			failedOwnedJobs: []*batchv1.Job{failedJob1},

			expectedFailurePolicyRule: nil,
			expectedJob:               nil,
		},
		{
			name:            "There is 1 failed job, there is 1 failure policy rule, and the failure policy rule does not match the job, therefore nil is returned for both the rule and job.",
			rules:           []jobset.FailurePolicyRule{failurePolicyRule2},
			failedOwnedJobs: []*batchv1.Job{failedJob1},

			expectedFailurePolicyRule: nil,
			expectedJob:               nil,
		},
		{
			name:            "There is 1 failed job, there are 3 failure policy rules, and the failed job matches the first failure policy rule, therefore the job and first failure policy rule are returned.",
			rules:           []jobset.FailurePolicyRule{failurePolicyRule1, failurePolicyRule2, failurePolicyRule3},
			failedOwnedJobs: []*batchv1.Job{failedJob1},

			expectedFailurePolicyRule: &failurePolicyRule1,
			expectedJob:               failedJob1,
		},
		{
			name:            "There is 1 failed job, there are 3 failure policy rules, the failed job does not match the first failure policy rule, and the failed job matches the second failure policy rule, therefore the job and second failure policy rule are returned.",
			rules:           []jobset.FailurePolicyRule{failurePolicyRule1, failurePolicyRule2, failurePolicyRule3},
			failedOwnedJobs: []*batchv1.Job{failedJob2},

			expectedFailurePolicyRule: &failurePolicyRule2,
			expectedJob:               failedJob2,
		},
		{
			name:            "There is 1 failed job, there are 3 failure policy rules, the failed job does not match the first nor second failure policy rules, and the failed job matches the third failure policy rule, therefore the job and the the third failure policy rule are returned.",
			rules:           []jobset.FailurePolicyRule{failurePolicyRule1, failurePolicyRule2, failurePolicyRule3},
			failedOwnedJobs: []*batchv1.Job{failedJob3},

			expectedFailurePolicyRule: &failurePolicyRule3,
			expectedJob:               failedJob3,
		},
		{
			name:            "There are 2 failed jobs and there are 0 failure policy rules, therefore nil is returned for both the rule and job.",
			rules:           []jobset.FailurePolicyRule{},
			failedOwnedJobs: []*batchv1.Job{failedJob1, failedJob2},

			expectedFailurePolicyRule: nil,
			expectedJob:               nil,
		},
		{
			name:            "There are 2 failed jobs, the second job failed before the first job, there is 1 failure policy rule, and the failure policy rule does not match any of the jobs, therefore nil is returned for both the rule and job.",
			rules:           []jobset.FailurePolicyRule{failurePolicyRule2},
			failedOwnedJobs: []*batchv1.Job{failedJob1, failedJob1DupEarlierFailure},

			expectedFailurePolicyRule: nil,
			expectedJob:               nil,
		},
		{
			name:            "There are 2 failed jobs, the second job failed before the first job, there is 1 failure policy rule, and the failure policy rule matches both jobs, therefore the second job and the failure policy rule are returned.",
			rules:           []jobset.FailurePolicyRule{failurePolicyRule1},
			failedOwnedJobs: []*batchv1.Job{failedJob1, failedJob1DupEarlierFailure},

			expectedFailurePolicyRule: &failurePolicyRule1,
			expectedJob:               failedJob1DupEarlierFailure,
		},
		{
			name:            "There are 2 failed jobs, the second job failed before the first job, there are 2 failure policy rules, the first failure policy does not match any of the jobs, and the second failure policy rule matches both jobs, therefore the second job and the second failure policy rule are returned.",
			rules:           []jobset.FailurePolicyRule{unmatchedFailurePolicyRule, failurePolicyRule1},
			failedOwnedJobs: []*batchv1.Job{failedJob1, failedJob1DupEarlierFailure},

			expectedFailurePolicyRule: &failurePolicyRule1,
			expectedJob:               failedJob1DupEarlierFailure,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actualRule, actualJob := findFirstFailedPolicyRuleAndJob(context.TODO(), tc.rules, tc.failedOwnedJobs)
			if diff := cmp.Diff(tc.expectedJob, actualJob); diff != "" {
				t.Errorf("unexpected finished value (+got/-want): %s", diff)
			}
			if diff := cmp.Diff(tc.expectedFailurePolicyRule, actualRule); diff != "" {
				t.Errorf("unexpected finished value (+got/-want): %s", diff)
			}
		})
	}
}

func TestApplyFailurePolicyRuleAction(t *testing.T) {
	matchingFailedJob := jobWithFailedCondition("failed-job", time.Now())

	testCases := []struct {
		name                 string
		jobSet               *jobset.JobSet
		matchingFailedJob    *batchv1.Job
		failurePolicyAction  jobset.FailurePolicyAction
		expectedJobSetStatus jobset.JobSetStatus
	}{
		{
			name:                "FailJobSet action",
			jobSet:              testutils.MakeJobSet("test-js", "default").FailurePolicy(&jobset.FailurePolicy{}).Obj(),
			matchingFailedJob:   matchingFailedJob,
			failurePolicyAction: jobset.FailJobSet,
			expectedJobSetStatus: jobset.JobSetStatus{
				TerminalState: string(jobset.JobSetFailed),
				Conditions: []metav1.Condition{
					{
						Type:   string(jobset.JobSetFailed),
						Status: metav1.ConditionTrue,
						Reason: constants.FailJobSetActionReason,
					},
				},
			},
		},
		{
			name: "RestartJobSet when restarts < maxRestarts increments restarts count and counts towards max",
			jobSet: testutils.MakeJobSet("test-js", "default").FailurePolicy(&jobset.FailurePolicy{MaxRestarts: 5}).
				SetStatus(jobset.JobSetStatus{
					Restarts:                1,
					RestartsCountTowardsMax: 1,
				}).
				Obj(),
			matchingFailedJob:   matchingFailedJob,
			failurePolicyAction: jobset.RestartJobSet,
			expectedJobSetStatus: jobset.JobSetStatus{
				Restarts:                2,
				RestartsCountTowardsMax: 2,
			},
		},
		{
			name: "RestartJobSet action when restarts >= maxRestarts fails the jobset",
			jobSet: testutils.MakeJobSet("test-js", "default").
				FailurePolicy(&jobset.FailurePolicy{MaxRestarts: 2}).
				SetStatus(jobset.JobSetStatus{
					Restarts:                2,
					RestartsCountTowardsMax: 2,
				}).
				Obj(),
			matchingFailedJob:   matchingFailedJob,
			failurePolicyAction: jobset.RestartJobSet,
			expectedJobSetStatus: jobset.JobSetStatus{
				Restarts:                2,
				RestartsCountTowardsMax: 2,
				TerminalState:           string(jobset.JobSetFailed),
				Conditions: []metav1.Condition{
					{
						Type:   string(jobset.JobSetFailed),
						Status: metav1.ConditionTrue,
						Reason: constants.ReachedMaxRestartsReason,
					},
				},
			},
		},
		{
			name: "RestartJobSetAndIgnoreMaxRestarts action does not count toward max restarts",
			jobSet: testutils.MakeJobSet("test-js", "default").
				FailurePolicy(&jobset.FailurePolicy{MaxRestarts: 1}).
				SetStatus(jobset.JobSetStatus{
					Restarts:                1,
					RestartsCountTowardsMax: 1,
				}).
				Obj(),
			matchingFailedJob:   matchingFailedJob,
			failurePolicyAction: jobset.RestartJobSetAndIgnoreMaxRestarts,
			expectedJobSetStatus: jobset.JobSetStatus{
				Restarts:                2,
				RestartsCountTowardsMax: 1, // not incremented
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			updateStatusOpts := &statusUpdateOpts{}
			jobSetCopy := tc.jobSet.DeepCopy()
			err := applyFailurePolicyRuleAction(context.TODO(), jobSetCopy, tc.matchingFailedJob, updateStatusOpts, tc.failurePolicyAction)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !updateStatusOpts.shouldUpdate {
				t.Fatalf("unexpected updateStatusOpts.shouldUpdate value: got %v, want true", updateStatusOpts.shouldUpdate)
			}

			opts := []cmp.Option{
				cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Message"),
				cmpopts.SortSlices(func(a, b metav1.Condition) bool { return a.Type < b.Type }),
			}

			if diff := cmp.Diff(tc.expectedJobSetStatus, jobSetCopy.Status, opts...); diff != "" {
				t.Errorf("unexpected JobSetStatus value after applying failure policy rule action (+got/-want): %s", diff)
			}
		})
	}
}
