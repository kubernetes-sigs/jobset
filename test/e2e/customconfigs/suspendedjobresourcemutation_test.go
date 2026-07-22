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

package customconfigs

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1alpha1 "sigs.k8s.io/jobset/api/config/v1alpha1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/features"
	testingutil "sigs.k8s.io/jobset/pkg/util/testing"
	"sigs.k8s.io/jobset/test/util"
)

// This test verifies that container resource requests/limits can be mutated
// on a suspended JobSet, and that the mutated values are propagated to the
// child Jobs (while still suspended) and eventually to the Pods once the
// JobSet is resumed. This is needed for integration with Kueue/DWS to
// right-size Pods before a workload is admitted.
//
// This relies on two feature gates being enabled:
//  1. JobSet's own `SuspendedJobResourceMutation` feature gate (toggled below).
//  2. The Kubernetes `MutablePodResourcesForSuspendedJobs` feature gate
//     (KEP 5440), which must be enabled on the cluster's kube-apiserver.
//     This feature gate was introduced as alpha (disabled by default) in
//     Kubernetes 1.35. Without it enabled cluster-wide, kube-apiserver will
//     reject the container resource mutation on the child Job object with an
//     "field is immutable" error, regardless of the JobSet-side feature gate.
var _ = ginkgo.Describe("SuspendedJobResourceMutation", ginkgo.Ordered, func() {
	var ns *corev1.Namespace

	ginkgo.BeforeAll(func() {
		if !clusterSupportsMutablePodResources(ctx, k8sClient) {
			ginkgo.Skip("cluster does not support MutablePodResourcesForSuspendedJobs (requires Kubernetes >= 1.35 with the alpha feature gate enabled)")
		}

		util.UpdateJobSetConfigurationAndRestart(ctx, k8sClient, defaultCfg, func(cfg *configv1alpha1.Configuration) {
			cfg.FeatureGates = map[string]bool{string(features.SuspendedJobResourceMutation): true}
		})
	})

	ginkgo.BeforeEach(func() {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "e2e-customconfigs-",
			},
		}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())

		gomega.Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: ns.Namespace, Name: ns.Name}, ns)
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.AfterEach(func() {
		if util.ShouldDumpNamespace() {
			var printer printers.YAMLPrinter

			ginkgo.By(fmt.Sprintf("dumping relevant resources in namespace %s", ns.Name))

			var jobsets jobset.JobSetList
			gomega.Expect(k8sClient.List(ctx, &jobsets, client.InNamespace(ns.Name))).To(gomega.Succeed())
			for _, js := range jobsets.Items {
				gomega.Expect(printer.PrintObj(&js, ginkgo.GinkgoWriter)).To(gomega.Succeed())
			}

			var jobs batchv1.JobList
			gomega.Expect(k8sClient.List(ctx, &jobs, client.InNamespace(ns.Name))).To(gomega.Succeed())
			for _, job := range jobs.Items {
				job.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   batchv1.SchemeGroupVersion.Group,
					Version: batchv1.SchemeGroupVersion.Version,
					Kind:    "Job",
				})
				gomega.Expect(printer.PrintObj(&job, ginkgo.GinkgoWriter)).To(gomega.Succeed())
			}
		}

		gomega.Expect(k8sClient.Delete(ctx, ns)).To(gomega.Succeed())
	})

	ginkgo.When("JobSet is suspended", func() {
		ginkgo.It("should allow mutating container resource requests and propagate them once resumed", func() {
			const containerName = "test-container"
			initialRequest := resource.MustParse("10m")
			updatedRequest := resource.MustParse("20m")

			js := suspendedResourceMutationTestJobSet(ns, containerName, initialRequest)
			jsKey := types.NamespacedName{Name: js.Name, Namespace: js.Namespace}

			ginkgo.By("creating a suspended JobSet with an initial resource request", func() {
				gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
			})

			ginkgo.By("waiting for the child Jobs to report Suspended=True and Active=0", func() {
				gomega.Eventually(func(g gomega.Gomega) {
					var jobList batchv1.JobList
					g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(ns.Name))).To(gomega.Succeed())
					g.Expect(jobList.Items).To(gomega.HaveLen(util.NumExpectedJobs(js)))
					for _, job := range jobList.Items {
						g.Expect(job.Status.Active).To(gomega.Equal(int32(0)))
						g.Expect(jobConditionStatus(&job, batchv1.JobSuspended)).To(gomega.Equal(corev1.ConditionTrue))
					}
				}, timeout, interval).Should(gomega.Succeed())
			})

			ginkgo.By("mutating the container resource requests while the JobSet is suspended", func() {
				gomega.Eventually(func(g gomega.Gomega) {
					g.Expect(k8sClient.Get(ctx, jsKey, js)).To(gomega.Succeed())
					containers := js.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec.Containers
					for i := range containers {
						containers[i].Resources.Requests = corev1.ResourceList{
							corev1.ResourceCPU: updatedRequest,
						}
					}
					g.Expect(k8sClient.Update(ctx, js)).To(gomega.Succeed())
				}, timeout, interval).Should(gomega.Succeed())
			})

			ginkgo.By("checking the JobSet spec reflects the mutated resource request while still suspended", func() {
				// Note: the mutated value is only propagated down to the child Jobs/Pods
				// when the JobSet is resumed (see resumeJob in the JobSet controller),
				// mirroring the existing behavior for annotations/labels/nodeSelector/
				// tolerations/schedulingGates. At this point we only confirm the JobSet
				// API object itself accepted the mutation while suspended.
				gomega.Expect(k8sClient.Get(ctx, jsKey, js)).To(gomega.Succeed())
				container := findContainer(js.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec.Containers, containerName)
				gomega.Expect(container).NotTo(gomega.BeNil())
				gomega.Expect(container.Resources.Requests[corev1.ResourceCPU]).To(gomega.Equal(updatedRequest))
			})

			ginkgo.By("resuming the JobSet", func() {
				gomega.Eventually(func(g gomega.Gomega) {
					g.Expect(k8sClient.Get(ctx, jsKey, js)).To(gomega.Succeed())
					js.Spec.Suspend = ptr.To(false)
					g.Expect(k8sClient.Update(ctx, js)).To(gomega.Succeed())
				}, timeout, interval).Should(gomega.Succeed())
			})

			ginkgo.By("checking the running Pods use the updated resource requests", func() {
				gomega.Eventually(func(g gomega.Gomega) {
					var podList corev1.PodList
					g.Expect(k8sClient.List(ctx, &podList, client.InNamespace(ns.Name))).To(gomega.Succeed())
					g.Expect(podList.Items).NotTo(gomega.BeEmpty())
					for _, pod := range podList.Items {
						container := findContainer(pod.Spec.Containers, containerName)
						g.Expect(container).NotTo(gomega.BeNil())
						g.Expect(container.Resources.Requests[corev1.ResourceCPU]).To(gomega.Equal(updatedRequest))
					}
				}, timeout, interval).Should(gomega.Succeed())
			})

			ginkgo.By("checking the jobset completes successfully", func() {
				util.JobSetCompleted(ctx, k8sClient, js, timeout)
			})
		})
	})
})

// jobConditionStatus returns the status of the given Job condition type, or
// corev1.ConditionUnknown if the condition is not present.
func jobConditionStatus(job *batchv1.Job, conditionType batchv1.JobConditionType) corev1.ConditionStatus {
	for _, cond := range job.Status.Conditions {
		if cond.Type == conditionType {
			return cond.Status
		}
	}
	return corev1.ConditionUnknown
}

// findContainer returns a pointer to the container with the given name, or
// nil if no such container exists.
func findContainer(containers []corev1.Container, name string) *corev1.Container {
	for i := range containers {
		if containers[i].Name == name {
			return &containers[i]
		}
	}
	return nil
}

// clusterSupportsMutablePodResources probes whether the cluster's kube-apiserver
// allows mutating container resources on a suspended Job. This requires the
// Kubernetes MutablePodResourcesForSuspendedJobs feature gate (KEP 5440),
// which is alpha (disabled by default) as of Kubernetes 1.35.
func clusterSupportsMutablePodResources(ctx context.Context, c client.Client) bool {
	probeNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "probe-mutable-resources-"},
	}
	if err := c.Create(ctx, probeNs); err != nil {
		return false
	}
	defer func() {
		_ = c.Delete(ctx, probeNs)
	}()

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "probe-mutable-resources",
			Namespace: probeNs.Name,
		},
		Spec: batchv1.JobSpec{
			Suspend: ptr.To(true),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:  "probe",
						Image: "busybox",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("10m"),
							},
						},
					}},
				},
			},
		},
	}
	if err := c.Create(ctx, job); err != nil {
		return false
	}
	defer func() {
		_ = c.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground))
	}()

	// Wait for the Job's Suspended condition to be set by the Job controller.
	// The apiserver only allows pod template resource mutation on Jobs that
	// have the Suspended condition, not merely spec.suspend=true.
	jobKey := types.NamespacedName{Name: job.Name, Namespace: job.Namespace}
	suspendedConditionReady := false
	gomega.Eventually(func(g gomega.Gomega) {
		g.Expect(c.Get(ctx, jobKey, job)).To(gomega.Succeed())
		g.Expect(jobConditionStatus(job, batchv1.JobSuspended)).To(gomega.Equal(corev1.ConditionTrue))
		suspendedConditionReady = true
	}, timeout, interval).Should(gomega.Succeed())

	if !suspendedConditionReady {
		return false
	}

	// Try mutating the container resources. If the apiserver rejects this,
	// the MutablePodResourcesForSuspendedJobs gate is not enabled.
	job.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("20m")
	return c.Update(ctx, job) == nil
}

func suspendedResourceMutationTestJobSet(ns *corev1.Namespace, containerName string, initialCPURequest resource.Quantity) *jobset.JobSet {
	jsName := "js-suspended-resource-mutation"
	rjobName := "rjob"
	replicas := 2

	js := testingutil.MakeJobSet(jsName, ns.Name).
		ReplicatedJob(testingutil.MakeReplicatedJob(rjobName).
			Job(testingutil.MakeJobTemplate("job", ns.Name).
				PodSpec(corev1.PodSpec{
					RestartPolicy: "Never",
					Containers: []corev1.Container{
						{
							Name:    containerName,
							Image:   "docker.io/library/bash:latest",
							Command: []string{"bash", "-c"},
							Args:    []string{"sleep 1"},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: initialCPURequest,
								},
							},
						},
					},
				}).Obj()).
			Replicas(int32(replicas)).
			Obj()).
		Obj()

	// Create the JobSet already suspended.
	js.Spec.Suspend = ptr.To(true)
	return js
}
