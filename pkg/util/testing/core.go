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

package testing

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

// IndexedJob constructs a simple indexed job template spec to use for testing.
func IndexedJob(name, ns string) batchv1.JobTemplateSpec {
	return batchv1.JobTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: batchv1.JobSpec{
			CompletionMode: completionModePtr(batchv1.IndexedCompletion),
			Parallelism:    pointer.Int32(1),
			Completions:    pointer.Int32(1),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: "Never",
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "busybox:latest",
						},
					},
				},
			},
		},
	}
}

func completionModePtr(m batchv1.CompletionMode) *batchv1.CompletionMode {
	return &m
}
