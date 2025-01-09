package controllers

import (
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// dependencyReachedStatus checks if dependant ReplicatedJob reaches Ready or Complete status.
func dependencyReachedStatus(dependsOnJob jobset.DependsOn, dependsOnJobReplicas int32, rJobsStatuses []jobset.ReplicatedJobStatus) bool {
	// If the actual status is empty, return false.
	actualStatus := findReplicatedJobStatus(rJobsStatuses, dependsOnJob.Name)
	if actualStatus == nil {
		return false
	}

	// For Complete status, number of replicas must be equal to number of succeeded Jobs.
	if dependsOnJob.Status == jobset.DependencyComplete && dependsOnJobReplicas == actualStatus.Succeeded {
		return true
	}

	// For Ready status, number of replicas must be equal to sum of ready, failed, and succeeded Jobs.
	if dependsOnJob.Status == jobset.DependencyReady && dependsOnJobReplicas == actualStatus.Failed+actualStatus.Ready+actualStatus.Succeeded {
		return true
	}

	return false
}
