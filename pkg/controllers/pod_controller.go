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
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

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

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

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
