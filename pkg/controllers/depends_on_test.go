package controllers

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

func TestDependencyReachedStatus(t *testing.T) {
	tests := []struct {
		name                 string
		dependsOnJob         jobset.DependsOn
		dependsOnJobReplicas int32
		rJobsStatuses        []jobset.ReplicatedJobStatus
		expected             bool
	}{
		{
			name: "status for ReplicatedJob is nil",
			dependsOnJob: jobset.DependsOn{
				Name: "initializer", Status: jobset.DependencyComplete,
			},
			dependsOnJobReplicas: 1,
			rJobsStatuses: []jobset.ReplicatedJobStatus{
				{
					Name:      "invalid",
					Ready:     0,
					Succeeded: 1,
					Failed:    0,
					Suspended: 0,
					Active:    0,
				},
			},
			expected: false,
		},
		{
			name: "depends on ReplicatedJob reaches complete status",
			dependsOnJob: jobset.DependsOn{
				Name: "initializer", Status: jobset.DependencyComplete,
			},
			dependsOnJobReplicas: 2,
			rJobsStatuses: []jobset.ReplicatedJobStatus{
				{
					Name:      "initializer",
					Ready:     0,
					Succeeded: 2,
					Failed:    0,
					Suspended: 0,
					Active:    0,
				},
			},
			expected: true,
		},
		{
			name: "depends on ReplicatedJob doesn't reach complete status",
			dependsOnJob: jobset.DependsOn{
				Name: "initializer", Status: jobset.DependencyComplete,
			},
			dependsOnJobReplicas: 2,
			rJobsStatuses: []jobset.ReplicatedJobStatus{
				{
					Name:      "initializer",
					Ready:     1,
					Succeeded: 1,
					Failed:    0,
					Suspended: 0,
					Active:    0,
				},
			},
			expected: false,
		},
		{
			name: "depends on ReplicatedJob reaches ready status",
			dependsOnJob: jobset.DependsOn{
				Name: "initializer", Status: jobset.DependencyReady,
			},
			dependsOnJobReplicas: 3,
			rJobsStatuses: []jobset.ReplicatedJobStatus{
				{
					Name:      "initializer",
					Ready:     1,
					Succeeded: 1,
					Failed:    1,
					Suspended: 0,
					Active:    0,
				},
			},
			expected: true,
		},
		{
			name: "depends on ReplicatedJob doesn't reach ready status",
			dependsOnJob: jobset.DependsOn{
				Name: "initializer", Status: jobset.DependencyReady,
			},
			dependsOnJobReplicas: 3,
			rJobsStatuses: []jobset.ReplicatedJobStatus{
				{
					Name:      "initializer",
					Ready:     2,
					Succeeded: 0,
					Failed:    0,
					Suspended: 0,
					Active:    1,
				},
			},
			expected: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := dependencyReachedStatus(tc.dependsOnJob, tc.dependsOnJobReplicas, tc.rJobsStatuses)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("unexpected finished value (+got/-want): %s", diff)
			}
		})
	}
}
