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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
)

// replicatedJobsStarted returns a boolean value indicating if all replicatedJob
// replicas (jobs) have started, regardless of whether they are active, succeeded,
// or failed.
func allReplicasStarted(replicas int32, rjJobStatus *jobset.ReplicatedJobStatus) bool {
	return replicas == rjJobStatus.Failed+rjJobStatus.Ready+rjJobStatus.Succeeded
}

// inOrderStartupPolicy returns true if the startup policy exists and is using an
// in order startup strategy. Otherwise, it returns false.
func inOrderStartupPolicy(sp *jobset.StartupPolicy) bool {
	return sp != nil && sp.StartupPolicyOrder == jobset.InOrder
}

// setInOrderStartupPolicyInProgressCondition sets a condition on the JobSet status indicating it is
// currently executing an in-order startup policy.
func setInOrderStartupPolicyInProgressCondition(js *jobset.JobSet, updateStatusOpts *statusUpdateOpts) {
	// Add a condition to the JobSet indicating the in order startup policy is executing.
	setCondition(js, &conditionOpts{
		eventType: corev1.EventTypeNormal,
		condition: &metav1.Condition{
			Type:    string(jobset.JobSetStartupPolicyInProgress),
			Status:  metav1.ConditionTrue,
			Reason:  constants.InOrderStartupPolicyInProgressReason,
			Message: constants.InOrderStartupPolicyInProgressMessage,
		},
	}, updateStatusOpts)
}

// setInOrderStartupPolicyCompletedCondition sets a condition on the JobSet status indicating it has finished
// running an in-order startup policy to completion.
func setInOrderStartupPolicyCompletedCondition(js *jobset.JobSet, updateStatusOpts *statusUpdateOpts) {
	setCondition(js, &conditionOpts{
		eventType: corev1.EventTypeNormal,
		condition: &metav1.Condition{
			Type:    string(jobset.JobSetStartupPolicyCompleted),
			Status:  metav1.ConditionTrue,
			Reason:  constants.InOrderStartupPolicyCompletedReason,
			Message: constants.InOrderStartupPolicyCompletedMessage,
		},
	}, updateStatusOpts)
}
