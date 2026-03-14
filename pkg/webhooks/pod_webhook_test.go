/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webhooks

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/controllers"
)

func TestDefault(t *testing.T) {
	testcases := []struct {
		name         string
		pod          *corev1.Pod
		existingObjs []runtime.Object
		wantPod      *corev1.Pod
		wantErr      error
	}{
		{
			name: "not a jobset pod",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "p",
				},
			},
			wantPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "p",
				},
			},
		},
		{
			name: "using node selector strategy",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "p",
					Annotations: map[string]string{
						"jobset.sigs.k8s.io/jobset-name":         "js",
						"alpha.jobset.sigs.k8s.io/node-selector": "",
					},
				},
			},
			wantPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "p",
					Annotations: map[string]string{
						"jobset.sigs.k8s.io/jobset-name":         "js",
						"alpha.jobset.sigs.k8s.io/node-selector": "",
					},
				},
			},
		},
		{
			name: "jobset pod, no exclusive placement, no priority",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "p",
					Annotations: map[string]string{
						"jobset.sigs.k8s.io/jobset-name": "js",
					},
				},
			},
			wantPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "p",
					Annotations: map[string]string{
						"jobset.sigs.k8s.io/jobset-name": "js",
					},
				},
			},
		},
		{
			name: "jobset pod, no exclusive placement, with priority",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "p",
					Annotations: map[string]string{
						"jobset.sigs.k8s.io/jobset-name": "js",
					},
				},
				Spec: corev1.PodSpec{
					Priority: ptr.To(int32(100)),
				},
			},
			wantPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "p",
					Annotations: map[string]string{
						"jobset.sigs.k8s.io/jobset-name": "js",
					},
					Labels: map[string]string{
						"jobset.sigs.k8s.io/priority": "100",
					},
				},
				Spec: corev1.PodSpec{
					Priority: ptr.To(int32(100)),
				},
			},
		},
		{
			name: "leader pod with exclusive placement",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "p-leader",
					Labels: map[string]string{
						"jobset.sigs.k8s.io/job-key": "js-rjob-0",
					},
					Annotations: map[string]string{
						"jobset.sigs.k8s.io/jobset-name":              "js",
						"alpha.jobset.sigs.k8s.io/exclusive-topology": "topology.kubernetes.io/zone",
						"batch.kubernetes.io/job-completion-index":    "0",
						"jobset.sigs.k8s.io/replicatedjob-name":       "rjob",
						"jobset.sigs.k8s.io/job-index":                "0",
					},
				},
				Spec: corev1.PodSpec{
					Priority: ptr.To(int32(100)),
				},
			},
			wantPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "p-leader",
					Labels: map[string]string{
						"jobset.sigs.k8s.io/job-key":  "js-rjob-0",
						"jobset.sigs.k8s.io/priority": "100",
					},
					Annotations: map[string]string{
						"jobset.sigs.k8s.io/jobset-name":              "js",
						"alpha.jobset.sigs.k8s.io/exclusive-topology": "topology.kubernetes.io/zone",
						"batch.kubernetes.io/job-completion-index":    "0",
						"jobset.sigs.k8s.io/replicatedjob-name":       "rjob",
						"jobset.sigs.k8s.io/job-index":                "0",
					},
				},
				Spec: corev1.PodSpec{
					Priority: ptr.To(int32(100)),
					Affinity: &corev1.Affinity{
						PodAffinity: &corev1.PodAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
								{
									LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      "jobset.sigs.k8s.io/job-key",
											Operator: "In",
											Values:   []string{"js-rjob-0"},
										},
									}},
									TopologyKey:       "topology.kubernetes.io/zone",
									NamespaceSelector: &metav1.LabelSelector{},
								},
							},
						},
						PodAntiAffinity: &corev1.PodAntiAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
								{
									LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      "jobset.sigs.k8s.io/job-key",
											Operator: "Exists",
										},
										{
											Key:      "jobset.sigs.k8s.io/job-key",
											Operator: "NotIn",
											Values:   []string{"js-rjob-0"},
										},
										{
											Key:      "jobset.sigs.k8s.io/priority",
											Operator: "In",
											Values:   []string{"100"},
										},
									}},
									TopologyKey:       "topology.kubernetes.io/zone",
									NamespaceSelector: &metav1.LabelSelector{},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "follower pod, leader not scheduled",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "js-rjob-0-1-follower",
					Namespace: "default",
					Labels: map[string]string{
						"jobset.sigs.k8s.io/jobset-name":        "js",
						"jobset.sigs.k8s.io/replicatedjob-name": "rjob",
						"jobset.sigs.k8s.io/job-index":          "0",
					},
					Annotations: map[string]string{
						"jobset.sigs.k8s.io/jobset-name":              "js",
						"alpha.jobset.sigs.k8s.io/exclusive-topology": "topology.kubernetes.io/zone",
						"batch.kubernetes.io/job-completion-index":    "1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{UID: types.UID("job-uid-1"), Kind: "Job", Controller: ptr.To(true)},
					},
				},
				Spec: corev1.PodSpec{
					Priority: ptr.To(int32(100)),
				},
			},
			existingObjs: []runtime.Object{
				&corev1.Pod{ // Leader pod, not scheduled.
					ObjectMeta: metav1.ObjectMeta{
						Name:      "js-rjob-0-0-leader",
						Namespace: "default",
						Labels: map[string]string{
							"jobset.sigs.k8s.io/jobset-name":        "js",
							"jobset.sigs.k8s.io/replicatedjob-name": "rjob",
							"jobset.sigs.k8s.io/job-index":          "0",
						},
						Annotations: map[string]string{
							"alpha.jobset.sigs.k8s.io/exclusive-topology": "topology.kubernetes.io/zone",
							"batch.kubernetes.io/job-completion-index":    "0",
						},
						OwnerReferences: []metav1.OwnerReference{
							{UID: types.UID("job-uid-1"), Kind: "Job", Controller: ptr.To(true)},
						},
					},
				},
			},
			wantPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "js-rjob-0-1-follower",
					Namespace: "default",
					Labels: map[string]string{
						"jobset.sigs.k8s.io/jobset-name":        "js",
						"jobset.sigs.k8s.io/replicatedjob-name": "rjob",
						"jobset.sigs.k8s.io/job-index":          "0",
						"jobset.sigs.k8s.io/priority":           "100",
					},
					Annotations: map[string]string{
						"jobset.sigs.k8s.io/jobset-name":              "js",
						"alpha.jobset.sigs.k8s.io/exclusive-topology": "topology.kubernetes.io/zone",
						"batch.kubernetes.io/job-completion-index":    "1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{UID: types.UID("job-uid-1"), Kind: "Job", Controller: ptr.To(true)},
					},
				},
				Spec: corev1.PodSpec{
					Priority: ptr.To(int32(100)),
				},
			},
		},
		{
			name: "follower pod, leader scheduled",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "js-rjob-0-1-follower",
					Namespace: "default",
					Labels: map[string]string{
						"jobset.sigs.k8s.io/jobset-name":        "js",
						"jobset.sigs.k8s.io/replicatedjob-name": "rjob",
						"jobset.sigs.k8s.io/job-index":          "0",
					},
					Annotations: map[string]string{
						"jobset.sigs.k8s.io/jobset-name":              "js",
						"alpha.jobset.sigs.k8s.io/exclusive-topology": "topology.kubernetes.io/zone",
						"batch.kubernetes.io/job-completion-index":    "1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{UID: types.UID("job-uid-1"), Kind: "Job", Controller: ptr.To(true)},
					},
				},
				Spec: corev1.PodSpec{
					Priority: ptr.To(int32(100)),
				},
			},
			existingObjs: []runtime.Object{
				&corev1.Pod{ // Leader pod, scheduled.
					ObjectMeta: metav1.ObjectMeta{
						Name:      "js-rjob-0-0-leader",
						Namespace: "default",
						Labels: map[string]string{
							"jobset.sigs.k8s.io/jobset-name":        "js",
							"jobset.sigs.k8s.io/replicatedjob-name": "rjob",
							"jobset.sigs.k8s.io/job-index":          "0",
						},
						Annotations: map[string]string{
							"alpha.jobset.sigs.k8s.io/exclusive-topology": "topology.kubernetes.io/zone",
							"batch.kubernetes.io/job-completion-index":    "0",
						},
						OwnerReferences: []metav1.OwnerReference{
							{UID: types.UID("job-uid-1"), Kind: "Job", Controller: ptr.To(true)},
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "node-a",
					},
				},
				&corev1.Node{ // Node where leader is scheduled.
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-a",
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "zone-a",
						},
					},
				},
			},
			wantPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "js-rjob-0-1-follower",
					Namespace: "default",
					Labels: map[string]string{
						"jobset.sigs.k8s.io/jobset-name":        "js",
						"jobset.sigs.k8s.io/replicatedjob-name": "rjob",
						"jobset.sigs.k8s.io/job-index":          "0",
						"jobset.sigs.k8s.io/priority":           "100",
					},
					Annotations: map[string]string{
						"jobset.sigs.k8s.io/jobset-name":              "js",
						"alpha.jobset.sigs.k8s.io/exclusive-topology": "topology.kubernetes.io/zone",
						"batch.kubernetes.io/job-completion-index":    "1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{UID: types.UID("job-uid-1"), Kind: "Job", Controller: ptr.To(true)},
					},
				},
				Spec: corev1.PodSpec{
					Priority: ptr.To(int32(100)),
					NodeSelector: map[string]string{
						"topology.kubernetes.io/zone": "zone-a",
					},
				},
			},
		},
		{
			name: "follower pod, leader scheduled on missing node",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "js-rjob-0-1-follower",
					Namespace: "default",
					Labels: map[string]string{
						"jobset.sigs.k8s.io/jobset-name":        "js",
						"jobset.sigs.k8s.io/replicatedjob-name": "rjob",
						"jobset.sigs.k8s.io/job-index":          "0",
					},
					Annotations: map[string]string{
						"jobset.sigs.k8s.io/jobset-name":              "js",
						"alpha.jobset.sigs.k8s.io/exclusive-topology": "topology.kubernetes.io/zone",
						"batch.kubernetes.io/job-completion-index":    "1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{UID: types.UID("job-uid-1"), Kind: "Job", Controller: ptr.To(true)},
					},
				},
				Spec: corev1.PodSpec{
					Priority: ptr.To(int32(100)),
				},
			},
			existingObjs: []runtime.Object{
				&corev1.Pod{ // Leader pod, scheduled on missing node
					ObjectMeta: metav1.ObjectMeta{
						Name:      "js-rjob-0-0-leader",
						Namespace: "default",
						Labels: map[string]string{
							"jobset.sigs.k8s.io/jobset-name":        "js",
							"jobset.sigs.k8s.io/replicatedjob-name": "rjob",
							"jobset.sigs.k8s.io/job-index":          "0",
						},
						Annotations: map[string]string{
							"alpha.jobset.sigs.k8s.io/exclusive-topology": "topology.kubernetes.io/zone",
							"batch.kubernetes.io/job-completion-index":    "0",
						},
						OwnerReferences: []metav1.OwnerReference{
							{UID: types.UID("job-uid-1"), Kind: "Job", Controller: ptr.To(true)},
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "node-a",
					},
				},
			},
			wantPod: nil,
			wantErr: apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "nodes"}, "node-a"),
		},
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tc.existingObjs...).
				WithIndex(&corev1.Pod{}, "podName", controllers.IndexPodName).
				Build()

			p := &podWebhook{client: fakeClient}
			podCopy := tc.pod.DeepCopy()
			err := p.Default(context.Background(), podCopy)

			if diff := cmp.Diff(tc.wantErr, err); diff != "" {
				fmt.Printf("%+v", err)
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}

			if tc.wantPod == nil {
				return
			}

			if diff := cmp.Diff(tc.wantPod, podCopy); tc.wantPod != nil && diff != "" {
				t.Errorf("pod mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPodWebhookValidateCreate(t *testing.T) {
	testCases := []struct {
		name         string
		pod          *corev1.Pod
		existingObjs []runtime.Object
		wantErr      string
	}{
		{
			name: "follower pod returns transient error when leader is not scheduled even without node selector",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "js-rjob-0-1-follower",
					Namespace: "default",
					Labels: map[string]string{
						jobset.JobSetNameKey:        "js",
						jobset.ReplicatedJobNameKey: "rjob",
						jobset.JobIndexKey:          "0",
					},
					Annotations: map[string]string{
						jobset.JobSetNameKey:                       "js",
						jobset.ExclusiveKey:                        "topology.kubernetes.io/zone",
						"batch.kubernetes.io/job-completion-index": "1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{UID: types.UID("job-uid-1"), Kind: "Job", Controller: ptr.To(true)},
					},
				},
			},
			existingObjs: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "js-rjob-0-0-leader",
						Namespace: "default",
						Labels: map[string]string{
							jobset.JobSetNameKey:        "js",
							jobset.ReplicatedJobNameKey: "rjob",
							jobset.JobIndexKey:          "0",
						},
						Annotations: map[string]string{
							jobset.ExclusiveKey:                        "topology.kubernetes.io/zone",
							"batch.kubernetes.io/job-completion-index": "0",
						},
						OwnerReferences: []metav1.OwnerReference{
							{UID: types.UID("job-uid-1"), Kind: "Job", Controller: ptr.To(true)},
						},
					},
				},
			},
			wantErr: "leader pod not yet scheduled, not creating follower pod. this is an expected, transient error",
		},
		{
			name: "follower pod requires node selector after leader is scheduled",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "js-rjob-0-1-follower",
					Namespace: "default",
					Labels: map[string]string{
						jobset.JobSetNameKey:        "js",
						jobset.ReplicatedJobNameKey: "rjob",
						jobset.JobIndexKey:          "0",
					},
					Annotations: map[string]string{
						jobset.JobSetNameKey:                       "js",
						jobset.ExclusiveKey:                        "topology.kubernetes.io/zone",
						"batch.kubernetes.io/job-completion-index": "1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{UID: types.UID("job-uid-1"), Kind: "Job", Controller: ptr.To(true)},
					},
				},
			},
			existingObjs: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "js-rjob-0-0-leader",
						Namespace: "default",
						Labels: map[string]string{
							jobset.JobSetNameKey:        "js",
							jobset.ReplicatedJobNameKey: "rjob",
							jobset.JobIndexKey:          "0",
						},
						Annotations: map[string]string{
							jobset.ExclusiveKey:                        "topology.kubernetes.io/zone",
							"batch.kubernetes.io/job-completion-index": "0",
						},
						OwnerReferences: []metav1.OwnerReference{
							{UID: types.UID("job-uid-1"), Kind: "Job", Controller: ptr.To(true)},
						},
					},
					Spec: corev1.PodSpec{NodeName: "node-a"},
				},
			},
			wantErr: "follower pod node selector not set despite leader pod being scheduled",
		},
		{
			name: "follower pod requires topology node selector key after leader is scheduled",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "js-rjob-0-1-follower",
					Namespace: "default",
					Labels: map[string]string{
						jobset.JobSetNameKey:        "js",
						jobset.ReplicatedJobNameKey: "rjob",
						jobset.JobIndexKey:          "0",
					},
					Annotations: map[string]string{
						jobset.JobSetNameKey:                       "js",
						jobset.ExclusiveKey:                        "topology.kubernetes.io/zone",
						"batch.kubernetes.io/job-completion-index": "1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{UID: types.UID("job-uid-1"), Kind: "Job", Controller: ptr.To(true)},
					},
				},
				Spec: corev1.PodSpec{
					NodeSelector: map[string]string{
						"kubernetes.io/hostname": "node-a",
					},
				},
			},
			existingObjs: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "js-rjob-0-0-leader",
						Namespace: "default",
						Labels: map[string]string{
							jobset.JobSetNameKey:        "js",
							jobset.ReplicatedJobNameKey: "rjob",
							jobset.JobIndexKey:          "0",
						},
						Annotations: map[string]string{
							jobset.ExclusiveKey:                        "topology.kubernetes.io/zone",
							"batch.kubernetes.io/job-completion-index": "0",
						},
						OwnerReferences: []metav1.OwnerReference{
							{UID: types.UID("job-uid-1"), Kind: "Job", Controller: ptr.To(true)},
						},
					},
					Spec: corev1.PodSpec{NodeName: "node-a"},
				},
			},
			wantErr: "follower pod node selector for topology domain not found. missing selector: topology.kubernetes.io/zone",
		},
		{
			name: "follower pod allows create when leader is scheduled and topology node selector is set",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "js-rjob-0-1-follower",
					Namespace: "default",
					Labels: map[string]string{
						jobset.JobSetNameKey:        "js",
						jobset.ReplicatedJobNameKey: "rjob",
						jobset.JobIndexKey:          "0",
					},
					Annotations: map[string]string{
						jobset.JobSetNameKey:                       "js",
						jobset.ExclusiveKey:                        "topology.kubernetes.io/zone",
						"batch.kubernetes.io/job-completion-index": "1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{UID: types.UID("job-uid-1"), Kind: "Job", Controller: ptr.To(true)},
					},
				},
				Spec: corev1.PodSpec{
					NodeSelector: map[string]string{
						"topology.kubernetes.io/zone": "zone-a",
					},
				},
			},
			existingObjs: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "js-rjob-0-0-leader",
						Namespace: "default",
						Labels: map[string]string{
							jobset.JobSetNameKey:        "js",
							jobset.ReplicatedJobNameKey: "rjob",
							jobset.JobIndexKey:          "0",
						},
						Annotations: map[string]string{
							jobset.ExclusiveKey:                        "topology.kubernetes.io/zone",
							"batch.kubernetes.io/job-completion-index": "0",
						},
						OwnerReferences: []metav1.OwnerReference{
							{UID: types.UID("job-uid-1"), Kind: "Job", Controller: ptr.To(true)},
						},
					},
					Spec: corev1.PodSpec{NodeName: "node-a"},
				},
			},
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
				WithRuntimeObjects(tc.existingObjs...).
				WithIndex(&corev1.Pod{}, controllers.PodNameKey, controllers.IndexPodName).
				Build()

			p := &podWebhook{client: fakeClient}
			_, err := p.ValidateCreate(t.Context(), tc.pod)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error to contain %q, got %q", tc.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

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
			if err != nil && !tc.wantErr {
				t.Errorf("unexpected error: %v", err)
				return
			}
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

			if tc.wantResult == nil && result != nil {
				t.Errorf("Unexpected error: %v", result)
			} else if tc.wantResult != nil && result == nil {
				t.Error("Expected error, but got nil")
			} else if tc.wantResult != nil && result != nil && tc.wantResult.Error() != result.Error() {
				t.Errorf("Expected error: %v, got: %v", tc.wantResult, result)
			}
		})
	}
}

func TestLeaderPodForFollower(t *testing.T) {
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
