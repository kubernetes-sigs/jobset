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

	batchv1 "k8s.io/api/batch/v1"
	schedulingv1alpha1 "k8s.io/api/scheduling/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

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

func TestConstructWorkloadForJobSetAsGang(t *testing.T) {
	tests := []struct {
		name                 string
		jobSet               *jobset.JobSet
		expectedPodGroupName string
		expectedMinCount     int32
		expectedPodGroups    int
	}{
		{
			name: "single replicated job with parallelism",
			jobSet: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-jobset",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "worker",
							Replicas: 3,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To(int32(4)),
								},
							},
						},
					},
				},
			},
			expectedPodGroupName: "test-jobset",
			expectedMinCount:     12, // 3 replicas * 4 parallelism
			expectedPodGroups:    1,
		},
		{
			name: "multiple replicated jobs",
			jobSet: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ml-training",
					Namespace: "ml-team",
					UID:       "ml-uid",
				},
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "leader",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To(int32(1)),
								},
							},
						},
						{
							Name:     "worker",
							Replicas: 10,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To(int32(8)),
								},
							},
						},
					},
				},
			},
			expectedPodGroupName: "ml-training",
			expectedMinCount:     81, // (1*1) + (10*8)
			expectedPodGroups:    1,
		},
		{
			name: "replicated job with completions less than parallelism",
			jobSet: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "batch-job",
					Namespace: "default",
					UID:       "batch-uid",
				},
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "worker",
							Replicas: 2,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To(int32(10)),
									Completions: ptr.To(int32(5)),
								},
							},
						},
					},
				},
			},
			expectedPodGroupName: "batch-job",
			expectedMinCount:     10, // 2 replicas * min(10, 5)
			expectedPodGroups:    1,
		},
		{
			name: "replicated job with no parallelism set (defaults to 1)",
			jobSet: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-job",
					Namespace: "default",
					UID:       "simple-uid",
				},
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "worker",
							Replicas: 3,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									// No parallelism set, defaults to 1
								},
							},
						},
					},
				},
			},
			expectedPodGroupName: "simple-job",
			expectedMinCount:     3, // 3 replicas * 1 (default parallelism)
			expectedPodGroups:    1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			workload := ConstructWorkloadForJobSetAsGang(tc.jobSet)

			// Verify workload metadata
			if workload.Name != tc.jobSet.Name {
				t.Errorf("expected workload name %s, got %s", tc.jobSet.Name, workload.Name)
			}
			if workload.Namespace != tc.jobSet.Namespace {
				t.Errorf("expected namespace %s, got %s", tc.jobSet.Namespace, workload.Namespace)
			}

			// Verify labels
			if workload.Labels[jobset.JobSetNameKey] != tc.jobSet.Name {
				t.Errorf("expected JobSetNameKey label %s, got %s", tc.jobSet.Name, workload.Labels[jobset.JobSetNameKey])
			}
			if workload.Labels[jobset.JobSetUIDKey] != string(tc.jobSet.UID) {
				t.Errorf("expected JobSetUIDKey label %s, got %s", string(tc.jobSet.UID), workload.Labels[jobset.JobSetUIDKey])
			}

			// Verify pod groups
			if len(workload.Spec.PodGroups) != tc.expectedPodGroups {
				t.Errorf("expected %d pod groups, got %d", tc.expectedPodGroups, len(workload.Spec.PodGroups))
			}

			if len(workload.Spec.PodGroups) > 0 {
				podGroup := workload.Spec.PodGroups[0]
				if podGroup.Name != tc.expectedPodGroupName {
					t.Errorf("expected pod group name %s, got %s", tc.expectedPodGroupName, podGroup.Name)
				}

				if podGroup.Policy.Gang == nil {
					t.Fatal("expected gang policy to be set")
				}
				if podGroup.Policy.Gang.MinCount != tc.expectedMinCount {
					t.Errorf("expected minCount %d, got %d", tc.expectedMinCount, podGroup.Policy.Gang.MinCount)
				}
			}
		})
	}
}

func TestConstructWorkloadForJobSetGangPerReplicatedJob(t *testing.T) {
	tests := []struct {
		name              string
		jobSet            *jobset.JobSet
		expectedPodGroups int
		podGroupChecks    []struct {
			name     string
			minCount int32
		}
	}{
		{
			name: "single replicated job",
			jobSet: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-jobset",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "worker",
							Replicas: 3,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To(int32(4)),
								},
							},
						},
					},
				},
			},
			expectedPodGroups: 1,
			podGroupChecks: []struct {
				name     string
				minCount int32
			}{
				{name: "worker", minCount: 12}, // 3 * 4
			},
		},
		{
			name: "multiple replicated jobs",
			jobSet: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ml-training",
					Namespace: "ml-team",
					UID:       "ml-uid",
				},
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "leader",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To(int32(1)),
								},
							},
						},
						{
							Name:     "worker",
							Replicas: 5,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To(int32(8)),
								},
							},
						},
						{
							Name:     "ps",
							Replicas: 2,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To(int32(2)),
								},
							},
						},
					},
				},
			},
			expectedPodGroups: 3,
			podGroupChecks: []struct {
				name     string
				minCount int32
			}{
				{name: "leader", minCount: 1},  // 1 * 1
				{name: "worker", minCount: 40}, // 5 * 8
				{name: "ps", minCount: 4},      // 2 * 2
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			workload := ConstructWorkloadForJobSetGangPerReplicatedJob(tc.jobSet)

			// Verify workload metadata
			if workload.Name != tc.jobSet.Name {
				t.Errorf("expected workload name %s, got %s", tc.jobSet.Name, workload.Name)
			}

			// Verify pod groups count
			if len(workload.Spec.PodGroups) != tc.expectedPodGroups {
				t.Errorf("expected %d pod groups, got %d", tc.expectedPodGroups, len(workload.Spec.PodGroups))
			}

			// Verify each pod group
			for i, check := range tc.podGroupChecks {
				if i >= len(workload.Spec.PodGroups) {
					t.Errorf("expected pod group %d to exist", i)
					continue
				}

				podGroup := workload.Spec.PodGroups[i]
				if podGroup.Name != check.name {
					t.Errorf("pod group %d: expected name %s, got %s", i, check.name, podGroup.Name)
				}

				if podGroup.Policy.Gang == nil {
					t.Errorf("pod group %d: expected gang policy to be set", i)
					continue
				}

				if podGroup.Policy.Gang.MinCount != check.minCount {
					t.Errorf("pod group %d: expected minCount %d, got %d", i, check.minCount, podGroup.Policy.Gang.MinCount)
				}
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
			name: "workload template with custom labels and annotations",
			jobSet: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "custom-jobset",
					Namespace: "custom-ns",
					UID:       "custom-uid",
				},
				Spec: jobset.JobSetSpec{
					GangPolicy: &jobset.GangPolicy{
						Workload: &schedulingv1alpha1.Workload{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"custom-label": "custom-value",
								},
								Annotations: map[string]string{
									"custom-annotation": "annotation-value",
								},
							},
							Spec: schedulingv1alpha1.WorkloadSpec{
								PodGroups: []schedulingv1alpha1.PodGroup{
									{
										Name: "custom-group",
										Policy: schedulingv1alpha1.PodGroupPolicy{
											Gang: &schedulingv1alpha1.GangSchedulingPolicy{
												MinCount: 100,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectedName:      "custom-jobset",
			expectedNamespace: "custom-ns",
			expectedLabels: map[string]string{
				"custom-label":       "custom-value",
				jobset.JobSetNameKey: "custom-jobset",
				jobset.JobSetUIDKey:  "custom-uid",
			},
		},
		{
			name: "workload template with existing jobset labels (should be overwritten)",
			jobSet: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-jobset",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: jobset.JobSetSpec{
					GangPolicy: &jobset.GangPolicy{
						Workload: &schedulingv1alpha1.Workload{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									jobset.JobSetNameKey: "wrong-name",
									jobset.JobSetUIDKey:  "wrong-uid",
								},
							},
							Spec: schedulingv1alpha1.WorkloadSpec{
								PodGroups: []schedulingv1alpha1.PodGroup{
									{
										Name: "group1",
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
			},
			expectedName:      "test-jobset",
			expectedNamespace: "default",
			expectedLabels: map[string]string{
				jobset.JobSetNameKey: "test-jobset",
				jobset.JobSetUIDKey:  "test-uid",
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
			if len(workload.Spec.PodGroups) != len(tc.jobSet.Spec.GangPolicy.Workload.Spec.PodGroups) {
				t.Errorf("expected %d pod groups, got %d",
					len(tc.jobSet.Spec.GangPolicy.Workload.Spec.PodGroups),
					len(workload.Spec.PodGroups))
			}

			// Verify annotations are copied
			if tc.jobSet.Spec.GangPolicy.Workload.Annotations != nil {
				for key, expectedValue := range tc.jobSet.Spec.GangPolicy.Workload.Annotations {
					if actualValue, ok := workload.Annotations[key]; !ok {
						t.Errorf("expected annotation %s to exist", key)
					} else if actualValue != expectedValue {
						t.Errorf("annotation %s: expected %s, got %s", key, expectedValue, actualValue)
					}
				}
			}
		})
	}
}

func TestGetPodGroupName(t *testing.T) {
	jobSetAsGang := jobset.JobSetAsGang
	jobSetGangPerReplicatedJob := jobset.JobSetGangPerReplicatedJob
	jobSetWorkloadTemplate := jobset.JobSetWorkloadTemplate

	tests := []struct {
		name     string
		jobSet   *jobset.JobSet
		rjobName string
		expected string
	}{
		{
			name: "nil gang policy returns empty string",
			jobSet: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jobset",
				},
				Spec: jobset.JobSetSpec{
					GangPolicy: nil,
				},
			},
			rjobName: "worker",
			expected: "",
		},
		{
			name: "nil gang policy option returns empty string",
			jobSet: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jobset",
				},
				Spec: jobset.JobSetSpec{
					GangPolicy: &jobset.GangPolicy{
						Policy: nil,
					},
				},
			},
			rjobName: "worker",
			expected: "",
		},
		{
			name: "JobSetAsGang returns jobset name",
			jobSet: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-training-job",
				},
				Spec: jobset.JobSetSpec{
					GangPolicy: &jobset.GangPolicy{
						Policy: &jobSetAsGang,
					},
				},
			},
			rjobName: "worker",
			expected: "my-training-job",
		},
		{
			name: "JobSetAsGang returns jobset name regardless of rjob name",
			jobSet: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "distributed-training",
				},
				Spec: jobset.JobSetSpec{
					GangPolicy: &jobset.GangPolicy{
						Policy: &jobSetAsGang,
					},
				},
			},
			rjobName: "leader",
			expected: "distributed-training",
		},
		{
			name: "JobSetGangPerReplicatedJob returns rjob name",
			jobSet: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-training-job",
				},
				Spec: jobset.JobSetSpec{
					GangPolicy: &jobset.GangPolicy{
						Policy: &jobSetGangPerReplicatedJob,
					},
				},
			},
			rjobName: "worker",
			expected: "worker",
		},
		{
			name: "JobSetGangPerReplicatedJob returns different rjob names correctly",
			jobSet: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-training-job",
				},
				Spec: jobset.JobSetSpec{
					GangPolicy: &jobset.GangPolicy{
						Policy: &jobSetGangPerReplicatedJob,
					},
				},
			},
			rjobName: "parameter-server",
			expected: "parameter-server",
		},
		{
			name: "JobSetWorkloadTemplate returns rjob name",
			jobSet: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "custom-workload-job",
				},
				Spec: jobset.JobSetSpec{
					GangPolicy: &jobset.GangPolicy{
						Policy: &jobSetWorkloadTemplate,
						Workload: &schedulingv1alpha1.Workload{
							Spec: schedulingv1alpha1.WorkloadSpec{
								PodGroups: []schedulingv1alpha1.PodGroup{
									{Name: "custom-group"},
								},
							},
						},
					},
				},
			},
			rjobName: "workers",
			expected: "workers",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := GetPodGroupName(tc.jobSet, tc.rjobName)
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}
