/*
Copyright 2024 The Kubernetes Authors.
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
	"fmt"
	"strconv"

	batchv1 "k8s.io/api/batch/v1"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// jobsToDeleteDownScale gathers the excess jobs during a downscale
// and deletes the jobs
func jobsToDeleteDownScale(replicatedJobs []jobset.ReplicatedJob, replicatedJobStatus []jobset.ReplicatedJobStatus, jobItems []batchv1.Job) ([]*batchv1.Job, error) {
	jobsToDelete := []*batchv1.Job{}
	for _, replicatedJob := range replicatedJobs {
		status := findReplicatedJobStatus(replicatedJobStatus, replicatedJob.Name)
		countOfJobsToDelete := status.Ready - replicatedJob.Replicas
		if countOfJobsToDelete > 0 {
			jobsWeDeleted := 0
			for _, val := range jobItems {
				if val.Labels[jobset.ReplicatedJobNameKey] != replicatedJob.Name {
					continue
				}
				jobIndex, err := strconv.Atoi(val.Labels[jobset.JobIndexKey])
				if err != nil {
					return nil, fmt.Errorf("unable get integer from job index key")
				}
				if jobIndex >= int(countOfJobsToDelete) {
					jobsWeDeleted = jobsWeDeleted + 1
					jobsToDelete = append(jobsToDelete, &val)
				}
				if jobsWeDeleted == int(countOfJobsToDelete) {
					continue
				}
			}
		}
	}
	return jobsToDelete, nil
}
