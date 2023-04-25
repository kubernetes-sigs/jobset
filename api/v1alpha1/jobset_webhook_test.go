package v1alpha1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/utils/pointer"
)

func TestJobSetDefaulting(t *testing.T) {
	testCases := []struct {
		name string
		js   *JobSet
		want *JobSet
	}{
		{
			name: "job completion mode is unset",
			js: &JobSet{
				Spec: JobSetSpec{
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{},
							},
							Network: &Network{EnableDNSHostnames: pointer.Bool(true)},
						},
					},
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								},
							},
							Network: &Network{EnableDNSHostnames: pointer.Bool(true)},
						},
					},
				},
			},
		},
		{
			name: "job completion mode is set to non-indexed",
			js: &JobSet{
				Spec: JobSetSpec{
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: completionModePtr(batchv1.NonIndexedCompletion),
								},
							},
							Network: &Network{EnableDNSHostnames: pointer.Bool(true)},
						},
					},
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: completionModePtr(batchv1.NonIndexedCompletion),
								},
							},
							Network: &Network{EnableDNSHostnames: pointer.Bool(true)},
						},
					},
				},
			},
		},
		{
			name: "enableDNSHostnames is unset",
			js: &JobSet{
				Spec: JobSetSpec{
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								},
							},
						},
					},
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								},
							},
							Network: &Network{EnableDNSHostnames: pointer.Bool(true)},
						},
					},
				},
			},
		},
		{
			name: "enableDNSHostnames is false",
			js: &JobSet{
				Spec: JobSetSpec{
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: completionModePtr(batchv1.NonIndexedCompletion),
								},
							},
							Network: &Network{EnableDNSHostnames: pointer.Bool(false)},
						},
					},
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: completionModePtr(batchv1.NonIndexedCompletion),
								},
							},
							Network: &Network{EnableDNSHostnames: pointer.Bool(false)},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.js.Default()
			if diff := cmp.Diff(tc.want, tc.js); diff != "" {
				t.Errorf("unexpected jobset defaulting: (-want/+got): %s", diff)
			}
		})
	}
}
