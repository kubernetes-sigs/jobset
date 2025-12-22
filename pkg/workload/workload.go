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

package workload

import (
	schedulingv1alpha1 "k8s.io/api/scheduling/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// ConstructWorkloadForJobSetAsGang creates a single Workload with one PodGroup containing all pods from all replicated jobs.
// This ensures that all pods across the entire JobSet are scheduled together as a gang.
func ConstructWorkloadForJobSetAsGang(js *jobset.JobSet) *schedulingv1alpha1.Workload {
	// Calculate total pod count across all replicated jobs
	totalPodCount := int32(0)

	for _, rjob := range js.Spec.ReplicatedJobs {
		// Get parallelism (defaults to 1 if not set)
		parallelism := int32(1)
		if rjob.Template.Spec.Parallelism != nil {
			parallelism = *rjob.Template.Spec.Parallelism
		}

		// Calculate pods per job
		podsPerJob := parallelism
		if rjob.Template.Spec.Completions != nil && *rjob.Template.Spec.Completions < parallelism {
			podsPerJob = *rjob.Template.Spec.Completions
		}

		// Total pods for this replicated job
		podCountForRJob := podsPerJob * rjob.Replicas
		totalPodCount += podCountForRJob
	}

	// Create a single PodGroup with all pods from the entire JobSet
	podGroup := schedulingv1alpha1.PodGroup{
		Name: js.Name, // Use JobSet name as the single PodGroup name
		Policy: schedulingv1alpha1.PodGroupPolicy{
			Gang: &schedulingv1alpha1.GangSchedulingPolicy{
				MinCount: totalPodCount,
			},
		},
	}

	workload := &schedulingv1alpha1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GenWorkloadName(js),
			Namespace: js.Namespace,
			Labels: map[string]string{
				jobset.JobSetNameKey: js.Name,
				jobset.JobSetUIDKey:  string(js.UID),
			},
		},
		Spec: schedulingv1alpha1.WorkloadSpec{
			PodGroups: []schedulingv1alpha1.PodGroup{podGroup},
		},
	}

	return workload
}

// ConstructWorkloadForJobSetGangPerReplicatedJob creates a Workload for each replicated job.
// This allows scheduling to happen in parallel.
func ConstructWorkloadForJobSetGangPerReplicatedJob(js *jobset.JobSet) *schedulingv1alpha1.Workload {
	// Calculate total pod count across all replicated jobs
	totalPodCount := int32(0)
	var podGroups []schedulingv1alpha1.PodGroup

	for _, rjob := range js.Spec.ReplicatedJobs {
		// Get parallelism (defaults to 1 if not set)
		parallelism := int32(1)
		if rjob.Template.Spec.Parallelism != nil {
			parallelism = *rjob.Template.Spec.Parallelism
		}

		// Calculate pods per job
		podsPerJob := parallelism
		if rjob.Template.Spec.Completions != nil && *rjob.Template.Spec.Completions < parallelism {
			podsPerJob = *rjob.Template.Spec.Completions
		}

		// Total pods for this replicated job
		podCountForRJob := podsPerJob * rjob.Replicas
		totalPodCount += podCountForRJob

		// Create a PodGroup for this replicated job with gang scheduling policy
		podGroup := schedulingv1alpha1.PodGroup{
			Name: rjob.Name,
			Policy: schedulingv1alpha1.PodGroupPolicy{
				Gang: &schedulingv1alpha1.GangSchedulingPolicy{
					MinCount: podCountForRJob,
				},
			},
		}
		podGroups = append(podGroups, podGroup)
	}

	workload := &schedulingv1alpha1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GenWorkloadName(js),
			Namespace: js.Namespace,
			Labels: map[string]string{
				jobset.JobSetNameKey: js.Name,
				jobset.JobSetUIDKey:  string(js.UID),
			},
		},
		Spec: schedulingv1alpha1.WorkloadSpec{
			PodGroups: podGroups,
		},
	}

	return workload
}

// ConstructWorkloadFromTemplate creates a Workload from the user-provided template.
func ConstructWorkloadFromTemplate(js *jobset.JobSet) *schedulingv1alpha1.Workload {
	workloadTemplate := js.Spec.GangPolicy.Workload.DeepCopy()

	workload := &schedulingv1alpha1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:        GenWorkloadName(js),
			Namespace:   js.Namespace,
			Labels:      workloadTemplate.Labels,
			Annotations: workloadTemplate.Annotations,
		},
		Spec: workloadTemplate.Spec,
	}

	// Ensure JobSet labels are present
	if workload.Labels == nil {
		workload.Labels = make(map[string]string)
	}
	workload.Labels[jobset.JobSetNameKey] = js.Name
	workload.Labels[jobset.JobSetUIDKey] = string(js.UID)

	return workload
}

// GenWorkloadName generates the name for the Workload resource.
func GenWorkloadName(js *jobset.JobSet) string {
	return js.Name
}

// GetPodGroupName determines the pod group name for a ReplicatedJob based on the gang policy option.
// For JobSetAsGang, all pods belong to a single PodGroup named after the JobSet.
// For JobSetGangPerReplicatedJob, each ReplicatedJob has its own PodGroup.
// For JobSetWorkloadTemplate, the user-specified PodGroup names are used (matching rjob.Name).
func GetPodGroupName(js *jobset.JobSet, rjobName string) string {
	if js.Spec.GangPolicy == nil || js.Spec.GangPolicy.Policy == nil {
		return ""
	}

	switch *js.Spec.GangPolicy.Policy {
	case jobset.JobSetAsGang:
		// All pods in the entire JobSet belong to a single PodGroup
		return js.Name
	case jobset.JobSetGangPerReplicatedJob:
		// Each ReplicatedJob has its own PodGroup
		return rjobName
	case jobset.JobSetWorkloadTemplate:
		// For templates, the user defines PodGroups; assume they match ReplicatedJob names
		return rjobName
	default:
		return rjobName
	}
}
