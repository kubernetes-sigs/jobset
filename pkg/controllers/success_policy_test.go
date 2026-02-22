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

package controllers

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	batchv1 "k8s.io/api/batch/v1"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	testutils "sigs.k8s.io/jobset/pkg/util/testing"
)

func TestJobMatchesSuccessPolicy(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		jobName    = "test-job"
		ns         = "default"
	)

	tests := []struct {
		name     string
		js       *jobset.JobSet
		job      *batchv1.Job
		expected bool
	}{
		{
			name: "jobset's TargetReplicatedJobs is empty",
			js: testutils.MakeJobSet(jobSetName, ns).
				SuccessPolicy(&jobset.SuccessPolicy{
					TargetReplicatedJobs: []string{},
				}).Obj(),
			job: testutils.MakeJob(jobName, ns).
				JobLabels(map[string]string{jobset.ReplicatedJobNameKey: "test-replicated-job-1"}).
				Obj(),
			expected: true,
		},
		{
			name: "job matches one of the TargetReplicatedJobs",
			js: testutils.MakeJobSet(jobSetName, ns).
				SuccessPolicy(&jobset.SuccessPolicy{
					TargetReplicatedJobs: []string{"test-replicated-job-1", "test-replicated-job-2"},
				}).Obj(),
			job: testutils.MakeJob(jobName, ns).
				JobLabels(map[string]string{jobset.ReplicatedJobNameKey: "test-replicated-job-1"}).
				Obj(),
			expected: true,
		},
		{
			name: "job does not match any of the TargetReplicatedJobs",
			js: testutils.MakeJobSet(jobSetName, ns).
				SuccessPolicy(&jobset.SuccessPolicy{
					TargetReplicatedJobs: []string{"test-replicated-job-1", "test-replicated-job-2"},
				}).Obj(),
			job: testutils.MakeJob(jobName, ns).
				JobLabels(map[string]string{jobset.ReplicatedJobNameKey: "test-replicated-job-3"}).
				Obj(),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := jobMatchesSuccessPolicy(tc.js, tc.job)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("unexpected finished value (+got/-want): %s", diff)
			}
		})
	}
}

func TestReplicatedJobMatchesSuccessPolicy(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)

	tests := []struct {
		name          string
		js            *jobset.JobSet
		replicatedJob jobset.ReplicatedJob
		expected      bool
	}{
		{
			name: "jobset's TargetReplicatedJobs is empty",
			js: testutils.MakeJobSet(jobSetName, ns).
				SuccessPolicy(&jobset.SuccessPolicy{
					TargetReplicatedJobs: []string{},
				}).Obj(),
			replicatedJob: testutils.MakeReplicatedJob("test-replicated-job-1").Obj(),
			expected:      true,
		},
		{
			name: "matching case",
			js: testutils.MakeJobSet(jobSetName, ns).
				SuccessPolicy(&jobset.SuccessPolicy{
					TargetReplicatedJobs: []string{"test-replicated-job-1", "test-replicated-job-2"},
				}).Obj(),
			replicatedJob: testutils.MakeReplicatedJob("test-replicated-job-1").Obj(),
			expected:      true,
		},
		{
			name: "non-matching case",
			js: testutils.MakeJobSet(jobSetName, ns).
				SuccessPolicy(&jobset.SuccessPolicy{
					TargetReplicatedJobs: []string{"test-replicated-job-1", "test-replicated-job-2"},
				}).Obj(),
			replicatedJob: testutils.MakeReplicatedJob("test-replicated-job-3").Obj(),
			expected:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := replicatedJobMatchesSuccessPolicy(tc.js, &tc.replicatedJob)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("unexpected finished value (+got/-want): %s", diff)
			}
		})
	}
}

func TestNumJobsMatchingSuccessPolicy(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		jobName    = "test-job"
		ns         = "default"
	)

	tests := []struct {
		name     string
		js       *jobset.JobSet
		jobs     []*batchv1.Job
		expected int
	}{
		{
			name: "one job matches the success policy",
			js: testutils.MakeJobSet(jobSetName, ns).
				SuccessPolicy(&jobset.SuccessPolicy{
					TargetReplicatedJobs: []string{"test-replicated-job-1", "test-replicated-job-2"},
				}).Obj(),
			jobs: []*batchv1.Job{
				testutils.MakeJob(jobName, ns).
					JobLabels(map[string]string{jobset.ReplicatedJobNameKey: "test-replicated-job-1"}).
					Obj(),
				testutils.MakeJob(jobName, ns).
					JobLabels(map[string]string{jobset.ReplicatedJobNameKey: "test-replicated-job-3"}).
					Obj(),
			},
			expected: 1,
		},
		{
			name: "all jobs matches the success policy",
			js: testutils.MakeJobSet(jobSetName, ns).
				SuccessPolicy(&jobset.SuccessPolicy{
					TargetReplicatedJobs: []string{"test-replicated-job-1", "test-replicated-job-2"},
				}).Obj(),
			jobs: []*batchv1.Job{
				testutils.MakeJob(jobName, ns).
					JobLabels(map[string]string{jobset.ReplicatedJobNameKey: "test-replicated-job-1"}).
					Obj(),
				testutils.MakeJob(jobName, ns).
					JobLabels(map[string]string{jobset.ReplicatedJobNameKey: "test-replicated-job-2"}).
					Obj(),
			},
			expected: 2,
		},
		{
			name: "no jobs match the success policy",
			js: testutils.MakeJobSet(jobSetName, ns).
				SuccessPolicy(&jobset.SuccessPolicy{
					TargetReplicatedJobs: []string{"test-replicated-job-1", "test-replicated-job-2"},
				}).Obj(),
			jobs: []*batchv1.Job{
				testutils.MakeJob(jobName, ns).
					JobLabels(map[string]string{jobset.ReplicatedJobNameKey: "test-replicated-job-3"}).
					Obj(),
				testutils.MakeJob(jobName, ns).
					JobLabels(map[string]string{jobset.ReplicatedJobNameKey: "test-replicated-job-4"}).
					Obj(),
			},
			expected: 0,
		},
		{
			name: "empty target replicated jobs, all job matches the success policy",
			js: testutils.MakeJobSet(jobSetName, ns).
				SuccessPolicy(&jobset.SuccessPolicy{
					TargetReplicatedJobs: []string{},
				}).Obj(),
			jobs: []*batchv1.Job{
				testutils.MakeJob(jobName, ns).
					JobLabels(map[string]string{jobset.ReplicatedJobNameKey: "test-replicated-job-1"}).
					Obj(),
				testutils.MakeJob(jobName, ns).
					JobLabels(map[string]string{jobset.ReplicatedJobNameKey: "test-replicated-job-2"}).
					Obj(),
			},
			expected: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := numJobsMatchingSuccessPolicy(tc.js, tc.jobs)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("unexpected finished value (+got/-want): %s", diff)
			}
		})
	}
}

func TestNumJobsExpectedToSucceed(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)

	tests := []struct {
		name     string
		js       *jobset.JobSet
		expected int
	}{
		{
			name: "any job completion fulfills success policy",
			js: testutils.MakeJobSet(jobSetName, ns).
				SuccessPolicy(&jobset.SuccessPolicy{
					Operator: jobset.OperatorAny,
				}).Obj(),
			expected: 1,
		},
		{
			name: "all replicated jobs match success policy",
			js: testutils.MakeJobSet(jobSetName, ns).
				SuccessPolicy(&jobset.SuccessPolicy{
					Operator:             jobset.OperatorAll,
					TargetReplicatedJobs: []string{"test-replicated-job-1", "test-replicated-job-2"},
				}).
				ReplicatedJob(testutils.MakeReplicatedJob("test-replicated-job-1").
					Replicas(1).Obj()).
				ReplicatedJob(testutils.MakeReplicatedJob("test-replicated-job-2").
					Replicas(2).Obj()).Obj(),
			expected: 3,
		},
		{
			name: "some replicated jobs don't match success policy",
			js: testutils.MakeJobSet(jobSetName, ns).
				SuccessPolicy(&jobset.SuccessPolicy{
					Operator:             jobset.OperatorAll,
					TargetReplicatedJobs: []string{"test-replicated-job-1", "test-replicated-job-3"},
				}).
				ReplicatedJob(testutils.MakeReplicatedJob("test-replicated-job-1").
					Replicas(1).Obj()).
				ReplicatedJob(testutils.MakeReplicatedJob("test-replicated-job-2").
					Replicas(2).Obj()).
				ReplicatedJob(testutils.MakeReplicatedJob("test-replicated-job-3").
					Replicas(3).Obj()).Obj(),
			expected: 4,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := numJobsExpectedToSucceed(tc.js)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("unexpected finished value (+got/-want): %s", diff)
			}
		})
	}
}
