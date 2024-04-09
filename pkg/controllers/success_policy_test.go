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
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

func TestJobMatchesSuccessPolicy(t *testing.T) {
	// Create test data
	js1 := &jobset.JobSet{
		Spec: jobset.JobSetSpec{
			SuccessPolicy: &jobset.SuccessPolicy{
				TargetReplicatedJobs: []string{},
			},
		},
	}

	js2 := &jobset.JobSet{
		Spec: jobset.JobSetSpec{
			SuccessPolicy: &jobset.SuccessPolicy{
				TargetReplicatedJobs: []string{"job1", "job2"},
			},
		},
	}

	job1 := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				jobset.ReplicatedJobNameKey: "job1",
			},
		},
	}

	job2 := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				jobset.ReplicatedJobNameKey: "job3",
			},
		},
	}

	// Test case 1: Verify the scenario when TargetReplicatedJobs is empty
	if !jobMatchesSuccessPolicy(js1, job1) {
		t.Error("Validation failed when TargetReplicatedJobs is empty")
	}

	// Test case 2: Verify the scenario when the job matches one of the TargetReplicatedJobs
	if !jobMatchesSuccessPolicy(js2, job1) {
		t.Error("Validation failed when the job matches one of the TargetReplicatedJobs")
	}

	// Test case 3: Verify the scenario when the job does not match any of the TargetReplicatedJobs
	if jobMatchesSuccessPolicy(js2, job2) {
		t.Error("Validation failed when the job does not match any of the TargetReplicatedJobs")
	}
}

func TestReplicatedJobMatchesSuccessPolicy(t *testing.T) {
	// Create test data
	testJobSet := &jobset.JobSet{
		Spec: jobset.JobSetSpec{
			SuccessPolicy: &jobset.SuccessPolicy{
				TargetReplicatedJobs: []string{"job1", "job2"},
			},
		},
	}
	testReplicatedJob1 := &jobset.ReplicatedJob{Name: "job1"}
	testReplicatedJob2 := &jobset.ReplicatedJob{Name: "job3"}

	// Test case 1: Matching case
	if !replicatedJobMatchesSuccessPolicy(testJobSet, testReplicatedJob1) {
		t.Error("Expected the replicated job to match the success policy")
	}

	// Test case 2: Non-matching case
	if replicatedJobMatchesSuccessPolicy(testJobSet, testReplicatedJob2) {
		t.Error("Expected the replicated job not to match the success policy")
	}

	// Test case 3: Empty target replicated jobs
	emptyTargetJobSet := &jobset.JobSet{
		Spec: jobset.JobSetSpec{
			SuccessPolicy: &jobset.SuccessPolicy{
				TargetReplicatedJobs: []string{},
			},
		},
	}
	if !replicatedJobMatchesSuccessPolicy(emptyTargetJobSet, testReplicatedJob1) {
		t.Error("Expected the replicated job to match the success policy for an empty target job set")
	}
}

func TestNumJobsMatchingSuccessPolicy(t *testing.T) {
	js := &jobset.JobSet{
		Spec: jobset.JobSetSpec{
			SuccessPolicy: &jobset.SuccessPolicy{
				TargetReplicatedJobs: []string{"job1", "job2"},
			},
		},
	}
	jobs := []*batchv1.Job{
		{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					jobset.ReplicatedJobNameKey: "job1",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					jobset.ReplicatedJobNameKey: "job3",
				},
			},
		},
	}

	result := numJobsMatchingSuccessPolicy(js, jobs)

	expected := 1
	if result != expected {
		t.Errorf("Expected %d, but got %d", expected, result)
	}
}

func TestNumJobsExpectedToSucceed(t *testing.T) {
	// Create a test jobset
	testJobSetAny := &jobset.JobSet{
		Spec: jobset.JobSetSpec{
			SuccessPolicy: &jobset.SuccessPolicy{
				Operator: jobset.OperatorAny,
			},
		},
	}
	expectedAny := 1
	if result := numJobsExpectedToSucceed(testJobSetAny); result != expectedAny {
		t.Errorf("Expected %d, but got %d", expectedAny, result)
	}

	testJobSetAll := &jobset.JobSet{
		Spec: jobset.JobSetSpec{
			SuccessPolicy: &jobset.SuccessPolicy{
				Operator: jobset.OperatorAll,
				TargetReplicatedJobs: []string{"job1", "job2"},
			},
			ReplicatedJobs: []jobset.ReplicatedJob{
				{Name: "job1", Replicas: 1},
				{Name: "job2", Replicas: 2},
				{Name: "job3", Replicas: 3},
			},
		},
	}

	expectedAll := 3
	if result := numJobsExpectedToSucceed(testJobSetAll); result != expectedAll {
		t.Errorf("Expected %d, but got %d", expectedAll, result)
	}
}

