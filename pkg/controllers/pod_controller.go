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
	"errors"
	"fmt"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"sigs.k8s.io/jobset/pkg/constants"
	"sigs.k8s.io/jobset/pkg/util/placement"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

const (
	// PodNameKey is the key used for building an index where the key is the
	// pod name (without the random suffix), and the value is the pod itself.
	PodNameKey string = "podName"

	// podJobKey is the key used for building an index where the key is the hash of
	// the namespaced job name of the job that owns this pod, and value is
	// the pod itself.
	podJobKey string = "podJobKey"
)

// PodReconciler reconciles a Pod owned by a JobSet using exclusive placement.
type PodReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Record events.EventRecorder
}

func NewPodReconciler(client client.Client, scheme *runtime.Scheme, record events.EventRecorder) *PodReconciler {
	return &PodReconciler{Client: client, Scheme: scheme, Record: record}
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		WithEventFilter(predicate.NewPredicateFuncs(func(object client.Object) bool {
			// Only reconcile leader pods which have been scheduled which are part of
			// JobSets using exclusive placement.
			pod, ok := object.(*corev1.Pod)
			return ok && placement.IsLeaderPod(pod) && podScheduled(pod) && usingExclusivePlacement(pod) && !podDeleted(pod)
		})).
		Complete(r)
}

func SetupPodIndexes(ctx context.Context, indexer client.FieldIndexer) error {
	// Build index where key is the hash of the namespaced job name of the job that owns this pod,
	// and value is the pod itself.
	if err := indexer.IndexField(ctx, &corev1.Pod{}, podJobKey, IndexPodJob); err != nil {
		return err
	}
	// Build index where the key is the pod name (without the random suffix), and the value is the pod itself.
	return indexer.IndexField(ctx, &corev1.Pod{}, PodNameKey, IndexPodName)
}

func IndexPodJob(obj client.Object) []string {
	pod := obj.(*corev1.Pod)
	// Make sure the pod is part of a JobSet using exclusive placement.
	if _, exists := pod.Annotations[jobset.ExclusiveKey]; !exists {
		return nil
	}
	jobKey, exists := pod.Labels[jobset.JobKey]
	if !exists {
		return nil
	}
	return []string{jobKey}
}

func IndexPodName(obj client.Object) []string {
	pod := obj.(*corev1.Pod)
	// Make sure the pod is part of a JobSet using exclusive placement.
	if _, exists := pod.Annotations[jobset.ExclusiveKey]; !exists {
		return nil
	}
	podName, err := removePodNameSuffix(pod.Name)
	if err != nil {
		return nil
	}
	return []string{podName}
}

// +kubebuilder:rbac:groups="",resources=events,verbs=create;watch;update;patch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch

// Reconcile attempts to enforce that the pods that belong to the same job are
// scheduled on the same topology domain exclusively if the parent JobSet is using
// exclusive placement.
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// In the following, we aim to enforce that for JobSets using exclusive placement,
	// pods that belong to the same job are scheduled on the same topology domain exclusively.
	// We do this by performing the following steps:
	// 1) Reconcile leader pods which are scheduled and using exclusive placement.
	// 2) For a given leader pod, check all follower pods's nodeSelectors are all
	//    configured to select the same topology as the leader pod is currently placed on.
	// 3) If the above condition is false, we delete all the follower pods in this job to
	//    allow them to be rescheduled correctly.
	var leaderPod corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &leaderPod); err != nil {
		// we'll ignore not-found errors, since there is nothing we can do here.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log := ctrl.LoggerFrom(ctx).WithValues("pod", klog.KObj(&leaderPod))
	ctx = ctrl.LoggerInto(ctx, log)
	log.V(2).Info("Reconciling Pod")

	// Get all the pods owned by the same job as this pod.
	jobKey, exists := leaderPod.Labels[jobset.JobKey]
	if !exists {
		return ctrl.Result{}, fmt.Errorf("job key label not found on leader pod: %q", leaderPod.Name)
	}
	podList, err := r.listPodsForJob(ctx, leaderPod.Namespace, jobKey)
	if err != nil {
		log.Error(err, "listing pods for job")
		return ctrl.Result{}, err
	}

	// Validate all follower pods in this job are assigned to the same topology as the leader pod.
	// If not, then delete all the job's follower pods so they can be recreated and rescheduled correctly.
	valid, err := r.validatePodPlacements(ctx, &leaderPod, podList)
	if err != nil {
		log.Error(err, "validating pod placements")
		return ctrl.Result{}, err
	}
	if !valid {
		log.V(2).Info(fmt.Sprintf("deleting follower pods for leader pod: %s", leaderPod.Name))
		return ctrl.Result{}, r.deleteFollowerPods(ctx, podList.Items)
	}
	return ctrl.Result{}, nil
}

// listPodsForJobKey returns a list of pods owned by a specific job, using the
// jobKey (SHA1 hash of the namespaced job name) label selector.
func (r *PodReconciler) listPodsForJob(ctx context.Context, ns, jobKey string) (*corev1.PodList, error) {
	var podList corev1.PodList
	if err := r.List(ctx, &podList, client.InNamespace(ns), &client.MatchingFields{podJobKey: jobKey}); err != nil {
		return nil, err
	}

	return &podList, nil
}

// validatePodPlacements returns true if the topology value specified in follower pods' nodeSelectors
// matches that of their leader pod, and the leader pod exists. Otherwise, it returns false.
func (r *PodReconciler) validatePodPlacements(ctx context.Context, leaderPod *corev1.Pod, podList *corev1.PodList) (bool, error) {
	// We know exclusive key is set since we have an event filter for this.
	topologyKey := leaderPod.Annotations[jobset.ExclusiveKey]
	leaderTopology, err := r.leaderPodTopology(ctx, leaderPod, topologyKey)
	if err != nil {
		return false, err
	}

	// Validate all follower pods are assigned to the same topology as the leader pod.
	for _, pod := range podList.Items {
		if placement.IsLeaderPod(&pod) {
			continue
		}
		followerTopology, err := followerPodTopology(&pod, topologyKey)
		if err != nil {
			return false, err
		}
		if followerTopology != leaderTopology {
			return false, fmt.Errorf("follower topology %q != leader topology %q", followerTopology, leaderTopology)
		}
	}
	return true, nil
}

// deleteFollowerPods deletes follower pods (non-index 0), parallelizing up to `maxParallelism` requests.
func (r *PodReconciler) deleteFollowerPods(ctx context.Context, pods []corev1.Pod) error {
	lock := &sync.Mutex{}
	var finalErrs []error

	workqueue.ParallelizeUntil(ctx, constants.MaxParallelism, len(pods), func(i int) {
		pod := pods[i]
		// Do not delete leader pod.
		if placement.IsLeaderPod(&pod) {
			return
		}

		// Add condition to pod status so that a podFailurePolicy can be used to ignore
		// deletions by this controller done to handle race conditions.
		condition := corev1.PodCondition{
			Type:    corev1.DisruptionTarget,
			Status:  corev1.ConditionTrue,
			Reason:  constants.ExclusivePlacementViolationReason,
			Message: constants.ExclusivePlacementViolationMessage,
		}

		// If pod status already has this condition, we don't need to send the update again.
		if updatePodCondition(&pod, condition) {
			if err := r.Status().Update(ctx, &pod); err != nil {
				lock.Lock()
				defer lock.Unlock()
				finalErrs = append(finalErrs, err)
				return
			}
		}

		// Delete the pod.
		backgroundPolicy := metav1.DeletePropagationBackground
		if err := r.Delete(ctx, &pod, &client.DeleteOptions{PropagationPolicy: &backgroundPolicy}); client.IgnoreNotFound(err) != nil {
			lock.Lock()
			defer lock.Unlock()
			finalErrs = append(finalErrs, err)
			return
		}
	})
	return errors.Join(finalErrs...)
}

// leaderPodTopology returns the topology value (e.g., node pool name, zone, etc.)
// for a given leader pod and topology key, by checking the node labels on the node
// the leader is currently scheduled on.
func (r *PodReconciler) leaderPodTopology(ctx context.Context, pod *corev1.Pod, topologyKey string) (string, error) {
	log := ctrl.LoggerFrom(ctx)

	nodeName := pod.Spec.NodeName
	ns := pod.Namespace

	// Get node the leader pod is running on.
	var node corev1.Node
	if err := r.Get(ctx, types.NamespacedName{Name: nodeName, Namespace: ns}, &node); err != nil {
		// We'll ignore not-found errors, since there is nothing we can do here.
		// A node may not exist temporarily due to a maintenance event or other scenarios.
		log.Error(err, fmt.Sprintf("getting node %s", nodeName))
		return "", client.IgnoreNotFound(err)
	}

	// Get topology (e.g. node pool name) from node labels.
	topology, exists := node.Labels[topologyKey]
	if !exists {
		return "", fmt.Errorf("node does not have topology label: %s", topologyKey)
	}
	return topology, nil
}

// followerPodTopology returns the topology value (e.g., node pool name, zone, etc.)
// for a given follower pod and topology key, by checking the target topology
// defined in the pod's nodeSelector.
func followerPodTopology(pod *corev1.Pod, topologyKey string) (string, error) {
	if pod.Spec.NodeSelector == nil {
		return "", fmt.Errorf("pod %s nodeSelector is nil", pod.Name)
	}
	topology, exists := pod.Spec.NodeSelector[topologyKey]
	if !exists {
		return "", fmt.Errorf("pod %s nodeSelector is missing key: %s", pod.Name, topologyKey)
	}
	return topology, nil
}

// usingExclusivePlacement returns true if the pod is part of a JobSet using
// exclusive placement, otherwise it returns false.
func usingExclusivePlacement(pod *corev1.Pod) bool {
	_, exists := pod.Annotations[jobset.ExclusiveKey]
	return exists
}

// podScheduled returns true if the pod has been scheduled, otherwise it returns false.
func podScheduled(pod *corev1.Pod) bool {
	return pod.Spec.NodeName != ""
}

// podDeleted returns true if hte pod has been marked for deletion, otherwise it returns false.
func podDeleted(pod *corev1.Pod) bool {
	return pod.DeletionTimestamp != nil
}

// removePodNameSuffix removes the random suffix appended to pod names.
func removePodNameSuffix(podName string) (string, error) {
	parts := strings.Split(podName, "-")

	// For a pod that belongs to a jobset, the minimum number of parts (split on "-" character)
	// is 5, given the pod name format is defined as:  <jobset>-<replicatedJob>-<jobIndex>-<podIndex>-<randomSuffix>
	if len(parts) < 5 {
		return "", fmt.Errorf("invalid pod name: %s", podName)
	}
	return strings.Join(parts[:len(parts)-1], "-"), nil
}

// Update the pod status with the given condition.
func updatePodCondition(pod *corev1.Pod, condition corev1.PodCondition) bool {
	condition.LastTransitionTime = metav1.Now()
	for i, val := range pod.Status.Conditions {
		if condition.Type == val.Type && condition.Status != val.Status {
			pod.Status.Conditions[i] = condition
			// Condition found but different status so we should update
			return true
		} else if condition.Type == val.Type && condition.Status == val.Status {
			// Duplicate condition so no update
			return false
		}
	}
	// Condition doesn't exist, update only if the status is true.
	if condition.Status == corev1.ConditionTrue {
		pod.Status.Conditions = append(pod.Status.Conditions, condition)
		return true
	}
	return false
}
