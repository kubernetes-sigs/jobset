package webhooks

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/controllers"
)

func TestLeaderPodName(t *testing.T) {
	cases := []struct {
		desc    string
		pod     *corev1.Pod
		want    string
		wantErr bool
	}{
		{
			desc: "valid pod",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod",
					Labels: map[string]string{
						jobset.JobSetNameKey:        "js",
						jobset.ReplicatedJobNameKey: "rjob",
						jobset.JobIndexKey:          "0",
					},
				},
			},
			want: "js-rjob-0-0",
		},
		{
			desc: "pod missing labels",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod",
					Labels: map[string]string{
						jobset.JobSetNameKey:        "js",
						jobset.ReplicatedJobNameKey: "rjob",
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := genLeaderPodName(tc.pod)
			// Handle unexpected error.
			if err != nil && !tc.wantErr {
				t.Errorf("unexpected error: %v", err)
				return
			}
			// Handle missing an expected error.
			if err == nil && tc.wantErr {
				t.Errorf("expected error, got nil")
				return
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("unexpected diff (-want/+got): %s", diff)
			}
		})
	}
}

func TestPodsOwnedBySameJob(t *testing.T) {
	testCases := []struct {
		name        string
		leaderPod   *corev1.Pod
		followerPod *corev1.Pod
		wantResult  error
	}{
		{
			name:        "pods owned by the same job",
			leaderPod:   createPodWithOwner("leader-pod", "job-uid-1"),
			followerPod: createPodWithOwner("follower-pod", "job-uid-1"),
			wantResult:  nil,
		},
		{
			name:        "pods owned by different jobs",
			leaderPod:   createPodWithOwner("leader-pod", "job-uid-1"),
			followerPod: createPodWithOwner("follower-pod", "job-uid-2"),
			wantResult:  fmt.Errorf("follower pod owner UID (%s) != leader pod owner UID (%s)", "job-uid-2", "job-uid-1"),
		},
		{
			name:        "follower pod with no owner",
			leaderPod:   createPodWithOwner("leader-pod", "job-uid-1"),
			followerPod: createPodWithOwner("follower-pod", ""),
			wantResult:  fmt.Errorf("follower pod has no owner reference"),
		},
		{
			name:        "leader pod with no owner",
			leaderPod:   createPodWithOwner("leader-pod", ""),
			followerPod: createPodWithOwner("follower-pod", "job-uid-2"),
			wantResult:  fmt.Errorf("leader pod \"leader-pod\" has no owner reference"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := podsOwnedBySameJob(tc.leaderPod, tc.followerPod)

			// Want no error, but we got an error.
			if tc.wantResult == nil && result != nil {
				t.Errorf("Unexpected error: %v", result)

				// Want an error, but we got no error.
			} else if tc.wantResult != nil && result == nil {
				t.Error("Expected error, but got nil")

				// Want an error but the error we got is not the expected error.
			} else if tc.wantResult != nil && result != nil && tc.wantResult.Error() != result.Error() {
				t.Errorf("Expected error: %v, got: %v", tc.wantResult, result)
			}
		})
	}
}

func TestLeaderPodForFollower(t *testing.T) {
	// Common owner reference for pods in the same job.
	ownerRef := metav1.OwnerReference{UID: types.UID("job-uid-1"), Kind: "Job", Controller: ptr.To(true)}
	annotations := map[string]string{jobset.ExclusiveKey: "topology.kubernetes.io/zone"}
	leaderPod := makeTestPod("js-rjob-0-0-xyz", &ownerRef, annotations)
	followerPod := makeTestPod("js-rjob-0-1-abc", &ownerRef, annotations)

	testCases := []struct {
		name          string
		followerPod   *corev1.Pod
		existingPods  []runtime.Object
		wantLeaderPod *corev1.Pod
		wantErr       string
	}{
		{
			name:          "valid leader pod exists",
			followerPod:   followerPod,
			existingPods:  []runtime.Object{leaderPod},
			wantLeaderPod: leaderPod,
		},
		{
			name: "follower pod missing labels",
			followerPod: func() *corev1.Pod {
				p := followerPod.DeepCopy()
				delete(p.Labels, jobset.JobIndexKey)
				return p
			}(),
			wantErr: "pod missing label: jobset.sigs.k8s.io/job-index",
		},
		{
			name:         "no leader pod found",
			followerPod:  followerPod,
			existingPods: []runtime.Object{},
			wantErr:      "expected 1 leader pod (js-rjob-0-0), but got 0",
		},
		{
			name:        "multiple leader pods found",
			followerPod: followerPod,
			existingPods: []runtime.Object{
				leaderPod,
				makeTestPod("js-rjob-0-0-uvw", &ownerRef, annotations),
			},
			wantErr: "expected 1 leader pod (js-rjob-0-0), but got 2",
		},
		{
			name:        "leader pod is terminating",
			followerPod: followerPod,
			existingPods: []runtime.Object{
				func() *corev1.Pod {
					p := leaderPod.DeepCopy()
					p.DeletionTimestamp = ptr.To(metav1.Now())
					p.Finalizers = []string{"batch.kubernetes.io/job-tracking"}
					return p
				}(),
			},
			wantErr: "expected 1 leader pod (js-rjob-0-0), but got 0",
		},
		{
			name:        "leader and follower owned by different jobs",
			followerPod: followerPod,
			existingPods: []runtime.Object{
				makeTestPod("js-rjob-0-0-xyz", &metav1.OwnerReference{UID: types.UID("job-uid-2"), Kind: "Job", Controller: ptr.To(true)}, annotations),
			},
			wantErr: "follower pod owner UID (job-uid-1) != leader pod owner UID (job-uid-2)",
		},
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tc.existingPods...).
				WithIndex(&corev1.Pod{}, controllers.PodNameKey, controllers.IndexPodName).
				Build()
			p := &podWebhook{client: fakeClient}

			got, err := p.leaderPodForFollower(t.Context(), tc.followerPod)

			if tc.wantErr != "" {
				if err == nil {
					t.Errorf("expected error %q, got nil", tc.wantErr)
					return
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("expected error to contain %q, got %q", tc.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if diff := cmp.Diff(tc.wantLeaderPod, got); diff != "" {
				t.Errorf("unexpected diff (-want/+got): %s", diff)
			}
		})
	}
}

func createPodWithOwner(name string, ownerUID string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	if ownerUID != "" {
		pod.OwnerReferences = []metav1.OwnerReference{
			{UID: types.UID(ownerUID), Kind: "Job", Controller: ptr.To(true)},
		}
	}
	return pod
}

func makeTestPod(name string, owner *metav1.OwnerReference, annotations map[string]string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				jobset.JobSetNameKey:        "js",
				jobset.ReplicatedJobNameKey: "rjob",
				jobset.JobIndexKey:          "0",
			},
			Annotations: annotations,
		},
	}
	if owner != nil {
		pod.OwnerReferences = []metav1.OwnerReference{*owner}
	}
	return pod
}
