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
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
)

const interval = time.Millisecond * 250

func NumExpectedJobs(js *jobset.JobSet) int {
	expectedJobs := 0
	for _, rjob := range js.Spec.ReplicatedJobs {
		expectedJobs += int(rjob.Replicas)
	}
	return expectedJobs
}

func NumJobs(ctx context.Context, k8sClient client.Client, js *jobset.JobSet) (int, error) {
	var jobList batchv1.JobList
	if err := k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace)); err != nil {
		return -1, err
	}
	return len(jobList.Items), nil
}

func NumJobsReadyOrSucceeded(ctx context.Context, k8sClient client.Client, js *jobset.JobSet, replicatedJobName string) (int32, error) {
	var jobSet jobset.JobSet
	jobSetKey := types.NamespacedName{Namespace: js.Namespace, Name: js.Name}
	if err := k8sClient.Get(ctx, jobSetKey, &jobSet); err != nil {
		return 0, err
	}

	for _, rJobStatus := range jobSet.Status.ReplicatedJobsStatus {
		if rJobStatus.Name == replicatedJobName {
			return max(rJobStatus.Ready, rJobStatus.Succeeded), nil
		}
	}
	return 0, nil
}

func NumJobsByRestartAttempt(ctx context.Context, k8sClient client.Client, js *jobset.JobSet) (map[int]int, error) {
	var jobList batchv1.JobList
	if err := k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace)); err != nil {
		return nil, err
	}
	res := make(map[int]int)
	for _, job := range jobList.Items {
		restartAttempt, ok := job.Labels[constants.RestartsKey]
		if !ok {
			return nil, fmt.Errorf("job %s/%s does not have a restart attempt label", job.Namespace, job.Name)
		}
		attempt, err := strconv.Atoi(restartAttempt)
		if err != nil {
			return nil, fmt.Errorf("job %s/%s has an invalid restart attempt label: %v", job.Namespace, job.Name, err)
		}
		res[attempt]++
	}
	return res, nil
}

func JobSetCompleted(ctx context.Context, k8sClient client.Client, js *jobset.JobSet, timeout time.Duration) {
	ginkgo.By(fmt.Sprintf("checking jobset status is: %s", jobset.JobSetCompleted))
	conditions := []metav1.Condition{
		{
			Type:   string(jobset.JobSetCompleted),
			Status: metav1.ConditionTrue,
		},
	}
	terminalState := string(jobset.JobSetCompleted)
	gomega.Eventually(checkJobSetStatus, timeout, interval).WithArguments(ctx, k8sClient, js, conditions).Should(gomega.Equal(true))
	gomega.Eventually(checkJobSetTerminalState, timeout, interval).WithArguments(ctx, k8sClient, js, terminalState).Should(gomega.Equal(true))
}

func JobSetFailed(ctx context.Context, k8sClient client.Client, js *jobset.JobSet, timeout time.Duration) {
	ginkgo.By(fmt.Sprintf("checking jobset status is: %s", jobset.JobSetFailed))
	conditions := []metav1.Condition{
		{
			Type:   string(jobset.JobSetFailed),
			Status: metav1.ConditionTrue,
		},
	}
	terminalState := string(jobset.JobSetFailed)
	gomega.Eventually(checkJobSetStatus, timeout, interval).WithArguments(ctx, k8sClient, js, conditions).Should(gomega.Equal(true))
	gomega.Eventually(checkJobSetTerminalState, timeout, interval).WithArguments(ctx, k8sClient, js, terminalState).Should(gomega.Equal(true))
}

func JobSetSuspended(ctx context.Context, k8sClient client.Client, js *jobset.JobSet, timeout time.Duration) {
	ginkgo.By(fmt.Sprintf("checking jobset status is: %s", jobset.JobSetSuspended))
	conditions := []metav1.Condition{
		{
			Type:   string(jobset.JobSetSuspended),
			Status: metav1.ConditionTrue,
		},
	}
	gomega.Eventually(checkJobSetStatus, timeout, interval).WithArguments(ctx, k8sClient, js, conditions).Should(gomega.Equal(true))
}

func JobSetResumed(ctx context.Context, k8sClient client.Client, js *jobset.JobSet, timeout time.Duration) {
	ginkgo.By("checking jobset status is resumed")
	conditions := []metav1.Condition{
		{
			Type:   string(jobset.JobSetSuspended),
			Status: metav1.ConditionFalse,
		},
	}
	gomega.Eventually(checkJobSetStatus, timeout, interval).WithArguments(ctx, k8sClient, js, conditions).Should(gomega.Equal(true))
}

func JobSetStartupPolicyComplete(ctx context.Context, k8sClient client.Client, js *jobset.JobSet, timeout time.Duration) {
	ginkgo.By(fmt.Sprintf("checking jobset condition %q status is %q", jobset.JobSetStartupPolicyCompleted, metav1.ConditionTrue))
	conditions := []metav1.Condition{
		{
			Type:   string(jobset.JobSetStartupPolicyCompleted),
			Status: metav1.ConditionTrue,
		},
	}
	gomega.Eventually(checkJobSetStatus, timeout, interval).WithArguments(ctx, k8sClient, js, conditions).Should(gomega.Equal(true))
}

func JobSetStartupPolicyNotFinished(ctx context.Context, k8sClient client.Client, js *jobset.JobSet, timeout time.Duration) {
	ginkgo.By(fmt.Sprintf("checking jobset condition %q status is %q", jobset.JobSetStartupPolicyCompleted, metav1.ConditionFalse))
	conditions := []metav1.Condition{
		{
			Type:   string(jobset.JobSetStartupPolicyInProgress),
			Status: metav1.ConditionTrue,
		},
	}
	gomega.Eventually(checkJobSetStatus, timeout, interval).WithArguments(ctx, k8sClient, js, conditions).Should(gomega.Equal(true))
}

func JobSetActive(ctx context.Context, k8sClient client.Client, js *jobset.JobSet, timeout time.Duration) {
	ginkgo.By("checking jobset status is active")
	gomega.Consistently(checkJobSetActive, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(true))
}

// checkJobSetActive performs a check if the JobSet is active.
// A JobSet is not active when any of the conditions JobSetFailed, JobSetComplete, or JobSetSuspended are true.
// A JobSet is otherwise considered active.
func checkJobSetActive(ctx context.Context, k8sClient client.Client, js *jobset.JobSet) (bool, error) {
	var fetchedJS jobset.JobSet
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: js.Namespace, Name: js.Name}, &fetchedJS); err != nil {
		return false, err
	}

	forbiddenTypes := []string{string(jobset.JobSetFailed), string(jobset.JobSetCompleted), string(jobset.JobSetSuspended)}

	for _, c := range fetchedJS.Status.Conditions {
		if slices.Contains(forbiddenTypes, c.Type) && c.Status == metav1.ConditionTrue {
			return false, nil
		}
	}
	return true, nil
}

// checkJobSetStatus check if the JobSet status matches the expected conditions.
func checkJobSetStatus(ctx context.Context, k8sClient client.Client, js *jobset.JobSet, conditions []metav1.Condition) (bool, error) {
	var fetchedJS jobset.JobSet
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: js.Namespace, Name: js.Name}, &fetchedJS); err != nil {
		return false, err
	}
	found := 0
	for _, want := range conditions {
		for _, c := range fetchedJS.Status.Conditions {
			if c.Type == want.Type && c.Status == want.Status {
				found += 1
			}
		}
	}
	return found == len(conditions), nil
}

// checkJobSetTerminalState check if the JobSet is in the expected terminal state.
func checkJobSetTerminalState(ctx context.Context, k8sClient client.Client, js *jobset.JobSet, terminalState string) (bool, error) {
	var fetchedJS jobset.JobSet
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: js.Namespace, Name: js.Name}, &fetchedJS); err != nil {
		return false, err
	}
	return fetchedJS.Status.TerminalState == terminalState, nil
}

// DeleteNamespace deletes all objects the tests typically create in the namespace.
func DeleteNamespace(ctx context.Context, c client.Client, ns *corev1.Namespace) error {
	if ns == nil {
		return nil
	}
	err := c.DeleteAllOf(ctx, &jobset.JobSet{}, client.InNamespace(ns.Name), client.PropagationPolicy(metav1.DeletePropagationForeground))
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	err = c.DeleteAllOf(ctx, &batchv1.Job{}, client.InNamespace(ns.Name), client.PropagationPolicy(metav1.DeletePropagationForeground))
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	err = c.DeleteAllOf(ctx, &corev1.Service{}, client.InNamespace(ns.Name), client.PropagationPolicy(metav1.DeletePropagationForeground))
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err := c.Delete(ctx, ns, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func JobsFromReplicatedJob(jobList *batchv1.JobList, rjob string) []*batchv1.Job {
	matching := make([]*batchv1.Job, 0)
	for i := 0; i < len(jobList.Items); i++ {
		if jobList.Items[i].Labels[jobset.ReplicatedJobNameKey] == rjob {
			matching = append(matching, &jobList.Items[i])
		}
	}
	return matching
}

// ExpectJobsDeletionTimestamp checks that the jobs' deletion timestamp is set or not set for the provided number of jobs.
func ExpectJobsDeletionTimestamp(ctx context.Context, c client.Client, js *jobset.JobSet, numJobs int, timeout time.Duration) {
	ginkgo.By("checking that jobset jobs deletion timestamp is set")
	gomega.Eventually(func() (bool, error) {
		var jobList batchv1.JobList
		if err := c.List(ctx, &jobList, client.InNamespace(js.Namespace)); err != nil {
			return false, err
		}
		numJobs := numJobs
		for _, job := range jobList.Items {
			if job.DeletionTimestamp != nil {
				numJobs--
			}
		}
		return numJobs == 0, nil
	}, timeout, interval).Should(gomega.Equal(true))
}

func JobSetDeleted(ctx context.Context, k8sClient client.Client, js *jobset.JobSet, timeout time.Duration) {
	ginkgo.By("checking jobset is deleted")
	gomega.Eventually(func() (bool, error) {
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: js.Namespace, Name: js.Name}, js)
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}, timeout, interval).Should(gomega.Equal(true))
}

// RemoveJobSetFinalizer removes the provided finalizer from the jobset and updates it.
func RemoveJobSetFinalizer(ctx context.Context, k8sClient client.Client, js *jobset.JobSet, finalizer string, timeout time.Duration) {
	ginkgo.By("removing jobset finalizers")
	gomega.Eventually(func() (bool, error) {
		// We get the latest version of the jobset before removing the finalizer.
		var fresh jobset.JobSet
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &fresh); err != nil {
			return false, err
		}
		removeJobSetFinalizer(&fresh, finalizer)
		if err := k8sClient.Update(ctx, &fresh); err != nil {
			return false, err
		}
		return true, nil
	}, timeout, interval).Should(gomega.Equal(true))
}

// removeJobSetFinalizer removes the provided finalizer from the jobset.
func removeJobSetFinalizer(js *jobset.JobSet, finalizer string) {
	for i, f := range js.Finalizers {
		if f == finalizer {
			js.Finalizers = append(js.Finalizers[:i], js.Finalizers[i+1:]...)
		}
	}
}
