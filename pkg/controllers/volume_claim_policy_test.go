package controllers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/events"
	"k8s.io/klog/v2/ktesting"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	testutils "sigs.k8s.io/jobset/pkg/util/testing"
)

func TestReconcileVolumeClaimPolicies(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)

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

	tests := []struct {
		name         string
		jobSet       *jobset.JobSet
		existingPVC  *corev1.PersistentVolumeClaim
		expectedPVCs []corev1.PersistentVolumeClaim
		expectErrStr string
	}{
		{
			name: "create multiple PVCs with different retention policies and custom labels/annotations",
			jobSet: testutils.MakeJobSet(jobSetName, ns).
				VolumeClaimPolicies([]jobset.VolumeClaimPolicy{
					{
						Templates: []corev1.PersistentVolumeClaim{
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "volume-delete",
								},
								Spec: testPVCSpec,
							},
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "volume-custom",
									Labels: map[string]string{
										"custom-label": "custom-value",
									},
									Annotations: map[string]string{
										"custom-annotation": "custom-value",
									},
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
									Name: "volume-retain",
								},
								Spec: testPVCSpec,
							},
						},
						RetentionPolicy: &jobset.VolumeRetentionPolicy{
							WhenDeleted: ptr.To(jobset.RetentionPolicyRetain),
						},
					},
				}).Obj(),
			expectedPVCs: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "volume-delete-test-jobset",
						Namespace: ns,
						Labels: map[string]string{
							jobset.JobSetNameKey: jobSetName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         "jobset.x-k8s.io/v1alpha2",
								Kind:               "JobSet",
								Name:               jobSetName,
								Controller:         ptr.To(true),
								BlockOwnerDeletion: ptr.To(true),
							},
						},
					},
					Spec: testPVCSpec,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "volume-custom-test-jobset",
						Namespace: ns,
						Labels: map[string]string{
							jobset.JobSetNameKey: jobSetName,
							"custom-label":       "custom-value",
						},
						Annotations: map[string]string{
							"custom-annotation": "custom-value",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion:         "jobset.x-k8s.io/v1alpha2",
								Kind:               "JobSet",
								Name:               jobSetName,
								Controller:         ptr.To(true),
								BlockOwnerDeletion: ptr.To(true),
							},
						},
					},
					Spec: testPVCSpec,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "volume-retain-test-jobset",
						Namespace: ns,
						Labels: map[string]string{
							jobset.JobSetNameKey: jobSetName,
						},
					},
					Spec: testPVCSpec,
				},
			},
		},
		{
			name: "PVC already exists and should not be created again",
			jobSet: testutils.MakeJobSet(jobSetName, ns).
				VolumeClaimPolicies([]jobset.VolumeClaimPolicy{
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
				}).Obj(),
			existingPVC: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-volume-test-jobset",
					Namespace: ns,
				},
				Spec: testPVCSpec,
			},
			expectedPVCs: []corev1.PersistentVolumeClaim{},
		},
		{
			name: "PVC creation fails with unexpected error",
			jobSet: testutils.MakeJobSet(jobSetName, ns).
				VolumeClaimPolicies([]jobset.VolumeClaimPolicy{
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
				}).Obj(),
			expectErrStr: "unexpected error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var createdPVCs []*corev1.PersistentVolumeClaim
			_, ctx := ktesting.NewTestContext(t)
			scheme := runtime.NewScheme()
			utilruntime.Must(jobset.AddToScheme(scheme))
			utilruntime.Must(corev1.AddToScheme(scheme))
			fakeClientBuilder := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
				Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if tc.expectErrStr != "" {
						return errors.New(tc.expectErrStr)
					}
					pvc, ok := obj.(*corev1.PersistentVolumeClaim)
					if !ok {
						return nil
					}

					createdPVCs = append(createdPVCs, pvc)
					return nil
				},
			})

			if tc.existingPVC != nil {
				fakeClientBuilder.WithObjects(tc.existingPVC)
			}
			fakeClient := fakeClientBuilder.Build()

			// Create a JobSetReconciler instance with the fake client
			r := &JobSetReconciler{
				Client: fakeClient,
				Scheme: scheme,
				Record: events.NewFakeRecorder(32),
			}

			// Execute the function under test
			gotErr := r.reconcileVolumeClaimPolicies(ctx, tc.jobSet)

			if tc.expectErrStr != "" {
				if gotErr == nil {
					t.Errorf("expected error containing %q, got nil", tc.expectErrStr)
				} else if !strings.Contains(gotErr.Error(), tc.expectErrStr) {
					t.Errorf("expected error message contains %q, got %v", tc.expectErrStr, gotErr)
				}
				return
			}

			if gotErr != nil {
				t.Errorf("expected no error, got %v", gotErr)
			}

			for i, expectedPVC := range tc.expectedPVCs {
				if diff := cmp.Diff(expectedPVC, *createdPVCs[i]); diff != "" {
					t.Errorf("PVC[%d] mismatch (-want +got):\n%s", i, diff)
				}
			}
		})
	}
}

func TestAddVolumes(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)

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

	tests := []struct {
		name            string
		job             *batchv1.Job
		jobSet          *jobset.JobSet
		expectedVolumes []corev1.Volume
	}{
		{
			name: "add volumes for matching volumeMounts",
			job: testutils.MakeJob("test-job", ns).
				PodSpec(corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "busybox:latest",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "volume-1",
									MountPath: "/data1",
								},
								{
									Name:      "volume-2",
									MountPath: "/data2",
								},
							},
						},
					},
				}).Obj(),
			jobSet: testutils.MakeJobSet(jobSetName, ns).
				VolumeClaimPolicies([]jobset.VolumeClaimPolicy{
					{
						Templates: []corev1.PersistentVolumeClaim{
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "volume-1",
								},
								Spec: testPVCSpec,
							},
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "volume-2",
								},
								Spec: testPVCSpec,
							},
						},
					},
				}).Obj(),
			expectedVolumes: []corev1.Volume{
				{
					Name: "volume-1",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "volume-1-test-jobset",
						},
					},
				},
				{
					Name: "volume-2",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "volume-2-test-jobset",
						},
					},
				},
			},
		},
		{
			name: "do not add volumes when volumeMounts are missing",
			job: testutils.MakeJob("test-job", ns).
				PodSpec(corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "busybox:latest",
						},
					},
				}).Obj(),
			jobSet: testutils.MakeJobSet(jobSetName, ns).
				VolumeClaimPolicies([]jobset.VolumeClaimPolicy{
					{
						Templates: []corev1.PersistentVolumeClaim{
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "volume-1",
								},
								Spec: testPVCSpec,
							},
						},
					},
				}).Obj(),
			expectedVolumes: nil,
		},
		{
			name: "add only matching volumes when some volumeMounts exist",
			job: testutils.MakeJob("test-job", ns).
				PodSpec(corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "busybox:latest",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "volume-1",
									MountPath: "/data1",
								},
							},
						},
					},
				}).Obj(),
			jobSet: testutils.MakeJobSet(jobSetName, ns).
				VolumeClaimPolicies([]jobset.VolumeClaimPolicy{
					{
						Templates: []corev1.PersistentVolumeClaim{
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "volume-1",
								},
								Spec: testPVCSpec,
							},
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "volume-2",
								},
								Spec: testPVCSpec,
							},
						},
					},
				}).Obj(),
			expectedVolumes: []corev1.Volume{
				{
					Name: "volume-1",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "volume-1-test-jobset",
						},
					},
				},
			},
		},
		{
			name: "add volumes from multiple VolumeClaimPolicies with initContainer",
			job: testutils.MakeJob("test-job", ns).
				PodSpec(corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  "init-container",
							Image: "busybox:latest",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "volume-init",
									MountPath: "/init",
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "busybox:latest",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "volume-delete",
									MountPath: "/data1",
								},
								{
									Name:      "volume-retain",
									MountPath: "/data2",
								},
							},
						},
					},
				}).Obj(),
			jobSet: testutils.MakeJobSet(jobSetName, ns).
				VolumeClaimPolicies([]jobset.VolumeClaimPolicy{
					{
						Templates: []corev1.PersistentVolumeClaim{
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "volume-delete",
								},
								Spec: testPVCSpec,
							},
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "volume-init",
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
									Name: "volume-retain",
								},
								Spec: testPVCSpec,
							},
						},
						RetentionPolicy: &jobset.VolumeRetentionPolicy{
							WhenDeleted: ptr.To(jobset.RetentionPolicyRetain),
						},
					},
				}).Obj(),
			expectedVolumes: []corev1.Volume{
				{
					Name: "volume-delete",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "volume-delete-test-jobset",
						},
					},
				},
				{
					Name: "volume-init",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "volume-init-test-jobset",
						},
					},
				},
				{
					Name: "volume-retain",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "volume-retain-test-jobset",
						},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			addVolumes(tc.job, tc.jobSet)
			if diff := cmp.Diff(tc.expectedVolumes, tc.job.Spec.Template.Spec.Volumes); diff != "" {
				t.Errorf("volumes mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
