package v1alpha2

import (
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"
)

// TestPodTemplate is the default pod template spec used for testing.
var TestPodTemplate = corev1.PodTemplateSpec{
	Spec: corev1.PodSpec{
		RestartPolicy: "Never",
		Containers: []corev1.Container{
			{
				Name:  "test-container",
				Image: "busybox:latest",
			},
		},
	},
}

func TestJobSetDefaulting(t *testing.T) {
	defaultSuccessPolicy := &SuccessPolicy{Operator: OperatorAll}
	testCases := []struct {
		name string
		js   *JobSet
		want *JobSet
	}{
		{
			name: "job completion mode is unset",
			js: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: TestPodTemplate,
								},
							},
							Network: &Network{EnableDNSHostnames: pointer.Bool(true)},
						},
					},
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
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
					SuccessPolicy: defaultSuccessPolicy,
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
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
					SuccessPolicy: defaultSuccessPolicy,
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
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
					SuccessPolicy: defaultSuccessPolicy,
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								},
							},
						},
					},
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
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
					SuccessPolicy: defaultSuccessPolicy,
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
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
					SuccessPolicy: defaultSuccessPolicy,
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: completionModePtr(batchv1.NonIndexedCompletion),
								},
							},
							Network: &Network{EnableDNSHostnames: pointer.Bool(false)},
						},
					},
				},
			},
		},
		{
			name: "pod restart policy unset",
			js: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{},
									},
									CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								},
							},
							Network: &Network{EnableDNSHostnames: pointer.Bool(true)},
						},
					},
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											RestartPolicy: corev1.RestartPolicyOnFailure,
										},
									},
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
			name: "pod restart policy set",
			js: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											RestartPolicy: corev1.RestartPolicyAlways,
										},
									},
									CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								},
							},
							Network: &Network{EnableDNSHostnames: pointer.Bool(true)},
						},
					},
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											RestartPolicy: corev1.RestartPolicyAlways,
										},
									},
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
			name: "success policy unset",
			js: &JobSet{
				Spec: JobSetSpec{
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											RestartPolicy: corev1.RestartPolicyAlways,
										},
									},
									CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								},
							},
							Network: &Network{EnableDNSHostnames: pointer.Bool(true)},
						},
					},
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											RestartPolicy: corev1.RestartPolicyAlways,
										},
									},
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
			name: "success policy operator set, replicatedJobNames unset",
			js: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: &SuccessPolicy{
						Operator: OperatorAny,
					},
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											RestartPolicy: corev1.RestartPolicyAlways,
										},
									},
									CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								},
							},
							Network: &Network{EnableDNSHostnames: pointer.Bool(true)},
						},
					},
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: &SuccessPolicy{
						Operator: OperatorAny,
					},
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											RestartPolicy: corev1.RestartPolicyAlways,
										},
									},
									CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								},
							},
							Network: &Network{EnableDNSHostnames: pointer.Bool(true)},
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

func TestValidateCreate(t *testing.T) {
	testCases := []struct {
		name string
		js   *JobSet
		want error
	}{
		{
			name: "number of pods exceeds the limit",
			js: &JobSet{
				Spec: JobSetSpec{
					ReplicatedJobs: []ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: math.MaxInt32,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: pointer.Int32(math.MaxInt32),
								},
							},
						},
						{
							Name:     "test-jobset-replicated-job-1",
							Replicas: math.MinInt32,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: pointer.Int32(math.MinInt32),
								},
							},
						},
					},
					SuccessPolicy: &SuccessPolicy{},
				},
			},
			want: errors.Join(
				fmt.Errorf("the product of replicas and parallelism must not exceed 2147483647 for replicatedJob 'test-jobset-replicated-job-0'"),
				fmt.Errorf("the product of replicas and parallelism must not exceed 2147483647 for replicatedJob 'test-jobset-replicated-job-1'"),
			),
		},
		{
			name: "number of pods within the limit",
			js: &JobSet{
				Spec: JobSetSpec{
					ReplicatedJobs: []ReplicatedJob{
						{
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{},
							},
						},
					},
					SuccessPolicy: &SuccessPolicy{},
				},
			},
			want: errors.Join(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.js.ValidateCreate(), tc.want)
		})
	}
}
