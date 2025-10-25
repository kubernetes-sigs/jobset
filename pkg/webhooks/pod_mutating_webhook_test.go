/*
Copyright 2023 The Kubernetes Authors.

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
	"testing"

	"github.com/google/go-cmp/cmp"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
	"sigs.k8s.io/jobset/pkg/controllers"
)

func TestDefault(t *testing.T) {
	testcases := []struct {
		name         string
		pod          *corev1.Pod
		existingObjs []runtime.Object
		wantPod      *corev1.Pod
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
						jobset.JobSetNameKey:           "js",
						jobset.NodeSelectorStrategyKey: "",
					},
				},
			},
			wantPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "p",
					Annotations: map[string]string{
						jobset.JobSetNameKey:           "js",
						jobset.NodeSelectorStrategyKey: "",
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
						jobset.JobSetNameKey: "js",
					},
				},
			},
			wantPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "p",
					Annotations: map[string]string{
						jobset.JobSetNameKey: "js",
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
						jobset.JobSetNameKey: "js",
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
						jobset.JobSetNameKey: "js",
					},
					Labels: map[string]string{
						constants.PriorityKey: "100",
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
						jobset.JobKey: "js-rjob-0",
					},
					Annotations: map[string]string{
						jobset.JobSetNameKey:                 "js",
						jobset.ExclusiveKey:                  "topology.kubernetes.io/zone",
						batchv1.JobCompletionIndexAnnotation: "0",
						jobset.ReplicatedJobNameKey:          "rjob",
						jobset.JobIndexKey:                   "0",
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
						jobset.JobKey:         "js-rjob-0",
						constants.PriorityKey: "100",
					},
					Annotations: map[string]string{
						jobset.JobSetNameKey:                 "js",
						jobset.ExclusiveKey:                  "topology.kubernetes.io/zone",
						batchv1.JobCompletionIndexAnnotation: "0",
						jobset.ReplicatedJobNameKey:          "rjob",
						jobset.JobIndexKey:                   "0",
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
											Key:      jobset.JobKey,
											Operator: metav1.LabelSelectorOpIn,
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
											Key:      jobset.JobKey,
											Operator: metav1.LabelSelectorOpExists,
										},
										{
											Key:      jobset.JobKey,
											Operator: metav1.LabelSelectorOpNotIn,
											Values:   []string{"js-rjob-0"},
										},
										{
											Key:      constants.PriorityKey,
											Operator: metav1.LabelSelectorOpIn,
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
						jobset.JobSetNameKey:        "js",
						jobset.ReplicatedJobNameKey: "rjob",
						jobset.JobIndexKey:          "0",
					},
					Annotations: map[string]string{
						jobset.JobSetNameKey:                 "js",
						jobset.ExclusiveKey:                  "topology.kubernetes.io/zone",
						batchv1.JobCompletionIndexAnnotation: "1",
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
							jobset.JobSetNameKey:        "js",
							jobset.ReplicatedJobNameKey: "rjob",
							jobset.JobIndexKey:          "0",
						},
						Annotations: map[string]string{
							jobset.ExclusiveKey:                  "topology.kubernetes.io/zone",
							batchv1.JobCompletionIndexAnnotation: "0",
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
						jobset.JobSetNameKey:        "js",
						jobset.ReplicatedJobNameKey: "rjob",
						jobset.JobIndexKey:          "0",
						constants.PriorityKey:       "100",
					},
					Annotations: map[string]string{
						jobset.JobSetNameKey:                 "js",
						jobset.ExclusiveKey:                  "topology.kubernetes.io/zone",
						batchv1.JobCompletionIndexAnnotation: "1",
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
						jobset.JobSetNameKey:        "js",
						jobset.ReplicatedJobNameKey: "rjob",
						jobset.JobIndexKey:          "0",
					},
					Annotations: map[string]string{
						jobset.JobSetNameKey:                 "js",
						jobset.ExclusiveKey:                  "topology.kubernetes.io/zone",
						batchv1.JobCompletionIndexAnnotation: "1",
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
							jobset.JobSetNameKey:        "js",
							jobset.ReplicatedJobNameKey: "rjob",
							jobset.JobIndexKey:          "0",
						},
						Annotations: map[string]string{
							jobset.ExclusiveKey:                  "topology.kubernetes.io/zone",
							batchv1.JobCompletionIndexAnnotation: "0",
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
						jobset.JobSetNameKey:        "js",
						jobset.ReplicatedJobNameKey: "rjob",
						jobset.JobIndexKey:          "0",
						constants.PriorityKey:       "100",
					},
					Annotations: map[string]string{
						jobset.JobSetNameKey:                 "js",
						jobset.ExclusiveKey:                  "topology.kubernetes.io/zone",
						batchv1.JobCompletionIndexAnnotation: "1",
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
				WithIndex(&corev1.Pod{}, controllers.PodNameKey, controllers.IndexPodName).
				Build()

			p := &podWebhook{client: fakeClient}
			podCopy := tc.pod.DeepCopy()
			_ = p.Default(context.Background(), podCopy)

			if diff := cmp.Diff(tc.wantPod, podCopy); diff != "" {
				t.Errorf("pod mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
