package webhooks

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/controllers"
	"sigs.k8s.io/jobset/pkg/util/shared"
)

//+kubebuilder:webhook:path=/validate--v1-pod,mutating=false,failurePolicy=fail,sideEffects=None,groups="",resources=pods,verbs=create,versions=v1,name=vpod.kb.io,sideEffects=None,admissionReviewVersions=v1

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (p *podWebhook) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil, fmt.Errorf("expected a Pod but got a %T", obj)
	}

	// If pod is part of a JobSet that is using the node selector exclusive placement strategy,
	// we don't need to validate anything.
	_, usingNodeSelectorStrategy := pod.Annotations[jobset.NodeSelectorStrategyKey]
	if usingNodeSelectorStrategy {
		return nil, nil
	}

	// If pod is not part of a JobSet using exclusive placement, we don't need to validate anything.
	topologyKey, usingExclusivePlacement := pod.Annotations[jobset.ExclusiveKey]
	if !usingExclusivePlacement {
		return nil, nil
	}

	// Do not validate anything else for leader pods, proceed with creation immediately.
	if !shared.IsLeaderPod(pod) {
		// If a follower pod node selector has not been set, reject the creation.
		if pod.Spec.NodeSelector == nil {
			return nil, fmt.Errorf("follower pod node selector not set")
		}
		if _, exists := pod.Spec.NodeSelector[topologyKey]; !exists {
			return nil, fmt.Errorf("follower pod node selector not set")
		}
		// For follower pods, validate leader pod exists and is scheduled.
		leaderScheduled, err := p.leaderPodScheduled(ctx, pod)
		if err != nil {
			return nil, err
		}
		if !leaderScheduled {
			return nil, fmt.Errorf("leader pod not yet scheduled, not creating follower pod %q", pod.Name)
		}
	}
	return nil, nil
}

func (p *podWebhook) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (p *podWebhook) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (p *podWebhook) leaderPodScheduled(ctx context.Context, pod *corev1.Pod) (bool, error) {
	leaderPod, err := p.leaderPodForFollower(ctx, pod)
	if err != nil {
		return false, err
	}
	return leaderPod.Spec.NodeName != "", nil
}

func (p *podWebhook) leaderPodForFollower(ctx context.Context, pod *corev1.Pod) (*corev1.Pod, error) {
	// Generate the expected leader pod name for this follower pod.
	log := ctrl.LoggerFrom(ctx)
	log.Info(fmt.Sprintf("generating leader pod name for follower pod: %s", pod.Name))
	leaderPodName, err := genLeaderPodName(pod)
	if err != nil {
		log.Error(err, "getting leader pod name for follower pod")
		return nil, err
	}

	// Get the leader pod object via the pod name index.
	var podList corev1.PodList
	if err := p.client.List(ctx, &podList, client.InNamespace(pod.Namespace), &client.MatchingFields{controllers.PodNameKey: leaderPodName}); err != nil {
		return nil, err
	}

	// Validate there is only 1 leader pod for this job.
	if len(podList.Items) != 1 {
		return nil, fmt.Errorf("too many leader pods for this job (expected 1, got %d", len(podList.Items))
	}

	// Check if the leader pod is scheduled.
	leaderPod := &podList.Items[0]
	return leaderPod, nil
}

// GenLeaderPodName accepts the name of a pod that is part of a jobset as input, and
// returns the name of the pod with completion index 0 in the same child job.
func genLeaderPodName(pod *corev1.Pod) (string, error) {
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
	return shared.GenLeaderPodName(jobSet, replicatedJob, jobIndex), nil
}
