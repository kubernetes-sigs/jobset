// placement package provides utility functions that are shared between the
// webhooks and controllers to implement the exclusive placement per topology feature.
package placement

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

// GenJobName deterministically generates the child job name from the given
// JobSet name, replicated job name, and job index.
func GenJobName(jsName, rjobName string, jobIndex int) string {
	return fmt.Sprintf("%s-%s-%d", jsName, rjobName, jobIndex)
}

// GenPodName returns the pod name for the given JobSet name, ReplicatedJob name,
// Job index, and Pod index.
func GenPodName(jobSet, replicatedJob, jobIndex, podIndex string) string {
	return fmt.Sprintf("%s-%s-%s-%s", jobSet, replicatedJob, jobIndex, podIndex)
}

// IsLeaderPod returns true if the given pod is a leader pod (job completion index of 0),
// otherwise it returns false.
func IsLeaderPod(pod *corev1.Pod) bool {
	return pod.Annotations[batchv1.JobCompletionIndexAnnotation] == "0"
}
