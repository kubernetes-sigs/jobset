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
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
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
	defaultStartupPolicy := &StartupPolicy{StartupPolicyOrder: AnyOrder}
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
					StartupPolicy: defaultStartupPolicy,
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
					ManagedBy: ptr.To(JobSetControllerName),
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
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
					ManagedBy: ptr.To(JobSetControllerName),
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
					ManagedBy: ptr.To(JobSetControllerName),
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
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
					ManagedBy: ptr.To(JobSetControllerName),
				},
			},
		},
		{
			name: "enableDNSHostnames is unset",
			js: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
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
					ManagedBy: ptr.To(JobSetControllerName),
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
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
					ManagedBy: ptr.To(JobSetControllerName),
				},
			},
		},
		{
			name: "enableDNSHostnames is false",
			js: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
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
					ManagedBy: ptr.To(JobSetControllerName),
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
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
					ManagedBy: ptr.To(JobSetControllerName),
				},
			},
		},
		{
			name: "pod restart policy unset",
			js: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
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
					ManagedBy: ptr.To(JobSetControllerName),
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
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
					ManagedBy: ptr.To(JobSetControllerName),
				},
			},
		},
		{
			name: "pod restart policy set",
			js: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
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
					ManagedBy: ptr.To(JobSetControllerName),
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
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
					ManagedBy: ptr.To(JobSetControllerName),
				},
			},
		},
		{
			name: "success policy unset",
			js: &JobSet{
				Spec: JobSetSpec{
					StartupPolicy: defaultStartupPolicy,
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
					ManagedBy: ptr.To(JobSetControllerName),
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					StartupPolicy: defaultStartupPolicy,
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
					ManagedBy: ptr.To(JobSetControllerName),
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
					Network:       &Network{EnableDNSHostnames: ptr.To(true)},
					StartupPolicy: defaultStartupPolicy,
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
					ManagedBy: ptr.To(JobSetControllerName),
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: &SuccessPolicy{
						Operator: OperatorAny,
					},
					StartupPolicy: defaultStartupPolicy,
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
					ManagedBy: ptr.To(JobSetControllerName),
				},
			},
		},
		{
			name: "startup policy order InOrder set",
			js: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: &SuccessPolicy{
						Operator: OperatorAny,
					},
					Network: &Network{EnableDNSHostnames: ptr.To(true)},
					StartupPolicy: &StartupPolicy{
						StartupPolicyOrder: InOrder,
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
						},
					},
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: &SuccessPolicy{
						Operator: OperatorAny,
					},
					StartupPolicy: &StartupPolicy{
						StartupPolicyOrder: InOrder,
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
					ManagedBy: ptr.To(JobSetControllerName),
				},
			},
		},
		{
			name: "managed-by label is unset",
			js: &JobSet{
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
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
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
					ManagedBy: ptr.To(JobSetControllerName),
				},
			},
		},
		{
			name: "when provided, managed-by label is preserved",
			js: &JobSet{
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
					ManagedBy: ptr.To("other-controller"),
				},
			},
			want: &JobSet{
				Spec: JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
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
					ManagedBy: ptr.To("other-controller"),
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
	managedByFieldPath := field.NewPath("spec", "managedBy")

	notDomainPrefixedPathControllerName := "notDomainPrefixedPathControllerName"
	var notDomainPrefixedPathControllerErrors []error
	for _, err := range validation.IsDomainPrefixedPath(managedByFieldPath, notDomainPrefixedPathControllerName) {
		notDomainPrefixedPathControllerErrors = append(notDomainPrefixedPathControllerErrors, err)
	}

	maxManagedByLength := 63
	tooLongControllerName := "foo.bar/" + strings.Repeat("a", maxManagedByLength)
	tooLongControllerNameError := field.TooLongMaxLength(managedByFieldPath, tooLongControllerName, maxManagedByLength)

	validObjectMeta := metav1.ObjectMeta{
		Name: "js",
	}

	testCases := []struct {
		name string
		js   *JobSet
		want error
	}{
		{
			name: "number of pods exceeds the limit",
			js: &JobSet{
				ObjectMeta: validObjectMeta,
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
				ObjectMeta: validObjectMeta,
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
					SuccessPolicy: &SuccessPolicy{},
				},
			},
			want: errors.Join(),
		},
		{
			name: "success policy has non matching replicated job",
			js: &JobSet{
				ObjectMeta: validObjectMeta,
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
				ObjectMeta: validObjectMeta,
				Spec: JobSetSpec{
					Network: &Network{
						EnableDNSHostnames: ptr.To(true),
						Subdomain:          strings.Repeat("a", 64),
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
				fmt.Errorf(subdomainTooLongErrMsg),
			),
		},
		{
			name: "jobset name with invalid character",
			js: &JobSet{
				ObjectMeta: validObjectMeta,
				Spec: JobSetSpec{
					ReplicatedJobs: []ReplicatedJob{
						{
							Name:     "username.llama65b",
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
				fmt.Errorf("a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')"),
			),
		},
		{
			name: "jobset name will result in job name being too long",
			js: &JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: strings.Repeat("a", 62),
				},
				Spec: JobSetSpec{
					ReplicatedJobs: []ReplicatedJob{
						{
							Name:     "rj",
							Replicas: 101,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{},
							},
						},
					},
					SuccessPolicy: &SuccessPolicy{},
				},
			},
			want: errors.Join(
				fmt.Errorf(jobNameTooLongErrorMsg),
			),
		},
		{
			name: "jobset name will result in a pod name being too long",
			js: &JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: strings.Repeat("a", 56),
				},
				Spec: JobSetSpec{
					ReplicatedJobs: []ReplicatedJob{
						{
							Name:     "rj",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(1)),
									Parallelism:    ptr.To(int32(1)),
								},
							},
						},
					},
					SuccessPolicy: &SuccessPolicy{},
				},
			},
			want: errors.Join(
				fmt.Errorf(podNameTooLongErrorMsg),
			),
		},
		{
			name: "jobset controller name is not a domain-prefixed path",
			js: &JobSet{
				ObjectMeta: validObjectMeta,
				Spec: JobSetSpec{
					ManagedBy: ptr.To(notDomainPrefixedPathControllerName),
					ReplicatedJobs: []ReplicatedJob{
						{
							Name:     "rj",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(1)),
									Parallelism:    ptr.To(int32(1)),
								},
							},
						},
					},
					SuccessPolicy: &SuccessPolicy{},
				},
			},
			want: errors.Join(
				notDomainPrefixedPathControllerErrors...,
			),
		},
		{
			name: "jobset controller name is too long",
			js: &JobSet{
				ObjectMeta: validObjectMeta,
				Spec: JobSetSpec{
					ManagedBy: ptr.To(tooLongControllerName),
					ReplicatedJobs: []ReplicatedJob{
						{
							Name:     "rj",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(1)),
									Parallelism:    ptr.To(int32(1)),
								},
							},
						},
					},
					SuccessPolicy: &SuccessPolicy{},
				},
			},
			want: errors.Join(
				tooLongControllerNameError,
			),
		},
		{
			name: "jobset controller name is set and valid",
			js: &JobSet{
				ObjectMeta: validObjectMeta,
				Spec: JobSetSpec{
					ManagedBy: ptr.To(JobSetControllerName),
					ReplicatedJobs: []ReplicatedJob{
						{
							Name:     "rj",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(1)),
									Parallelism:    ptr.To(int32(1)),
								},
							},
						},
					},
					SuccessPolicy: &SuccessPolicy{},
				},
			},
			want: errors.Join(),
		},
		{
			name: "jobset controller name is unset",
			js: &JobSet{
				ObjectMeta: validObjectMeta,
				Spec: JobSetSpec{
					ManagedBy: ptr.To(JobSetControllerName),
					ReplicatedJobs: []ReplicatedJob{
						{
							Name:     "rj",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(1)),
									Parallelism:    ptr.To(int32(1)),
								},
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
			_, err := tc.js.ValidateCreate()
			assert.Equal(t, err, tc.want)
		})
	}
}
