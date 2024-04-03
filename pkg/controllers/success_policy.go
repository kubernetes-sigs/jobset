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
	batchv1 "k8s.io/api/batch/v1"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"

	"sigs.k8s.io/jobset/pkg/util/collections"
)

// TODO: add unit tests for the functions in this file.
// TestJobMatchesSuccessPolicy tests the jobMatchesSuccessPolicy function.
func TestJobMatchesSuccessPolicy(t *testing.T) {
	js := &jobset.JobSet{
		Spec: jobset.JobSetSpec{
			SuccessPolicy: jobset.SuccessPolicy{
				TargetReplicatedJobs: []string{"job1", "job2"},
			},
		},
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				jobset.ReplicatedJobNameKey: "job1",
			},
		},
	}

	result := jobMatchesSuccessPolicy(js, job)

	expected := true
	if result != expected {
		t.Errorf("Expected %t but got %t", expected, result)
	}
}

// TestReplicatedJobMatchesSuccessPolicy tests the replicatedJobMatchesSuccessPolicy function.
func TestReplicatedJobMatchesSuccessPolicy(t *testing.T) {
	js := &jobset.JobSet{
		Spec: jobset.JobSetSpec{
			SuccessPolicy: jobset.SuccessPolicy{
				TargetReplicatedJobs: []string{"job1", "job2"},
			},
		},
	}
	rjob := &jobset.ReplicatedJob{Name: "job1"}

	result := replicatedJobMatchesSuccessPolicy(js, rjob)

	expected := true
	if result != expected {
		t.Errorf("Expected %t but got %t", expected, result)
	}
}

// TestNumJobsMatchingSuccessPolicy tests the numJobsMatchingSuccessPolicy function.
func TestNumJobsMatchingSuccessPolicy(t *testing.T) {
	js := &jobset.JobSet{
		Spec: jobset.JobSetSpec{
			SuccessPolicy: jobset.SuccessPolicy{
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
		t.Errorf("Expected %d but got %d", expected, result)
	}
}

// TestNumJobsExpectedToSucceed tests the numJobsExpectedToSucceed function.
func TestNumJobsExpectedToSucceed(t *testing.T) {
	js := &jobset.JobSet{
		Spec: jobset.JobSetSpec{
			SuccessPolicy: jobset.SuccessPolicy{
				Operator: jobset.OperatorAll,
				TargetReplicatedJobs: []string{"job1", "job2"},
			},
		},
		Spec.ReplicatedJobs = []jobset.ReplicatedJob{
			{Name: "job1", Replicas: 2},
			{Name: "job2", Replicas: 3},
			{Name: "job3", Replicas: 1},
		},
	}

	result := numJobsExpectedToSucceed(js)

	expected := 5
	if result != expected {
		t.Errorf("Expected %d but got %d", expected, result)
	}
}

// jobMatchesSuccessPolicy returns a boolean value indicating if the Job is part of a
// ReplicatedJob that matches the JobSet's success policy.
func jobMatchesSuccessPolicy(js *jobset.JobSet, job *batchv1.Job) bool {
	return len(js.Spec.SuccessPolicy.TargetReplicatedJobs) == 0 || collections.Contains(js.Spec.SuccessPolicy.TargetReplicatedJobs, job.ObjectMeta.Labels[jobset.ReplicatedJobNameKey])
}

// replicatedJobMatchesSuccessPolicy returns a boolean value indicating if the ReplicatedJob
// matches the JobSet's success policy.
func replicatedJobMatchesSuccessPolicy(js *jobset.JobSet, rjob *jobset.ReplicatedJob) bool {
	return len(js.Spec.SuccessPolicy.TargetReplicatedJobs) == 0 || collections.Contains(js.Spec.SuccessPolicy.TargetReplicatedJobs, rjob.Name)
}

// replicatedJobMatchesSuccessPolicy returns the number of jobs in the given slice `jobs`
// which match the JobSet's success policy.
func numJobsMatchingSuccessPolicy(js *jobset.JobSet, jobs []*batchv1.Job) int {
	total := 0
	for _, job := range jobs {
		if jobMatchesSuccessPolicy(js, job) {
			total += 1
		}
	}
	return total
}

// numJobsExpectedToSucceed the number of jobs that must complete successfully
// in order to satisfy the JobSet's success policy and mark the JobSet complete.
// This is determined based on the JobSet spec.
func numJobsExpectedToSucceed(js *jobset.JobSet) int {
	total := 0
	switch js.Spec.SuccessPolicy.Operator {
	case jobset.OperatorAny:
		total = 1
	case jobset.OperatorAll:
		for _, rjob := range js.Spec.ReplicatedJobs {
			if replicatedJobMatchesSuccessPolicy(js, &rjob) {
				total += int(rjob.Replicas)
			}
		}
	}
	return total
}
