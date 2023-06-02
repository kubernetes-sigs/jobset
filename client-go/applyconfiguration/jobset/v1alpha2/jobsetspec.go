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
// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1alpha2

// JobSetSpecApplyConfiguration represents an declarative configuration of the JobSetSpec type for use
// with apply.
type JobSetSpecApplyConfiguration struct {
	ReplicatedJobs []ReplicatedJobApplyConfiguration `json:"replicatedJobs,omitempty"`
	SuccessPolicy  *SuccessPolicyApplyConfiguration  `json:"successPolicy,omitempty"`
	FailurePolicy  *FailurePolicyApplyConfiguration  `json:"failurePolicy,omitempty"`
	Suspend        *bool                             `json:"suspend,omitempty"`
}

// JobSetSpecApplyConfiguration constructs an declarative configuration of the JobSetSpec type for use with
// apply.
func JobSetSpec() *JobSetSpecApplyConfiguration {
	return &JobSetSpecApplyConfiguration{}
}

// WithReplicatedJobs adds the given value to the ReplicatedJobs field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the ReplicatedJobs field.
func (b *JobSetSpecApplyConfiguration) WithReplicatedJobs(values ...*ReplicatedJobApplyConfiguration) *JobSetSpecApplyConfiguration {
	for i := range values {
		if values[i] == nil {
			panic("nil value passed to WithReplicatedJobs")
		}
		b.ReplicatedJobs = append(b.ReplicatedJobs, *values[i])
	}
	return b
}

// WithSuccessPolicy sets the SuccessPolicy field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the SuccessPolicy field is set to the value of the last call.
func (b *JobSetSpecApplyConfiguration) WithSuccessPolicy(value *SuccessPolicyApplyConfiguration) *JobSetSpecApplyConfiguration {
	b.SuccessPolicy = value
	return b
}

// WithFailurePolicy sets the FailurePolicy field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the FailurePolicy field is set to the value of the last call.
func (b *JobSetSpecApplyConfiguration) WithFailurePolicy(value *FailurePolicyApplyConfiguration) *JobSetSpecApplyConfiguration {
	b.FailurePolicy = value
	return b
}

// WithSuspend sets the Suspend field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Suspend field is set to the value of the last call.
func (b *JobSetSpecApplyConfiguration) WithSuspend(value bool) *JobSetSpecApplyConfiguration {
	b.Suspend = &value
	return b
}
