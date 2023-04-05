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
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	jobsetv1alpha "sigs.k8s.io/jobset/api/v1alpha1"
)

type realClock struct{}

func (_ realClock) Now() time.Time { return time.Now() }

// clock knows how to get the current time.
// It can be used to fake out timing for testing.
type Clock interface {
	Now() time.Time
}

func IsJobFinished(job *batchv1.Job) (bool, batchv1.JobConditionType) {
	for _, c := range job.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
			return true, c.Type
		}
	}
	return false, ""
}

func jobIndex(jobSet *jobsetv1alpha.JobSet, job *batchv1.Job) (int, error) {
	for i, jobTemplate := range jobSet.Spec.Jobs {
		if generateJobName(jobSet, &jobTemplate) == job.Name {
			return i, nil
		}
	}
	return -1, fmt.Errorf("JobSet %s does not contain Job %s", jobSet.Name, job.Name)
}

func generateJobName(jobSet *jobsetv1alpha.JobSet, jobTemplate *jobsetv1alpha.ReplicatedJob) string {
	return fmt.Sprintf("%s-%s", jobSet.Name, jobTemplate.Template.Name)
}
