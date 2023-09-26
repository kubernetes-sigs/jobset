package webhooks

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

func TestGenLeaderPodName(t *testing.T) {
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
