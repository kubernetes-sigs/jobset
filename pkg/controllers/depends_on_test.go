package controllers

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	testutils "sigs.k8s.io/jobset/pkg/util/testing"
)

func TestDependencyReachedStatus(t *testing.T) {
	rJobModelInitializer := "model-initializer"
	rJobDatasetInitializer := "dataset-initializer"
	rJobTrainer := "trainer-node"

	tests := []struct {
		name          string
		rJob          jobset.ReplicatedJob
		rJobReplicas  map[string]int32
		rJobsStatuses []jobset.ReplicatedJobStatus
		expected      bool
	}{
		{
			name: "ReplicatedJob doesn't have any dependencies",
			rJob: testutils.MakeReplicatedJob(rJobModelInitializer).
				Obj(),
			rJobReplicas: map[string]int32{
				rJobModelInitializer: 1,
			},
			rJobsStatuses: []jobset.ReplicatedJobStatus{},
			expected:      true,
		},
		{
			name: "status for ReplicatedJob is nil",
			rJob: testutils.MakeReplicatedJob(rJobTrainer).
				DependsOn(
					[]jobset.DependsOn{
						{
							Name:   rJobModelInitializer,
							Status: jobset.DependencyComplete,
						},
					},
				).
				Obj(),
			rJobReplicas: map[string]int32{
				rJobModelInitializer: 1,
				rJobTrainer:          1,
			},
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
			name: "rJobStatuses is nil",
			rJob: testutils.MakeReplicatedJob(rJobTrainer).
				DependsOn(
					[]jobset.DependsOn{
						{
							Name:   rJobModelInitializer,
							Status: jobset.DependencyComplete,
						},
					},
				).
				Obj(),
			rJobReplicas: map[string]int32{
				rJobModelInitializer: 1,
				rJobTrainer:          1,
			},
			rJobsStatuses: nil,
			expected:      false,
		},
		{
			name: "depends on ReplicatedJob reaches complete status",
			rJob: testutils.MakeReplicatedJob(rJobTrainer).
				DependsOn(
					[]jobset.DependsOn{
						{
							Name:   rJobModelInitializer,
							Status: jobset.DependencyComplete,
						},
						{
							Name:   rJobDatasetInitializer,
							Status: jobset.DependencyComplete,
						},
					},
				).
				Obj(),
			rJobReplicas: map[string]int32{
				rJobModelInitializer:   2,
				rJobDatasetInitializer: 2,
				rJobTrainer:            1,
			},
			rJobsStatuses: []jobset.ReplicatedJobStatus{
				{
					Name:      rJobModelInitializer,
					Ready:     0,
					Succeeded: 2,
					Failed:    0,
					Suspended: 0,
					Active:    0,
				},
				{
					Name:      rJobDatasetInitializer,
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
			name: "one depends on ReplicatedJob doesn't reach complete status",
			rJob: testutils.MakeReplicatedJob(rJobTrainer).
				DependsOn(
					[]jobset.DependsOn{
						{
							Name:   rJobModelInitializer,
							Status: jobset.DependencyComplete,
						},
						{
							Name:   rJobDatasetInitializer,
							Status: jobset.DependencyComplete,
						},
					},
				).
				Obj(),
			rJobReplicas: map[string]int32{
				rJobModelInitializer:   2,
				rJobDatasetInitializer: 2,
				rJobTrainer:            1,
			},
			rJobsStatuses: []jobset.ReplicatedJobStatus{
				{
					Name:      rJobModelInitializer,
					Ready:     1,
					Succeeded: 1,
					Failed:    0,
					Suspended: 0,
					Active:    0,
				},
				{
					Name:      rJobDatasetInitializer,
					Ready:     0,
					Succeeded: 2,
					Failed:    0,
					Suspended: 0,
					Active:    0,
				},
			},
			expected: false,
		},
		{
			name: "two depends on ReplicatedJob doesn't reach complete status",
			rJob: testutils.MakeReplicatedJob(rJobTrainer).
				DependsOn(
					[]jobset.DependsOn{
						{
							Name:   rJobModelInitializer,
							Status: jobset.DependencyComplete,
						},
						{
							Name:   rJobDatasetInitializer,
							Status: jobset.DependencyComplete,
						},
					},
				).
				Obj(),
			rJobReplicas: map[string]int32{
				rJobModelInitializer:   2,
				rJobDatasetInitializer: 2,
				rJobTrainer:            1,
			},
			rJobsStatuses: []jobset.ReplicatedJobStatus{
				{
					Name:      rJobModelInitializer,
					Ready:     1,
					Succeeded: 1,
					Failed:    0,
					Suspended: 0,
					Active:    0,
				},
				{
					Name:      rJobDatasetInitializer,
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
			rJob: testutils.MakeReplicatedJob(rJobTrainer).
				DependsOn(
					[]jobset.DependsOn{
						{
							Name:   rJobModelInitializer,
							Status: jobset.DependencyReady,
						},
						{
							Name:   rJobDatasetInitializer,
							Status: jobset.DependencyReady,
						},
					},
				).
				Obj(),
			rJobReplicas: map[string]int32{
				rJobModelInitializer:   3,
				rJobDatasetInitializer: 3,
				rJobTrainer:            1,
			},
			rJobsStatuses: []jobset.ReplicatedJobStatus{
				{
					Name:      rJobModelInitializer,
					Ready:     1,
					Succeeded: 1,
					Failed:    1,
					Suspended: 0,
					Active:    0,
				},
				{
					Name:      rJobDatasetInitializer,
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
			name: "one depends on ReplicatedJob doesn't reach ready status",
			rJob: testutils.MakeReplicatedJob(rJobTrainer).
				DependsOn(
					[]jobset.DependsOn{
						{
							Name:   rJobModelInitializer,
							Status: jobset.DependencyReady,
						},
						{
							Name:   rJobDatasetInitializer,
							Status: jobset.DependencyReady,
						},
					},
				).
				Obj(),
			rJobReplicas: map[string]int32{
				rJobModelInitializer:   3,
				rJobDatasetInitializer: 3,
				rJobTrainer:            1,
			},
			rJobsStatuses: []jobset.ReplicatedJobStatus{
				{
					Name:      rJobModelInitializer,
					Ready:     2,
					Succeeded: 0,
					Failed:    0,
					Suspended: 0,
					Active:    1,
				},
				{
					Name:      rJobDatasetInitializer,
					Ready:     1,
					Succeeded: 1,
					Failed:    1,
					Suspended: 0,
					Active:    0,
				},
			},
			expected: false,
		},
		{
			name: "two depends on ReplicatedJobs doesn't reach ready status",
			rJob: testutils.MakeReplicatedJob(rJobTrainer).
				DependsOn(
					[]jobset.DependsOn{
						{
							Name:   rJobModelInitializer,
							Status: jobset.DependencyReady,
						},
						{
							Name:   rJobDatasetInitializer,
							Status: jobset.DependencyReady,
						},
					},
				).
				Obj(),
			rJobReplicas: map[string]int32{
				rJobModelInitializer:   3,
				rJobDatasetInitializer: 3,
				rJobTrainer:            1,
			},
			rJobsStatuses: []jobset.ReplicatedJobStatus{
				{
					Name:      rJobModelInitializer,
					Ready:     2,
					Succeeded: 0,
					Failed:    0,
					Suspended: 0,
					Active:    1,
				},
				{
					Name:      rJobDatasetInitializer,
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
			actual := dependencyReachedStatus(tc.rJob, tc.rJobReplicas, tc.rJobsStatuses)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("unexpected finished value (+got/-want): %s", diff)
			}
		})
	}
}
