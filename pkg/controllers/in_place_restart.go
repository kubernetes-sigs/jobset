/*
Copyright The Kubernetes Authors.
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
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
)

// isInPlaceRestartStrategy returns true if the JobSet is configured to use the in-place restart strategy.
func isInPlaceRestartStrategy(js *jobset.JobSet) bool {
	return js.Spec.FailurePolicy != nil && js.Spec.FailurePolicy.RestartStrategy == jobset.InPlaceRestart
}

// reconcileInPlaceRestart reconciles the in-place restart for the JobSet.
func (r *JobSetReconciler) reconcileInPlaceRestart(ctx context.Context, js *jobset.JobSet, updateStatusOpts *statusUpdateOpts) error {
	log := ctrl.LoggerFrom(ctx)
	log.V(2).Info("Reconciling in-place restart")

	// Get Pods associated with this JobSet
	associatedPods, err := r.getAssociatedPods(ctx, js)
	if err != nil {
		log.Error(err, "getting associated pods")
		return err
	}

	// Fail JobSet if any container has exceeded max restarts
	// This is only done for handling the specific edge case of a container failing so fast that the barrier is never lifted
	maxContainerRestartCount := getMaxContainerRestartCount(associatedPods)
	if maxContainerRestartCount > js.Spec.FailurePolicy.MaxRestarts {
		log.Info("Individual container restart count has exceeded max restarts, failing JobSet")
		setJobSetFailedCondition(js, constants.ReachedMaxRestartsReason, constants.ReachedMaxRestartsMessage, updateStatusOpts)
		return nil
	}

	// Extract Pod in-place restart attempts from associated Pods
	podInPlaceRestartAttempts, err := getPodInPlaceRestartAttempts(associatedPods)
	if err != nil {
		log.Error(err, "getting Pod in-place restart attempts")
		return err
	}

	// Fail JobSet if any Pod in-place restart attempt that counts towards max restarts exceeds max restarts
	if exceededMaxRestarts(js, podInPlaceRestartAttempts) {
		log.Info("JobSet has exceeded max restarts during in-place restart, failing JobSet")
		setJobSetFailedCondition(js, constants.ReachedMaxRestartsReason, constants.ReachedMaxRestartsMessage, updateStatusOpts)
		return nil
	}

	// Get total number of pods
	totalNumberOfPods, err := getTotalNumberOfPods(js)
	if err != nil {
		log.Error(err, "getting total number of pods")
		return err
	}

	// If all associated Pods are at the same in-place restart attempt, set the current in-place restart attempt to the common value
	// This will make the agents lift their barriers to allow the worker container to start running
	// This is idempotent
	if len(podInPlaceRestartAttempts) == totalNumberOfPods && allEqual(podInPlaceRestartAttempts) {
		updateCurrentInPlaceRestartAttempt(log, js, podInPlaceRestartAttempts, updateStatusOpts)
		return nil
	}

	// Otherwise, if there is no Pod in-place restart attempt or if the maximum Pod in-place restart attempt is 0, ignore
	// This is expected when the JobSet is first created
	if len(podInPlaceRestartAttempts) == 0 || slices.Max(podInPlaceRestartAttempts) == 0 {
		return nil
	}

	// Otherwise, there is a mistmatch in the Pod in-place restart attempts, so set the previous in-place restart attempt to the maximum value minus one
	// This is done to make sure Pods not in the latest in-place restart attempt are restarted in-place to reach the latest in-place restart attempt
	// This is idempotent
	updatePreviousInPlaceRestartAttempt(log, js, podInPlaceRestartAttempts, updateStatusOpts)

	return nil
}

// getAssociatedPods returns all pods associated with the JobSet.
func (r *JobSetReconciler) getAssociatedPods(ctx context.Context, js *jobset.JobSet) (*corev1.PodList, error) {
	namespacedJobSet := js.Namespace + "/" + js.Name
	var associatedPods corev1.PodList
	if err := r.List(ctx, &associatedPods, client.InNamespace(js.Namespace), client.MatchingFields{
		constants.PodsIndexByJobSetKey: namespacedJobSet,
	}); err != nil {
		return nil, err
	}

	return &associatedPods, nil
}

// getMaxContainerRestartCount returns the maximum restart count of containers in the associated Pods.
func getMaxContainerRestartCount(associatedPods *corev1.PodList) int32 {
	maxRestartCount := int32(0)
	for _, pod := range associatedPods.Items {
		for _, containerStatus := range pod.Status.InitContainerStatuses {
			if containerStatus.RestartCount > maxRestartCount {
				maxRestartCount = containerStatus.RestartCount
			}
		}
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if containerStatus.RestartCount > maxRestartCount {
				maxRestartCount = containerStatus.RestartCount
			}
		}
	}

	return maxRestartCount
}

// getPodInPlaceRestartAttempts returns the in-place restart attempts of all pods associated with the JobSet.
func getPodInPlaceRestartAttempts(associatedPods *corev1.PodList) ([]int32, error) {
	podInPlaceRestartAttempts := []int32{}
	for _, pod := range associatedPods.Items {
		// Skip it if the pod is failed
		// Failed Pods might persist while their new copy already exists
		if pod.Status.Phase == corev1.PodFailed {
			continue
		}
		rawPodInPlaceRestartAttempt, ok := pod.Annotations[jobset.InPlaceRestartAttemptKey]
		// Skip it if the annotation is missing
		// It is likely due to the time between the Pod being created and the agent creating the annotation
		if !ok {
			continue
		}
		podInPlaceRestartAttempt, err := strconv.Atoi(rawPodInPlaceRestartAttempt)
		if err != nil {
			return nil, fmt.Errorf("invalid value for annotation %s, must be non-negative integer", jobset.InPlaceRestartAttemptKey)
		}
		if podInPlaceRestartAttempt < 0 {
			return nil, fmt.Errorf("invalid value for annotation %s, must be non-negative integer", jobset.InPlaceRestartAttemptKey)
		}
		podInPlaceRestartAttempts = append(podInPlaceRestartAttempts, int32(podInPlaceRestartAttempt))
	}

	return podInPlaceRestartAttempts, nil
}

// exceededMaxRestarts returns true if any Pod in-place restart attempt that counts towards max restarts exceeds max restarts.
func exceededMaxRestarts(js *jobset.JobSet, podInPlaceRestartAttempts []int32) bool {
	podInPlaceRestartAttempt := int32(0)
	if len(podInPlaceRestartAttempts) > 0 {
		podInPlaceRestartAttempt = slices.Max(podInPlaceRestartAttempts)
	}
	uncountedRestarts := js.Status.Restarts - js.Status.RestartsCountTowardsMax
	podInPlaceRestartsCountTowardsMax := podInPlaceRestartAttempt - uncountedRestarts

	return podInPlaceRestartsCountTowardsMax > js.Spec.FailurePolicy.MaxRestarts
}

// getTotalNumberOfPods returns the total number of pods for the JobSet.
func getTotalNumberOfPods(js *jobset.JobSet) (int, error) {
	totalNumberOfPods := 0
	for _, rjob := range js.Spec.ReplicatedJobs {
		jobTemplate := rjob.Template
		if jobTemplate.Spec.Completions == nil || jobTemplate.Spec.Parallelism == nil || *jobTemplate.Spec.Completions != *jobTemplate.Spec.Parallelism {
			return 0, fmt.Errorf("%s: in-place restart requires jobTemplate.spec.completions == jobTemplate.spec.parallelism != nil", rjob.Name)
		}
		totalNumberOfPods += int(rjob.Replicas * *jobTemplate.Spec.Parallelism)
	}

	return totalNumberOfPods, nil
}

// allEqual returns true if all values in the slice are equal.
func allEqual(values []int32) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if value != values[0] {
			return false
		}
	}

	return true
}

// updateCurrentInPlaceRestartAttempt updates the current in-place restart attempt of the JobSet.
func updateCurrentInPlaceRestartAttempt(log logr.Logger, js *jobset.JobSet, podInPlaceRestartAttempts []int32, updateStatusOpts *statusUpdateOpts) {
	// New value is the common value of all Pod in-place restart attempts
	// Since all values are equal, pick the first one
	newCurrentInPlaceRestartAttempt := podInPlaceRestartAttempts[0]
	// If the value didn't change, skip
	if js.Status.CurrentInPlaceRestartAttempt != nil && *js.Status.CurrentInPlaceRestartAttempt == newCurrentInPlaceRestartAttempt {
		return
	}
	// Else, update the status
	js.Status.CurrentInPlaceRestartAttempt = ptr.To(newCurrentInPlaceRestartAttempt)
	updateStatusOpts.shouldUpdate = true
	log.Info("Updated current in-place restart attempt", "currentInPlaceRestartAttempt", newCurrentInPlaceRestartAttempt)
}

// updatePreviousInPlaceRestartAttempt updates the previous in-place restart attempt of the JobSet.
func updatePreviousInPlaceRestartAttempt(log logr.Logger, js *jobset.JobSet, podInPlaceRestartAttempts []int32, updateStatusOpts *statusUpdateOpts) {
	// slices.Max(podInPlaceRestartAttempts) can't be calculated with empty slices
	// This is transient. Eventually the Pods start running and their agents create the annotation
	if len(podInPlaceRestartAttempts) == 0 {
		return
	}
	// New value is the maximum value of all Pod in-place restart attempts minus one
	newPreviousInPlaceRestartAttempt := slices.Max(podInPlaceRestartAttempts) - 1
	// If the new value is less than the old value, skip
	// This might happen while the JobSet is restarting since the Pod with the highest in-place restart attempt might not have been fully recreated yet
	if js.Status.PreviousInPlaceRestartAttempt != nil && newPreviousInPlaceRestartAttempt < *js.Status.PreviousInPlaceRestartAttempt {
		return
	}
	// If the value didn't change, skip
	if js.Status.PreviousInPlaceRestartAttempt != nil && *js.Status.PreviousInPlaceRestartAttempt == newPreviousInPlaceRestartAttempt {
		return
	}
	// Else, update the status
	js.Status.PreviousInPlaceRestartAttempt = ptr.To(newPreviousInPlaceRestartAttempt)
	updateStatusOpts.shouldUpdate = true
	log.Info("Updated previous in-place restart attempt", "previousInPlaceRestartAttempt", newPreviousInPlaceRestartAttempt)
}
