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

package v1alpha1

import (
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	JobSetNameKey         string = "jobset.sigs.k8s.io/jobset-name"
	ReplicatedJobReplicas string = "jobset.sigs.k8s.io/replicatedjob-replicas"
	ReplicatedJobNameKey  string = "jobset.sigs.k8s.io/replicatedjob-name"
	JobIndexKey           string = "jobset.sigs.k8s.io/job-index"
	JobNameKey            string = "job-name" // TODO(#26): Migrate to the fully qualified label name.
	ExclusiveKey          string = "alpha.jobset.sigs.k8s.io/exclusive"
)

type JobSetConditionType string

// These are built-in conditions of a JobSet.
const (
	// JobSetComplete means the job has completed its execution.
	JobSetCompleted JobSetConditionType = "Completed"
	// JobSetFailed means the job has failed its execution.
	JobSetFailed JobSetConditionType = "Failed"
	// JobSetSuspended means the job is suspended
	JobSetSuspended JobSetConditionType = "Suspended"
)

// JobSetSpec defines the desired state of JobSet
type JobSetSpec struct {
	// ReplicatedJobs is the group of jobs that will form the set.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable"
	ReplicatedJobs []ReplicatedJob `json:"replicatedJobs,omitempty"`

	// FailurePolicy, if set, configures when to declare the JobSet as
	// failed.
	// The JobSet is always declared failed if all jobs in the set
	// finished with status failed.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable"
	FailurePolicy *FailurePolicy `json:"failurePolicy,omitempty"`

	// Suspend suspends all running child Jobs when set to true.
	Suspend *bool `json:"suspend,omitempty"`
}

// JobSetStatus defines the observed state of JobSet
type JobSetStatus struct {
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Restarts tracks the number of times the JobSet has restarted (i.e. recreated in case of RecreateAll policy).
	Restarts int `json:"restarts,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// JobSet is the Schema for the jobsets API
type JobSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              JobSetSpec   `json:"spec,omitempty"`
	Status            JobSetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// JobSetList contains a list of JobSet
type JobSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []JobSet `json:"items"`
}

type ReplicatedJob struct {
	// Name is the name of the entry and will be used as a suffix
	// for the Job name.
	Name string `json:"name"`
	// Template defines the template of the Job that will be created.
	Template batchv1.JobTemplateSpec `json:"template"`
	// Network defines the networking options for the job.
	// +optional
	Network *Network `json:"network,omitempty"`
	// Replicas is the number of jobs that will be created from this ReplicatedJob's template.
	// Jobs names will be in the format: <jobSet.name>-<spec.replicatedJob.name>-<job-index>
	// +kubebuilder:default=1
	Replicas int `json:"replicas,omitempty"`
}

type Network struct {
	// EnableDNSHostnames allows pods to be reached via their hostnames.
	// Pods will be reachable using the fully qualified pod hostname, which is in the format:
	// <jobSet.name>-<spec.replicatedJob.name>-<job-index>-<pod-index>.<jobSet.name>-<spec.replicatedJob.name>
	// +optional
	EnableDNSHostnames *bool `json:"enableDNSHostnames,omitempty"`
}

type FailurePolicy struct {
	// MaxRestarts defines the limit on the number of JobSet restarts.
	// A restart is achieved by recreating all active child jobs.
	MaxRestarts int `json:"maxRestarts,omitempty"`
}

func init() {
	SchemeBuilder.Register(&JobSet{}, &JobSetList{})
}
