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

package util

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/apimachinery/pkg/types"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	configv1alpha1 "sigs.k8s.io/jobset/api/config/v1alpha1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
)

const (
	timeout  = 10 * time.Minute
	interval = time.Millisecond * 250
)

func NumExpectedJobs(js *jobset.JobSet) int {
	expectedJobs := 0
	for _, rjob := range js.Spec.ReplicatedJobs {
		expectedJobs += int(rjob.Replicas)
	}
	return expectedJobs
}

func NumJobs(ctx context.Context, k8sClient client.Client, js *jobset.JobSet) (int, error) {
	var jobList batchv1.JobList
	if err := k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace), client.MatchingLabels{jobset.JobSetNameKey: js.Name}); err != nil {
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
	if err := k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace), client.MatchingLabels{jobset.JobSetNameKey: js.Name}); err != nil {
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

func ShouldDumpNamespace() bool {
	return os.Getenv("JOBSET_E2E_TESTS_DUMP_NAMESPACE") == "true"
}

func getNamespace() string {
	namespace := os.Getenv("NAMESPACE")
	if namespace == "" {
		namespace = "jobset-system"
	}
	return namespace
}

func JobSetReadyForTesting(ctx context.Context, k8sClient client.Client) {
	ginkgo.By("waiting for resources to be ready for testing")
	deploymentKey := types.NamespacedName{Namespace: getNamespace(), Name: "jobset-controller-manager"}
	deployment := &appsv1.Deployment{}
	pods := &corev1.PodList{}
	gomega.Eventually(func(g gomega.Gomega) error {
		// Get controller-manager deployment.
		g.Expect(k8sClient.Get(ctx, deploymentKey, deployment)).To(gomega.Succeed())
		// Get pods matches for controller-manager deployment.
		g.Expect(k8sClient.List(ctx, pods, client.InNamespace(deploymentKey.Namespace), client.MatchingLabels(deployment.Spec.Selector.MatchLabels))).To(gomega.Succeed())
		for _, pod := range pods.Items {
			for _, cs := range pod.Status.ContainerStatuses {
				// To make sure that we don't have restarts of controller-manager.
				// If we have that's mean that something went wrong, and there is
				// no needs to continue trying check availability.
				if cs.RestartCount > 0 {
					return gomega.StopTrying(fmt.Sprintf("%q in %q has restarted %d times", cs.Name, pod.Name, cs.RestartCount))
				}
			}
		}
		// To verify that webhooks are ready, checking is deployment have condition Available=True.
		g.Expect(deployment.Status.Conditions).To(gomega.ContainElement(gomega.BeComparableTo(
			appsv1.DeploymentCondition{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
			cmpopts.IgnoreFields(appsv1.DeploymentCondition{}, "Reason", "Message", "LastUpdateTime", "LastTransitionTime")),
		))
		return nil
	}, timeout, interval).Should(gomega.Succeed())
}

func GetJobSetConfiguration(ctx context.Context, k8sClient client.Client) *configv1alpha1.Configuration {
	var cm corev1.ConfigMap
	gomega.ExpectWithOffset(1, k8sClient.Get(ctx, types.NamespacedName{
		Namespace: getNamespace(),
		Name:      "jobset-manager-config",
	}, &cm)).To(gomega.Succeed())

	cfg := &configv1alpha1.Configuration{}
	gomega.ExpectWithOffset(1, yaml.Unmarshal([]byte(cm.Data["controller_manager_config.yaml"]), cfg)).To(gomega.Succeed())
	return cfg
}

func UpdateJobSetConfigurationAndRestart(ctx context.Context, k8sClient client.Client, defaultCfg *configv1alpha1.Configuration, applyChanges func(cfg *configv1alpha1.Configuration)) {
	ginkgo.By("updating jobset controller configuration")
	cfg := defaultCfg.DeepCopy()
	applyChanges(cfg)

	data, err := yaml.Marshal(cfg)
	gomega.ExpectWithOffset(1, err).NotTo(gomega.HaveOccurred())

	var cm corev1.ConfigMap
	gomega.ExpectWithOffset(1, k8sClient.Get(ctx, types.NamespacedName{
		Namespace: getNamespace(),
		Name:      "jobset-manager-config",
	}, &cm)).To(gomega.Succeed())

	cm.Data["controller_manager_config.yaml"] = string(data)
	gomega.ExpectWithOffset(1, k8sClient.Update(ctx, &cm)).To(gomega.Succeed())

	RestartJobSetController(ctx, k8sClient)
}

func RestartJobSetController(ctx context.Context, k8sClient client.Client) {
	ginkgo.By("restarting jobset controller")
	deploymentKey := types.NamespacedName{Namespace: getNamespace(), Name: "jobset-controller-manager"}
	updateDeploymentAndWaitForProgressing(ctx, k8sClient, deploymentKey, func(deployment *appsv1.Deployment) {
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = make(map[string]string)
		}
		deployment.Spec.Template.Annotations["jobset.sigs.k8s.io/restartedAt"] = time.Now().Format(time.RFC3339)
	})
	waitForDeploymentAvailability(ctx, k8sClient, deploymentKey)
	waitForWebhookEndpointsReady(ctx, k8sClient, deploymentKey)
}

func updateDeploymentAndWaitForProgressing(ctx context.Context, k8sClient client.Client, key types.NamespacedName, applyChanges func(deployment *appsv1.Deployment)) {
	deployment := &appsv1.Deployment{}

	var beforeObservedGeneration int64
	gomega.Eventually(func(g gomega.Gomega) {
		g.Expect(k8sClient.Get(ctx, key, deployment)).To(gomega.Succeed())
		g.Expect(deployment.Generation).To(gomega.Equal(deployment.Status.ObservedGeneration))
		beforeObservedGeneration = deployment.Status.ObservedGeneration
		applyChanges(deployment)
		g.Expect(k8sClient.Update(ctx, deployment)).To(gomega.Succeed())
	}, timeout, interval).Should(gomega.Succeed())

	gomega.Eventually(func(g gomega.Gomega) {
		g.Expect(k8sClient.Get(ctx, key, deployment)).To(gomega.Succeed())
		g.Expect(deployment.Status.ObservedGeneration).NotTo(gomega.Equal(beforeObservedGeneration))
	}, timeout, interval).Should(gomega.Succeed())
}

func waitForDeploymentAvailability(ctx context.Context, k8sClient client.Client, key types.NamespacedName) {
	ginkgo.By(fmt.Sprintf("waiting for availability of deployment: %q", key))
	gomega.Eventually(func(g gomega.Gomega) {
		deployment := &appsv1.Deployment{}
		g.Expect(k8sClient.Get(ctx, key, deployment)).To(gomega.Succeed())
		desiredReplicas := *deployment.Spec.Replicas
		g.Expect(deployment.Status.ObservedGeneration).To(gomega.Equal(deployment.Generation))
		g.Expect(deployment.Status.Replicas).To(gomega.Equal(desiredReplicas))
		g.Expect(deployment.Status.UpdatedReplicas).To(gomega.Equal(desiredReplicas))
		g.Expect(deployment.Status.AvailableReplicas).To(gomega.Equal(desiredReplicas))
		// For K8s 1.35+ with DeploymentReplicaSetTerminatingReplicas feature gate.
		// On older versions, TerminatingReplicas is nil, so this is always true.
		g.Expect(ptr.Deref(deployment.Status.TerminatingReplicas, 0)).To(gomega.BeZero())
		g.Expect(deployment.Status.Conditions).To(gomega.ContainElement(gomega.BeComparableTo(
			appsv1.DeploymentCondition{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
			cmpopts.IgnoreFields(appsv1.DeploymentCondition{}, "Reason", "Message", "LastUpdateTime", "LastTransitionTime")),
		))

		selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		pods := &corev1.PodList{}
		g.Expect(k8sClient.List(ctx, pods,
			client.InNamespace(key.Namespace),
			client.MatchingLabelsSelector{Selector: selector},
		)).To(gomega.Succeed())
		readyPods := 0
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp != nil {
				continue
			}
			for _, c := range pod.Status.Conditions {
				if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
					readyPods++
				}
			}
		}
		g.Expect(int32(readyPods)).To(gomega.Equal(desiredReplicas))
	}, timeout, interval).Should(gomega.Succeed())
}

func waitForWebhookEndpointsReady(ctx context.Context, k8sClient client.Client, key types.NamespacedName) {
	ginkgo.By(fmt.Sprintf("waiting for webhook endpoints to match ready pods: %q", key))
	gomega.Eventually(func(g gomega.Gomega) {
		deployment := &appsv1.Deployment{}
		g.Expect(k8sClient.Get(ctx, key, deployment)).To(gomega.Succeed())

		selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		pods := &corev1.PodList{}
		g.Expect(k8sClient.List(ctx, pods,
			client.InNamespace(key.Namespace),
			client.MatchingLabelsSelector{Selector: selector},
		)).To(gomega.Succeed())

		readyPodIPs := sets.New[string]()
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp == nil && pod.Status.PodIP != "" {
				for _, c := range pod.Status.Conditions {
					if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
						readyPodIPs.Insert(pod.Status.PodIP)
					}
				}
			}
		}

		endpointSlices := &discoveryv1.EndpointSliceList{}
		g.Expect(k8sClient.List(ctx, endpointSlices,
			client.InNamespace(key.Namespace),
			client.MatchingLabels{discoveryv1.LabelServiceName: "jobset-webhook-service"},
		)).To(gomega.Succeed())

		endpointIPs := sets.New[string]()
		for _, slice := range endpointSlices.Items {
			for _, ep := range slice.Endpoints {
				if ep.Conditions.Ready == nil || *ep.Conditions.Ready {
					endpointIPs.Insert(ep.Addresses...)
				}
			}
		}
		g.Expect(endpointIPs).To(gomega.Equal(readyPodIPs))
	}, timeout, interval).Should(gomega.Succeed())
}
