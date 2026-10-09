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

package scheduling

import (
	"fmt"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	schedulingv1alpha3 "k8s.io/api/scheduling/v1alpha3"
	schedulingv1beta1 "k8s.io/api/scheduling/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	resourcehelper "k8s.io/component-helpers/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/controllers"
	testutil "sigs.k8s.io/jobset/test/util"
)

var _ = ginkgo.Describe("Workload-Aware Scheduling E2E", func() {

	ginkgo.It("should create per-RJ PodGroups when leaf overrides are present", func() {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "e2e-sched-perrj-"},
		}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		}()

		js := &jobset.JobSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gang-perrj",
				Namespace: ns.Name,
			},
			Spec: jobset.JobSetSpec{
				SuccessPolicy: &jobset.SuccessPolicy{Operator: jobset.OperatorAll},
				Network:       &jobset.Network{EnableDNSHostnames: boolPtr(true)},
				Scheduling: &jobset.JobSetScheduling{
					ReplicatedJobs: []jobset.ReplicatedJobScheduling{
						{
							TargetReplicatedJobs: []string{"driver"},
							SchedulingPolicy: &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{
								Basic: &schedulingv1alpha3.WorkloadCompositePodGroupBasicSchedulingPolicy{},
							},
						},
						{
							TargetReplicatedJobs: []string{"workers"},
							SchedulingPolicy: &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{
								Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{},
							},
						},
					},
				},
				ReplicatedJobs: makeE2ERJobs("driver", 1, "workers", 2),
			},
		}

		ginkgo.By("creating the JobSet")
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

		ginkgo.By("verifying Workload has per-RJ templates")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadByPrefix(g, ns.Name, js.Name)
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(2))
			g.Expect(workload.Spec.ControllerRef).NotTo(gomega.BeNil())
			g.Expect(workload.Spec.ControllerRef.Kind).To(gomega.Equal("JobSet"))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying driver PodGroup has Basic policy")
		gomega.Eventually(func(g gomega.Gomega) {
			pg := getPodGroupByPrefix(g, ns.Name, fmt.Sprintf("%s-driver", js.Name))
			g.Expect(pg.Spec.SchedulingPolicy.Basic).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.SchedulingPolicy.Gang).To(gomega.BeNil())
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying workers PodGroup has Gang policy")
		gomega.Eventually(func(g gomega.Gomega) {
			pg := getPodGroupByPrefix(g, ns.Name, fmt.Sprintf("%s-workers", js.Name))
			g.Expect(pg.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			// minGroupCount counts child groups, not pods. Both worker
			// replicas (parallelism 1 each) belong to this PodGroup.
			g.Expect(pg.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(2)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying child Jobs have per-RJ scheduling annotations")
		gomega.Eventually(func(g gomega.Gomega) {
			var jobList batchv1.JobList
			g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(ns.Name))).To(gomega.Succeed())
			g.Expect(jobList.Items).To(gomega.HaveLen(3)) // 1 driver + 2 workers
			for _, job := range jobList.Items {
				g.Expect(job.Annotations).To(gomega.HaveKey(controllers.SchedulingGroupTemplateNameKey))
				// Per-RJ template names include an identity hash suffix.
				g.Expect(job.Annotations[controllers.SchedulingGroupTemplateNameKey]).To(
					gomega.HavePrefix(job.Labels[jobset.ReplicatedJobNameKey] + "-"))
			}
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying the JobSet completes successfully")
		testutil.JobSetCompleted(ctx, k8sClient, js, timeout)
	})

	ginkgo.It("should create a single PodGroup when top-level gang with no leaf overrides", func() {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "e2e-sched-topgang-"},
		}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		}()

		js := &jobset.JobSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gang-topgang",
				Namespace: ns.Name,
			},
			Spec: jobset.JobSetSpec{
				SuccessPolicy: &jobset.SuccessPolicy{Operator: jobset.OperatorAll},
				Network:       &jobset.Network{EnableDNSHostnames: boolPtr(true)},
				Scheduling: &jobset.JobSetScheduling{
					SchedulingPolicy: &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{},
					},
					// No ReplicatedJobs → top-level gang.
				},
				ReplicatedJobs: makeE2ERJobs("driver", 1, "workers", 2),
			},
		}

		ginkgo.By("creating the JobSet with top-level gang")
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

		ginkgo.By("verifying Workload has a single PodGroupTemplate")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadByPrefix(g, ns.Name, js.Name)
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(1))
			g.Expect(workload.Spec.PodGroupTemplates[0].Name).To(gomega.HavePrefix(js.Name + "-"))
			g.Expect(workload.Spec.PodGroupTemplates[0].SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(workload.Spec.PodGroupTemplates[0].SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(3)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying a single PodGroup named after the JobSet")
		gomega.Eventually(func(g gomega.Gomega) {
			var pgList schedulingv1beta1.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(ns.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.HaveLen(1))
			g.Expect(pgList.Items[0].Name).To(gomega.HavePrefix(js.Name + "-"))
			g.Expect(pgList.Items[0].Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(pgList.Items[0].Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(3)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying child Jobs reference the JobSet name as template name")
		gomega.Eventually(func(g gomega.Gomega) {
			var jobList batchv1.JobList
			g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(ns.Name))).To(gomega.Succeed())
			g.Expect(jobList.Items).To(gomega.HaveLen(3)) // 1 driver + 2 workers
			for _, job := range jobList.Items {
				g.Expect(job.Annotations).To(gomega.HaveKeyWithValue(
					controllers.SchedulingGroupTemplateNameKey, gomega.HavePrefix(js.Name+"-")))
			}
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying the JobSet completes successfully")
		testutil.JobSetCompleted(ctx, k8sClient, js, timeout)
	})

	ginkgo.It("should create a single PodGroup with computed minCount when scheduling is empty", func() {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "e2e-sched-default-"},
		}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		}()

		js := &jobset.JobSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gang-default",
				Namespace: ns.Name,
			},
			Spec: jobset.JobSetSpec{
				SuccessPolicy:  &jobset.SuccessPolicy{Operator: jobset.OperatorAll},
				Network:        &jobset.Network{EnableDNSHostnames: boolPtr(true)},
				Scheduling:     &jobset.JobSetScheduling{}, // empty → defaults to top-level gang
				ReplicatedJobs: makeE2ERJobs("driver", 1, "workers", 2),
			},
		}

		ginkgo.By("creating the JobSet with default scheduling")
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

		ginkgo.By("verifying single PodGroup with computed minCount")
		gomega.Eventually(func(g gomega.Gomega) {
			var pgList schedulingv1beta1.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(ns.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.HaveLen(1))
			g.Expect(pgList.Items[0].Name).To(gomega.HavePrefix(js.Name + "-"))
			g.Expect(pgList.Items[0].Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			// driver: 1 replica x parallelism 1, workers: 2 replicas x parallelism 1, total=3
			g.Expect(pgList.Items[0].Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(3)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying the JobSet completes successfully")
		testutil.JobSetCompleted(ctx, k8sClient, js, timeout)
	})

	ginkgo.It("should not create scheduling objects when JobSet is suspended and recreate on resume", func() {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "e2e-sched-suspend-"},
		}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		}()

		js := &jobset.JobSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gang-suspend",
				Namespace: ns.Name,
			},
			Spec: jobset.JobSetSpec{
				Suspend:       boolPtr(true),
				SuccessPolicy: &jobset.SuccessPolicy{Operator: jobset.OperatorAll},
				Network:       &jobset.Network{EnableDNSHostnames: boolPtr(true)},
				Scheduling: &jobset.JobSetScheduling{
					SchedulingPolicy: &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{},
					},
				},
				ReplicatedJobs: []jobset.ReplicatedJob{
					{
						Name:     "workers",
						Replicas: 1,
						Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{
								Parallelism:    int32Ptr(4),
								Completions:    int32Ptr(4),
								CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										RestartPolicy: corev1.RestartPolicyNever,
										Containers: []corev1.Container{{
											Name:    "worker",
											Image:   "docker.io/library/bash:latest",
											Command: []string{"bash", "-c"},
											Args:    []string{"sleep 10"},
										}},
									},
								},
							},
						},
					},
				},
			},
		}

		ginkgo.By("creating a suspended JobSet with gang scheduling")
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

		ginkgo.By("verifying no Workload is created while suspended")
		gomega.Consistently(func(g gomega.Gomega) {
			var workloadList schedulingv1beta1.WorkloadList
			g.Expect(k8sClient.List(ctx, &workloadList, client.InNamespace(ns.Name))).To(gomega.Succeed())
			g.Expect(workloadList.Items).To(gomega.BeEmpty())
		}, "10s", interval).Should(gomega.Succeed())

		ginkgo.By("verifying no PodGroups are created while suspended")
		var pgList schedulingv1beta1.PodGroupList
		gomega.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(ns.Name))).To(gomega.Succeed())
		gomega.Expect(pgList.Items).To(gomega.BeEmpty())

		ginkgo.By("resuming the JobSet")
		gomega.Eventually(func(g gomega.Gomega) {
			var latest jobset.JobSet
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: ns.Name}, &latest)).To(gomega.Succeed())
			latest.Spec.Suspend = boolPtr(false)
			g.Expect(k8sClient.Update(ctx, &latest)).To(gomega.Succeed())
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying Workload is created after resume")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadByPrefix(g, ns.Name, js.Name)
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(1))
			g.Expect(workload.Spec.ControllerRef).NotTo(gomega.BeNil())
			g.Expect(workload.Spec.ControllerRef.Kind).To(gomega.Equal("JobSet"))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying PodGroup is created after resume with correct gang policy")
		gomega.Eventually(func(g gomega.Gomega) {
			pg := getPodGroupByPrefix(g, ns.Name, js.Name)
			g.Expect(pg.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(4)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying the JobSet completes successfully")
		testutil.JobSetCompleted(ctx, k8sClient, js, timeout)
	})

	ginkgo.It("should gang-schedule a single ReplicatedJob with multiple pods", func() {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "e2e-sched-single-rj-"},
		}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		}()

		js := &jobset.JobSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gang-single-rj",
				Namespace: ns.Name,
			},
			Spec: jobset.JobSetSpec{
				SuccessPolicy: &jobset.SuccessPolicy{Operator: jobset.OperatorAll},
				Network:       &jobset.Network{EnableDNSHostnames: boolPtr(true)},
				Scheduling: &jobset.JobSetScheduling{
					SchedulingPolicy: &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{},
					},
				},
				ReplicatedJobs: []jobset.ReplicatedJob{
					{
						Name:     "workers",
						Replicas: 1,
						Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{
								Parallelism:    int32Ptr(4),
								Completions:    int32Ptr(4),
								CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										RestartPolicy: corev1.RestartPolicyNever,
										Containers: []corev1.Container{{
											Name:    "worker",
											Image:   "docker.io/library/bash:latest",
											Command: []string{"bash", "-c"},
											Args:    []string{"sleep 10"},
										}},
									},
								},
							},
						},
					},
				},
			},
		}

		ginkgo.By("creating the JobSet with a single ReplicatedJob and gang scheduling")
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

		ginkgo.By("verifying a single PodGroup with minCount=4")
		gomega.Eventually(func(g gomega.Gomega) {
			var pgList schedulingv1beta1.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(ns.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.HaveLen(1))
			g.Expect(pgList.Items[0].Name).To(gomega.HavePrefix(js.Name + "-"))
			g.Expect(pgList.Items[0].Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(pgList.Items[0].Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(4)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying Workload has a single PodGroupTemplate")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadByPrefix(g, ns.Name, js.Name)
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(1))
			g.Expect(workload.Spec.ControllerRef).NotTo(gomega.BeNil())
			g.Expect(workload.Spec.ControllerRef.Kind).To(gomega.Equal("JobSet"))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying exactly 1 child Job with scheduling annotations")
		gomega.Eventually(func(g gomega.Gomega) {
			var jobList batchv1.JobList
			g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(ns.Name))).To(gomega.Succeed())
			g.Expect(jobList.Items).To(gomega.HaveLen(1))
			job := jobList.Items[0]
			g.Expect(job.Annotations).To(gomega.HaveKeyWithValue(
				controllers.SchedulingGroupTemplateNameKey, gomega.HavePrefix(js.Name+"-")))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying the JobSet completes successfully")
		testutil.JobSetCompleted(ctx, k8sClient, js, timeout)
	})

	ginkgo.It("should use per-RJ Gang PodGroups when DependsOn is configured with per-RJ scheduling", func() {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "e2e-sched-depends-"},
		}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		}()

		js := &jobset.JobSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gang-depends",
				Namespace: ns.Name,
			},
			Spec: jobset.JobSetSpec{
				SuccessPolicy: &jobset.SuccessPolicy{Operator: jobset.OperatorAll},
				Network:       &jobset.Network{EnableDNSHostnames: boolPtr(true)},
				// Sequenced startup requires explicit per-ReplicatedJob scheduling; a
				// top-level (or defaulted) policy with DependsOn is rejected by CEL.
				Scheduling: &jobset.JobSetScheduling{
					ReplicatedJobs: []jobset.ReplicatedJobScheduling{
						{TargetReplicatedJobs: []string{"driver"}, SchedulingPolicy: &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{}}},
						{TargetReplicatedJobs: []string{"workers"}, SchedulingPolicy: &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{}}},
					},
				},
				ReplicatedJobs: []jobset.ReplicatedJob{
					{
						Name:     "driver",
						Replicas: 1,
						Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{
								Parallelism:    int32Ptr(1),
								Completions:    int32Ptr(1),
								CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										RestartPolicy: corev1.RestartPolicyNever,
										Containers: []corev1.Container{{
											Name:    "driver",
											Image:   "docker.io/library/bash:latest",
											Command: []string{"bash", "-c"},
											Args:    []string{"sleep 10"},
										}},
									},
								},
							},
						},
					},
					{
						Name:     "workers",
						Replicas: 2,
						DependsOn: []jobset.DependsOn{
							{Name: "driver", Status: jobset.DependencyReady},
						},
						Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{
								Parallelism:    int32Ptr(1),
								Completions:    int32Ptr(1),
								CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										RestartPolicy: corev1.RestartPolicyNever,
										Containers: []corev1.Container{{
											Name:    "worker",
											Image:   "docker.io/library/bash:latest",
											Command: []string{"bash", "-c"},
											Args:    []string{"sleep 10"},
										}},
									},
								},
							},
						},
					},
				},
			},
		}

		ginkgo.By("creating the JobSet with DependsOn and per-RJ gangs")
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

		ginkgo.By("verifying Workload has per-RJ PodGroupTemplates (not a single top-level one)")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadByPrefix(g, ns.Name, js.Name)
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(2))
			templateNames := []string{
				workload.Spec.PodGroupTemplates[0].Name,
				workload.Spec.PodGroupTemplates[1].Name,
			}
			g.Expect(templateNames).To(gomega.ConsistOf(gomega.HavePrefix("driver-"), gomega.HavePrefix("workers-")))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying per-RJ PodGroups are created with correct minCounts")
		gomega.Eventually(func(g gomega.Gomega) {
			driverPG := getPodGroupByPrefix(g, ns.Name, fmt.Sprintf("%s-driver", js.Name))
			g.Expect(driverPG.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(driverPG.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(1)))

			workersPG := getPodGroupByPrefix(g, ns.Name, fmt.Sprintf("%s-workers", js.Name))
			g.Expect(workersPG.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(workersPG.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(2)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying driver Job starts first and has per-RJ annotations")
		gomega.Eventually(func(g gomega.Gomega) {
			var jobList batchv1.JobList
			g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(ns.Name))).To(gomega.Succeed())
			// At minimum the driver job should exist.
			g.Expect(len(jobList.Items)).To(gomega.BeNumerically(">=", 1))
			for _, job := range jobList.Items {
				rjName := job.Labels[jobset.ReplicatedJobNameKey]
				g.Expect(job.Annotations).To(gomega.HaveKeyWithValue(
					controllers.SchedulingGroupTemplateNameKey, gomega.HavePrefix(rjName+"-")))
			}
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying the JobSet completes successfully")
		testutil.JobSetCompleted(ctx, k8sClient, js, timeout)
	})

	ginkgo.It("should preempt low-priority JobSet when high-priority JobSet needs resources", func() {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "e2e-sched-preempt-"},
		}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		}()

		// Create PriorityClasses with generated, unique names. PriorityClasses are
		// cluster-scoped, so fixed names would not be safe to create/delete
		// concurrently with other instances of this spec (e.g. parallel Ginkgo
		// processes or retries) or other specs relying on the same names.
		lowPC := &schedulingv1.PriorityClass{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "e2e-low-priority-"},
			Value:      1,
		}
		highPC := &schedulingv1.PriorityClass{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "e2e-high-priority-"},
			Value:      100000,
		}
		gomega.Expect(k8sClient.Create(ctx, lowPC)).To(gomega.Succeed())
		gomega.Expect(k8sClient.Create(ctx, highPC)).To(gomega.Succeed())
		defer func() {
			_ = k8sClient.Delete(ctx, lowPC)
			_ = k8sClient.Delete(ctx, highPC)
		}()

		ginkgo.By("selecting a node where one gang fits but two gangs cannot coexist")
		var nodes corev1.NodeList
		var pods corev1.PodList
		gomega.Expect(k8sClient.List(ctx, &nodes)).To(gomega.Succeed())
		gomega.Expect(k8sClient.List(ctx, &pods)).To(gomega.Succeed())
		node, cpuRequest, err := preemptionResources(nodes.Items, pods.Items)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		ginkgo.GinkgoWriter.Printf("Preemption node: %s, CPU request per pod: %s\n", node.Name, cpuRequest.String())
		nodeSelector := map[string]string{corev1.LabelHostname: node.Labels[corev1.LabelHostname]}

		// Each of the four pods requests 20% of the selected node's allocatable CPU.
		// Existing requests are accounted for when selecting the node, so one gang
		// fits without relying on a fixed CPU count or preempting unrelated pods.
		lpJS := &jobset.JobSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "lp-js",
				Namespace: ns.Name,
			},
			Spec: jobset.JobSetSpec{
				SuccessPolicy: &jobset.SuccessPolicy{Operator: jobset.OperatorAll},
				Scheduling: &jobset.JobSetScheduling{
					SchedulingPolicy: &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{},
					},
					DisruptionMode: &schedulingv1alpha3.WorkloadCompositePodGroupDisruptionMode{
						All: &schedulingv1alpha3.WorkloadCompositePodGroupAllDisruptionMode{},
					},
				},
				ReplicatedJobs: []jobset.ReplicatedJob{
					{
						Name:     "workers",
						Replicas: 2,
						Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{
								Parallelism:    int32Ptr(2),
								Completions:    int32Ptr(2),
								BackoffLimit:   int32Ptr(10),
								CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										TerminationGracePeriodSeconds: int64Ptr(0),
										PriorityClassName:             lowPC.Name,
										NodeSelector:                  nodeSelector,
										RestartPolicy:                 corev1.RestartPolicyNever,
										Containers: []corev1.Container{{
											Name:    "worker",
											Image:   "docker.io/library/bash:latest",
											Command: []string{"bash", "-c"},
											Args:    []string{"sleep infinity"},
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													corev1.ResourceCPU: cpuRequest,
												},
											},
										}},
									},
								},
							},
						},
					},
				},
			},
		}

		ginkgo.By("creating the low-priority JobSet")
		gomega.Expect(k8sClient.Create(ctx, lpJS)).To(gomega.Succeed())

		ginkgo.By("waiting for all low-priority pods to be running")
		gomega.Eventually(func(g gomega.Gomega) {
			g.Expect(runningPodCount(g, ns.Name, "lp-js")).To(gomega.Equal(preemptionPodCount))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying low-priority PodGroup has disruption mode All and correct priority")
		gomega.Eventually(func(g gomega.Gomega) {
			pg := getPodGroupByPrefix(g, ns.Name, lpJS.Name)
			g.Expect(pg.Spec.DisruptionMode).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.DisruptionMode.All).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.PriorityClassName).To(gomega.Equal(lowPC.Name))
		}, timeout, interval).Should(gomega.Succeed())

		// Both gangs are constrained to the same node. Their combined CPU requests
		// exceed its allocatable CPU, so the high-priority gang requires preemption.
		hpJS := &jobset.JobSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "hp-js",
				Namespace: ns.Name,
			},
			Spec: jobset.JobSetSpec{
				SuccessPolicy: &jobset.SuccessPolicy{Operator: jobset.OperatorAll},
				Scheduling: &jobset.JobSetScheduling{
					SchedulingPolicy: &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{},
					},
					DisruptionMode: &schedulingv1alpha3.WorkloadCompositePodGroupDisruptionMode{
						All: &schedulingv1alpha3.WorkloadCompositePodGroupAllDisruptionMode{},
					},
				},
				ReplicatedJobs: []jobset.ReplicatedJob{
					{
						Name:     "workers",
						Replicas: 2,
						Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{
								Parallelism:    int32Ptr(2),
								Completions:    int32Ptr(2),
								BackoffLimit:   int32Ptr(10),
								CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										TerminationGracePeriodSeconds: int64Ptr(0),
										PriorityClassName:             highPC.Name,
										NodeSelector:                  nodeSelector,
										RestartPolicy:                 corev1.RestartPolicyNever,
										Containers: []corev1.Container{{
											Name:    "worker",
											Image:   "docker.io/library/bash:latest",
											Command: []string{"bash", "-c"},
											Args:    []string{"sleep infinity"},
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													corev1.ResourceCPU: cpuRequest,
												},
											},
										}},
									},
								},
							},
						},
					},
				},
			},
		}

		ginkgo.By("creating the high-priority JobSet")
		gomega.Expect(k8sClient.Create(ctx, hpJS)).To(gomega.Succeed())

		ginkgo.By("verifying high-priority PodGroup has correct priority and disruption")
		gomega.Eventually(func(g gomega.Gomega) {
			pg := getPodGroupByPrefix(g, ns.Name, hpJS.Name)
			g.Expect(pg.Spec.DisruptionMode).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.DisruptionMode.All).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.PriorityClassName).To(gomega.Equal(highPC.Name))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("waiting for all high-priority pods to be running")
		gomega.Eventually(func(g gomega.Gomega) {
			g.Expect(runningPodCount(g, ns.Name, "hp-js")).To(gomega.Equal(preemptionPodCount))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying low-priority pods were preempted (no longer all running)")
		gomega.Eventually(func(g gomega.Gomega) {
			running := runningPodCount(g, ns.Name, "lp-js")
			g.Expect(running).To(gomega.BeNumerically("<", preemptionPodCount))
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should patch Gang minCount in place when ElasticJobSet changes parallelism", func() {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "e2e-sched-elastic-"},
		}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		}()

		js := &jobset.JobSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gang-elastic",
				Namespace: ns.Name,
			},
			Spec: jobset.JobSetSpec{
				SuccessPolicy: &jobset.SuccessPolicy{Operator: jobset.OperatorAll},
				Network:       &jobset.Network{EnableDNSHostnames: boolPtr(true)},
				Scheduling: &jobset.JobSetScheduling{
					SchedulingPolicy: &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{},
					},
				},
				ReplicatedJobs: []jobset.ReplicatedJob{
					{
						Name:     "workers",
						Replicas: 1,
						Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{
								Parallelism:    int32Ptr(2),
								Completions:    int32Ptr(2),
								CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										RestartPolicy: corev1.RestartPolicyNever,
										Containers: []corev1.Container{{
											Name:    "worker",
											Image:   "docker.io/library/bash:latest",
											Command: []string{"bash", "-c"},
											Args:    []string{"sleep 120"},
										}},
									},
								},
							},
						},
					},
				},
			},
		}

		ginkgo.By("creating the JobSet with parallelism=2")
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

		ginkgo.By("verifying PodGroup has minCount=2")
		gomega.Eventually(func(g gomega.Gomega) {
			pg := getPodGroupByPrefix(g, ns.Name, js.Name)
			g.Expect(pg.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(2)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("recording original Workload UID")
		var originalUID types.UID
		gomega.Eventually(func(g gomega.Gomega) {
			originalUID = getWorkloadByPrefix(g, ns.Name, js.Name).UID
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("scaling parallelism and completions from 2 to 4")
		gomega.Eventually(func(g gomega.Gomega) {
			var latest jobset.JobSet
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: ns.Name}, &latest)).To(gomega.Succeed())
			latest.Spec.ReplicatedJobs[0].Template.Spec.Parallelism = int32Ptr(4)
			latest.Spec.ReplicatedJobs[0].Template.Spec.Completions = int32Ptr(4)
			g.Expect(k8sClient.Update(ctx, &latest)).To(gomega.Succeed())
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying the Workload is patched in place, keeping the same UID")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadByPrefix(g, ns.Name, js.Name)
			g.Expect(workload.UID).To(gomega.Equal(originalUID))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying PodGroup has updated minCount=4")
		gomega.Eventually(func(g gomega.Gomega) {
			pg := getPodGroupByPrefix(g, ns.Name, js.Name)
			g.Expect(pg.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(4)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying child Job parallelism and completions are updated to 4")
		gomega.Eventually(func(g gomega.Gomega) {
			var jobList batchv1.JobList
			g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(ns.Name))).To(gomega.Succeed())
			g.Expect(jobList.Items).To(gomega.HaveLen(1))
			job := jobList.Items[0]
			g.Expect(job.Spec.Parallelism).To(gomega.Equal(int32Ptr(4)), "child Job parallelism should be updated to 4")
			g.Expect(job.Spec.Completions).To(gomega.Equal(int32Ptr(4)), "child Job completions should be updated to 4")
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying 4 pods are running for the scaled Job")
		gomega.Eventually(func(g gomega.Gomega) {
			g.Expect(runningPodCount(g, ns.Name, js.Name)).To(gomega.Equal(4))
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should create one PodGroup per Job when job is set (Gang-of-Gangs per-Job model)", func() {
		// Mirrors site/static/examples/scheduling/per-ij-job-independent.yaml:
		// "launcher" shares a single per-RJ PodGroup, while "worker" uses
		// job so each of its replicas is gang-scheduled
		// independently, in its own PodGroup, instead of sharing one PodGroup
		// across every worker replica.
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "e2e-sched-per-ij-"},
		}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
		}()

		makeWorkerPod := func(name string) corev1.PodTemplateSpec {
			return corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:    name,
						Image:   "docker.io/library/bash:latest",
						Command: []string{"bash", "-c"},
						Args:    []string{"sleep 10"},
					}},
				},
			}
		}

		js := &jobset.JobSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gang-per-ij",
				Namespace: ns.Name,
			},
			Spec: jobset.JobSetSpec{
				SuccessPolicy: &jobset.SuccessPolicy{Operator: jobset.OperatorAll},
				Network:       &jobset.Network{EnableDNSHostnames: boolPtr(true)},
				Scheduling: &jobset.JobSetScheduling{
					ReplicatedJobs: []jobset.ReplicatedJobScheduling{
						{
							TargetReplicatedJobs: []string{"launcher"},
							SchedulingPolicy: &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{
								Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{},
							},
						},
						{
							TargetReplicatedJobs: []string{"worker"},
							Job: &jobset.JobScheduling{
								DisruptionMode: &schedulingv1alpha3.WorkloadPodGroupDisruptionMode{All: &schedulingv1alpha3.WorkloadPodGroupAllDisruptionMode{}},
							},
						},
					},
				},
				ReplicatedJobs: []jobset.ReplicatedJob{
					{
						Name:     "launcher",
						Replicas: 1,
						Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{
								Parallelism:    int32Ptr(1),
								Completions:    int32Ptr(1),
								CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								Template:       makeWorkerPod("launcher"),
							},
						},
					},
					{
						Name:     "worker",
						Replicas: 2,
						Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{
								Parallelism:    int32Ptr(2),
								Completions:    int32Ptr(2),
								CompletionMode: completionModePtr(batchv1.IndexedCompletion),
								Template:       makeWorkerPod("worker"),
							},
						},
					},
				},
			},
		}

		ginkgo.By("creating the JobSet")
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

		ginkgo.By("verifying the Workload has one shared launcher template and one per-Job template per worker replica")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadByPrefix(g, ns.Name, js.Name)
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(3))

			templateNames := make([]string, len(workload.Spec.PodGroupTemplates))
			for i, tmpl := range workload.Spec.PodGroupTemplates {
				templateNames[i] = tmpl.Name
			}
			g.Expect(templateNames).To(gomega.ConsistOf(
				gomega.HavePrefix("launcher-"), gomega.HavePrefix("worker-0-"), gomega.HavePrefix("worker-1-")))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying a PodGroup exists per worker Job, named after that Job, sized to its own parallelism")
		gomega.Eventually(func(g gomega.Gomega) {
			var jobList batchv1.JobList
			g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(ns.Name),
				client.MatchingLabels{jobset.ReplicatedJobNameKey: "worker"})).To(gomega.Succeed())
			g.Expect(jobList.Items).To(gomega.HaveLen(2))

			for _, job := range jobList.Items {
				// Each per-Job PodGroup is now identity-hashed; read its name
				// from the Job's scheduling group rather than assuming job.Name.
				g.Expect(job.Spec.Template.Spec.SchedulingGroup).NotTo(gomega.BeNil())
				var pg schedulingv1beta1.PodGroup
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: *job.Spec.Template.Spec.SchedulingGroup.PodGroupName, Namespace: ns.Name,
				}, &pg)).To(gomega.Succeed())
				g.Expect(pg.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
				// minCount is the Job's own parallelism (2), not the worker
				// ReplicatedJob's total pod count across both replicas (4).
				g.Expect(pg.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(2)))
			}
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying the launcher PodGroup is unaffected by the worker's per-Job policy")
		gomega.Eventually(func(g gomega.Gomega) {
			launcherPG := getPodGroupByPrefix(g, ns.Name, fmt.Sprintf("%s-launcher", js.Name))
			g.Expect(launcherPG.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(launcherPG.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(1)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying exactly 3 PodGroups exist in total (1 launcher + 2 per-Job workers)")
		gomega.Eventually(func(g gomega.Gomega) {
			var pgList schedulingv1beta1.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(ns.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.HaveLen(3))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying the JobSet completes successfully")
		testutil.JobSetCompleted(ctx, k8sClient, js, timeout)
	})
})

// makeE2ERJobs creates ReplicatedJobs in name/replicas pairs for E2E tests.
func makeE2ERJobs(args ...interface{}) []jobset.ReplicatedJob {
	var rjobs []jobset.ReplicatedJob
	for i := 0; i < len(args); i += 2 {
		name := args[i].(string)
		replicas := args[i+1].(int)
		rjobs = append(rjobs, jobset.ReplicatedJob{
			Name:     name,
			Replicas: int32(replicas),
			Template: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Parallelism:    int32Ptr(1),
					Completions:    int32Ptr(1),
					CompletionMode: completionModePtr(batchv1.IndexedCompletion),
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyNever,
							Containers: []corev1.Container{{
								Name:    name,
								Image:   "docker.io/library/bash:latest",
								Command: []string{"bash", "-c"},
								Args:    []string{"sleep 10"},
							}},
						},
					},
				},
			},
		})
	}
	return rjobs
}

func boolPtr(b bool) *bool                                               { return &b }
func int32Ptr(i int32) *int32                                            { return &i }
func completionModePtr(m batchv1.CompletionMode) *batchv1.CompletionMode { return &m }
func int64Ptr(i int64) *int64                                            { return &i }

// getWorkloadByPrefix lists Workloads in the namespace and returns the single
// one whose (identity-hashed) name begins with prefix+"-". Workload names are
// now "<jobSetName>-<hash>", so callers pass the JobSet name as the prefix
// rather than asserting an exact name.
func getWorkloadByPrefix(g gomega.Gomega, ns, prefix string) schedulingv1beta1.Workload {
	var wlList schedulingv1beta1.WorkloadList
	g.Expect(k8sClient.List(ctx, &wlList, client.InNamespace(ns))).To(gomega.Succeed())
	var matches []schedulingv1beta1.Workload
	for i := range wlList.Items {
		if strings.HasPrefix(wlList.Items[i].Name, prefix+"-") {
			matches = append(matches, wlList.Items[i])
		}
	}
	g.Expect(matches).To(gomega.HaveLen(1), "expected exactly one Workload with prefix %q, got %d", prefix, len(matches))
	return matches[0]
}

// getPodGroupByPrefix lists PodGroups in the namespace and returns the single
// one whose (identity-hashed) name begins with prefix+"-". PodGroup names are
// now "<base>-<hash>", so callers pass the logical base name as the prefix
// rather than asserting an exact name.
func getPodGroupByPrefix(g gomega.Gomega, ns, prefix string) schedulingv1beta1.PodGroup {
	var pgList schedulingv1beta1.PodGroupList
	g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(ns))).To(gomega.Succeed())
	var matches []schedulingv1beta1.PodGroup
	for i := range pgList.Items {
		if strings.HasPrefix(pgList.Items[i].Name, prefix+"-") {
			matches = append(matches, pgList.Items[i])
		}
	}
	g.Expect(matches).To(gomega.HaveLen(1), "expected exactly one PodGroup with prefix %q, got %d", prefix, len(matches))
	return matches[0]
}

const preemptionPodCount = 4

// preemptionResources selects a Ready, schedulable node with enough unrequested
// CPU for one gang. Each pod requests 20% of allocatable CPU (rounded up), leaving
// headroom for system pods while ensuring two four-pod gangs cannot coexist.
func preemptionResources(nodes []corev1.Node, pods []corev1.Pod) (*corev1.Node, resource.Quantity, error) {
	for i := range nodes {
		node := &nodes[i]
		if node.Spec.Unschedulable || node.Labels[corev1.LabelHostname] == "" {
			continue
		}
		ready := false
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				ready = true
			}
		}
		for _, taint := range node.Spec.Taints {
			if taint.Effect == corev1.TaintEffectNoSchedule || taint.Effect == corev1.TaintEffectNoExecute {
				ready = false
			}
		}
		if !ready {
			continue
		}
		allocatable := node.Status.Allocatable.Cpu().MilliValue()
		if allocatable <= 0 {
			continue
		}
		available := allocatable
		for j := range pods {
			pod := &pods[j]
			if pod.Spec.NodeName != node.Name || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
				continue
			}
			requests := resourcehelper.PodRequests(pod, resourcehelper.PodResourcesOptions{})
			available -= requests.Cpu().MilliValue()
		}
		request := (allocatable + 4) / 5
		if available >= preemptionPodCount*request {
			return node, *resource.NewMilliQuantity(request, resource.DecimalSI), nil
		}
	}
	return nil, resource.Quantity{}, fmt.Errorf("no Ready, untainted node has enough free CPU for the preemption gang")
}

// runningPodCount returns the number of running pods in the given namespace
// that match the given JobSet label.
func runningPodCount(g gomega.Gomega, ns, jsName string) int {
	var podList corev1.PodList
	g.Expect(k8sClient.List(ctx, &podList,
		client.InNamespace(ns),
		client.MatchingLabels{jobset.JobSetNameKey: jsName},
	)).To(gomega.Succeed())
	count := 0
	for _, p := range podList.Items {
		if p.Status.Phase == corev1.PodRunning {
			count++
		}
	}
	return count
}
