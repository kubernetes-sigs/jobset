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
	"slices"
	"strconv"

	batchv1 "k8s.io/api/batch/v1"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

func indexFunc(a, b batchv1.Job) int {
	jobIndexA, errA := strconv.Atoi(a.Labels[jobset.JobIndexKey])
	jobIndexB, errB := strconv.Atoi(b.Labels[jobset.JobIndexKey])
	if errA != nil {
		return 0
	}
	if errB != nil {
		return 0
	}
	if jobIndexA > jobIndexB {
		return 1
	} else if jobIndexA < jobIndexB {
		return -1
	} else {
		return 0
	}
}

// jobsToDeleteForDownScale gathers the excess jobs during a downscale
// and deletes the jobs
func jobsToDeleteForDownScale(replicatedJobs []jobset.ReplicatedJob, replicatedJobStatuses []jobset.ReplicatedJobStatus, jobItems []batchv1.Job) ([]*batchv1.Job, error) {
	jobsToDelete := []*batchv1.Job{}
	type payload struct {
		batchJobs []batchv1.Job
		rjStatus  jobset.ReplicatedJobStatus
		replicas  int32
	}
	replicatedJobToBatchJobMap := map[string]payload{}
	for _, replicatedJob := range replicatedJobs {
		status := findReplicatedJobStatus(replicatedJobStatuses, replicatedJob.Name)
		newPayload := &payload{}
		newPayload.rjStatus = status
		newPayload.replicas = replicatedJob.Replicas
		for _, val := range jobItems {
			if val.Labels[jobset.ReplicatedJobNameKey] != replicatedJob.Name {
				continue
			}
			newPayload.batchJobs = append(newPayload.batchJobs, val)
		}
		slices.SortFunc(newPayload.batchJobs, indexFunc)
		replicatedJobToBatchJobMap[replicatedJob.Name] = *newPayload
	}
	for _, jobAndStatus := range replicatedJobToBatchJobMap {
		countOfJobsToDelete := jobAndStatus.rjStatus.Ready - jobAndStatus.replicas
		if countOfJobsToDelete > 0 {
			jobsWeDeleted := 0
			for i := len(jobAndStatus.batchJobs) - 1; i >= 0; i-- {

				jobIndex, err := strconv.Atoi(jobAndStatus.batchJobs[i].Labels[jobset.JobIndexKey])
				if err != nil {
					return nil, fmt.Errorf("unable get integer from job index key")
				}
				if jobIndex >= int(countOfJobsToDelete) {
					jobsWeDeleted = jobsWeDeleted + 1
					jobsToDelete = append(jobsToDelete, &jobAndStatus.batchJobs[i])
				}
				if jobsWeDeleted == int(countOfJobsToDelete) {
					break
				}
			}
		}
	}
	return jobsToDelete, nil
}
