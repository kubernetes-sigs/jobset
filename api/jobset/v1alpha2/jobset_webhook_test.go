package v1alpha2

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
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
					Network:       &Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: TestPodTemplate,
								},
							},
						},
					},
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					Network:       &Network{EnableDNSHostnames: ptr.To(true)},
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
		},
		{
			name: "job completion mode is set to non-indexed",
			js: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					Network:       &Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: completionModePtr(batchv1.NonIndexedCompletion),
								},
							},
						},
					},
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					Network:       &Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: completionModePtr(batchv1.NonIndexedCompletion),
								},
							},
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
					Network:       &Network{EnableDNSHostnames: ptr.To(true)},
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
		},
		{
			name: "subdomain defaults to jobset name",
			js: &JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "custom-jobset",
				},
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
				ObjectMeta: metav1.ObjectMeta{
					Name: "custom-jobset",
				},
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					Network: &Network{
						EnableDNSHostnames: ptr.To(true),
						Subdomain:          "custom-jobset",
					},
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
		},
		{
			name: "enableDNSHostnames is false",
			js: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					Network:       &Network{EnableDNSHostnames: ptr.To(false)},
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: completionModePtr(batchv1.NonIndexedCompletion),
								},
							},
						},
					},
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					Network:       &Network{EnableDNSHostnames: ptr.To(false)},
					ReplicatedJobs: []ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: completionModePtr(batchv1.NonIndexedCompletion),
								},
							},
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
					Network:       &Network{EnableDNSHostnames: ptr.To(true)},
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
						},
					},
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					Network:       &Network{EnableDNSHostnames: ptr.To(true)},
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
					Network:       &Network{EnableDNSHostnames: ptr.To(true)},
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
						},
					},
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					Network:       &Network{EnableDNSHostnames: ptr.To(true)},
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
						},
					},
				},
			},
		},
		{
			name: "success policy unset",
			js: &JobSet{
				Spec: JobSetSpec{
					Network: &Network{EnableDNSHostnames: ptr.To(true)},
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
						},
					},
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					Network:       &Network{EnableDNSHostnames: ptr.To(true)},
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
					Network: &Network{EnableDNSHostnames: ptr.To(true)},
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
						},
					},
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: &SuccessPolicy{
						Operator: OperatorAny,
					},
					Network: &Network{EnableDNSHostnames: ptr.To(true)},
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
									Parallelism: ptr.To[int32](math.MaxInt32),
								},
							},
						},
						{
							Name:     "test-jobset-replicated-job-1",
							Replicas: math.MinInt32,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To[int32](math.MinInt32),
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
		{
			name: "success policy has non matching replicated job",
			js: &JobSet{
				Spec: JobSetSpec{
					ReplicatedJobs: []ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{},
							},
						},
					},
					SuccessPolicy: &SuccessPolicy{
						TargetReplicatedJobs: []string{"do not exist"},
					},
				},
			},
			want: errors.Join(
				fmt.Errorf("invalid replicatedJob name 'do not exist' does not appear in .spec.ReplicatedJobs"),
			),
		},
		{
			name: "network has invalid dns name",
			js: &JobSet{
				Spec: JobSetSpec{
					Network: &Network{
						EnableDNSHostnames: ptr.To(true),
						Subdomain:          strings.Repeat("a", 257),
					},
					ReplicatedJobs: []ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{},
							},
						},
					},
					SuccessPolicy: &SuccessPolicy{},
				},
			},
			want: errors.Join(
				fmt.Errorf("must be no more than 253 characters"),
			),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.js.ValidateCreate()
			assert.Equal(t, err, tc.want)
		})
	}
}
