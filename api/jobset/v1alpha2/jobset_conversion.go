/*
Copyright 2025 The Kubernetes Authors.
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

package v1alpha2

import (
	"encoding/base64"
	"encoding/json"

	"sigs.k8s.io/controller-runtime/pkg/conversion"
	jobsetv1 "sigs.k8s.io/jobset/api/jobset/v1"
)

// ConvertTo converts this JobSet to the storage version (v1).
func (src *JobSet) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*jobsetv1.JobSet)
	dst.ObjectMeta = src.ObjectMeta

	dst.Status.Conditions = src.Status.Conditions
	dst.Status.Restarts = src.Status.Restarts
	dst.Status.RestartsCountTowardsMax = src.Status.RestartsCountTowardsMax
	dst.Status.TerminalState = src.Status.TerminalState
	for i, job := range src.Status.ReplicatedJobsStatus {
		dst.Status.ReplicatedJobsStatus[i].Name = job.Name
		dst.Status.ReplicatedJobsStatus[i].Ready = job.Ready
		dst.Status.ReplicatedJobsStatus[i].Succeeded = job.Succeeded
		dst.Status.ReplicatedJobsStatus[i].Failed = job.Failed
		dst.Status.ReplicatedJobsStatus[i].Active = job.Active
		dst.Status.ReplicatedJobsStatus[i].Suspended = job.Suspended
	}

	for i, job := range src.Spec.ReplicatedJobs {
		dst.Spec.ReplicatedJobs[i].Name = job.Name
		dst.Spec.ReplicatedJobs[i].GroupName = job.GroupName
		dst.Spec.ReplicatedJobs[i].Template = job.Template
		dst.Spec.ReplicatedJobs[i].Replicas = job.Replicas
		for j, dependsOn := range job.DependsOn {
			dst.Spec.ReplicatedJobs[i].DependsOn[j].Name = dependsOn.Name
			dst.Spec.ReplicatedJobs[i].DependsOn[j].Status = (jobsetv1.DependsOnStatus)(dependsOn.Status)
		}
	}

	dst.Spec.Network.EnableDNSHostnames = src.Spec.Network.EnableDNSHostnames
	dst.Spec.Network.PublishNotReadyAddresses = src.Spec.Network.PublishNotReadyAddresses
	dst.Spec.Network.Subdomain = src.Spec.Network.Subdomain

	dst.Spec.SuccessPolicy.Operator = (jobsetv1.Operator)(src.Spec.SuccessPolicy.Operator)
	dst.Spec.SuccessPolicy.TargetReplicatedJobs = src.Spec.SuccessPolicy.TargetReplicatedJobs

	dst.Spec.FailurePolicy.MaxRestarts = src.Spec.FailurePolicy.MaxRestarts
	dst.Spec.FailurePolicy.RestartStrategy = (jobsetv1.JobSetRestartStrategy)(src.Spec.FailurePolicy.RestartStrategy)
	for i, policy := range src.Spec.FailurePolicy.Rules {
		dst.Spec.FailurePolicy.Rules[i].Name = policy.Name
		dst.Spec.FailurePolicy.Rules[i].Action = (jobsetv1.FailurePolicyAction)(policy.Action)
		dst.Spec.FailurePolicy.Rules[i].OnJobFailureReasons = policy.OnJobFailureReasons
		dst.Spec.FailurePolicy.Rules[i].TargetReplicatedJobs = policy.TargetReplicatedJobs
	}

	// Add the annotation if StartupPolicy is set.
	if src.Spec.StartupPolicy != nil {
		if dst.Annotations == nil {
			dst.Annotations = make(map[string]string)
		}
		// Store the startup policy in the annotation.
		jsonData, err := json.Marshal(src.Spec.StartupPolicy)
		if err != nil {
			return err
		}
		encodedData := base64.StdEncoding.EncodeToString(jsonData)
		dst.Annotations[jobsetv1.StartupPolicyAnnotation] = encodedData
	}

	dst.Spec.Suspend = src.Spec.Suspend
	dst.Spec.Coordinator.ReplicatedJob = src.Spec.Coordinator.ReplicatedJob
	dst.Spec.Coordinator.JobIndex = src.Spec.Coordinator.JobIndex
	dst.Spec.Coordinator.PodIndex = src.Spec.Coordinator.PodIndex
	dst.Spec.ManagedBy = src.Spec.ManagedBy
	dst.Spec.TTLSecondsAfterFinished = src.Spec.TTLSecondsAfterFinished

	return nil
}

// ConvertFrom converts from the storage version (v1) to this version.
func (dst *JobSet) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*jobsetv1.JobSet)
	dst.ObjectMeta = src.ObjectMeta

	dst.Status.Conditions = src.Status.Conditions
	dst.Status.Restarts = src.Status.Restarts
	dst.Status.RestartsCountTowardsMax = src.Status.RestartsCountTowardsMax
	dst.Status.TerminalState = src.Status.TerminalState
	for i, job := range src.Status.ReplicatedJobsStatus {
		dst.Status.ReplicatedJobsStatus[i].Name = job.Name
		dst.Status.ReplicatedJobsStatus[i].Ready = job.Ready
		dst.Status.ReplicatedJobsStatus[i].Succeeded = job.Succeeded
		dst.Status.ReplicatedJobsStatus[i].Failed = job.Failed
		dst.Status.ReplicatedJobsStatus[i].Active = job.Active
		dst.Status.ReplicatedJobsStatus[i].Suspended = job.Suspended
	}

	for i, job := range src.Spec.ReplicatedJobs {
		dst.Spec.ReplicatedJobs[i].Name = job.Name
		dst.Spec.ReplicatedJobs[i].GroupName = job.GroupName
		dst.Spec.ReplicatedJobs[i].Template = job.Template
		dst.Spec.ReplicatedJobs[i].Replicas = job.Replicas
		for j, dependsOn := range job.DependsOn {
			dst.Spec.ReplicatedJobs[i].DependsOn[j].Name = dependsOn.Name
			dst.Spec.ReplicatedJobs[i].DependsOn[j].Status = (DependsOnStatus)(dependsOn.Status)
		}
	}

	dst.Spec.Network.EnableDNSHostnames = src.Spec.Network.EnableDNSHostnames
	dst.Spec.Network.PublishNotReadyAddresses = src.Spec.Network.PublishNotReadyAddresses
	dst.Spec.Network.Subdomain = src.Spec.Network.Subdomain

	dst.Spec.SuccessPolicy.Operator = (Operator)(src.Spec.SuccessPolicy.Operator)
	dst.Spec.SuccessPolicy.TargetReplicatedJobs = src.Spec.SuccessPolicy.TargetReplicatedJobs

	dst.Spec.FailurePolicy.MaxRestarts = src.Spec.FailurePolicy.MaxRestarts
	dst.Spec.FailurePolicy.RestartStrategy = (JobSetRestartStrategy)(src.Spec.FailurePolicy.RestartStrategy)
	for i, policy := range src.Spec.FailurePolicy.Rules {
		dst.Spec.FailurePolicy.Rules[i].Name = policy.Name
		dst.Spec.FailurePolicy.Rules[i].Action = (FailurePolicyAction)(policy.Action)
		dst.Spec.FailurePolicy.Rules[i].OnJobFailureReasons = policy.OnJobFailureReasons
		dst.Spec.FailurePolicy.Rules[i].TargetReplicatedJobs = policy.TargetReplicatedJobs
	}

	// Restore StartupPolicy from the annotation if it exists.
	encodedData := src.ObjectMeta.Annotations[jobsetv1.StartupPolicyAnnotation]
	if encodedData != "" {
		decodedData, err := base64.StdEncoding.DecodeString(encodedData)
		if err != nil {
			return err
		}

		var startupPolicy StartupPolicy
		err = json.Unmarshal(decodedData, &startupPolicy)
		if err != nil {
			return err
		}
		dst.Spec.StartupPolicy = &startupPolicy
	}

	dst.Spec.Suspend = src.Spec.Suspend
	dst.Spec.Coordinator.ReplicatedJob = src.Spec.Coordinator.ReplicatedJob
	dst.Spec.Coordinator.JobIndex = src.Spec.Coordinator.JobIndex
	dst.Spec.Coordinator.PodIndex = src.Spec.Coordinator.PodIndex
	dst.Spec.ManagedBy = src.Spec.ManagedBy
	dst.Spec.TTLSecondsAfterFinished = src.Spec.TTLSecondsAfterFinished

	return nil
}
