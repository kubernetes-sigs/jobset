// shared package provides utility functions that are shared between the
// webhooks and the controllers.
package shared

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

func GenJobName(jsName, rjobName string, jobIndex int) string {
	return fmt.Sprintf("%s-%s-%d", jsName, rjobName, jobIndex)
}

func IsLeaderPod(pod *corev1.Pod) bool {
	return pod.Annotations[batchv1.JobCompletionIndexAnnotation] == "0"
}

// GenLeaderPodName returns the name of the leader pod (pod with completion index 0)
// for a given job in a jobset.
func GenLeaderPodName(jobSet, replicatedJob, jobIndex string) string {
	return fmt.Sprintf("%s-%s-%s-0", jobSet, replicatedJob, jobIndex)
}
