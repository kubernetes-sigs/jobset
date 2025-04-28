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
	"slices"

	batchv1 "k8s.io/api/batch/v1"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// jobMatchesSuccessPolicy returns a boolean value indicating if the Job is part of a
// ReplicatedJob that matches the JobSet's success policy.
func jobMatchesSuccessPolicy(js *jobset.JobSet, job *batchv1.Job) bool {
	return len(js.Spec.SuccessPolicy.TargetReplicatedJobs) == 0 || slices.Contains(js.Spec.SuccessPolicy.TargetReplicatedJobs, job.Labels[jobset.ReplicatedJobNameKey])
}

// replicatedJobMatchesSuccessPolicy returns a boolean value indicating if the ReplicatedJob
// matches the JobSet's success policy.
func replicatedJobMatchesSuccessPolicy(js *jobset.JobSet, rjob *jobset.ReplicatedJob) bool {
	return len(js.Spec.SuccessPolicy.TargetReplicatedJobs) == 0 || slices.Contains(js.Spec.SuccessPolicy.TargetReplicatedJobs, rjob.Name)
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
