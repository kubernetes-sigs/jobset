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
