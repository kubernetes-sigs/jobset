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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// JobSetSpec defines the desired state of JobSet
type JobSetSpec struct {
	// Jobs is the group of jobs that will form the set.
	Jobs []JobTemplate `json:"jobs"`

	// If sequentialStartup if set, then spec.jobs will be created one at a time
	// according to their order in the list. A job is created only after
	// the pods of the previous one are Ready.
	SequentialStartup bool `json:"sequentialStartup,omitempty"`
}

// JobSetStatus defines the observed state of JobSet
type JobSetStatus struct {
	// A list of pointers to currently running jobs.
	// +optional
	Active []corev1.ObjectReference `json:"active,omitempty"`
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
type JobTemplate struct {
	// Name is the name of the entry and will be used as a suffix
	// for the Job name.
	Name string `json:"name"`
	// Template defines the template of the Job that will be created.
	Template *batchv1.JobTemplateSpec `json:"template,omitempty"`
	// Network defines the networking options for the job.
	Network *Network `json:"network"`
}
type Network struct {
	// HeadlessService determines if a headless service will be created for
	// the associated job. The service name is the same as the JobSet name,
	// which will also be set in the field spec.jobs[*].template.spec.subdomain.
	HeadlessService bool `json:"headlessService"`
}

func init() {
	SchemeBuilder.Register(&JobSet{}, &JobSetList{})
}
