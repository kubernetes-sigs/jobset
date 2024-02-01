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

package controllers

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// replicatedJobsStarted tells if the replicated jobSet is ready
// or it has finished
// If replicas is equal then we assume that replicatedJob is considered ready
func replicatedJobsStarted(replicas int32, rjJobStatus jobset.ReplicatedJobStatus) bool {
	return replicas == rjJobStatus.Failed+rjJobStatus.Ready+rjJobStatus.Succeeded
}

func inOrderStartupPolicy(sp *jobset.StartupPolicy) bool {
	return sp != nil && sp.StartupPolicyOrder == jobset.InOrder
}

func generateStartupPolicyCondition(policyComplete bool) metav1.Condition {
	condition := metav1.ConditionFalse
	message := "startup policy in order starting"
	if policyComplete {
		condition = metav1.ConditionTrue
		message = "all replicated jobs have started"
	}
	return metav1.Condition{
		Type:    string(jobset.JobSetStartupPolicyCompleted),
		Status:  condition,
		Reason:  "StartupPolicyInOrder",
		Message: message}
}
