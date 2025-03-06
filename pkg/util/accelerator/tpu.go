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
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	// ctrl "sigs.k8s.io/controller-runtime"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

const (
	TpuResourceName                            = corev1.ResourceName("google.com/tpu")
	megascaleCoordinatorAddressEnvVariableName = "MEGASCALE_COORDINATOR_ADDRESS"
	megascaleNumSlicesEnvVariableName          = "MEGASCALE_NUM_SLICES"
	megascaleSliceIdEnvVariableName            = "MEGASCALE_SLICE_ID"
	coordinatorAddressPattern                  = "%[1]s-%[2]s-0-0.%[3]s"
	annotationPattern                          = "metadata.annotations['%s']"
)

func IsTpuMultiSliceJob(job batchv1.Job) bool {
	if job.Spec.CompletionMode == nil || *job.Spec.CompletionMode != batchv1.IndexedCompletion {
		return false
	}
	if job.Spec.Template.Spec.Subdomain == "" {
		return false
	}
	if !podRequestsTpus(job.Spec.Template) {
		return false
	}
	return true
}

func podRequestsTpus(podTemplate corev1.PodTemplateSpec) bool {
	for _, container := range podTemplate.Spec.Containers {
		if containerRequestTpus(container) {
			return true
		}
		for _, container := range podTemplate.Spec.InitContainers {
			if containerRequestTpus(container) {
				return true
			}
		}
	}
	return false
}

func containerRequestTpus(container corev1.Container) bool {
	if limits := container.Resources.Limits; limits != nil {
		if tpuResourceQuantity := limits[TpuResourceName]; !tpuResourceQuantity.IsZero() {
			return true
		}
	}
	if requests := container.Resources.Requests; requests != nil {
		if tpuResourceQuantity := requests[TpuResourceName]; !tpuResourceQuantity.IsZero() {
			return true
		}
	}
	return false
}

func AddTpuMultiSliceEnvVariables(job *batchv1.Job) {
	addTpuMultiSliceEnvVariablesToPod(&job.Spec.Template, job.Spec.Template.Spec.Subdomain)
}

func addTpuMultiSliceEnvVariablesToPod(podTemplate *corev1.PodTemplateSpec, subdomain string) {
	for i := range podTemplate.Spec.Containers {
		addTpuMultiSliceEnvVariablesToContainer(&podTemplate.Spec.Containers[i], *podTemplate, subdomain)
	}
	for i := range podTemplate.Spec.InitContainers {
		addTpuMultiSliceEnvVariablesToContainer(&podTemplate.Spec.InitContainers[i], *podTemplate, subdomain)
	}
}

func addTpuMultiSliceEnvVariablesToContainer(container *corev1.Container, podTemplate corev1.PodTemplateSpec, subdomain string) {
	if containerSetsTpuMultiSliceEnvVariables(*container) {
		return
	}
	if !containerRequestTpus(*container) {
		return
	}
	addMegascaleCoordinatorAddressEnvVariable(container, podTemplate, subdomain)
	addMegascaleNumSlicesEnvVariable(container, podTemplate)
	addMegascaleSliceIdEnvVariable(container, podTemplate)
}

func containerSetsTpuMultiSliceEnvVariables(container corev1.Container) bool {
	for _, env := range container.Env {
		if env.Name == megascaleNumSlicesEnvVariableName || env.Name == megascaleSliceIdEnvVariableName || env.Name == megascaleCoordinatorAddressEnvVariableName {
			return true
		}
	}
	return false
}

func addMegascaleCoordinatorAddressEnvVariable(container *corev1.Container, podTemplate corev1.PodTemplateSpec, subdomain string) {
	if podTemplate.Annotations[jobset.CoordinatorKey] != "" {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name: megascaleCoordinatorAddressEnvVariableName,
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: fmt.Sprintf(annotationPattern, jobset.CoordinatorKey),
					},
				},
			},
		)
		return
	}
	jobSetName := podTemplate.Annotations[jobset.JobSetNameKey]
	replicatedJobName := podTemplate.Annotations[jobset.ReplicatedJobNameKey]
	container.Env = append(container.Env,
		corev1.EnvVar{
			Name:  megascaleCoordinatorAddressEnvVariableName,
			Value: fmt.Sprintf(coordinatorAddressPattern, jobSetName, replicatedJobName, subdomain),
		},
	)
}

func addMegascaleNumSlicesEnvVariable(container *corev1.Container, podTemplate corev1.PodTemplateSpec) {
	if podTemplate.Annotations[jobset.GroupReplicasKey] != "" {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name: megascaleNumSlicesEnvVariableName,
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: fmt.Sprintf(annotationPattern, jobset.GroupReplicasKey),
					},
				},
			},
		)
		return
	}
	if podTemplate.Annotations[jobset.GlobalReplicasKey] != "" {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name: megascaleNumSlicesEnvVariableName,
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: fmt.Sprintf(annotationPattern, jobset.GlobalReplicasKey),
					},
				},
			},
		)
		return
	}
	if podTemplate.Annotations[jobset.ReplicatedJobReplicas] != "" {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name: megascaleNumSlicesEnvVariableName,
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: fmt.Sprintf(annotationPattern, jobset.ReplicatedJobReplicas),
					},
				},
			},
		)
		return
	}
}

func addMegascaleSliceIdEnvVariable(container *corev1.Container, podTemplate corev1.PodTemplateSpec) {
	if podTemplate.Annotations[jobset.JobGroupIndexKey] != "" {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name: megascaleSliceIdEnvVariableName,
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: fmt.Sprintf(annotationPattern, jobset.JobGroupIndexKey),
					},
				},
			},
		)
		return
	}
	if podTemplate.Annotations[jobset.JobGlobalIndexKey] != "" {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name: megascaleSliceIdEnvVariableName,
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: fmt.Sprintf(annotationPattern, jobset.JobGlobalIndexKey),
					},
				},
			},
		)
		return
	}
	if podTemplate.Annotations[jobset.JobIndexKey] != "" {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name: megascaleSliceIdEnvVariableName,
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: fmt.Sprintf(annotationPattern, jobset.JobIndexKey),
					},
				},
			},
		)
		return
	}
}
