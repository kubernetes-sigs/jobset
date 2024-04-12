package webhooks

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha1"
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
