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

package workload

import (
	"testing"

	schedulingv1alpha1 "k8s.io/api/scheduling/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

func TestGenWorkloadName(t *testing.T) {
	tests := []struct {
		name     string
		jobSet   *jobset.JobSet
		expected string
	}{
		{
			name: "simple jobset name",
			jobSet: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jobset",
				},
			},
			expected: "test-jobset",
		},
		{
			name: "jobset with namespace",
			jobSet: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-training-job",
					Namespace: "ml-team",
				},
			},
			expected: "my-training-job",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := GenWorkloadName(tc.jobSet)
			if result != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, result)
			}
		})
	}
}

func TestConstructWorkloadFromTemplate(t *testing.T) {
	tests := []struct {
		name              string
		jobSet            *jobset.JobSet
		expectedName      string
		expectedNamespace string
		expectedLabels    map[string]string
	}{
		{
			name: "workload template with single pod group",
			jobSet: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-jobset",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: jobset.JobSetSpec{
					GangPolicy: &jobset.GangPolicy{
						WorkloadTemplate: &schedulingv1alpha1.WorkloadSpec{
							PodGroups: []schedulingv1alpha1.PodGroup{
								{
									Name: "all-pods",
									Policy: schedulingv1alpha1.PodGroupPolicy{
										Gang: &schedulingv1alpha1.GangSchedulingPolicy{
											MinCount: 10,
										},
									},
								},
							},
						},
					},
				},
			},
			expectedName:      "test-jobset",
			expectedNamespace: "default",
			expectedLabels: map[string]string{
				jobset.JobSetNameKey: "test-jobset",
				jobset.JobSetUIDKey:  "test-uid",
			},
		},
		{
			name: "workload template with multiple pod groups",
			jobSet: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ml-training",
					Namespace: "ml-team",
					UID:       "ml-uid",
				},
				Spec: jobset.JobSetSpec{
					GangPolicy: &jobset.GangPolicy{
						WorkloadTemplate: &schedulingv1alpha1.WorkloadSpec{
							PodGroups: []schedulingv1alpha1.PodGroup{
								{
									Name: "leader",
									Policy: schedulingv1alpha1.PodGroupPolicy{
										Gang: &schedulingv1alpha1.GangSchedulingPolicy{
											MinCount: 1,
										},
									},
								},
								{
									Name: "workers",
									Policy: schedulingv1alpha1.PodGroupPolicy{
										Gang: &schedulingv1alpha1.GangSchedulingPolicy{
											MinCount: 8,
										},
									},
								},
							},
						},
					},
				},
			},
			expectedName:      "ml-training",
			expectedNamespace: "ml-team",
			expectedLabels: map[string]string{
				jobset.JobSetNameKey: "ml-training",
				jobset.JobSetUIDKey:  "ml-uid",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			workload := ConstructWorkloadFromTemplate(tc.jobSet)

			// Verify workload metadata
			if workload.Name != tc.expectedName {
				t.Errorf("expected workload name %s, got %s", tc.expectedName, workload.Name)
			}
			if workload.Namespace != tc.expectedNamespace {
				t.Errorf("expected namespace %s, got %s", tc.expectedNamespace, workload.Namespace)
			}

			// Verify labels
			for key, expectedValue := range tc.expectedLabels {
				if actualValue, ok := workload.Labels[key]; !ok {
					t.Errorf("expected label %s to exist", key)
				} else if actualValue != expectedValue {
					t.Errorf("label %s: expected %s, got %s", key, expectedValue, actualValue)
				}
			}

			// Verify spec is copied from template
			if len(workload.Spec.PodGroups) != len(tc.jobSet.Spec.GangPolicy.WorkloadTemplate.PodGroups) {
				t.Errorf("expected %d pod groups, got %d",
					len(tc.jobSet.Spec.GangPolicy.WorkloadTemplate.PodGroups),
					len(workload.Spec.PodGroups))
			}
		})
	}
}
