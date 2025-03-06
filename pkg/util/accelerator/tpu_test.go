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

package accelerator

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestIsTpuMultiSliceJob(t *testing.T) {
	completionModeIndexed := batchv1.IndexedCompletion
	completionModeNonIndexed := batchv1.NonIndexedCompletion

	tests := []struct {
		name string
		job  batchv1.Job
		want bool
	}{
		{
			name: "empty Job",
			job:  batchv1.Job{},
			want: false,
		},
		{
			name: "empty Pod template spec",
			job: batchv1.Job{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{},
				},
			},
			want: false,
		},
		{
			name: "valid TPU multi slice job",
			job: batchv1.Job{
				Spec: batchv1.JobSpec{
					CompletionMode: &completionModeIndexed,
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Subdomain: "test-subdomain",
							Containers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											"google.com/tpu": resource.MustParse("4"),
										},
									},
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "not indexed completion",
			job: batchv1.Job{
				Spec: batchv1.JobSpec{
					CompletionMode: &completionModeNonIndexed,
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Subdomain: "test-subdomain",
							Containers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											"google.com/tpu": resource.MustParse("4"),
										},
									},
								},
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "no subdomain",
			job: batchv1.Job{
				Spec: batchv1.JobSpec{
					CompletionMode: &completionModeIndexed,
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											"google.com/tpu": resource.MustParse("4"),
										},
									},
								},
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "no TPU request",
			job: batchv1.Job{
				Spec: batchv1.JobSpec{
					CompletionMode: &completionModeIndexed,
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Subdomain: "test-subdomain",
							Containers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											corev1.ResourceCPU: resource.MustParse("4"),
										},
									},
								},
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "valid TPU multi slice job with TPU init container",
			job: batchv1.Job{
				Spec: batchv1.JobSpec{
					CompletionMode: &completionModeIndexed,
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Subdomain: "test-subdomain",
							InitContainers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											"google.com/tpu": resource.MustParse("4"),
										},
									},
								},
							},
							Containers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											corev1.ResourceCPU: resource.MustParse("4"),
										},
									},
								},
							},
						},
					},
				},
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsTpuMultiSliceJob(tc.job)
			if got != tc.want {
				t.Errorf("expected %t, got %t", tc.want, got)
			}
		})
	}
}

func TestAddTpuMultiSliceEnvVariables(t *testing.T) {
	tests := []struct {
		name                          string
		job                           batchv1.Job
		wantInitContainerEnvVariables [][]corev1.EnvVar
		wantContainerEnvVariables     [][]corev1.EnvVar
	}{
		{
			name: "empty",
			job: batchv1.Job{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{},
						},
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{
								{
									Env: []corev1.EnvVar{},
								},
							},
							Containers: []corev1.Container{
								{
									Env: []corev1.EnvVar{},
								},
							},
						},
					},
				},
			},
			wantInitContainerEnvVariables: [][]corev1.EnvVar{
				{},
			},
			wantContainerEnvVariables: [][]corev1.EnvVar{
				{},
			},
		},
		{
			name: "valid TPU multi slice job",
			job: batchv1.Job{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"jobset.sigs.k8s.io/coordinator":     "test",
								"jobset.sigs.k8s.io/group-replicas":  "1",
								"jobset.sigs.k8s.io/job-group-index": "0",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											"google.com/tpu": resource.MustParse("4"),
										},
									},
								},
							},
						},
					},
				},
			},
			wantInitContainerEnvVariables: [][]corev1.EnvVar{
				{},
			},
			wantContainerEnvVariables: [][]corev1.EnvVar{
				{
					{
						Name: "MEGASCALE_COORDINATOR_ADDRESS",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.annotations['jobset.sigs.k8s.io/coordinator']",
							},
						},
					},
					{
						Name: "MEGASCALE_NUM_SLICES",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.annotations['jobset.sigs.k8s.io/group-replicas']",
							},
						},
					},
					{
						Name: "MEGASCALE_SLICE_ID",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.annotations['jobset.sigs.k8s.io/job-group-index']",
							},
						},
					},
				},
			},
		},
		{
			name: "valid TPU multi slice job with 1 CPU container and 1 TPU container",
			job: batchv1.Job{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"jobset.sigs.k8s.io/coordinator":     "test",
								"jobset.sigs.k8s.io/group-replicas":  "1",
								"jobset.sigs.k8s.io/job-group-index": "0",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											corev1.ResourceCPU: resource.MustParse("4"),
										},
									},
								},
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											"google.com/tpu": resource.MustParse("4"),
										},
									},
								},
							},
						},
					},
				},
			},
			wantInitContainerEnvVariables: [][]corev1.EnvVar{
				{},
			},
			wantContainerEnvVariables: [][]corev1.EnvVar{
				nil,
				{
					{
						Name: "MEGASCALE_COORDINATOR_ADDRESS",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.annotations['jobset.sigs.k8s.io/coordinator']",
							},
						},
					},
					{
						Name: "MEGASCALE_NUM_SLICES",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.annotations['jobset.sigs.k8s.io/group-replicas']",
							},
						},
					},
					{
						Name: "MEGASCALE_SLICE_ID",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.annotations['jobset.sigs.k8s.io/job-group-index']",
							},
						},
					},
				},
			},
		},
		{
			name: "valid TPU multi slice job with TPU init container",
			job: batchv1.Job{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"jobset.sigs.k8s.io/coordinator":     "test",
								"jobset.sigs.k8s.io/group-replicas":  "1",
								"jobset.sigs.k8s.io/job-group-index": "0",
							},
						},
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											"google.com/tpu": resource.MustParse("4"),
										},
									},
								},
							},
							Containers: []corev1.Container{
								{
									Env: []corev1.EnvVar{},
								},
							},
						},
					},
				},
			},
			wantInitContainerEnvVariables: [][]corev1.EnvVar{
				{
					{
						Name: "MEGASCALE_COORDINATOR_ADDRESS",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.annotations['jobset.sigs.k8s.io/coordinator']",
							},
						},
					},
					{
						Name: "MEGASCALE_NUM_SLICES",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.annotations['jobset.sigs.k8s.io/group-replicas']",
							},
						},
					},
					{
						Name: "MEGASCALE_SLICE_ID",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.annotations['jobset.sigs.k8s.io/job-group-index']",
							},
						},
					},
				},
			},
			wantContainerEnvVariables: [][]corev1.EnvVar{
				{},
			},
		},
		{
			name: "valid TPU multi slice job with prepopulated env variables",
			job: batchv1.Job{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"jobset.sigs.k8s.io/coordinator":     "test",
								"jobset.sigs.k8s.io/group-replicas":  "1",
								"jobset.sigs.k8s.io/job-group-index": "0",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											"google.com/tpu": resource.MustParse("4"),
										},
									},
									Env: []corev1.EnvVar{
										{
											Name:  "MEGASCALE_COORDINATOR_ADDRESS",
											Value: "test",
										},
										{
											Name:  "MEGASCALE_NUM_SLICES",
											Value: "1",
										},
										{
											Name:  "MEGASCALE_SLICE_ID",
											Value: "0",
										},
									},
								},
							},
						},
					},
				},
			},
			wantInitContainerEnvVariables: [][]corev1.EnvVar{
				{},
			},
			wantContainerEnvVariables: [][]corev1.EnvVar{
				{
					{
						Name:  "MEGASCALE_COORDINATOR_ADDRESS",
						Value: "test",
					},
					{
						Name:  "MEGASCALE_NUM_SLICES",
						Value: "1",
					},
					{
						Name:  "MEGASCALE_SLICE_ID",
						Value: "0",
					},
				},
			},
		},
		{
			name: "valid TPU multi slice job without group annotations",
			job: batchv1.Job{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"jobset.sigs.k8s.io/jobset-name":        "jobsetName",
								"jobset.sigs.k8s.io/replicatedjob-name": "replicatedJobName",
								"jobset.sigs.k8s.io/global-replicas":    "1",
								"jobset.sigs.k8s.io/job-global-index":   "0",
							},
						},
						Spec: corev1.PodSpec{
							Subdomain: "subdomainName",
							Containers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											"google.com/tpu": resource.MustParse("4"),
										},
									},
								},
							},
						},
					},
				},
			},
			wantInitContainerEnvVariables: [][]corev1.EnvVar{
				{},
			},
			wantContainerEnvVariables: [][]corev1.EnvVar{
				{
					{
						Name:  "MEGASCALE_COORDINATOR_ADDRESS",
						Value: "jobsetName-replicatedJobName-0-0.subdomainName",
					},
					{
						Name: "MEGASCALE_NUM_SLICES",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.annotations['jobset.sigs.k8s.io/global-replicas']",
							},
						},
					},
					{
						Name: "MEGASCALE_SLICE_ID",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.annotations['jobset.sigs.k8s.io/job-global-index']",
							},
						},
					},
				},
			},
		},
		{
			name: "valid TPU multi slice job without group and global annotations",
			job: batchv1.Job{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"jobset.sigs.k8s.io/jobset-name":            "jobsetName",
								"jobset.sigs.k8s.io/replicatedjob-name":     "replicatedJobName",
								"jobset.sigs.k8s.io/replicatedjob-replicas": "1",
								"jobset.sigs.k8s.io/job-index":              "0",
							},
						},
						Spec: corev1.PodSpec{
							Subdomain: "subdomainName",
							Containers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											"google.com/tpu": resource.MustParse("4"),
										},
									},
								},
							},
						},
					},
				},
			},
			wantInitContainerEnvVariables: [][]corev1.EnvVar{
				{},
			},
			wantContainerEnvVariables: [][]corev1.EnvVar{
				{
					{
						Name:  "MEGASCALE_COORDINATOR_ADDRESS",
						Value: "jobsetName-replicatedJobName-0-0.subdomainName",
					},
					{
						Name: "MEGASCALE_NUM_SLICES",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.annotations['jobset.sigs.k8s.io/replicatedjob-replicas']",
							},
						},
					},
					{
						Name: "MEGASCALE_SLICE_ID",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.annotations['jobset.sigs.k8s.io/job-index']",
							},
						},
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			AddTpuMultiSliceEnvVariables(&tc.job)
			for i := range tc.job.Spec.Template.Spec.InitContainers {
				got := tc.job.Spec.Template.Spec.InitContainers[i].Env
				want := tc.wantInitContainerEnvVariables[i]
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("mismatch for init container %d (-want +got):\n%s", i, diff)
				}
			}
			for i := range tc.job.Spec.Template.Spec.Containers {
				got := tc.job.Spec.Template.Spec.Containers[i].Env
				want := tc.wantContainerEnvVariables[i]
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("mismatch for container %d (-want +got):\n%s", i, diff)
				}
			}
		})
	}
}
