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
	JobIndexLabel string = "jobset.sigs.k8s.io/job-index"
	RestartsLabel string = "jobset.sigs.k8s.io/restart-attempt"
	JobNameKey    string = "job-name" // TODO: Migrate to the fully qualified label name.
)

type JobSetConditionType string

// These are built-in conditions of a JobSet.
const (
	// JobSetComplete means the job has completed its execution.
	JobSetCompleted JobSetConditionType = "Completed"
	// JobSetFailed means the job has failed its execution.
	JobSetFailed JobSetConditionType = "Failed"
)

// JobSetSpec defines the desired state of JobSet
type JobSetSpec struct {
	// Jobs is the group of jobs that will form the set.
	// +listType=map
	// +listMapKey=name
	Jobs []ReplicatedJob `json:"jobs"`

	// FailurePolicy, if set, configures when to declare the JobSet as
	// failed.
	// The JobSet is always declared failed if all jobs in the set
	// finished with status failed.
	FailurePolicy *FailurePolicy `json:"failurePolicy,omitempty"`
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

// +kubebuilder:validation:XValidation:rule="!has(self.network) || !has(self.network.enableDNSHostnames) || !self.network.enableDNSHostnames || (has(self.template.spec.completionMode) && self.template.spec.completionMode == \"Indexed\")", message="EnableDNSHostnames requires job to be in indexed completion mode"
type ReplicatedJob struct {
	// Name is the name of the entry and will be used as a suffix
	// for the Job name.
	Name string `json:"name"`
	// Template defines the template of the Job that will be created.
	Template batchv1.JobTemplateSpec `json:"template,omitempty"`
	// Network defines the networking options for the job.
	Network *Network `json:"network"`
	// Replicas is the number of jobs that will be created from this ReplicatedJob's template.
	// Jobs names will be in the format: <jobSet.name>-<spec.replicatedJob.name>-<job-index>
	// +kubebuilder:default=1
	Replicas int `json:"replicas,omitempty"`
	// Exclusive defines that the jobs are 1:1 with the specified topology. This is enforced
	// against all jobs, whether or not they are created by JobSet.
	Exclusive *Exclusive `json:"exclusive,omitempty"`
}

type Network struct {
	// EnableDNSHostnames allows pods to be reached via their hostnames.
	// Pods will be reachable using the fully qualified pod hostname, which is in the format:
	// <jobSet.name>-<spec.replicatedJob.name>-<job-index>-<pod-index>.<jobSet.name>-<spec.replicatedJob.name>-<job-index>
	// +optional
	EnableDNSHostnames *bool `json:"enableDNSHostnames,omitempty"`
}

type Exclusive struct {
	// TopologyKey refers to the topology on which exclusive placement will be
	// enforced (e.g., node, rack, zone etc.)
	TopologyKey string `json:"topologyKey,omitempty"`
	// A label query over the set of namespaces that exclusiveness applies to. Defaults to the job's namespace.
	// An empty selector ({}) matches all namespaces.
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
}

type TargetOperator string

// TerminationPolicyTargetAny applies to any job in the JobSet.
const TerminationPolicyTargetAny TargetOperator = "Any"

type RestartPolicy string

const (
	// RestartPolicyNone means no jobs will be restarted.
	RestartPolicyNone RestartPolicy = "None"
	// RestartPolicyRecreate means jobs will be recreated when the termination policy is triggered.
	RestartPolicyRecreateAll RestartPolicy = "RecreateAll"
)

type FailurePolicy struct {
	// +kubebuilder:validation:XValidation:rule="self == \"Any\""
	// +kubebuilder:default="Any"
	Operator TargetOperator `json:"operator"`
	// +kubebuilder:default="None"
	RestartPolicy RestartPolicy `json:"restartPolicy"`
	MaxRestarts   int           `json:"maxRestarts,omitempty"`
}

func init() {
	SchemeBuilder.Register(&JobSet{}, &JobSetList{})
}
