package controllers

import (
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// isJobReachedStatus checks if ReplicatedJob reaches Ready or Complete status.
func isJobReachedStatus(dependsOnJobName string, dependsOnJobStatus jobset.DependsOnStatus, rJobsReplicas map[string]int32, rJobsStatuses []jobset.ReplicatedJobStatus) bool {

	// If the actual status is empty, return false.
	actualStatus := findReplicatedJobStatus(rJobsStatuses, dependsOnJobName)
	if actualStatus == nil {
		return false
	}

	// For Complete status, number of replicas must be equal to number of succeeded Jobs.
	if dependsOnJobStatus == jobset.CompleteStatus && rJobsReplicas[dependsOnJobName] == actualStatus.Succeeded {
		return true
	}

	// For Ready status, number of replicas must be equal to sum of ready, failed, and succeeded Jobs.
	if dependsOnJobStatus == jobset.ReadyStatus && rJobsReplicas[dependsOnJobName] == actualStatus.Failed+actualStatus.Ready+actualStatus.Succeeded {
		return true
	}

	return false
}
