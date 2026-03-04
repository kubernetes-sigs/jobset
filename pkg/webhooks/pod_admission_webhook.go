package webhooks

import (
	"context"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/controllers"
	"sigs.k8s.io/jobset/pkg/util/placement"
)

//+kubebuilder:webhook:path=/validate--v1-pod,mutating=false,failurePolicy=fail,sideEffects=None,groups="",resources=pods,verbs=create,versions=v1,name=vpod.kb.io,sideEffects=None,admissionReviewVersions=v1

// ValidateCreate validates that follower pods (job completion index != 0) part of a JobSet using exclusive
// placement are only admitted after the leader pod (job completion index == 0) has been scheduled.
func (p *podWebhook) ValidateCreate(ctx context.Context, pod *corev1.Pod) (admission.Warnings, error) {
	// If this pod is not part of a JobSet, we don't need to validate anything.
	// We can check the existence of the JobSetName annotation to determine this.
	if _, isJobSetPod := pod.Annotations[jobset.JobSetNameKey]; !isJobSetPod {
		return nil, nil
	}

	// If pod is part of a JobSet that is using the node selector exclusive placement strategy,
	// we don't need to validate anything.
	if _, usingNodeSelectorStrategy := pod.Annotations[jobset.NodeSelectorStrategyKey]; usingNodeSelectorStrategy {
		return nil, nil
	}

	// If pod is not part of a JobSet using exclusive placement, we don't need to validate anything.
	topologyKey, usingExclusivePlacement := pod.Annotations[jobset.ExclusiveKey]
	if !usingExclusivePlacement {
		return nil, nil
	}

	// Do not validate anything else for leader pods, proceed with creation immediately.
	if placement.IsLeaderPod(pod) {
		return nil, nil
	}
	// If a follower pod node selector has not been set, reject the creation.
	if pod.Spec.NodeSelector == nil {
		return nil, fmt.Errorf("follower pod node selector not set")
	}
	if _, exists := pod.Spec.NodeSelector[topologyKey]; !exists {
		return nil, fmt.Errorf("follower pod node selector for topology domain not found. missing selector: %s", topologyKey)
	}
	// For follower pods, validate leader pod exists and is scheduled.
	leaderScheduled, err := p.leaderPodScheduled(ctx, pod)
	if err != nil {
		return nil, err
	}
	if !leaderScheduled {
		return nil, fmt.Errorf("leader pod not yet scheduled, not creating follower pod. this is an expected, transient error")
	}
	return nil, nil
}

func (p *podWebhook) ValidateUpdate(ctx context.Context, oldObj, newObj *corev1.Pod) (admission.Warnings, error) {
	return nil, nil
}

func (p *podWebhook) ValidateDelete(ctx context.Context, obj *corev1.Pod) (admission.Warnings, error) {
	return nil, nil
}

func (p *podWebhook) leaderPodScheduled(ctx context.Context, pod *corev1.Pod) (bool, error) {
	log := ctrl.LoggerFrom(ctx)
	leaderPod, err := p.leaderPodForFollower(ctx, pod)
	if err != nil {
		return false, err
	}
	scheduled := leaderPod.Spec.NodeName != ""
	if !scheduled {
		log.V(2).Info(fmt.Sprintf("leader pod %s is not yet scheduled", leaderPod.Name))
	}
	return scheduled, nil
}

func (p *podWebhook) leaderPodForFollower(ctx context.Context, pod *corev1.Pod) (*corev1.Pod, error) {
	// Generate the expected leader pod name for this follower pod.
	log := ctrl.LoggerFrom(ctx)
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
	podList.Items = slices.DeleteFunc(podList.Items, func(p corev1.Pod) bool {
		return !p.DeletionTimestamp.IsZero()
	})

	// Validate there is only 1 leader pod for this job.
	if len(podList.Items) != 1 {
		return nil, fmt.Errorf("expected 1 leader pod (%s), but got %d. this is an expected, transient error", leaderPodName, len(podList.Items))
	}

	// Validate leader pod has same owner UID as the follower, to ensure they are part of the same Job.
	// This is necessary to handle a race condition where the JobSet is restarted (deleting and recreating
	// all jobs), and a leader pod may land on different node pools than they were originally scheduled on.
	// Then when the follower pods are recreated, and we look up the leader pod using the index which maps
	// [pod name without random suffix] -> corev1.Pod object, we may get a stale index entry and inject
	// the wrong nodeSelector, using the topology the leader pod was originally scheduled on before the
	// restart.
	leaderPod := &podList.Items[0]
	if err := podsOwnedBySameJob(leaderPod, pod); err != nil {
		return nil, err
	}

	return leaderPod, nil
}

// genLeaderPodName accepts the name of a pod that is part of a jobset as input, and
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
	leaderPodName := placement.GenPodName(jobSet, replicatedJob, jobIndex, "0")
	return leaderPodName, nil
}

// podsOwnedBySameJob returns an error if the leader pod and
// follower pod are not owned by the same Job UID. Otherwise, it returns nil.
func podsOwnedBySameJob(leaderPod, followerPod *corev1.Pod) error {
	followerOwnerRef := metav1.GetControllerOf(followerPod)
	if followerOwnerRef == nil {
		return fmt.Errorf("follower pod has no owner reference")
	}
	leaderOwnerRef := metav1.GetControllerOf(leaderPod)
	if leaderOwnerRef == nil {
		return fmt.Errorf("leader pod %q has no owner reference", leaderPod.Name)
	}
	if followerOwnerRef.UID != leaderOwnerRef.UID {
		return fmt.Errorf("follower pod owner UID (%s) != leader pod owner UID (%s)", string(followerOwnerRef.UID), string(leaderOwnerRef.UID))
	}
	return nil
}
