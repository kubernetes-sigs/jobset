package webhooks

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/features"
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

type jobSetDefaultingTestCase struct {
	name string
	js   *jobset.JobSet
	want *jobset.JobSet
}

func TestJobSetDefaulting(t *testing.T) {
	defaultSuccessPolicy := &jobset.SuccessPolicy{Operator: jobset.OperatorAll}
	defaultStartupPolicy := &jobset.StartupPolicy{StartupPolicyOrder: jobset.AnyOrder}
	defaultNetwork := &jobset.Network{EnableDNSHostnames: ptr.To(true), PublishNotReadyAddresses: ptr.To(true)}

	jobCompletionTests := []jobSetDefaultingTestCase{
		{
			name: "job completion mode is unset",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					Network:       defaultNetwork,
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
					Network:       defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
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
					Network:       defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: ptr.To(batchv1.NonIndexedCompletion),
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
					Network:       defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: ptr.To(batchv1.NonIndexedCompletion),
								},
							},
						},
					},
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
		},
	}

	enablingDNSHostnameTests := []jobSetDefaultingTestCase{
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
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
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
					Network:       defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
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
									CompletionMode: ptr.To(batchv1.NonIndexedCompletion),
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
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(false), PublishNotReadyAddresses: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: ptr.To(batchv1.NonIndexedCompletion),
								},
							},
						},
					},
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
		},
	}

	publishNotReadyNetworkAddresessTests := []jobSetDefaultingTestCase{
		{
			name: "PublishNotReadyNetworkAddresess is false",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					Network:       &jobset.Network{PublishNotReadyAddresses: ptr.To(false)},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: ptr.To(batchv1.NonIndexedCompletion),
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
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(true), PublishNotReadyAddresses: ptr.To(false)},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: ptr.To(batchv1.NonIndexedCompletion),
								},
							},
						},
					},
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
		},
		{
			name: "PublishNotReadyNetworkAddresess is true",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(false), PublishNotReadyAddresses: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: ptr.To(batchv1.NonIndexedCompletion),
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
					Network:       &jobset.Network{EnableDNSHostnames: ptr.To(false), PublishNotReadyAddresses: ptr.To(true)},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: ptr.To(batchv1.NonIndexedCompletion),
								},
							},
						},
					},
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
		},
	}

	podRestartTests := []jobSetDefaultingTestCase{
		{
			name: "pod restart policy unset",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					Network:       defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{},
									},
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
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
					Network:       defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											RestartPolicy: corev1.RestartPolicyOnFailure,
										},
									},
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
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
					Network:       defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											RestartPolicy: corev1.RestartPolicyAlways,
										},
									},
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
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
					Network:       defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											RestartPolicy: corev1.RestartPolicyAlways,
										},
									},
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
								},
							},
						},
					},
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
		},
	}

	successPolicyTests := []jobSetDefaultingTestCase{
		{
			name: "success policy unset",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					StartupPolicy: defaultStartupPolicy,
					Network:       defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											RestartPolicy: corev1.RestartPolicyAlways,
										},
									},
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
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
					Network:       defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											RestartPolicy: corev1.RestartPolicyAlways,
										},
									},
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
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
					Network:       defaultNetwork,
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
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
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
					Network:       defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											RestartPolicy: corev1.RestartPolicyAlways,
										},
									},
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
								},
							},
						},
					},
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
		},
	}

	startupPolicyTests := []jobSetDefaultingTestCase{
		{
			name: "startup policy order InOrder set",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{
						Operator: jobset.OperatorAny,
					},
					Network: defaultNetwork,
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
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
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
					StartupPolicy: &jobset.StartupPolicy{
						StartupPolicyOrder: jobset.InOrder,
					},
					Network: defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											RestartPolicy: corev1.RestartPolicyAlways,
										},
									},
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
								},
							},
						},
					},
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
		},
	}

	managedByTests := []jobSetDefaultingTestCase{
		{
			name: "managedBy field is left nil",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					Network:       defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
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
					Network:       defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
								},
							},
						},
					},
				},
			},
		},
		{
			name: "when provided, managedBy field is preserved",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					Network:       defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
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
					Network:       defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
								},
							},
						},
					},
					ManagedBy: ptr.To("other-controller"),
				},
			},
		},
	}

	failurePolicyRuleNameTests := []jobSetDefaultingTestCase{
		{
			name: "failure policy rule name is defaulted when: there is one rule and it does not have a name",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					Network:       defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
								},
							},
						},
					},
					FailurePolicy: &jobset.FailurePolicy{
						Rules: make([]jobset.FailurePolicyRule, 1),
					},
				},
			},
			want: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					Network:       defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
								},
							},
						},
					},
					FailurePolicy: &jobset.FailurePolicy{
						Rules: []jobset.FailurePolicyRule{
							{Name: "failurePolicyRule0"},
						},
					},
				},
			},
		},
		{
			name: "failure policy rule name is defaulted when: there are two rules, the first rule has a name, the second rule does not have a name",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					Network:       defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
								},
							},
						},
					},
					FailurePolicy: &jobset.FailurePolicy{
						Rules: []jobset.FailurePolicyRule{
							{Name: "ruleWithAName"},
							{},
						},
					},
				},
			},
			want: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					Network:       defaultNetwork,
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
								},
							},
						},
					},
					FailurePolicy: &jobset.FailurePolicy{
						Rules: []jobset.FailurePolicyRule{
							{Name: "ruleWithAName"},
							{Name: "failurePolicyRule1"},
						},
					},
				},
			},
		},
	}

	volumeRetentionPolicyTests := []jobSetDefaultingTestCase{
		{
			name: "volumeClaimPolicy retentionPolicy is defaulted to Delete when nil",
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					SuccessPolicy: defaultSuccessPolicy,
					StartupPolicy: defaultStartupPolicy,
					Network:       defaultNetwork,
					VolumeClaimPolicies: []jobset.VolumeClaimPolicy{
						{
							Templates: []corev1.PersistentVolumeClaim{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "test-volume",
									},
								},
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
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
					Network:       defaultNetwork,
					VolumeClaimPolicies: []jobset.VolumeClaimPolicy{
						{
							Templates: []corev1.PersistentVolumeClaim{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "test-volume",
									},
								},
							},
							RetentionPolicy: &jobset.VolumeRetentionPolicy{
								WhenDeleted: ptr.To(jobset.RetentionPolicyDelete),
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template:       TestPodTemplate,
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
								},
							},
						},
					},
					ManagedBy: ptr.To(jobset.JobSetControllerName),
				},
			},
		},
	}

	testGroups := [][]jobSetDefaultingTestCase{
		jobCompletionTests,
		enablingDNSHostnameTests,
		publishNotReadyNetworkAddresessTests,
		podRestartTests,
		successPolicyTests,
		startupPolicyTests,
		startupPolicyTests,
		managedByTests,
		failurePolicyRuleNameTests,
		volumeRetentionPolicyTests,
	}
	var testCases []jobSetDefaultingTestCase
	for _, testGroup := range testGroups {
		testCases = append(testCases, testGroup...)
	}

	fakeClient := fake.NewFakeClient()
	webhook := &jobSetWebhook{client: fakeClient}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			obj := tc.js.DeepCopyObject()
			if err := webhook.Default(context.TODO(), obj.(*jobset.JobSet)); err != nil {
				t.Errorf("unexpected error defaulting jobset: %v", err)
			}
			if diff := cmp.Diff(tc.want, obj.(*jobset.JobSet)); diff != "" {
				t.Errorf("unexpected jobset defaulting: (-want/+got): %s", diff)
			}
		})
	}
}

type validationTestCase struct {
	name                 string
	enableInPlaceRestart bool
	enableRestartJob     bool
	js                   *jobset.JobSet
	want                 error
	existingObjs         []runtime.Object // objects to pre-populate in the fake client
}

// TestValidateCreate tests the ValidateCreate method of the jobset webhook.
// Each test case specifies a list of expected errors.
// For each test case, the function TestValidateCreate checks that each
// expected error is present in the list of errors returned by
// ValidateCreate. It is okay if ValidateCreate returns errors
// beyond the expected errors.
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

	uncategorizedTests := []validationTestCase{
		{
			name: "generateName that produces overlong child job names is rejected",
			js: &jobset.JobSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "JobSet",
					APIVersion: "jobset.x-k8s.io/v1alpha2",
				},
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: strings.Repeat("a", 49) + "-",
				},
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "worker",
							GroupName: "default",
							Replicas:  1,
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
				errors.New(jobNameTooLongErrorMsg),
			),
		},
		{
			name: "generateName that produces overlong PVC names is rejected",
			js: &jobset.JobSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "JobSet",
					APIVersion: "jobset.x-k8s.io/v1alpha2",
				},
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: strings.Repeat("a", 49) + "-",
				},
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "worker",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "test",
													Image: "bash:latest",
													VolumeMounts: []corev1.VolumeMount{
														{
															Name:      "datasets",
															MountPath: "/mnt/data",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{
						Operator: jobset.OperatorAll,
					},
					VolumeClaimPolicies: []jobset.VolumeClaimPolicy{
						{
							Templates: []corev1.PersistentVolumeClaim{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "datasets",
									},
								},
							},
						},
					},
				},
			},
			want: errors.Join(
				errors.New("VolumeClaimPolicies template name is too long"),
			),
		},
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
							Name:      "test-jobset-replicated-job-0",
							GroupName: "default",
							Replicas:  math.MaxInt32,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To[int32](math.MaxInt32),
									Template:    validPodTemplateSpec,
								},
							},
						},
						{
							Name:      "test-jobset-replicated-job-1",
							GroupName: "default",
							Replicas:  math.MinInt32,
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
							Name:      "test-jobset-replicated-job-0",
							GroupName: "default",
							Replicas:  1,
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
							Name:      "test-jobset-replicated-job-0",
							GroupName: "default",
							Replicas:  1,
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
							Name:      "test-jobset-replicated-job-0",
							GroupName: "default",
							Replicas:  1,
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
				errors.New(subdomainTooLongErrMsg),
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
							Name:      "username.llama65b",
							GroupName: "default",
							Replicas:  1,
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
			name: "group name is not DNS 1035 compliant",
			js: &jobset.JobSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "JobSet",
					APIVersion: "jobset.x-k8s.io/v1alpha2",
				},
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "rj",
							GroupName: strings.Repeat("a", 64),
							Replicas:  1,
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
				errors.New(groupNameTooLongErrorMsg),
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
							Name:      "rj",
							GroupName: "default",
							Replicas:  101,
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
				errors.New(jobNameTooLongErrorMsg),
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
							Name:      "rj",
							GroupName: "default",
							Replicas:  1,
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
				errors.New(podNameTooLongErrorMsg),
			),
		},
	}

	jobsetControllerNameTests := []validationTestCase{
		{
			name: "jobset controller name is not a domain-prefixed path",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ManagedBy: ptr.To(notDomainPrefixedPathControllerName),
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "rj",
							GroupName: "default",
							Replicas:  1,
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
							Name:      "rj",
							GroupName: "default",
							Replicas:  1,
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
							Name:      "rj",
							GroupName: "default",
							Replicas:  1,
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
							Name:      "rj",
							GroupName: "default",
							Replicas:  1,
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

	failurePolicyTests := []validationTestCase{
		{
			name: "failure policy rule name is valid",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "rj",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(1)),
									Parallelism:    ptr.To(int32(1)),
								},
							},
						},
					},
					FailurePolicy: &jobset.FailurePolicy{
						Rules: []jobset.FailurePolicyRule{
							{Name: "superAwesomeFailurePolicyRule"},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{},
				},
			},
			want: errors.Join(),
		},
		{
			name: "jobset failure policy has an invalid on job failure reason",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					FailurePolicy: &jobset.FailurePolicy{
						MaxRestarts: 1,
						Rules: []jobset.FailurePolicyRule{
							{
								Action:              jobset.FailJobSet,
								OnJobFailureReasons: []string{"fakeReason"},
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "rj",
							GroupName: "default",
							Replicas:  1,
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
				fmt.Errorf("invalid job failure reason '%s' in failure policy is not a recognized job failure reason", "fakeReason"),
			),
		},
		{
			name: "jobset failure policy has an invalid replicated job",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					FailurePolicy: &jobset.FailurePolicy{
						MaxRestarts: 1,
						Rules: []jobset.FailurePolicyRule{
							{
								Action:               jobset.FailJobSet,
								TargetReplicatedJobs: []string{"fakeReplicatedJob"},
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "rj",
							GroupName: "default",
							Replicas:  1,
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
				fmt.Errorf("invalid replicatedJob name '%s' in failure policy does not appear in .spec.ReplicatedJobs", "fakeReplicatedJob"),
			),
		},
		{
			name: "jobset failure policy rule name is 0 characters long a.k.a unset",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "rj",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(1)),
									Parallelism:    ptr.To(int32(1)),
								},
							},
						},
					},
					FailurePolicy: &jobset.FailurePolicy{
						Rules: make([]jobset.FailurePolicyRule, 1),
					},
					SuccessPolicy: &jobset.SuccessPolicy{},
				},
			},
			want: errors.Join(
				fmt.Errorf("invalid failure policy rule name of length %v, the rule name must be at least %v characters long and at most %v characters long", 0, minRuleNameLength, maxRuleNameLength),
			),
		},
		{
			name: "jobset failure policy rule name is greater than 128 characters long",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "rj",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(1)),
									Parallelism:    ptr.To(int32(1)),
								},
							},
						},
					},
					FailurePolicy: &jobset.FailurePolicy{
						Rules: []jobset.FailurePolicyRule{
							{Name: strings.Repeat("a", 129)},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{},
				},
			},
			want: errors.Join(
				fmt.Errorf("invalid failure policy rule name of length %v, the rule name must be at least %v characters long and at most %v characters long", 129, minRuleNameLength, maxRuleNameLength),
			),
		},
		{
			name: "there are two failure policy rules with the same name",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "rj",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(1)),
									Parallelism:    ptr.To(int32(1)),
								},
							},
						},
					},
					FailurePolicy: &jobset.FailurePolicy{
						Rules: []jobset.FailurePolicyRule{
							{Name: "repeatedRuleName"},
							{Name: "repeatedRuleName"},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{},
				},
			},
			want: errors.Join(
				fmt.Errorf("rule names are not unique, rules with indices %v all have the same name '%v'", []int{0, 1}, "repeatedRuleName"),
			),
		},
		{
			name: "failure policy rule name does not start with an alphabetic character",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "rj",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(1)),
									Parallelism:    ptr.To(int32(1)),
								},
							},
						},
					},
					FailurePolicy: &jobset.FailurePolicy{
						Rules: []jobset.FailurePolicyRule{
							{Name: "1ruleToRuleThemAll"},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{},
				},
			},
			want: errors.Join(
				fmt.Errorf("invalid failure policy rule name '%v', a failure policy rule name must start with an alphabetic character, optionally followed by a string of alphanumeric characters or '_,:', and must end with an alphanumeric character or '_'", "1ruleToRuleThemAll"),
			),
		},
		{
			name: "failure policy rule name does not end with an alphanumeric nor '_'",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "rj",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(1)),
									Parallelism:    ptr.To(int32(1)),
								},
							},
						},
					},
					FailurePolicy: &jobset.FailurePolicy{
						Rules: []jobset.FailurePolicyRule{
							{Name: "ruleToRuleThemAll,"},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{},
				},
			},
			want: errors.Join(
				fmt.Errorf("invalid failure policy rule name '%v', a failure policy rule name must start with an alphabetic character, optionally followed by a string of alphanumeric characters or '_,:', and must end with an alphanumeric character or '_'", "ruleToRuleThemAll,"),
			),
		},
		{
			name: "coordinator replicated job does not exist",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					Coordinator: &jobset.Coordinator{
						ReplicatedJob: "fake-rjob",
						JobIndex:      0,
						PodIndex:      0,
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "replicatedjob-a",
							GroupName: "default",
							Replicas:  2,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(2)),
									Parallelism:    ptr.To(int32(2)),
								},
							},
						},
						{
							Name:      "replicatedjob-b",
							GroupName: "default",
							Replicas:  2,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(2)),
									Parallelism:    ptr.To(int32(2)),
								},
							},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{},
				},
			},
			want: errors.Join(
				fmt.Errorf("coordinator replicatedJob fake-rjob does not exist"),
			),
		},
		{
			name: "coordinator replicated job missing completions",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					Coordinator: &jobset.Coordinator{
						ReplicatedJob: "replicatedjob-a",
						JobIndex:      0,
						PodIndex:      0,
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "replicatedjob-a",
							GroupName: "default",
							Replicas:  2,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Parallelism:    ptr.To(int32(2)),
								},
							},
						},
						{
							Name:      "replicatedjob-b",
							GroupName: "default",
							Replicas:  2,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(2)),
									Parallelism:    ptr.To(int32(2)),
								},
							},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{},
				},
			},
			want: errors.Join(
				fmt.Errorf("job for coordinator pod must be indexed completion mode, and completions number must be set"),
			),
		},
		{
			name: "coordinator job index invalid",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					Coordinator: &jobset.Coordinator{
						ReplicatedJob: "replicatedjob-a",
						JobIndex:      2,
						PodIndex:      0,
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "replicatedjob-a",
							GroupName: "default",
							Replicas:  2,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(2)),
									Parallelism:    ptr.To(int32(2)),
								},
							},
						},
						{
							Name:      "replicatedjob-b",
							GroupName: "default",
							Replicas:  2,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(2)),
									Parallelism:    ptr.To(int32(2)),
								},
							},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{},
				},
			},
			want: errors.Join(
				fmt.Errorf("coordinator job index 2 is invalid for replicatedJob replicatedjob-a"),
			),
		},
		{
			name: "coordinator pod index invalid",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					Coordinator: &jobset.Coordinator{
						ReplicatedJob: "replicatedjob-a",
						JobIndex:      0,
						PodIndex:      2,
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "replicatedjob-a",
							GroupName: "default",
							Replicas:  2,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(2)),
									Parallelism:    ptr.To(int32(2)),
								},
							},
						},
						{
							Name:      "replicatedjob-b",
							GroupName: "default",
							Replicas:  2,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(2)),
									Parallelism:    ptr.To(int32(2)),
								},
							},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{},
				},
			},
			want: errors.Join(
				fmt.Errorf("coordinator pod index 2 is invalid for replicatedJob replicatedjob-a job index 0"),
			),
		},
		{
			name: "coordinator label value is too long due to long JobSet name",
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: strings.Repeat("a", 60),
				},
				Spec: jobset.JobSetSpec{
					Coordinator: &jobset.Coordinator{
						ReplicatedJob: "replicatedjob-a",
						JobIndex:      0,
						PodIndex:      0,
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "replicatedjob-a",
							GroupName: "default",
							Replicas:  1,
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
			want: fmt.Errorf("spec will lead to invalid label value"),
		},
		{
			name: "coordinator label value is too long due to long ReplicatedJob name",
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "jobset",
				},
				Spec: jobset.JobSetSpec{
					Coordinator: &jobset.Coordinator{
						ReplicatedJob: strings.Repeat("a", 60),
						JobIndex:      0,
						PodIndex:      0,
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      strings.Repeat("a", 60),
							GroupName: "default",
							Replicas:  1,
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
			want: fmt.Errorf("spec will lead to invalid label value"),
		},
		{
			name: "coordinator label value is too long due to long SubDomain",
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "jobset",
				},
				Spec: jobset.JobSetSpec{
					Network: &jobset.Network{
						Subdomain: strings.Repeat("a", 60),
					},
					Coordinator: &jobset.Coordinator{
						ReplicatedJob: "replicatedjob-a",
						JobIndex:      0,
						PodIndex:      0,
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "replicatedjob-a",
							GroupName: "default",
							Replicas:  1,
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
			want: fmt.Errorf("spec will lead to invalid label value"),
		},
		{
			name:             "jobset failure policy rule has RestartJob action and a replicated job with replicas > 1024",
			enableRestartJob: true,
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "rj",
							GroupName: "default",
							Replicas:  1025,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(1)),
									Parallelism:    ptr.To(int32(1)),
								},
							},
						},
					},
					FailurePolicy: &jobset.FailurePolicy{
						Rules: []jobset.FailurePolicyRule{
							{
								Action: jobset.RestartJob,
								Name:   "restartJobRule",
							},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{},
				},
			},
			want: errors.Join(
				fmt.Errorf("JobSet cannot have a failure policy rule with RestartJob or RestartJobAndIgnoreMaxRestarts action and a replicated job with replicas > 1024"),
			),
		},
		{
			name:             "jobset failure policy rule has RestartJobAndIgnoreMaxRestarts action and a replicated job with replicas > 1024",
			enableRestartJob: true,
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "rj",
							GroupName: "default",
							Replicas:  1025,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(1)),
									Parallelism:    ptr.To(int32(1)),
								},
							},
						},
					},
					FailurePolicy: &jobset.FailurePolicy{
						Rules: []jobset.FailurePolicyRule{
							{
								Action: jobset.RestartJobAndIgnoreMaxRestarts,
								Name:   "restartJobAndIgnoreMaxRestartsRule",
							},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{},
				},
			},
			want: errors.Join(
				fmt.Errorf("JobSet cannot have a failure policy rule with RestartJob or RestartJobAndIgnoreMaxRestarts action and a replicated job with replicas > 1024"),
			),
		},
		{
			name:             "jobset failure policy rule has RestartJob action and RestartJob feature gate is disabled",
			enableRestartJob: false,
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "rj",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(1)),
									Parallelism:    ptr.To(int32(1)),
								},
							},
						},
					},
					FailurePolicy: &jobset.FailurePolicy{
						Rules: []jobset.FailurePolicyRule{
							{
								Action: jobset.RestartJob,
								Name:   "restartJobRule",
							},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{},
				},
			},
			want: errors.Join(
				fmt.Errorf("RestartJob and RestartJobAndIgnoreMaxRestarts failure policy actions are not allowed when RestartJob feature gate is disabled"),
			),
		},
		{
			name:             "jobset failure policy rule has RestartJobAndIgnoreMaxRestarts action and RestartJob feature gate is disabled",
			enableRestartJob: false,
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "rj",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To(int32(1)),
									Parallelism:    ptr.To(int32(1)),
								},
							},
						},
					},
					FailurePolicy: &jobset.FailurePolicy{
						Rules: []jobset.FailurePolicyRule{
							{
								Action: jobset.RestartJobAndIgnoreMaxRestarts,
								Name:   "restartJobAndIgnoreMaxRestartsRule",
							},
						},
					},
					SuccessPolicy: &jobset.SuccessPolicy{},
				},
			},
			want: errors.Join(
				fmt.Errorf("RestartJob and RestartJobAndIgnoreMaxRestarts failure policy actions are not allowed when RestartJob feature gate is disabled"),
			),
		},
	}

	dependsOnTests := []validationTestCase{
		{
			name: "DependsOn is valid since job-2 depends on job-1 and job-3 depends on job-1",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
						{
							Name: "job-2",
							DependsOn: []jobset.DependsOn{
								{
									Name:   "job-1",
									Status: "Complete",
								},
							},
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
						{
							Name: "job-3",
							DependsOn: []jobset.DependsOn{
								{
									Name:   "job-1",
									Status: "Complete",
								},
							},
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
					},
				},
			},
			want: errors.Join(),
		},
		{
			name: "DependsOn is valid since job-2 depends on job-1 and job-3 depends on job-2",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
						{
							Name: "job-2",
							DependsOn: []jobset.DependsOn{
								{
									Name:   "job-1",
									Status: "Complete",
								},
							},
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
						{
							Name: "job-3",
							DependsOn: []jobset.DependsOn{
								{
									Name:   "job-2",
									Status: "Complete",
								},
							},
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
					},
				},
			},
			want: errors.Join(),
		},
		{
			name: "DependsOn is valid since job-3 depends on job-1 and job-2",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
						{
							Name:      "job-2",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
						{
							Name: "job-3",
							DependsOn: []jobset.DependsOn{
								{
									Name:   "job-1",
									Status: "Complete",
								},
								{
									Name:   "job-2",
									Status: "Complete",
								},
							},
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
					},
				},
			},
			want: errors.Join(),
		},
		{
			name: "DependsOn is invalid since job-2 depends on job-3",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
						{
							Name: "job-2",
							DependsOn: []jobset.DependsOn{
								{
									Name:   "job-3",
									Status: "Complete",
								},
							},
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
						{
							Name: "job-3",
							DependsOn: []jobset.DependsOn{
								{
									Name:   "job-1",
									Status: "Complete",
								},
							},
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
					},
				},
			},
			want: errors.Join(fmt.Errorf("replicatedJob: job-2 cannot depend on replicatedJob: job-3")),
		},
		{
			name: "DependsOn is invalid since job-2 depends on job-3 and job-1",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
						{
							Name: "job-2",
							DependsOn: []jobset.DependsOn{
								{
									Name:   "job-1",
									Status: "Complete",
								},
								{
									Name:   "job-3",
									Status: "Complete",
								},
							},
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
						{
							Name: "job-3",
							DependsOn: []jobset.DependsOn{
								{
									Name:   "job-1",
									Status: "Complete",
								},
							},
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
					},
				},
			},
			want: errors.Join(fmt.Errorf("replicatedJob: job-2 cannot depend on replicatedJob: job-3")),
		},
		{
			name: "job-2 depends on invalid ReplicatedJob",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
						{
							Name: "job-2",
							DependsOn: []jobset.DependsOn{
								{
									Name:   "invalid",
									Status: "Complete",
								},
							},
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
					},
				},
			},
			want: errors.Join(fmt.Errorf("replicatedJob: job-2 cannot depend on replicatedJob: invalid")),
		},
		{
			name: "dependsOn should not fail if there are no replicated jobs",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy:  &jobset.SuccessPolicy{},
					ReplicatedJobs: []jobset.ReplicatedJob{},
				},
			},
		},
		{
			name: "dependsOn cannot be set for first replicated job",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name: "job-1",
							DependsOn: []jobset.DependsOn{
								{
									Name:   "invalid",
									Status: "Complete",
								},
							},
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
					},
				},
			},
			want: errors.Join(fmt.Errorf("DependsOn can't be set for the first ReplicatedJob")),
		},
	}

	testPVCSpec := corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{
			corev1.ReadWriteMany,
		},
		Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Gi"),
			},
		},
	}

	// boundPVCSpec is testPVCSpec as the API server stores it once the claim has been
	// created and bound: volumeMode and storageClassName filled in by defaulting, and
	// volumeName assigned by the binder. This is the shape a PVC retained by
	// retentionPolicy.whenDeleted: Retain has when the JobSet is re-applied.
	boundPVCSpec := *testPVCSpec.DeepCopy()
	boundPVCSpec.VolumeMode = ptr.To(corev1.PersistentVolumeFilesystem)
	boundPVCSpec.StorageClassName = ptr.To("standard")
	boundPVCSpec.VolumeName = "pv-abc123"

	volumeClaimPolicyTests := []validationTestCase{
		{
			name: "volumeClaimPolicy is valid since volume exists in the container",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					VolumeClaimPolicies: []jobset.VolumeClaimPolicy{
						{
							Templates: []corev1.PersistentVolumeClaim{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "test-volume",
									},
									Spec: testPVCSpec,
								},
							},
							RetentionPolicy: &jobset.VolumeRetentionPolicy{
								WhenDeleted: ptr.To(jobset.RetentionPolicyDelete),
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "test",
													Image: "bash:latest",
													VolumeMounts: []corev1.VolumeMount{
														{
															Name:      "test-volume",
															MountPath: "/test/path",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: errors.Join(),
		},
		{
			name: "volumeClaimPolicy is valid since volume exists in the initContainer",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					VolumeClaimPolicies: []jobset.VolumeClaimPolicy{
						{
							Templates: []corev1.PersistentVolumeClaim{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "test-volume",
									},
									Spec: testPVCSpec,
								},
							},
							RetentionPolicy: &jobset.VolumeRetentionPolicy{
								WhenDeleted: ptr.To(jobset.RetentionPolicyDelete),
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "test",
													Image: "bash:latest",
												},
											},
										},
									},
								},
							},
						},
						{
							Name:      "job-2",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											InitContainers: []corev1.Container{
												{
													Name:  "test-init",
													Image: "bash:latest",
													VolumeMounts: []corev1.VolumeMount{
														{
															Name:      "test-volume",
															MountPath: "/test/path",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: errors.Join(),
		},
		{
			name: "volumeClaimPolicy is invalid since template names are not unique",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					VolumeClaimPolicies: []jobset.VolumeClaimPolicy{
						{
							Templates: []corev1.PersistentVolumeClaim{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "duplicate-volume",
									},
									Spec: testPVCSpec,
								},
							},
							RetentionPolicy: &jobset.VolumeRetentionPolicy{
								WhenDeleted: ptr.To(jobset.RetentionPolicyDelete),
							},
						},
						{
							Templates: []corev1.PersistentVolumeClaim{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "duplicate-volume",
									},
									Spec: testPVCSpec,
								},
							},
							RetentionPolicy: &jobset.VolumeRetentionPolicy{
								WhenDeleted: ptr.To(jobset.RetentionPolicyRetain),
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "test",
													Image: "bash:latest",
													VolumeMounts: []corev1.VolumeMount{
														{
															Name:      "duplicate-volume",
															MountPath: "/test/path",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: errors.Join(fmt.Errorf("names must be unique for VolumeClaimPolicies template")),
		},
		{
			name: "volumeClaimPolicy is invalid since template name has uppercase letters",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					VolumeClaimPolicies: []jobset.VolumeClaimPolicy{
						{
							Templates: []corev1.PersistentVolumeClaim{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "InvalidVolumeName",
									},
									Spec: testPVCSpec,
								},
							},
							RetentionPolicy: &jobset.VolumeRetentionPolicy{
								WhenDeleted: ptr.To(jobset.RetentionPolicyDelete),
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "test",
													Image: "bash:latest",
													VolumeMounts: []corev1.VolumeMount{
														{
															Name:      "InvalidVolumeName",
															MountPath: "/test/path",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: errors.Join(fmt.Errorf("a lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters")),
		},
		{
			name: "volumeClaimPolicy is invalid since template name is too long",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					VolumeClaimPolicies: []jobset.VolumeClaimPolicy{
						{
							Templates: []corev1.PersistentVolumeClaim{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: strings.Repeat("a", 64),
									},
									Spec: testPVCSpec,
								},
							},
							RetentionPolicy: &jobset.VolumeRetentionPolicy{
								WhenDeleted: ptr.To(jobset.RetentionPolicyDelete),
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "test",
													Image: "bash:latest",
													VolumeMounts: []corev1.VolumeMount{
														{
															Name:      strings.Repeat("a", 64),
															MountPath: "/test/path",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: errors.Join(fmt.Errorf("VolumeClaimPolicies template name is too long")),
		},
		{
			name: "volumeClaimPolicy is invalid since no matching volumeMount in containers",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					VolumeClaimPolicies: []jobset.VolumeClaimPolicy{
						{
							Templates: []corev1.PersistentVolumeClaim{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "test-volume",
									},
									Spec: testPVCSpec,
								},
							},
							RetentionPolicy: &jobset.VolumeRetentionPolicy{
								WhenDeleted: ptr.To(jobset.RetentionPolicyDelete),
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "test",
													Image: "bash:latest",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: errors.Join(fmt.Errorf("replicatedJob containers don't have a matching volumeMount")),
		},
		{
			name: "volumeClaimPolicy is invalid since volume name conflicts with existing volume",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					VolumeClaimPolicies: []jobset.VolumeClaimPolicy{
						{
							Templates: []corev1.PersistentVolumeClaim{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "conflicting-volume",
									},
									Spec: testPVCSpec,
								},
							},
							RetentionPolicy: &jobset.VolumeRetentionPolicy{
								WhenDeleted: ptr.To(jobset.RetentionPolicyDelete),
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "test",
													Image: "bash:latest",
													VolumeMounts: []corev1.VolumeMount{
														{
															Name:      "conflicting-volume",
															MountPath: "/test/path",
														},
													},
												},
											},
											Volumes: []corev1.Volume{
												{
													Name: "conflicting-volume",
													VolumeSource: corev1.VolumeSource{
														EmptyDir: &corev1.EmptyDirVolumeSource{},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: errors.Join(fmt.Errorf("volume name conflicts with VolumeClaimPolicy template name")),
		},
		{
			name: "volumeClaimPolicy is valid when existing PVC matches template spec",
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "js",
					Namespace: "default",
				},
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					VolumeClaimPolicies: []jobset.VolumeClaimPolicy{
						{
							Templates: []corev1.PersistentVolumeClaim{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "test-volume",
									},
									Spec: testPVCSpec,
								},
							},
							RetentionPolicy: &jobset.VolumeRetentionPolicy{
								WhenDeleted: ptr.To(jobset.RetentionPolicyRetain),
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "test",
													Image: "bash:latest",
													VolumeMounts: []corev1.VolumeMount{
														{
															Name:      "test-volume",
															MountPath: "/test/path",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			existingObjs: []runtime.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-volume-js",
						Namespace: "default",
					},
					Spec: testPVCSpec,
				},
			},
			want: errors.Join(),
		},
		{
			name: "volumeClaimPolicy retention policy must be retain for existing PVC",
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "js",
					Namespace: "default",
				},
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					VolumeClaimPolicies: []jobset.VolumeClaimPolicy{
						{
							Templates: []corev1.PersistentVolumeClaim{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "test-volume",
									},
									Spec: testPVCSpec,
								},
							},
							RetentionPolicy: &jobset.VolumeRetentionPolicy{
								WhenDeleted: ptr.To(jobset.RetentionPolicyDelete),
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "test",
													Image: "bash:latest",
													VolumeMounts: []corev1.VolumeMount{
														{
															Name:      "test-volume",
															MountPath: "/test/path",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			existingObjs: []runtime.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-volume-js",
						Namespace: "default",
					},
					Spec: testPVCSpec,
				},
			},
			want: errors.Join(fmt.Errorf("retentionPolicy must be retain when PVC exist")),
		},
		{
			name: "volumeClaimPolicy with unset whenDeleted defaults to delete and is rejected for existing PVC",
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "js",
					Namespace: "default",
				},
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					VolumeClaimPolicies: []jobset.VolumeClaimPolicy{
						{
							Templates: []corev1.PersistentVolumeClaim{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "test-volume",
									},
									Spec: testPVCSpec,
								},
							},
							RetentionPolicy: &jobset.VolumeRetentionPolicy{},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "test",
													Image: "bash:latest",
													VolumeMounts: []corev1.VolumeMount{
														{
															Name:      "test-volume",
															MountPath: "/test/path",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			existingObjs: []runtime.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-volume-js",
						Namespace: "default",
					},
					Spec: testPVCSpec,
				},
			},
			want: errors.Join(fmt.Errorf("retentionPolicy must be retain when PVC exist")),
		},
		{
			name: "volumeClaimPolicy is invalid when existing PVC does not match template spec",
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "js",
					Namespace: "default",
				},
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					VolumeClaimPolicies: []jobset.VolumeClaimPolicy{
						{
							Templates: []corev1.PersistentVolumeClaim{
								{
									ObjectMeta: metav1.ObjectMeta{
										Name: "test-volume",
									},
									Spec: testPVCSpec,
								},
							},
							RetentionPolicy: &jobset.VolumeRetentionPolicy{
								WhenDeleted: ptr.To(jobset.RetentionPolicyRetain),
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											Containers: []corev1.Container{
												{
													Name:  "test",
													Image: "bash:latest",
													VolumeMounts: []corev1.VolumeMount{
														{
															Name:      "test-volume",
															MountPath: "/test/path",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			existingObjs: []runtime.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-volume-js",
						Namespace: "default",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteOnce,
						},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("2Gi"),
							},
						},
					},
				},
			},
			want: errors.Join(fmt.Errorf("spec does not match existing PVC")),
		},
		{
			// Regression test for #1307: a PVC retained across JobSet deletions carries
			// values the API server populated for it, which the template never has.
			// Re-applying the unchanged manifest must still be admitted.
			name: "volumeClaimPolicy is valid when existing PVC was defaulted and bound by the API server",
			js:   jobSetWithVolumeClaimTemplate(testPVCSpec),
			existingObjs: []runtime.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-volume-js",
						Namespace: "default",
					},
					Spec: boundPVCSpec,
				},
			},
			want: errors.Join(),
		},
		{
			name: "volumeClaimPolicy is invalid when template pins a different storageClassName than the existing PVC",
			js: jobSetWithVolumeClaimTemplate(func() corev1.PersistentVolumeClaimSpec {
				spec := *testPVCSpec.DeepCopy()
				spec.StorageClassName = ptr.To("premium")
				return spec
			}()),
			existingObjs: []runtime.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-volume-js",
						Namespace: "default",
					},
					Spec: boundPVCSpec,
				},
			},
			want: errors.Join(fmt.Errorf("spec does not match existing PVC test-volume-js in namespace default, differing field(s): storageClassName")),
		},
		{
			name: "volumeClaimPolicy is invalid when template pins a different volumeName than the existing PVC",
			js: jobSetWithVolumeClaimTemplate(func() corev1.PersistentVolumeClaimSpec {
				spec := *testPVCSpec.DeepCopy()
				spec.VolumeName = "some-other-pv"
				return spec
			}()),
			existingObjs: []runtime.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-volume-js",
						Namespace: "default",
					},
					Spec: boundPVCSpec,
				},
			},
			want: errors.Join(fmt.Errorf("spec does not match existing PVC test-volume-js in namespace default, differing field(s): volumeName")),
		},
		{
			name: "volumeClaimPolicy is invalid when template pins a different volumeMode than the existing PVC",
			js: jobSetWithVolumeClaimTemplate(func() corev1.PersistentVolumeClaimSpec {
				spec := *testPVCSpec.DeepCopy()
				spec.VolumeMode = ptr.To(corev1.PersistentVolumeBlock)
				return spec
			}()),
			existingObjs: []runtime.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-volume-js",
						Namespace: "default",
					},
					Spec: boundPVCSpec,
				},
			},
			want: errors.Join(fmt.Errorf("spec does not match existing PVC test-volume-js in namespace default, differing field(s): volumeMode")),
		},
	}

	inPlaceRestartTests := []validationTestCase{
		{
			name:                 "InPlaceRestart restart strategy cannot be set when InPlaceRestart feature gate is disabled",
			enableInPlaceRestart: false,
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					FailurePolicy: &jobset.FailurePolicy{
						RestartStrategy: jobset.InPlaceRestart,
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Template: validPodTemplateSpec,
								},
							},
						},
					},
				},
			},
			want: errors.Join(fmt.Errorf("InPlaceRestart restart strategy cannot be set when InPlaceRestart feature gate is disabled")),
		},
		{
			name:                 "InPlaceRestart restart strategy can be set when InPlaceRestart feature gate is enabled",
			enableInPlaceRestart: true,
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					FailurePolicy: &jobset.FailurePolicy{
						RestartStrategy: jobset.InPlaceRestart,
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism:          ptr.To[int32](1),
									Completions:          ptr.To[int32](1),
									BackoffLimit:         ptr.To[int32](math.MaxInt32),
									PodReplacementPolicy: ptr.To(batchv1.Failed),
									Template:             validPodTemplateSpec,
								},
							},
						},
					},
				},
			},
			want: nil,
		},
		{
			name:                 "InPlaceRestart enabled: backoffLimit is not MaxInt32",
			enableInPlaceRestart: true,
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					FailurePolicy: &jobset.FailurePolicy{
						RestartStrategy: jobset.InPlaceRestart,
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism:          ptr.To[int32](1),
									Completions:          ptr.To[int32](1),
									BackoffLimit:         ptr.To[int32](0),
									PodReplacementPolicy: ptr.To(batchv1.Failed),
									Template:             validPodTemplateSpec,
								},
							},
						},
					},
				},
			},
			want: errors.Join(fmt.Errorf("replicatedJob job-1: must be set to 2147483647 (MaxInt32) when in-place restart is enabled")),
		},
		{
			name:                 "InPlaceRestart enabled: podReplacementPolicy is not Failed",
			enableInPlaceRestart: true,
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					FailurePolicy: &jobset.FailurePolicy{
						RestartStrategy: jobset.InPlaceRestart,
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism:          ptr.To[int32](1),
									Completions:          ptr.To[int32](1),
									BackoffLimit:         ptr.To[int32](math.MaxInt32),
									PodReplacementPolicy: ptr.To(batchv1.TerminatingOrFailed),
									Template:             validPodTemplateSpec,
								},
							},
						},
					},
				},
			},
			want: errors.Join(fmt.Errorf("replicatedJob job-1: must be set to Failed when in-place restart is enabled")),
		},
		{
			name:                 "InPlaceRestart enabled: completions != parallelism",
			enableInPlaceRestart: true,
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					FailurePolicy: &jobset.FailurePolicy{
						RestartStrategy: jobset.InPlaceRestart,
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism:          ptr.To[int32](1),
									Completions:          ptr.To[int32](2),
									BackoffLimit:         ptr.To[int32](math.MaxInt32),
									PodReplacementPolicy: ptr.To(batchv1.Failed),
									Template:             validPodTemplateSpec,
								},
							},
						},
					},
				},
			},
			want: errors.Join(fmt.Errorf("replicatedJob job-1: completions and parallelism must be set and equal to each other when in-place restart is enabled")),
		},
		{
			name:                 "InPlaceRestart enabled: completions is nil",
			enableInPlaceRestart: true,
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					FailurePolicy: &jobset.FailurePolicy{
						RestartStrategy: jobset.InPlaceRestart,
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism:          ptr.To[int32](1),
									BackoffLimit:         ptr.To[int32](math.MaxInt32),
									PodReplacementPolicy: ptr.To(batchv1.Failed),
									Template:             validPodTemplateSpec,
								},
							},
						},
					},
				},
			},
			want: errors.Join(fmt.Errorf("replicatedJob job-1: completions and parallelism must be set and equal to each other when in-place restart is enabled")),
		},
		{
			name:                 "InPlaceRestart enabled: parallelism is nil",
			enableInPlaceRestart: true,
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					SuccessPolicy: &jobset.SuccessPolicy{},
					FailurePolicy: &jobset.FailurePolicy{
						RestartStrategy: jobset.InPlaceRestart,
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:      "job-1",
							GroupName: "default",
							Replicas:  1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Completions:          ptr.To[int32](1),
									BackoffLimit:         ptr.To[int32](math.MaxInt32),
									PodReplacementPolicy: ptr.To(batchv1.Failed),
									Template:             validPodTemplateSpec,
								},
							},
						},
					},
				},
			},
			want: errors.Join(fmt.Errorf("replicatedJob job-1: completions and parallelism must be set and equal to each other when in-place restart is enabled")),
		},
	}

	testGroups := [][]validationTestCase{
		uncategorizedTests,
		jobsetControllerNameTests,
		failurePolicyTests,
		dependsOnTests,
		volumeClaimPolicyTests,
		inPlaceRestartTests,
	}
	var testCases []validationTestCase
	for _, testGroup := range testGroups {
		testCases = append(testCases, testGroup...)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a new client with existing objects if provided
			testClient := fake.NewFakeClient(tc.existingObjs...)
			testWebhook := &jobSetWebhook{client: testClient}
			features.SetFeatureGateDuringTest(t, features.InPlaceRestart, tc.enableInPlaceRestart)
			features.SetFeatureGateDuringTest(t, features.RestartJob, tc.enableRestartJob)
			_, err := testWebhook.ValidateCreate(context.TODO(), tc.js.DeepCopy())
			if err != nil && tc.want != nil {
				// Verify it's specifically a 422 StatusError
				var statusErr *apierrors.StatusError
				require.ErrorAs(t, err, &statusErr, "expected *apierrors.StatusError")
				assert.Equal(t, int32(422), statusErr.ErrStatus.Code)
				assert.Equal(t, metav1.StatusReasonInvalid, statusErr.ErrStatus.Reason)

				// Verify all expected error substrings are present
				for _, wantErr := range strings.Split(tc.want.Error(), "\n") {
					assert.Contains(t, err.Error(), wantErr)
				}
			} else if err != nil && tc.want == nil {
				t.Errorf("unexpected error: %v", err)
			} else if err == nil && tc.want != nil {
				t.Errorf("missing expected error: %v", tc.want)
			}
		})
	}
}

func TestGetJobSetNameForValidation(t *testing.T) {
	t.Parallel()

	t.Run("name is used directly", func(t *testing.T) {
		got := getJobSetNameForValidation(&jobset.JobSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "jobset-name",
			},
		})

		assert.Equal(t, "jobset-name", got)
	})

	t.Run("generateName uses apiserver truncation rules", func(t *testing.T) {
		got := getJobSetNameForValidation(&jobset.JobSet{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: strings.Repeat("a", 60),
			},
		})

		assert.Len(t, got, 63)
		assert.True(t, strings.HasPrefix(got, strings.Repeat("a", 58)))
	})

	t.Run("empty metadata returns empty name", func(t *testing.T) {
		got := getJobSetNameForValidation(&jobset.JobSet{})
		assert.Empty(t, got)
	})
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
		name                string
		oldJs               *jobset.JobSet
		js                  *jobset.JobSet
		want                error
		enableElasticJobSet bool
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
			name: "replicated job pod template can be updated for suspended jobset",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: 2,
							Template: batchv1.JobTemplateSpec{
								// Adding an annotation.
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To[int32](2),
									Template: corev1.PodTemplateSpec{
										ObjectMeta: metav1.ObjectMeta{
											Annotations: map[string]string{"key": "value"},
										},
									},
								},
							},
						},
					},
				},
			},
			oldJs: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					Suspend: ptr.To(true),
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
					},
				},
			},
		},
		{
			name: "replicated job pod template can be updated for jobset getting suspended",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					Suspend: ptr.To(true),
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: 2,
							Template: batchv1.JobTemplateSpec{
								// Adding an annotation.
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To[int32](2),
									Template: corev1.PodTemplateSpec{
										ObjectMeta: metav1.ObjectMeta{
											Annotations: map[string]string{"key": "value"},
										},
									},
								},
							},
						},
					},
				},
			},
			oldJs: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					Suspend: ptr.To(false),
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
					},
				},
			},
		},
		{
			name: "replicated job pod template cannot be updated for running jobset",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: 2,
							Template: batchv1.JobTemplateSpec{
								// Adding an annotation.
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To[int32](2),
									Template: corev1.PodTemplateSpec{
										ObjectMeta: metav1.ObjectMeta{
											Annotations: map[string]string{"key": "value"},
										},
									},
								},
							},
						},
					},
				},
			},
			oldJs: &jobset.JobSet{
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
					},
				},
			},
			want: field.ErrorList{
				field.Invalid(field.NewPath("spec").Child("replicatedJobs"), "", "field is immutable"),
			}.ToAggregate(),
		},
		{
			name: "schedulingGates for pod template can be updated for suspended JobSet",
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
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											// Adding a scheduling gate
											SchedulingGates: []corev1.PodSchedulingGate{
												{
													Name: "example.com/gate",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			oldJs: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					Suspend: ptr.To(true),
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
					},
				},
			},
		},
		{
			name: "schedulingGates for pod template cannot be updated for unsuspended JobSet",
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
									Template: corev1.PodTemplateSpec{
										Spec: corev1.PodSpec{
											// Adding a scheduling gate
											SchedulingGates: []corev1.PodSchedulingGate{
												{
													Name: "example.com/gate",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			oldJs: &jobset.JobSet{
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
					},
				},
			},
			want: field.ErrorList{
				field.Invalid(field.NewPath("spec").Child("replicatedJobs"), "", "field is immutable"),
			}.ToAggregate(),
		},
		{
			name: "replicated job name cannot be updated",
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "new-replicated-job-name",
							Replicas: 2,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To[int32](2),
								},
							},
						},
					},
				},
			},
			oldJs: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					Suspend: ptr.To(true),
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
					},
				},
			},
			want: field.ErrorList{
				field.Invalid(field.NewPath("spec").Child("replicatedJobs"), "", "field is immutable"),
			}.ToAggregate(),
		},
		{
			name: "valid scaling of parallelism and completions",
			oldJs: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: 2,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Parallelism:    ptr.To[int32](2),
									Completions:    ptr.To[int32](2),
								},
							},
						},
					},
				},
			},
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: 2,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Parallelism:    ptr.To[int32](8),
									Completions:    ptr.To[int32](8),
								},
							},
						},
					},
				},
			},
			enableElasticJobSet: true,
		},
		{
			name: "invalid scaling of parallelism to < 1",
			oldJs: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name: "test-jobset-replicated-job-0",
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Parallelism:    ptr.To[int32](2),
								},
							},
						},
					},
				},
			},
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name: "test-jobset-replicated-job-0",
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Parallelism:    ptr.To[int32](0),
								},
							},
						},
					},
				},
			},
			want: field.ErrorList{
				field.Invalid(field.NewPath("spec", "replicatedJobs").Index(0).Child("template", "spec", "parallelism"), int32(0), "parallelism must be >= 1"),
			}.ToAggregate(),
			enableElasticJobSet: true,
		},
		{
			name: "invalid scaling of completions to < 1",
			oldJs: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name: "test-jobset-replicated-job-0",
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To[int32](2),
								},
							},
						},
					},
				},
			},
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name: "test-jobset-replicated-job-0",
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To[int32](0),
								},
							},
						},
					},
				},
			},
			want: field.ErrorList{
				field.Invalid(field.NewPath("spec", "replicatedJobs").Index(0).Child("template", "spec", "completions"), int32(0), "completions must be >= 1"),
			}.ToAggregate(),
			enableElasticJobSet: true,
		},
		{
			name: "cannot scale parallelism if JobSet is Completed",
			oldJs: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name: "test-jobset-replicated-job-0",
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Parallelism:    ptr.To[int32](2),
								},
							},
						},
					},
				},
				Status: jobset.JobSetStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(jobset.JobSetCompleted),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name: "test-jobset-replicated-job-0",
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Parallelism:    ptr.To[int32](4),
								},
							},
						},
					},
				},
			},
			want: field.ErrorList{
				field.Forbidden(field.NewPath("spec", "replicatedJobs").Index(0).Child("template", "spec"), "Cannot mutate parallelism or completions when JobSet is in a terminal state (Completed or Failed)"),
			}.ToAggregate(),
			enableElasticJobSet: true,
		},
		{
			name: "cannot scale completions if JobSet is Failed",
			oldJs: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name: "test-jobset-replicated-job-0",
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To[int32](2),
								},
							},
						},
					},
				},
				Status: jobset.JobSetStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(jobset.JobSetFailed),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name: "test-jobset-replicated-job-0",
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Completions:    ptr.To[int32](4),
								},
							},
						},
					},
				},
			},
			want: field.ErrorList{
				field.Forbidden(field.NewPath("spec", "replicatedJobs").Index(0).Child("template", "spec"), "Cannot mutate parallelism or completions when JobSet is in a terminal state (Completed or Failed)"),
			}.ToAggregate(),
			enableElasticJobSet: true,
		},
		{
			name: "immutability still enforced for other fields during valid scale",
			oldJs: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Parallelism:    ptr.To[int32](2),
								},
							},
						},
					},
				},
			},
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: 2, // immutable field modified
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Parallelism:    ptr.To[int32](4), // Valid scale operation
								},
							},
						},
					},
				},
			},
			want: field.ErrorList{
				field.Invalid(field.NewPath("spec").Child("replicatedJobs"), "", "field is immutable"),
			}.ToAggregate(),
			enableElasticJobSet: true,
		},
		{
			name: "invalid scaling of parallelism and completions when feature gate is disabled",
			oldJs: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: 2,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To[int32](2),
									Completions: ptr.To[int32](2),
								},
							},
						},
					},
				},
			},
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: 2,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To[int32](8),
									Completions: ptr.To[int32](8),
								},
							},
						},
					},
				},
			},
			want: field.ErrorList{
				field.Invalid(field.NewPath("spec").Child("replicatedJobs"), "", "field is immutable"),
			}.ToAggregate(),
			enableElasticJobSet: false, // Feature gate disabled
		},
		{
			name: "invalid scaling: parallelism and completions must be equal",
			oldJs: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: 2,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Parallelism:    ptr.To[int32](2),
									Completions:    ptr.To[int32](2),
								},
							},
						},
					},
				},
			},
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: 2,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.IndexedCompletion),
									Parallelism:    ptr.To[int32](4),
									Completions:    ptr.To[int32](5), // mismatch
								},
							},
						},
					},
				},
			},
			want: field.ErrorList{
				field.Invalid(field.NewPath("spec", "replicatedJobs").Index(0).Child("template", "spec", "completions"), int32(5), "completions must be equal to parallelism for Elastic Indexed Jobs"),
			}.ToAggregate(),
			enableElasticJobSet: true,
		},
		{
			name: "adding a new ReplicatedJob while suspended does not panic and is rejected",
			oldJs: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					Suspend: ptr.To(true),
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "job-a",
							Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](2)}},
						},
					},
				},
			},
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					Suspend: ptr.To(true),
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "job-a",
							Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](2)}},
						},
						{
							Name:     "job-b", // New job added
							Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](2)}},
						},
					},
				},
			},
			want: field.ErrorList{
				field.Invalid(field.NewPath("spec").Child("replicatedJobs"), "", "field is immutable"),
			}.ToAggregate(),
		},
		{
			name: "reordering ReplicatedJobs evaluates ElasticJobSet scaling correctly by name",
			oldJs: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "job-a",
							Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](2), Completions: ptr.To[int32](2)}},
						},
						{
							Name:     "job-b",
							Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](2), Completions: ptr.To[int32](2)}},
						},
					},
				},
			},
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "job-b", // Swapped order
							Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](2), Completions: ptr.To[int32](2)}},
						},
						{
							Name:     "job-a",
							Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](4), Completions: ptr.To[int32](4)}}, // Valid scaling operation
						},
					},
				},
			},
			want: field.ErrorList{
				// We do not expect elastic scaling errors (e.g. parallelism mismatch)
				// proving that the webhook correctly mapped Job A to Job A and Job B to Job B.
				field.Invalid(field.NewPath("spec").Child("replicatedJobs"), "", "field is immutable"),
			}.ToAggregate(),
			enableElasticJobSet: true,
		},
		{
			name: "invalid scaling: immutability enforced for NonIndexed jobs even when ElasticJobSet is enabled",
			oldJs: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.NonIndexedCompletion),
									Parallelism:    ptr.To[int32](2),
									Completions:    ptr.To[int32](2),
								},
							},
						},
					},
				},
			},
			js: &jobset.JobSet{
				ObjectMeta: validObjectMeta,
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "test-jobset-replicated-job-0",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									CompletionMode: ptr.To(batchv1.NonIndexedCompletion),
									Parallelism:    ptr.To[int32](4), // Attempting to scale a NonIndexed job
									Completions:    ptr.To[int32](4),
								},
							},
						},
					},
				},
			},
			want: field.ErrorList{
				// Because it's NonIndexed, the webhook doesn't mask the fields,
				// so the standard immutability check catches it.
				field.Invalid(field.NewPath("spec").Child("replicatedJobs"), "", "field is immutable"),
			}.ToAggregate(),
			enableElasticJobSet: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewFakeClient()
			webhook := &jobSetWebhook{client: fakeClient}
			features.SetFeatureGateDuringTest(t, features.ElasticJobSet, tc.enableElasticJobSet)
			newObj := tc.js.DeepCopy()
			oldObj := tc.oldJs.DeepCopy()
			_, err := webhook.ValidateUpdate(context.TODO(), oldObj, newObj)
			if err != nil && tc.want != nil {
				var statusErr *apierrors.StatusError
				require.ErrorAs(t, err, &statusErr, "expected *apierrors.StatusError")
				assert.Equal(t, int32(422), statusErr.ErrStatus.Code)
				assert.Equal(t, metav1.StatusReasonInvalid, statusErr.ErrStatus.Reason)

				for _, cause := range statusErr.ErrStatus.Details.Causes {
					assert.Contains(t, tc.want.Error(), cause.Field)
					parts := strings.Split(cause.Message, ": ")
					coreReason := parts[len(parts)-1]
					assert.Contains(t, tc.want.Error(), coreReason)
				}
			} else if err != nil && tc.want == nil {
				t.Errorf("unexpected error: %v", err)
			} else if err == nil && tc.want != nil {
				t.Errorf("missing expected error: %v", tc.want)
			}
		})
	}
}

// jobSetWithVolumeClaimTemplate builds a JobSet named "js" with a single volume claim
// policy holding the given template spec under a Retain retention policy, mounted by its
// one replicatedJob.
func jobSetWithVolumeClaimTemplate(spec corev1.PersistentVolumeClaimSpec) *jobset.JobSet {
	return &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "js",
			Namespace: "default",
		},
		Spec: jobset.JobSetSpec{
			SuccessPolicy: &jobset.SuccessPolicy{},
			VolumeClaimPolicies: []jobset.VolumeClaimPolicy{
				{
					Templates: []corev1.PersistentVolumeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test-volume",
							},
							Spec: spec,
						},
					},
					RetentionPolicy: &jobset.VolumeRetentionPolicy{
						WhenDeleted: ptr.To(jobset.RetentionPolicyRetain),
					},
				},
			},
			ReplicatedJobs: []jobset.ReplicatedJob{
				{
					Name:      "job-1",
					GroupName: "default",
					Replicas:  1,
					Template: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{
											Name:  "test",
											Image: "bash:latest",
											VolumeMounts: []corev1.VolumeMount{
												{
													Name:      "test-volume",
													MountPath: "/test/path",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestPVCSpecMismatches(t *testing.T) {
	t.Parallel()

	// baseTemplate is what a user writes in spec.volumeClaimPolicies[].templates[].spec:
	// only the fields they care about, with everything else left to the API server.
	baseTemplate := corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Gi"),
			},
		},
	}

	// withExisting returns baseTemplate as the API server stores it, after applying the
	// given mutations on top of the defaults it fills in.
	withExisting := func(mutate func(*corev1.PersistentVolumeClaimSpec)) corev1.PersistentVolumeClaimSpec {
		spec := *baseTemplate.DeepCopy()
		spec.VolumeMode = ptr.To(corev1.PersistentVolumeFilesystem)
		spec.StorageClassName = ptr.To("standard")
		if mutate != nil {
			mutate(&spec)
		}
		return spec
	}

	withTemplate := func(mutate func(*corev1.PersistentVolumeClaimSpec)) corev1.PersistentVolumeClaimSpec {
		spec := *baseTemplate.DeepCopy()
		if mutate != nil {
			mutate(&spec)
		}
		return spec
	}

	testCases := map[string]struct {
		template corev1.PersistentVolumeClaimSpec
		existing corev1.PersistentVolumeClaimSpec
		want     []string
	}{
		"identical specs match": {
			template: baseTemplate,
			existing: *baseTemplate.DeepCopy(),
		},
		"template unset fields defaulted by the API server match": {
			template: baseTemplate,
			existing: withExisting(nil),
		},
		"bound PVC with a binder-assigned volumeName matches an unpinned template": {
			template: baseTemplate,
			existing: withExisting(func(s *corev1.PersistentVolumeClaimSpec) {
				s.VolumeName = "pv-abc123"
			}),
		},
		"equal storage requests in different units match": {
			template: withTemplate(func(s *corev1.PersistentVolumeClaimSpec) {
				s.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("1024Mi")
			}),
			existing: withExisting(nil),
		},
		"template that pins the same storageClassName matches": {
			template: withTemplate(func(s *corev1.PersistentVolumeClaimSpec) {
				s.StorageClassName = ptr.To("standard")
			}),
			existing: withExisting(nil),
		},
		"template that sets only dataSource matches a cross-populated existing PVC": {
			template: withTemplate(func(s *corev1.PersistentVolumeClaimSpec) {
				s.DataSource = &corev1.TypedLocalObjectReference{Kind: "PersistentVolumeClaim", Name: "src"}
			}),
			existing: withExisting(func(s *corev1.PersistentVolumeClaimSpec) {
				s.DataSource = &corev1.TypedLocalObjectReference{Kind: "PersistentVolumeClaim", Name: "src"}
				s.DataSourceRef = &corev1.TypedObjectReference{Kind: "PersistentVolumeClaim", Name: "src"}
			}),
		},
		"template that sets only dataSourceRef matches a cross-populated existing PVC": {
			template: withTemplate(func(s *corev1.PersistentVolumeClaimSpec) {
				s.DataSourceRef = &corev1.TypedObjectReference{Kind: "PersistentVolumeClaim", Name: "src"}
			}),
			existing: withExisting(func(s *corev1.PersistentVolumeClaimSpec) {
				s.DataSource = &corev1.TypedLocalObjectReference{Kind: "PersistentVolumeClaim", Name: "src"}
				s.DataSourceRef = &corev1.TypedObjectReference{Kind: "PersistentVolumeClaim", Name: "src"}
			}),
		},
		"existing PVC with a default volumeAttributesClassName matches an unset template": {
			template: baseTemplate,
			existing: withExisting(func(s *corev1.PersistentVolumeClaimSpec) {
				s.VolumeAttributesClassName = ptr.To("silver")
			}),
		},
		"nil and empty accessModes match": {
			template: withTemplate(func(s *corev1.PersistentVolumeClaimSpec) {
				s.AccessModes = nil
			}),
			existing: withExisting(func(s *corev1.PersistentVolumeClaimSpec) {
				s.AccessModes = []corev1.PersistentVolumeAccessMode{}
			}),
		},
		// accessModes, resources and selector are never populated by the API server, so
		// unlike the fields above they are compared as written even when the template
		// leaves them unset.
		"unset accessModes are still compared": {
			template: withTemplate(func(s *corev1.PersistentVolumeClaimSpec) {
				s.AccessModes = nil
			}),
			existing: withExisting(nil),
			want:     []string{"accessModes"},
		},
		// A retained PVC that has since been expanded no longer matches a template asking
		// for the original size. Rejecting is intentional: the size is the user's to
		// declare, not the API server's to fill in.
		"expanded existing PVC does not match the original template": {
			template: baseTemplate,
			existing: withExisting(func(s *corev1.PersistentVolumeClaimSpec) {
				s.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("2Gi")
			}),
			want: []string{"resources"},
		},
		"different storage request does not match": {
			template: withTemplate(func(s *corev1.PersistentVolumeClaimSpec) {
				s.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("2Gi")
			}),
			existing: withExisting(nil),
			want:     []string{"resources"},
		},
		"different accessModes do not match": {
			template: withTemplate(func(s *corev1.PersistentVolumeClaimSpec) {
				s.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}
			}),
			existing: withExisting(nil),
			want:     []string{"accessModes"},
		},
		"explicitly pinned storageClassName that differs does not match": {
			template: withTemplate(func(s *corev1.PersistentVolumeClaimSpec) {
				s.StorageClassName = ptr.To("premium")
			}),
			existing: withExisting(nil),
			want:     []string{"storageClassName"},
		},
		"empty storageClassName is distinct from the cluster default": {
			template: withTemplate(func(s *corev1.PersistentVolumeClaimSpec) {
				s.StorageClassName = ptr.To("")
			}),
			existing: withExisting(nil),
			want:     []string{"storageClassName"},
		},
		"explicitly pinned volumeName that differs does not match": {
			template: withTemplate(func(s *corev1.PersistentVolumeClaimSpec) {
				s.VolumeName = "some-other-pv"
			}),
			existing: withExisting(func(s *corev1.PersistentVolumeClaimSpec) {
				s.VolumeName = "pv-abc123"
			}),
			want: []string{"volumeName"},
		},
		"explicitly pinned volumeMode that differs does not match": {
			template: withTemplate(func(s *corev1.PersistentVolumeClaimSpec) {
				s.VolumeMode = ptr.To(corev1.PersistentVolumeBlock)
			}),
			existing: withExisting(nil),
			want:     []string{"volumeMode"},
		},
		"explicitly pinned volumeAttributesClassName that differs does not match": {
			template: withTemplate(func(s *corev1.PersistentVolumeClaimSpec) {
				s.VolumeAttributesClassName = ptr.To("gold")
			}),
			existing: withExisting(func(s *corev1.PersistentVolumeClaimSpec) {
				s.VolumeAttributesClassName = ptr.To("silver")
			}),
			want: []string{"volumeAttributesClassName"},
		},
		"different selector does not match": {
			template: withTemplate(func(s *corev1.PersistentVolumeClaimSpec) {
				s.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"tier": "fast"}}
			}),
			existing: withExisting(nil),
			want:     []string{"selector"},
		},
		"different dataSource does not match": {
			template: withTemplate(func(s *corev1.PersistentVolumeClaimSpec) {
				s.DataSource = &corev1.TypedLocalObjectReference{Kind: "PersistentVolumeClaim", Name: "src"}
			}),
			existing: withExisting(func(s *corev1.PersistentVolumeClaimSpec) {
				s.DataSource = &corev1.TypedLocalObjectReference{Kind: "PersistentVolumeClaim", Name: "other"}
			}),
			want: []string{"dataSource"},
		},
		"every differing field is reported": {
			template: withTemplate(func(s *corev1.PersistentVolumeClaimSpec) {
				s.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}
				s.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("2Gi")
				s.VolumeMode = ptr.To(corev1.PersistentVolumeBlock)
			}),
			existing: withExisting(nil),
			want:     []string{"accessModes", "resources", "volumeMode"},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// DeepCopy the template the way the API server does before admission, which
			// drops the cached string representation of resource quantities.
			got := pvcSpecMismatches(tc.template.DeepCopy(), &tc.existing)
			assert.ElementsMatch(t, tc.want, got)
		})
	}
}

// TestPVCSpecMismatchesDetectsEveryField guards the reflection-based comparison in
// pvcSpecMismatches against a field being added to PersistentVolumeClaimSpec upstream.
//
// That is the maintenance trap behind this whole check. A new field the API server
// populates on the user's behalf, left out of normalizePVCSpec, brings back exactly the
// bug this function exists to fix: a retained PVC stops matching the template that
// created it. The count assertion below fails as soon as such a field appears, so
// whoever bumps k8s.io/api has to decide which side of the comparison it belongs on
// rather than finding out from a bug report.
//
// Each field is also given a conflicting value to confirm it is genuinely compared and
// reported under its json name, so nothing can be silently dropped or surface as a Go
// field name in a user facing message.
func TestPVCSpecMismatchesDetectsEveryField(t *testing.T) {
	t.Parallel()

	// A fully populated PVC: every field set, as the API server would return it.
	existing := corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
		},
		Selector:                  &metav1.LabelSelector{MatchLabels: map[string]string{"tier": "fast"}},
		VolumeName:                "pv-abc123",
		StorageClassName:          ptr.To("standard"),
		VolumeMode:                ptr.To(corev1.PersistentVolumeFilesystem),
		DataSource:                &corev1.TypedLocalObjectReference{Kind: "PersistentVolumeClaim", Name: "src"},
		DataSourceRef:             &corev1.TypedObjectReference{Kind: "PersistentVolumeClaim", Name: "src"},
		VolumeAttributesClassName: ptr.To("silver"),
	}

	// One conflicting template per field, keyed by the json name it must be reported as.
	conflicts := map[string]func(*corev1.PersistentVolumeClaimSpec){
		"accessModes": func(s *corev1.PersistentVolumeClaimSpec) {
			s.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}
		},
		"resources": func(s *corev1.PersistentVolumeClaimSpec) {
			s.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("99Gi")
		},
		"selector": func(s *corev1.PersistentVolumeClaimSpec) {
			s.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"tier": "slow"}}
		},
		"volumeName": func(s *corev1.PersistentVolumeClaimSpec) {
			s.VolumeName = "some-other-pv"
		},
		"storageClassName": func(s *corev1.PersistentVolumeClaimSpec) {
			s.StorageClassName = ptr.To("premium")
		},
		"volumeMode": func(s *corev1.PersistentVolumeClaimSpec) {
			s.VolumeMode = ptr.To(corev1.PersistentVolumeBlock)
		},
		"dataSource": func(s *corev1.PersistentVolumeClaimSpec) {
			s.DataSource = &corev1.TypedLocalObjectReference{Kind: "PersistentVolumeClaim", Name: "other"}
		},
		"dataSourceRef": func(s *corev1.PersistentVolumeClaimSpec) {
			s.DataSourceRef = &corev1.TypedObjectReference{Kind: "PersistentVolumeClaim", Name: "other"}
		},
		"volumeAttributesClassName": func(s *corev1.PersistentVolumeClaimSpec) {
			s.VolumeAttributesClassName = ptr.To("gold")
		},
	}

	specType := reflect.TypeOf(corev1.PersistentVolumeClaimSpec{})
	require.Equal(t, specType.NumField(), len(conflicts),
		"PersistentVolumeClaimSpec has %d fields but %d are covered here: decide whether the "+
			"new field is one the API server populates, adding it to normalizePVCSpec if so, "+
			"then cover it above", specType.NumField(), len(conflicts))

	for i := range specType.NumField() {
		f := specType.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		require.NotEmpty(t, name, "field %s has no json tag and would be reported by its Go name", f.Name)
		require.Contains(t, conflicts, name, "field %s is not covered by this test", f.Name)
	}

	for name, mutate := range conflicts {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			template := *existing.DeepCopy()
			mutate(&template)
			assert.Contains(t, pvcSpecMismatches(&template, existing.DeepCopy()), name,
				"a template conflicting on %s was not reported", name)
		})
	}
}
