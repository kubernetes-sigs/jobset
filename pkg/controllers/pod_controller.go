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
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

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

	// parallelDeletions defines the maximum number of pod deletions that can be done concurrently.
	parallelDeletions int = 50
)

// PodReconciler reconciles a Pod owned by a JobSet using exclusive placement.
type PodReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Record record.EventRecorder
}

func NewPodReconciler(client client.Client, scheme *runtime.Scheme, record record.EventRecorder) *PodReconciler {
	return &PodReconciler{Client: client, Scheme: scheme, Record: record}
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		WithEventFilter(predicate.NewPredicateFuncs(func(object client.Object) bool {
			pod, ok := object.(*corev1.Pod)
			// Only reconcile pods that are part of JobSets using exclusive placement.
			return ok && usingExclusivePlacement(pod)
		})).
		Complete(r)
}

func SetupPodIndexes(ctx context.Context, indexer client.FieldIndexer) error {
	// Build index where key is the hash of the namespaced job name of the job that owns this pod,
	// and value is the pod itself.
	if err := indexer.IndexField(ctx, &corev1.Pod{}, podJobKey, func(obj client.Object) []string {
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
	}); err != nil {
		return err
	}

	// Build index where the key is the pod name (without the random suffix), and the value is the pod itself.
	return indexer.IndexField(ctx, &corev1.Pod{}, PodNameKey, func(obj client.Object) []string {
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
	})
}

// +kubebuilder:rbac:groups="",resources=events,verbs=create;watch;update;patch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch

// Reconcile attempts to enforce that the pods that belong to the same job are
// scheduled on the same topology domain exclusively (if the parent JobSet is using
// exclusive placement).
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// In the following, we aim to enforce that the pods that belong to the same job are
	// scheduled on the same topology domain exclusively. We accomplish this by checking
	// all pods for a given job, and validating that:
	// 1) The leader pod exists and is scheduled.
	// 2) The follower pods's nodeSelectors are all configured to select the same topology
	//    as the leader pod is currently placed on.
	// If either of these conditions are false, we delete all the pods in this job to
	// allow the scheduled process to restart from a clean slate.
	var pod corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {
		// we'll ignore not-found errors, since there is nothing we can do here.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log := ctrl.LoggerFrom(ctx).WithValues("pod", klog.KObj(&pod))
	ctx = ctrl.LoggerInto(ctx, log)
	log.V(2).Info("Reconciling Pod")

	// Check if this is the leader pod. If it is the leader pod and it hasn't been
	// scheduled, do nothing and return early.
	leader := placement.IsLeaderPod(&pod)
	if leader {
		log.Info(fmt.Sprintf("%q is a leader pod", pod.Name))
		if pod.Spec.NodeName == "" {
			log.Info("leader pod not scheduled")
			return ctrl.Result{}, nil
		}
	}

	// We need a reference to the scheduled leader pod of this job, to find the topology domain
	// it is scheduled in.
	leaderPod, err := r.leaderPodForFollower(ctx, &pod)
	if err != nil {
		return ctrl.Result{}, err
	}

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
	// If not, then delete all the job's pods so they can be recreated and rescheduled correctly.
	valid, err := r.validatePodPlacements(ctx, leaderPod, podList)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !valid {
		return ctrl.Result{}, r.deletePods(ctx, podList.Items)
	}
	return ctrl.Result{}, nil
}

// listPodsForJobKey returns a list of pods owned by a specific job, using the
// jobKey (SHA1 hash of the namespaced job name) label selector.
func (r *PodReconciler) listPodsForJob(ctx context.Context, ns, jobKey string) (*corev1.PodList, error) {
	log := ctrl.LoggerFrom(ctx)

	var podList corev1.PodList
	if err := r.List(ctx, &podList, client.InNamespace(ns), &client.MatchingFields{podJobKey: jobKey}); err != nil {
		log.Error(err, "listing pods")
		return nil, err
	}

	return &podList, nil
}

// getPodByName returns the Pod object for a given pod name.
func (r *PodReconciler) getPodByName(ctx context.Context, ns, podName string) (*corev1.Pod, error) {
	log := ctrl.LoggerFrom(ctx)

	var podList corev1.PodList
	if err := r.List(ctx, &podList, client.InNamespace(ns), &client.MatchingFields{PodNameKey: podName}); err != nil {
		log.Error(err, "listing pods")
		return nil, err
	}

	// Validate only 1 pod with this name exists.
	if len(podList.Items) != 1 {
		return nil, fmt.Errorf("expected 1 pod with name %q, got %d", podName, len(podList.Items))
	}

	return &podList.Items[0], nil
}

// leaderPodForFollower returns the Pod object for the leader pod (job completion index 0) for
// a given follower pod.
func (r *PodReconciler) leaderPodForFollower(ctx context.Context, pod *corev1.Pod) (*corev1.Pod, error) {
	log := ctrl.LoggerFrom(ctx)

	var leaderPod *corev1.Pod
	if placement.IsLeaderPod(pod) {
		log.Info(fmt.Sprintf("%q is a leader pod", pod.Name))
		leaderPod = pod
	} else {
		log.Info(fmt.Sprintf("%q is a follower pod", pod.Name))
		leaderPodName, err := leaderPodNameForFollower(pod)
		if err != nil {
			log.Error(err, "generating leader pod name")
			return nil, err
		}
		// Use pod name index to quickly fetch the leader pod object.
		leaderPod, err = r.getPodByName(ctx, pod.Namespace, leaderPodName)
		if err != nil {
			log.Error(err, "getting leader pod by name")
			return nil, err
		}
	}
	return leaderPod, nil
}

// validatePodPlacements returns true if the topology value specified in follower pods' nodeSelectors
// matches that of their leader pod, and the leader pod exists. Otherwise, it returns false.
func (r *PodReconciler) validatePodPlacements(ctx context.Context, leaderPod *corev1.Pod, podList *corev1.PodList) (bool, error) {
	// We know exclusive key is set since we have an event filter for this.
	topologyKey := leaderPod.Annotations[jobset.ExclusiveKey]
	leaderTopology, err := r.topologyFromPod(ctx, leaderPod, topologyKey)
	if err != nil {
		return false, err
	}

	// Validate all follower pods are assigned to the same topology as the leader pod.
	for _, pod := range podList.Items {
		if placement.IsLeaderPod(&pod) {
			continue
		}
		followerTopology, err := r.topologyFromPod(ctx, &pod, topologyKey)
		if err != nil {
			return false, err
		}
		if followerTopology != leaderTopology {
			return false, fmt.Errorf("follower topology %q != leader topology %q", followerTopology, leaderTopology)
		}
	}
	return true, nil
}

// topologyFromPod returns the topology value (e.g., node pool name, zone, etc.)
// for a given pod and topology key.
func (r *PodReconciler) topologyFromPod(ctx context.Context, pod *corev1.Pod, topologyKey string) (string, error) {
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
		return "", fmt.Errorf("node does not have topology label: %s", topology)
	}
	return topology, nil
}

// deletePods deletes the given pods, parallelizing up to `parallelDeletions` requests.
func (r *PodReconciler) deletePods(ctx context.Context, pods []corev1.Pod) error {
	lock := &sync.Mutex{}
	var finalErrs []error

	workqueue.ParallelizeUntil(ctx, parallelDeletions, len(pods), func(i int) {
		pod := pods[i]
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

// usingExclusivePlacement returns true if the pod is part of a JobSet using
// exclusive placement, otherwise it returns false.
func usingExclusivePlacement(pod *corev1.Pod) bool {
	_, exists := pod.Annotations[jobset.ExclusiveKey]
	return exists
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

// leaderPodNameForFollower accepts the name of a pod that is part of a jobset as input, and
// returns the name of the pod with completion index 0 in the same child job.
func leaderPodNameForFollower(pod *corev1.Pod) (string, error) {
	// Pod name format: <jobset>-<replicatedJob>-<jobIndex>-<podIndex>-<randomSuffix>
	jobSet, ok := pod.Labels[jobset.JobSetNameKey]
	if !ok {
		return "", fmt.Errorf("pod missing label: %s", jobset.JobSetNameKey)
	}
	replicatedJob, ok := pod.Labels[jobset.ReplicatedJobNameKey]
	if !ok {
		return "", fmt.Errorf("pod missing label: %s", jobset.ReplicatedJobNameKey)
	}
	jobIndex, ok := pod.Labels[jobset.JobIndexKey]
	if !ok {
		return "", fmt.Errorf("pod missing label: %s", jobset.JobIndexKey)
	}
	return placement.GenLeaderPodName(jobSet, replicatedJob, jobIndex), nil
}
