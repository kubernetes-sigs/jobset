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

package webhooks

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
)

// +kubebuilder:webhook:path=/mutate--v1-pod,mutating=true,failurePolicy=fail,groups="",resources=pods,verbs=create,versions=v1,name=mpod.kb.io,sideEffects=None,admissionReviewVersions=v1

// podWebhook for mutating webhook.
type podWebhook struct {
	client client.Client
}

func NewPodWebhook(client client.Client) *podWebhook {
	return &podWebhook{client: client}
}

// SetupWebhookWithManager configures the mutating webhook for pods.
func (p *podWebhook) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &corev1.Pod{}).
		WithDefaulter(p).
		WithValidator(p).
		Complete()
}

// Default mutates pods that are part of a JobSet
func (p *podWebhook) Default(ctx context.Context, pod *corev1.Pod) error {
	// If this pod is not part of a JobSet, skip it.
	if _, isJobSetPod := pod.Annotations[jobset.JobSetNameKey]; !isJobSetPod {
		return nil
	}
	// If the parent JobSet is using the node selector exclusive placement strategy (running
	// the hack/label_nodes.py script beforehand), skip it.
	if _, usingNodeSelectorStrategy := pod.Annotations[jobset.NodeSelectorStrategyKey]; usingNodeSelectorStrategy {
		return nil
	}
	// Add the priority label to make sure the Pod is compatible with other pods using the exclusive
	// placement feature.
	// See https://github.com/kubernetes-sigs/jobset/issues/1057 for more details.
	if pod.Spec.Priority != nil {
		if pod.Labels == nil {
			pod.Labels = make(map[string]string)
		}
		pod.Labels[constants.PriorityKey] = fmt.Sprint(*pod.Spec.Priority)
	}
	// If the parent JobSet is using the exclusive placement feature, patch the Pod accordingly
	if _, usingExclusivePlacement := pod.Annotations[jobset.ExclusiveKey]; usingExclusivePlacement {
		return p.patchPod(ctx, pod)
	}
	return nil
}

// patchPod will mutate pods in the following ways:
//  1. For leader pods (job completion index 0), pod affinities/anti-affinities for
//     exclusive placement per topology are injected.
//  2. For follower pods (job completion index != 0), nodeSelectors for the same topology
//     as their leader pod are injected.
func (p *podWebhook) patchPod(ctx context.Context, pod *corev1.Pod) error {
	log := ctrl.LoggerFrom(ctx)
	if pod.Annotations[batchv1.JobCompletionIndexAnnotation] == "0" {
		log.V(3).Info(fmt.Sprintf("pod webhook: setting exclusive affinities for pod: %s", pod.Name))
		setExclusiveAffinities(pod)
		return nil
	} else {
		log.V(3).Info(fmt.Sprintf("pod webhook: adding node selector for follower pod: %s", pod.Name))
		return p.setNodeSelector(ctx, pod)
	}
}

func setExclusiveAffinities(pod *corev1.Pod) {
	if pod.Spec.Affinity == nil {
		pod.Spec.Affinity = &corev1.Affinity{}
	}
	if pod.Spec.Affinity.PodAffinity == nil {
		pod.Spec.Affinity.PodAffinity = &corev1.PodAffinity{}
	}
	if pod.Spec.Affinity.PodAntiAffinity == nil {
		pod.Spec.Affinity.PodAntiAffinity = &corev1.PodAntiAffinity{}
	}
	// Pod affinity ensures the pods of this job land on the same topology domain.
	pod.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution = append(pod.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
		corev1.PodAffinityTerm{
			LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      jobset.JobKey,
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{pod.Labels[jobset.JobKey]},
				},
			}},
			TopologyKey:       pod.Annotations[jobset.ExclusiveKey],
			NamespaceSelector: &metav1.LabelSelector{},
		})
	// Pod anti-affinity ensures exclusively this job lands on the topology, preventing multiple jobs per topology domain.
	pod.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution = append(pod.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
		corev1.PodAffinityTerm{
			LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      jobset.JobKey,
					Operator: metav1.LabelSelectorOpExists,
				},
				{
					Key:      jobset.JobKey,
					Operator: metav1.LabelSelectorOpNotIn,
					Values:   []string{pod.Labels[jobset.JobKey]},
				},
				{
					Key:      constants.PriorityKey,
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{pod.Labels[constants.PriorityKey]},
				},
			}},
			TopologyKey:       pod.Annotations[jobset.ExclusiveKey],
			NamespaceSelector: &metav1.LabelSelector{},
		})
}

func (p *podWebhook) setNodeSelector(ctx context.Context, pod *corev1.Pod) error {
	log := ctrl.LoggerFrom(ctx)
	// Find leader pod (completion index 0) for this job.
	leaderPod, err := p.leaderPodForFollower(ctx, pod)
	if err != nil {
		log.Error(err, "finding leader pod for follower")
		// Return no error, validation webhook will reject creation of this follower pod.
		return nil
	}

	// If leader pod is not scheduled yet, return error to retry pod creation until leader is scheduled.
	if leaderPod.Spec.NodeName == "" {
		// Return no error, validation webhook will reject creation of this follower pod.
		return nil
	}

	// Get the exclusive topology value for the leader pod (i.e. name of nodepool, rack, etc.)
	topologyKey, ok := pod.Annotations[jobset.ExclusiveKey]
	if !ok {
		return fmt.Errorf("pod missing annotation: %s", jobset.ExclusiveKey)
	}
	topologyValue, err := p.topologyFromPod(ctx, leaderPod, topologyKey)
	if err != nil {
		log.Error(err, "getting topology from leader pod")
		return err
	}

	// Set node selector of follower pod so it's scheduled on the same topology as the leader.
	if pod.Spec.NodeSelector == nil {
		pod.Spec.NodeSelector = make(map[string]string)
	}
	log.V(2).Info(fmt.Sprintf("setting nodeSelector %s: %s to follow leader pod %s", topologyKey, topologyValue, leaderPod.Name))
	pod.Spec.NodeSelector[topologyKey] = topologyValue
	return nil
}

func (p *podWebhook) topologyFromPod(ctx context.Context, pod *corev1.Pod, topologyKey string) (string, error) {
	log := ctrl.LoggerFrom(ctx)

	nodeName := pod.Spec.NodeName

	// Get node the leader pod is running on.
	var node corev1.Node
	if err := p.client.Get(ctx, types.NamespacedName{Name: nodeName}, &node); err != nil {
		log.Error(err, fmt.Sprintf("getting node %s", nodeName))
		return "", err
	}

	// Get topology (e.g. node pool name) from node labels.
	topology, exists := node.Labels[topologyKey]
	if !exists {
		return "", fmt.Errorf("node does not have topology label: %s", topologyKey)
	}
	return topology, nil
}
