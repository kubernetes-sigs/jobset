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

package util

import (
	"context"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
	jobset "sigs.k8s.io/jobset/api/v1alpha1"
)

func CheckJobSetStatus(ctx context.Context, k8sClient client.Client, js *jobset.JobSet, condition jobset.JobSetConditionType) (bool, error) {
	var fetchedJS jobset.JobSet
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: js.Namespace, Name: js.Name}, &fetchedJS); err != nil {
		return false, err
	}
	for _, c := range fetchedJS.Status.Conditions {
		if c.Type == string(condition) {
			return true, nil
		}
	}
	return false, nil
}

func NumExpectedJobs(js *jobset.JobSet) int {
	expectedJobs := 0
	for _, rjob := range js.Spec.ReplicatedJobs {
		expectedJobs += rjob.Replicas
	}
	return expectedJobs
}

func CheckNumJobs(ctx context.Context, k8sClient client.Client, js *jobset.JobSet) (int, error) {
	var jobList batchv1.JobList
	if err := k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace)); err != nil {
		return -1, err
	}
	return len(jobList.Items), nil
}
