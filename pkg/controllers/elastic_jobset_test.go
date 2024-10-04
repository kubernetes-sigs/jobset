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

	"github.com/google/go-cmp/cmp"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

func TestJobsToDeleteDownScale(t *testing.T) {

	tests := []struct {
		name                        string
		replicatedJobs              []jobset.ReplicatedJob
		replicatedJobStatus         []jobset.ReplicatedJobStatus
		jobs                        []batchv1.Job
		expectedJobsThatWereDeleted []batchv1.Job
		gotError                    error
	}{
		{
			name: "no elastic downscale",
			replicatedJobs: []jobset.ReplicatedJob{
				{
					Name:     "test",
					Template: batchv1.JobTemplateSpec{},
					Replicas: 2,
				},
			},
			replicatedJobStatus: []jobset.ReplicatedJobStatus{
				{
					Name:  "test",
					Ready: 1,
				},
			},
			jobs: []batchv1.Job{
				{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{
							jobset.ReplicatedJobNameKey: "test",
							jobset.JobIndexKey:          "0",
						},
					},
				},
			},
		},
		{
			name: "elastic upscale; do nothing",
			replicatedJobs: []jobset.ReplicatedJob{
				{
					Name:     "test",
					Template: batchv1.JobTemplateSpec{},
					Replicas: 2,
				},
			},
			replicatedJobStatus: []jobset.ReplicatedJobStatus{
				{
					Name:  "test",
					Ready: 1,
				},
			},
			jobs: []batchv1.Job{
				{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{
							jobset.ReplicatedJobNameKey: "test",
							jobset.JobIndexKey:          "0",
						},
					},
				},
			},
		},
		{
			name: "elastic downscale is needed",
			replicatedJobs: []jobset.ReplicatedJob{
				{
					Name:     "test",
					Template: batchv1.JobTemplateSpec{},
					Replicas: 1,
				},
			},
			replicatedJobStatus: []jobset.ReplicatedJobStatus{
				{
					Name:  "test",
					Ready: 2,
				},
			},
			jobs: []batchv1.Job{
				{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{
							jobset.ReplicatedJobNameKey: "test",
							jobset.JobIndexKey:          "0",
						},
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{
							jobset.ReplicatedJobNameKey: "test",
							jobset.JobIndexKey:          "1",
						},
					},
				},
			},
			expectedJobsThatWereDeleted: []batchv1.Job{
				{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{
							jobset.ReplicatedJobNameKey: "test",
							jobset.JobIndexKey:          "1",
						},
					},
				},
			},
		},
		{
			name: "elastic downscale is needed for second replicated job",
			replicatedJobs: []jobset.ReplicatedJob{
				{
					Name:     "test",
					Template: batchv1.JobTemplateSpec{},
					Replicas: 2,
				},
				{
					Name:     "test-2",
					Template: batchv1.JobTemplateSpec{},
					Replicas: 2,
				},
			},
			replicatedJobStatus: []jobset.ReplicatedJobStatus{
				{
					Name:  "test",
					Ready: 2,
				},
				{
					Name:  "test-2",
					Ready: 4,
				},
			},
			jobs: []batchv1.Job{
				{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{
							jobset.ReplicatedJobNameKey: "test",
							jobset.JobIndexKey:          "0",
						},
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{
							jobset.ReplicatedJobNameKey: "test",
							jobset.JobIndexKey:          "1",
						},
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{
							jobset.ReplicatedJobNameKey: "test-2",
							jobset.JobIndexKey:          "2",
						},
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{
							jobset.ReplicatedJobNameKey: "test-2",
							jobset.JobIndexKey:          "3",
						},
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{
							jobset.ReplicatedJobNameKey: "test-2",
							jobset.JobIndexKey:          "0",
						},
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{
							jobset.ReplicatedJobNameKey: "test-2",
							jobset.JobIndexKey:          "1",
						},
					},
				},
			},
			expectedJobsThatWereDeleted: []batchv1.Job{
				{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{
							jobset.ReplicatedJobNameKey: "test-2",
							jobset.JobIndexKey:          "3",
						},
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{
							jobset.ReplicatedJobNameKey: "test-2",
							jobset.JobIndexKey:          "2",
						},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := jobsToDeleteForDownScale(tc.replicatedJobs, tc.replicatedJobStatus, tc.jobs)
			if diff := cmp.Diff(tc.gotError, err); diff != "" {
				t.Errorf("unexpected finished value (+got/-want): %s", diff)
			}
			if len(actual) != len(tc.expectedJobsThatWereDeleted) {
				t.Errorf("unexpected length mismatch for deleted jobs: got: %d want: %d", len(actual), len(tc.expectedJobsThatWereDeleted))
			}
			if tc.expectedJobsThatWereDeleted != nil {
				for i := range actual {
					actualReplicatedJobName := actual[i].ObjectMeta.Labels[jobset.ReplicatedJobNameKey]
					actualJobIndexKey := actual[i].ObjectMeta.Labels[jobset.JobIndexKey]
					expectedReplicatedJobName := tc.expectedJobsThatWereDeleted[i].ObjectMeta.Labels[jobset.ReplicatedJobNameKey]
					expectedJobIndexKey := tc.expectedJobsThatWereDeleted[i].ObjectMeta.Labels[jobset.JobIndexKey]
					if diff := cmp.Diff(actualReplicatedJobName, expectedReplicatedJobName); diff != "" {
						t.Errorf("unexpected replicated job name (+got/-want): %s", diff)
					}
					if diff := cmp.Diff(actualJobIndexKey, expectedJobIndexKey); diff != "" {
						t.Errorf("unexpected job index (+got/-want): %s", diff)
					}
				}
			}
		})
	}
}
