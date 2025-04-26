package controllers

import (
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// dependencyReachedStatus checks if dependant ReplicatedJob reaches Ready or Complete status.
// func dependencyReachedStatus(dependsOnJob jobset.DependsOn, dependsOnJobReplicas int32, rJobsStatuses []jobset.ReplicatedJobStatus) bool {
func dependencyReachedStatus(rJob jobset.ReplicatedJob, rJobReplicas map[string]int32, rJobsStatuses []jobset.ReplicatedJobStatus) bool {
	for _, dependsOnJob := range rJob.DependsOn {
		// If the actual status of dependant ReplicatedJob is empty, return false.
		actualStatus := findReplicatedJobStatus(rJobsStatuses, dependsOnJob.Name)
		if actualStatus == nil {
			return false
		}

		// For Complete status, number of replicas must be equal to number of succeeded Jobs.
		if dependsOnJob.Status == jobset.DependencyComplete && rJobReplicas[dependsOnJob.Name] != actualStatus.Succeeded {
			return false
		}

		// For Ready status, number of replicas must be equal to sum of ready, failed, and succeeded Jobs.
		if dependsOnJob.Status == jobset.DependencyReady && rJobReplicas[dependsOnJob.Name] != actualStatus.Failed+actualStatus.Ready+actualStatus.Succeeded {
			return false
		}
	}

	return true
}
