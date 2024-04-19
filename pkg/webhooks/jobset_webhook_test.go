package webhooks

import (
	"context"
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

	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
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
	defaultSuccessPolicy := &jobset.SuccessPolicy{Operator: jobset.OperatorAll}
	defaultStartupPolicy := &jobset.StartupPolicy{StartupPolicyOrder: jobset.AnyOrder}
	testCases := []struct {
		name string
		js   *jobset.JobSet
		want *jobset.JobSet
	}{
		{
			name: "job completion mode is unset",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: TestPodTemplate,
								},
							},
						},
					},
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
			want: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								},
							},
						},
					},
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
		},
		{
			name: "job completion mode is set to non-indexed",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: completionModePtr(batchv1.NonIndexedCompletion),
								},
							},
						},
					},
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
			want: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: completionModePtr(batchv1.NonIndexedCompletion),
								},
							},
						},
					},
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
		},
		{
			name: "enableDNSHostnames is unset",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								},
							},
						},
					},
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
			want: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								},
							},
						},
					},
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
		},
		{
			name: "enableDNSHostnames is false",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(false)},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: completionModePtr(batchv1.NonIndexedCompletion),
								},
							},
						},
					},
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
			want: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(false)},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: completionModePtr(batchv1.NonIndexedCompletion),
								},
							},
						},
					},
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
		},
		{
			name: "pod restart policy unset",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
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
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
			want: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
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
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
		},
		{
			name: "pod restart policy set",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
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
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
			want: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
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
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
		},
		{
			name: "success policy unset",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					StartupPolicy: defaultStartupPolicy,
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
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
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
			want: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					StartupPolicy: defaultStartupPolicy,
					SuccessPolicy: defaultSuccessPolicy,
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
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
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
		},
		{
			name: "success policy operator set, replicatedJobNames unset",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{
						Operator: jobset.OperatorAny,
					},
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(true)},
					StartupPolicy: defaultStartupPolicy,
					ReplicatedJobs: []jobset.ReplicatedJob{
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
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
			want: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{
						Operator: jobset.OperatorAny,
					},
					StartupPolicy: defaultStartupPolicy,
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
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
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
		},
		{
			name: "startup policy order InOrder set",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{
						Operator: jobset.OperatorAny,
					},
					Network: &jobset.Network{EnableDNSHostnames: ptr.To(true)},
					StartupPolicy: &jobset.StartupPolicy{
						StartupPolicyOrder: jobset.InOrder,
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
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
			want: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{
						Operator: jobset.OperatorAny,
					},
					StartupPolicy: &jobset.StartupPolicy{
						StartupPolicyOrder: jobset.InOrder,
					},
					Network: &jobset.Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
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
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
		},
		{
			name: "managed-by label is unset",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
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
			want: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								},
							},
						},
					},
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
		},
		{
			name: "when provided, managed-by label is preserved",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
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
			want: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
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
	fakeClient := fake.NewFakeClient()
	webhook, err := NewJobSetWebhook(fakeClient)
	if err != nil {
		t.Fatalf("error creating jobset webhook: %v", err)
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			obj := tc.js.DeepCopyObject()
			if err := webhook.Default(context.TODO(), obj); err != nil {
				t.Errorf("unexpected error defaulting jobset: %v", err)
			}
			if diff := cmp.Diff(tc.want, obj.(*jobset.JobSet)); diff != "" {
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

	validPodTemplateSpec := corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test",
					Image: "bash:latest",
				},
			},
		},
	}

	testCases := []struct {
		name string
		js   *jobset.JobSet
		want error
	}{
		{
			name: "number of pods exceeds the limit",
			js: &jobset.JobSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "JobSet",
					APIVersion: "jobset.x-k8s.io/v1alpha2",
				},
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: math.MaxInt32,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To[int32](math.MaxInt32),
									Template:    validPodTemplateSpec,
								},
							},
						},
						{
							Name:     "test-jobset-replicated-job-1",
							Replicas: math.MinInt32,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To[int32](math.MinInt32),
									Template:    validPodTemplateSpec,
								},
							},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{
						Operator: jobset.OperatorAll,
					},
				},
			},
			want: errors.Join(
				fmt.Errorf("the product of replicas and parallelism must not exceed 2147483647 for replicatedJob 'test-jobset-replicated-job-0'"),
				fmt.Errorf("the product of replicas and parallelism must not exceed 2147483647 for replicatedJob 'test-jobset-replicated-job-1'"),
			),
		},
		{
			name: "number of pods within the limit",
			js: &jobset.JobSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "JobSet",
					APIVersion: "jobset.x-k8s.io/v1alpha2",
				},
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{
						Operator: jobset.OperatorAll,
					},
				},
			},
			want: errors.Join(),
		},
		{
			name: "success policy has non matching replicated job",
			js: &jobset.JobSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "JobSet",
					APIVersion: "jobset.x-k8s.io/v1alpha2",
				},
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{
						Operator:             jobset.OperatorAll,
						TargetReplicatedJobs: []string{"does not exist"},
					},
				},
			},
			want: errors.Join(
				fmt.Errorf("invalid replicatedJob name 'does not exist' does not appear in .spec.ReplicatedJobs"),
			),
		},
		{
			name: "network has invalid dns name",
			js: &jobset.JobSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "JobSet",
					APIVersion: "jobset.x-k8s.io/v1alpha2",
				},
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					Network: &jobset.Network{
						EnableDNSHostnames: ptr.To(true),
						Subdomain:          strings.Repeat("a", 64),
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{
						Operator: jobset.OperatorAll,
					},
				},
			},
			want: errors.Join(
				fmt.Errorf(subdomainTooLongErrMsg),
			),
		},
		{
			name: "jobset name with invalid character",
			js: &jobset.JobSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "JobSet",
					APIVersion: "jobset.x-k8s.io/v1alpha2",
				},
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "username.llama65b",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{
						Operator: jobset.OperatorAll,
					},
				},
			},
			want: errors.Join(
				fmt.Errorf("a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')"),
			),
		},
		{
			name: "jobset name will result in job name being too long",
			js: &jobset.JobSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "JobSet",
					APIVersion: "jobset.x-k8s.io/v1alpha2",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: strings.Repeat("a", 62),
				},
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "rj",
							Replicas: 101,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{
						Operator: jobset.OperatorAll,
					},
				},
			},
			want: errors.Join(
				fmt.Errorf(jobNameTooLongErrorMsg),
			),
		},
		{
			name: "jobset name will result in a pod name being too long",
			js: &jobset.JobSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "JobSet",
					APIVersion: "jobset.x-k8s.io/v1alpha2",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: strings.Repeat("a", 56),
				},
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "rj",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(1)),
									Parallelism:    ptr.To(int32(1)),
									Template:       validPodTemplateSpec,
								},
							},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{
						Operator: jobset.OperatorAll,
					},
				},
			},
			want: errors.Join(
				fmt.Errorf(podNameTooLongErrorMsg),
			),
		},
		{
			name: "jobset controller name is not a domain-prefixed path",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ManagedBy: ptr.To(notDomainPrefixedPathControllerName),
					ReplicatedJobs: []jobset.ReplicatedJob{
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
					SuccessPolicy: &jobset.SuccessPolicy{},
				},
			},
			want: errors.Join(
				notDomainPrefixedPathControllerErrors...,
			),
		},
		{
			name: "jobset controller name is too long",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ManagedBy: ptr.To(tooLongControllerName),
					ReplicatedJobs: []jobset.ReplicatedJob{
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
					SuccessPolicy: &jobset.SuccessPolicy{},
				},
			},
			want: errors.Join(
				tooLongControllerNameError,
			),
		},
		{
			name: "jobset controller name is set and valid",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ManagedBy: ptr.To(jobset.JobSetControllerName),
					ReplicatedJobs: []jobset.ReplicatedJob{
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
					SuccessPolicy: &jobset.SuccessPolicy{},
				},
			},
			want: errors.Join(),
		},
		{
			name: "jobset controller name is unset",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
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
					SuccessPolicy: &jobset.SuccessPolicy{},
				},
			},
			want: errors.Join(),
		},
	}
	fakeClient := fake.NewFakeClient()
	webhook, err := NewJobSetWebhook(fakeClient)
	if err != nil {
		t.Fatalf("error creating jobset webhook: %v", err)
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := webhook.ValidateCreate(context.TODO(), tc.js.DeepCopyObject())
			if err != nil && tc.want != nil {
				assert.Contains(t, err.Error(), tc.want.Error())
			} else if err != nil && tc.want == nil {
				t.Errorf("unexpected error: %v", err)
			} else if err == nil && tc.want != nil {
				t.Errorf("missing expected error: %v", tc.want)
			}
		})
	}
}

func TestValidateUpdate(t *testing.T) {
	validObjectMeta := metav1.ObjectMeta{
		Name: "js",
	}
	validReplicatedJobs := []jobset.ReplicatedJob{
		{
			Name:     "test-jobset-replicated-job-0",
			Replicas: 1,
			Template: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Parallelism: ptr.To[int32](1),
				},
			},
		},
		{
			Name:     "test-jobset-replicated-job-1",
			Replicas: 1,
			Template: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Parallelism: ptr.To[int32](1),
				},
			},
		},
	}
	testCases := []struct {
		name  string
		oldJs *jobset.JobSet
		js    *jobset.JobSet
		want  error
	}{
		{
			name: "update suspend",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: validReplicatedJobs,
				},
			},
			oldJs: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					Suspend:        ptr.To(true),
					ReplicatedJobs: validReplicatedJobs,
				},
			},
		},
		{
			name: "update labels",
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "js", Labels: map[string]string{"hello": "world"}},
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: validReplicatedJobs,
				},
			},
			oldJs: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					Suspend:        ptr.To(true),
					ReplicatedJobs: validReplicatedJobs,
				},
			},
		},
		{
			name: "managedBy is immutable",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ManagedBy:      ptr.To("example.com/new"),
					ReplicatedJobs: validReplicatedJobs,
				},
			},
			oldJs: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ManagedBy:      ptr.To("example.com/old"),
					ReplicatedJobs: validReplicatedJobs,
				},
			},
			want: field.ErrorList{
				field.Invalid(field.NewPath("spec").Child("managedBy"), ptr.To("example.com/new"), "field is immutable"),
			}.ToAggregate(),
		},
		{
			name: "replicated jobs are immutable",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: 2,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To[int32](2),
								},
							},
						},
						{
							Name:     "test-jobset-replicated-job-1",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To[int32](1),
								},
							},
						},
					},
				},
			},
			oldJs: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					Suspend:        ptr.To(true),
					ReplicatedJobs: validReplicatedJobs,
				},
			},
			want: field.ErrorList{
				field.Invalid(field.NewPath("spec").Child("replicatedJobs"), []jobset.ReplicatedJob{
					{
						Name:     "test-jobset-replicated-job-0",
						Replicas: 2,
						Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{
								Parallelism: ptr.To[int32](2),
							},
						},
					},
					{
						Name:     "test-jobset-replicated-job-1",
						Replicas: 1,
						Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{
								Parallelism: ptr.To[int32](1),
							},
						},
					},
				}, "field is immutable"),
			}.ToAggregate(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewFakeClient()
			webhook, err := NewJobSetWebhook(fakeClient)
			assert.Nil(t, err)
			newObj := tc.js.DeepCopyObject()
			oldObj := tc.oldJs.DeepCopyObject()
			_, err = webhook.ValidateUpdate(context.TODO(), oldObj, newObj)
			if diff := cmp.Diff(tc.want, err); diff != "" {
				t.Errorf("ValidateResources() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
