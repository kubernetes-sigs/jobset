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
	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
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
		jobName            = "test-job"
		ns                 = "default"
	)

	tests := []struct {
		name             string
		rule             jobset.FailurePolicyRule
		failedJob        *batchv1.Job
		jobFailureReason string
		expected         bool
	}{
		{
			name: "failure policy rule matches on job failure reason",
			rule: jobset.FailurePolicyRule{
				OnJobFailureReasons:  []string{batchv1.JobReasonBackoffLimitExceeded},
				TargetReplicatedJobs: []string{replicatedJobName1},
			},
			failedJob: testutils.MakeJob(jobName, ns).JobLabels(
				map[string]string{jobset.ReplicatedJobNameKey: replicatedJobName1},
			).Obj(),
			jobFailureReason: batchv1.JobReasonBackoffLimitExceeded,
			expected:         true,
		},
		{
			name: "failure policy rule matches all on job failure reason",
			rule: jobset.FailurePolicyRule{
				TargetReplicatedJobs: []string{replicatedJobName1},
			},
			failedJob: testutils.MakeJob(jobName, ns).JobLabels(
				map[string]string{jobset.ReplicatedJobNameKey: replicatedJobName1},
			).Obj(),
			jobFailureReason: batchv1.JobReasonMaxFailedIndexesExceeded,
			expected:         true,
		},
		{
			name: "failure policy rule does not match on job failure reason",
			rule: jobset.FailurePolicyRule{
				OnJobFailureReasons:  []string{batchv1.JobReasonBackoffLimitExceeded},
				TargetReplicatedJobs: []string{replicatedJobName1},
			},
			failedJob: testutils.MakeJob(jobName, ns).JobLabels(
				map[string]string{jobset.ReplicatedJobNameKey: replicatedJobName1},
			).Obj(),
			jobFailureReason: batchv1.JobReasonDeadlineExceeded,
			expected:         false,
		},
		{
			name: "failure policy rule is not applicable to parent replicatedJob of failed job",
			rule: jobset.FailurePolicyRule{
				OnJobFailureReasons:  []string{batchv1.JobReasonBackoffLimitExceeded},
				TargetReplicatedJobs: []string{replicatedJobName1},
			},
			jobFailureReason: batchv1.JobReasonBackoffLimitExceeded,
			failedJob: testutils.MakeJob(jobName, ns).JobLabels(
				map[string]string{jobset.ReplicatedJobNameKey: replicatedJobName2},
			).Obj(),
			expected: false,
		},
		{
			name: "failure policy rule is applicable to all replicatedjobs when targetedReplicatedJobs is omitted",
			rule: jobset.FailurePolicyRule{
				OnJobFailureReasons: []string{batchv1.JobReasonBackoffLimitExceeded},
			},
			failedJob: testutils.MakeJob(jobName, ns).JobLabels(
				map[string]string{jobset.ReplicatedJobNameKey: replicatedJobName1},
			).Obj(),
			jobFailureReason: batchv1.JobReasonBackoffLimitExceeded,
			expected:         true,
		},
		{
			name: "failure policy rule is applicable to parent replicatedJob when targetedReplicatedJobs is specified",
			rule: jobset.FailurePolicyRule{
				OnJobFailureReasons:  []string{batchv1.JobReasonBackoffLimitExceeded},
				TargetReplicatedJobs: []string{replicatedJobName1},
			},
			failedJob: testutils.MakeJob(jobName, ns).JobLabels(
				map[string]string{jobset.ReplicatedJobNameKey: replicatedJobName1},
			).Obj(),
			jobFailureReason: batchv1.JobReasonBackoffLimitExceeded,
			expected:         true,
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

		failedJobNoReason1 = jobWithFailedCondition("job1", time.Now().Add(-6*time.Hour))
		failedJobNoReason2 = jobWithFailedCondition("job2", time.Now().Add(-3*time.Hour))
		failedJobNoReason3 = jobWithFailedCondition("job2", time.Now().Add(-1*time.Hour))

		failedJob1 = jobWithFailedConditionAndOpts("job1", time.Now().Add(-6*time.Hour),
			&failJobOptions{
				reason:                  ptr.To(batchv1.JobReasonBackoffLimitExceeded),
				parentReplicatedJobName: ptr.To(replicatedJobName),
			},
		)
		failedJob2 = jobWithFailedConditionAndOpts("job2", time.Now().Add(-3*time.Hour),
			&failJobOptions{
				reason:                  ptr.To(batchv1.JobReasonDeadlineExceeded),
				parentReplicatedJobName: ptr.To(replicatedJobName),
			},
		)
		failedJob3 = jobWithFailedConditionAndOpts("job3", time.Now().Add(-1*time.Hour),
			&failJobOptions{
				reason:                  ptr.To(batchv1.JobReasonFailedIndexes),
				parentReplicatedJobName: ptr.To(replicatedJobName),
			},
		)

		// ruleN matches failedJobN
		rule1 = jobset.FailurePolicyRule{
			Action:              jobset.RestartJobSet,
			OnJobFailureReasons: []string{batchv1.JobReasonBackoffLimitExceeded},
		}
		rule2 = jobset.FailurePolicyRule{
			Action:              jobset.RestartJobSet,
			OnJobFailureReasons: []string{batchv1.JobReasonDeadlineExceeded},
		}

		unmatchedRule = jobset.FailurePolicyRule{
			Action:              jobset.RestartJobSet,
			OnJobFailureReasons: []string{batchv1.JobReasonMaxFailedIndexesExceeded, batchv1.JobReasonPodFailurePolicy},
		}

		extraFailedJob = jobWithFailedConditionAndOpts("extra-job1", time.Now().Add(3*time.Hour),
			&failJobOptions{
				reason:                  ptr.To(batchv1.JobReasonDeadlineExceeded),
				parentReplicatedJobName: ptr.To(replicatedJobName),
			},
		)
	)
	tests := []struct {
		name            string
		rules           []jobset.FailurePolicyRule
		failedOwnedJobs []*batchv1.Job

		expectedFailurePolicyRule *jobset.FailurePolicyRule
		expectedJob               *batchv1.Job
	}{
		{
			name:            "failure policy rules are empty with no failed jobs",
			failedOwnedJobs: []*batchv1.Job{},

			expectedFailurePolicyRule: nil,
			expectedJob:               nil,
		},
		{
			name: "failure policy rules are empty with one failed job",
			failedOwnedJobs: []*batchv1.Job{
				failedJobNoReason1,
			},

			expectedFailurePolicyRule: nil,
			expectedJob:               nil,
		},
		{
			name:            "failure policy rules are empty with multiple failed jobs",
			failedOwnedJobs: []*batchv1.Job{failedJobNoReason3, failedJobNoReason1, failedJobNoReason2},

			expectedFailurePolicyRule: nil,
			expectedJob:               nil,
		},
		{
			name:            "failure policy rule does not match on job failure reasons",
			rules:           []jobset.FailurePolicyRule{unmatchedRule},
			failedOwnedJobs: []*batchv1.Job{failedJob3, failedJob1, failedJob2},

			expectedFailurePolicyRule: nil,
			expectedJob:               nil,
		},
		{
			name:            "failure policy rule matches first job to fail out of all jobs",
			rules:           []jobset.FailurePolicyRule{rule1},
			failedOwnedJobs: []*batchv1.Job{failedJob3, failedJob1, failedJob2},

			expectedFailurePolicyRule: &rule1,
			expectedJob:               failedJob1,
		},
		{
			name:            "failure policy rule matches second job to fail out of all jobs",
			rules:           []jobset.FailurePolicyRule{rule2},
			failedOwnedJobs: []*batchv1.Job{failedJob3, failedJob1, failedJob2},

			expectedFailurePolicyRule: &rule2,
			expectedJob:               failedJob2,
		},
		{
			name:            "failure policy rule matches multiple jobs and first failed job is the last one",
			rules:           []jobset.FailurePolicyRule{rule2},
			failedOwnedJobs: []*batchv1.Job{extraFailedJob, failedJob3, failedJob1, failedJob2},

			expectedFailurePolicyRule: &rule2,
			expectedJob:               failedJob2,
		},
		{
			name:  "first failed job that matches a failure policy rule is different from the first job to fail that matches the first matched failure policy rule",
			rules: []jobset.FailurePolicyRule{rule2, rule1},
			// failedJob1 is the first failedJob1 but does not match rule2 which is the first failure policy rule to be matched
			failedOwnedJobs: []*batchv1.Job{extraFailedJob, failedJob3, failedJob1, failedJob2},

			expectedFailurePolicyRule: &rule2,
			expectedJob:               failedJob2,
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
