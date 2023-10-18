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

// +k8s:openapi-gen=true
package v1alpha2

import (
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	JobSetNameKey         string = "jobset.sigs.k8s.io/jobset-name"
	ReplicatedJobReplicas string = "jobset.sigs.k8s.io/replicatedjob-replicas"
	ReplicatedJobNameKey  string = "jobset.sigs.k8s.io/replicatedjob-name"
	JobIndexKey           string = "jobset.sigs.k8s.io/job-index"
	JobKey                string = "jobset.sigs.k8s.io/job-key"
	JobNameKey            string = "job-name" // TODO(#26): Migrate to the fully qualified label name.
	ExclusiveKey          string = "alpha.jobset.sigs.k8s.io/exclusive-topology"
	// NodeSelectorStrategyKey is an annotation that acts as a flag, the value does not matter.
	// If set, the JobSet controller will automatically inject nodeSelectors for the JobSetNameKey label to
	// ensure exclusive job placement per topology, instead of injecting pod affinity/anti-affinites for this.
	// The user must add the JobSet name node label to the desired topologies separately.
	NodeSelectorStrategyKey string = "alpha.jobset.sigs.k8s.io/node-selector"
	NamespacedJobKey        string = "alpha.jobset.sigs.k8s.io/namespaced-job"
	NoScheduleTaintKey      string = "alpha.jobset.sigs.k8s.io/no-schedule"
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
	ReplicatedJobs []ReplicatedJob `json:"replicatedJobs,omitempty"`

	// Network defines the networking options for the jobset.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable"
	// +optional
	Network *Network `json:"network,omitempty"`

	// SuccessPolicy configures when to declare the JobSet as
	// succeeded.
	// The JobSet is always declared succeeded if all jobs in the set
	// finished with status complete.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable"
	SuccessPolicy *SuccessPolicy `json:"successPolicy,omitempty"`

	// FailurePolicy, if set, configures when to declare the JobSet as
	// failed.
	// The JobSet is always declared failed if any job in the set
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
	Restarts int32 `json:"restarts,omitempty"`

	// ReplicatedJobsStatus track the number of JobsReady for each replicatedJob.
	// +optional
	// +listType=map
	// +listMapKey=name
	ReplicatedJobsStatus []ReplicatedJobStatus `json:"replicatedJobsStatus,omitempty"`
}

// ReplicatedJobStatus defines the observed ReplicatedJobs Readiness.
type ReplicatedJobStatus struct {
	Name      string `json:"name"`
	Ready     int32  `json:"ready"`
	Succeeded int32  `json:"succeeded"`
	Failed    int32  `json:"failed"`
	Active    int32  `json:"active"`
}

// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Restarts",JSONPath=".status.restarts",type=string,description="Number of restarts"
// +kubebuilder:printcolumn:name="Completed",type="string",priority=0,JSONPath=".status.conditions[?(@.type==\"Completed\")].status"
// +kubebuilder:printcolumn:name="Age",JSONPath=".metadata.creationTimestamp",type=date,description="Time this JobSet was created"

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

	// Replicas is the number of jobs that will be created from this ReplicatedJob's template.
	// Jobs names will be in the format: <jobSet.name>-<spec.replicatedJob.name>-<job-index>
	// +kubebuilder:default=1
	Replicas int32 `json:"replicas,omitempty"`
}

type Network struct {
	// EnableDNSHostnames allows pods to be reached via their hostnames.
	// Pods will be reachable using the fully qualified pod hostname:
	// <jobSet.name>-<spec.replicatedJob.name>-<job-index>-<pod-index>.<subdomain>
	// +optional
	EnableDNSHostnames *bool `json:"enableDNSHostnames,omitempty"`

	// Subdomain is an explicit choice for a network subdomain name
	// When set, any replicated job in the set is added to this network.
	// Defaults to <jobSet.name> if not set.
	// +optional
	Subdomain string `json:"subdomain,omitempty"`
}

// Operator defines the target of a SuccessPolicy or FailurePolicy.
type Operator string

const (
	// OperatorAll applies to all jobs matching the jobSelector.
	OperatorAll Operator = "All"

	// OperatorAny applies to any single job matching the jobSelector.
	OperatorAny Operator = "Any"
)

type FailurePolicy struct {
	// MaxRestarts defines the limit on the number of JobSet restarts.
	// A restart is achieved by recreating all active child jobs.
	MaxRestarts int32 `json:"maxRestarts,omitempty"`
}

type SuccessPolicy struct {
	// Operator determines either All or Any of the selected jobs should succeed to consider the JobSet successful
	// +kubebuilder:validation:Enum=All;Any
	Operator Operator `json:"operator"`

	// TargetReplicatedJobs are the names of the replicated jobs the operator will apply to.
	// A null or empty list will apply to all replicatedJobs.
	// +optional
	// +listType=atomic
	TargetReplicatedJobs []string `json:"targetReplicatedJobs,omitempty"`
}

func init() {
	SchemeBuilder.Register(&JobSet{}, &JobSetList{})
}
