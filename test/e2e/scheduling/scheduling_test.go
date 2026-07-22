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

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	schedulingv1alpha3 "k8s.io/api/scheduling/v1alpha3"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
					Policy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 1},
					},
					ReplicatedJobPolicies: []jobset.ReplicatedJobSchedulingPolicy{
						{
							TargetReplicatedJob: "driver",
							Policy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
								Basic: &schedulingv1alpha3.BasicSchedulingPolicy{},
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
			var workload schedulingv1alpha3.Workload
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: js.Name, Namespace: ns.Name,
			}, &workload)).To(gomega.Succeed())
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(2))
			g.Expect(workload.Spec.ControllerRef).NotTo(gomega.BeNil())
			g.Expect(workload.Spec.ControllerRef.Kind).To(gomega.Equal("JobSet"))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying driver PodGroup has Basic policy")
		gomega.Eventually(func(g gomega.Gomega) {
			var pg schedulingv1alpha3.PodGroup
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: fmt.Sprintf("%s-driver", js.Name), Namespace: ns.Name,
			}, &pg)).To(gomega.Succeed())
			g.Expect(pg.Spec.SchedulingPolicy.Basic).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.SchedulingPolicy.Gang).To(gomega.BeNil())
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying workers PodGroup has Gang policy")
		gomega.Eventually(func(g gomega.Gomega) {
			var pg schedulingv1alpha3.PodGroup
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: fmt.Sprintf("%s-workers", js.Name), Namespace: ns.Name,
			}, &pg)).To(gomega.Succeed())
			g.Expect(pg.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(1)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying child Jobs have per-RJ scheduling annotations")
		gomega.Eventually(func(g gomega.Gomega) {
			var jobList batchv1.JobList
			g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(ns.Name))).To(gomega.Succeed())
			g.Expect(jobList.Items).To(gomega.HaveLen(3)) // 1 driver + 2 workers
			for _, job := range jobList.Items {
				g.Expect(job.Annotations).To(gomega.HaveKey(controllers.SchedulingGroupTemplateNameKey))
				g.Expect(job.Annotations).To(gomega.HaveKey(controllers.SchedulingParentCompositePodGroupKey))
				g.Expect(job.Annotations[controllers.SchedulingParentCompositePodGroupKey]).To(gomega.Equal(js.Name))
				// With leaf overrides, template name should be the RJ name, not the JS name.
				g.Expect(job.Annotations[controllers.SchedulingGroupTemplateNameKey]).NotTo(gomega.Equal(js.Name))
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
					Policy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 3},
					},
					// No ReplicatedJobPolicies → top-level gang.
				},
				ReplicatedJobs: makeE2ERJobs("driver", 1, "workers", 2),
			},
		}

		ginkgo.By("creating the JobSet with top-level gang")
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

		ginkgo.By("verifying Workload has a single PodGroupTemplate")
		gomega.Eventually(func(g gomega.Gomega) {
			var workload schedulingv1alpha3.Workload
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: js.Name, Namespace: ns.Name,
			}, &workload)).To(gomega.Succeed())
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(1))
			g.Expect(workload.Spec.PodGroupTemplates[0].Name).To(gomega.Equal(js.Name))
			g.Expect(workload.Spec.PodGroupTemplates[0].SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(workload.Spec.PodGroupTemplates[0].SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(3)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying a single PodGroup named after the JobSet")
		gomega.Eventually(func(g gomega.Gomega) {
			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(ns.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.HaveLen(1))
			g.Expect(pgList.Items[0].Name).To(gomega.Equal(js.Name))
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
					controllers.SchedulingGroupTemplateNameKey, js.Name))
				g.Expect(job.Annotations).To(gomega.HaveKeyWithValue(
					controllers.SchedulingParentCompositePodGroupKey, js.Name))
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
			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(ns.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.HaveLen(1))
			g.Expect(pgList.Items[0].Name).To(gomega.Equal(js.Name))
			g.Expect(pgList.Items[0].Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			// driver: 1*1=1, workers: 1*2=2, total=3
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
					Policy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 4},
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
			var workloadList schedulingv1alpha3.WorkloadList
			g.Expect(k8sClient.List(ctx, &workloadList, client.InNamespace(ns.Name))).To(gomega.Succeed())
			g.Expect(workloadList.Items).To(gomega.BeEmpty())
		}, "10s", interval).Should(gomega.Succeed())

		ginkgo.By("verifying no PodGroups are created while suspended")
		var pgList schedulingv1alpha3.PodGroupList
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
			var workload schedulingv1alpha3.Workload
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: js.Name, Namespace: ns.Name,
			}, &workload)).To(gomega.Succeed())
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(1))
			g.Expect(workload.Spec.ControllerRef).NotTo(gomega.BeNil())
			g.Expect(workload.Spec.ControllerRef.Kind).To(gomega.Equal("JobSet"))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying PodGroup is created after resume with correct gang policy")
		gomega.Eventually(func(g gomega.Gomega) {
			var pg schedulingv1alpha3.PodGroup
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: js.Name, Namespace: ns.Name,
			}, &pg)).To(gomega.Succeed())
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
					Policy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 4},
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
			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(ns.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.HaveLen(1))
			g.Expect(pgList.Items[0].Name).To(gomega.Equal(js.Name))
			g.Expect(pgList.Items[0].Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(pgList.Items[0].Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(4)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying Workload has a single PodGroupTemplate")
		gomega.Eventually(func(g gomega.Gomega) {
			var workload schedulingv1alpha3.Workload
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: js.Name, Namespace: ns.Name,
			}, &workload)).To(gomega.Succeed())
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
				controllers.SchedulingGroupTemplateNameKey, js.Name))
			g.Expect(job.Annotations).To(gomega.HaveKeyWithValue(
				controllers.SchedulingParentCompositePodGroupKey, js.Name))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying the JobSet completes successfully")
		testutil.JobSetCompleted(ctx, k8sClient, js, timeout)
	})

	ginkgo.It("should use per-RJ PodGroups when DependsOn is configured with top-level gang", func() {
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
				Scheduling: &jobset.JobSetScheduling{
					Policy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha3.GangSchedulingPolicy{},
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

		ginkgo.By("creating the JobSet with DependsOn and top-level gang")
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

		ginkgo.By("verifying Workload has per-RJ PodGroupTemplates (not a single top-level one)")
		gomega.Eventually(func(g gomega.Gomega) {
			var workload schedulingv1alpha3.Workload
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: js.Name, Namespace: ns.Name,
			}, &workload)).To(gomega.Succeed())
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(2))
			templateNames := []string{
				workload.Spec.PodGroupTemplates[0].Name,
				workload.Spec.PodGroupTemplates[1].Name,
			}
			g.Expect(templateNames).To(gomega.ConsistOf("driver", "workers"))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying per-RJ PodGroups are created with correct minCounts")
		gomega.Eventually(func(g gomega.Gomega) {
			var driverPG schedulingv1alpha3.PodGroup
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: fmt.Sprintf("%s-driver", js.Name), Namespace: ns.Name,
			}, &driverPG)).To(gomega.Succeed())
			g.Expect(driverPG.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(driverPG.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(1)))

			var workersPG schedulingv1alpha3.PodGroup
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: fmt.Sprintf("%s-workers", js.Name), Namespace: ns.Name,
			}, &workersPG)).To(gomega.Succeed())
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
					controllers.SchedulingGroupTemplateNameKey, rjName))
				g.Expect(job.Annotations).To(gomega.HaveKeyWithValue(
					controllers.SchedulingParentCompositePodGroupKey, js.Name))
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

		// Low-priority JobSet: 4 pods each requesting 7 CPUs = 28 CPUs.
		// This fills most of the cluster (2 workers × 16 CPUs = 32 CPUs allocatable).
		lpJS := &jobset.JobSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "lp-js",
				Namespace: ns.Name,
			},
			Spec: jobset.JobSetSpec{
				SuccessPolicy: &jobset.SuccessPolicy{Operator: jobset.OperatorAll},
				Scheduling: &jobset.JobSetScheduling{
					Policy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha3.GangSchedulingPolicy{},
					},
					Disruption: &schedulingv1alpha3.DisruptionMode{
						All: &schedulingv1alpha3.AllDisruptionMode{},
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
										RestartPolicy:                 corev1.RestartPolicyNever,
										Containers: []corev1.Container{{
											Name:    "worker",
											Image:   "docker.io/library/bash:latest",
											Command: []string{"bash", "-c"},
											Args:    []string{"sleep infinity"},
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													corev1.ResourceCPU: resource.MustParse("7"),
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
			g.Expect(podsInPhase(g, ns.Name, "lp-js", corev1.PodRunning)).To(gomega.Equal(4))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying low-priority PodGroup has disruption mode All and correct priority")
		gomega.Eventually(func(g gomega.Gomega) {
			var pg schedulingv1alpha3.PodGroup
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: lpJS.Name, Namespace: ns.Name,
			}, &pg)).To(gomega.Succeed())
			g.Expect(pg.Spec.DisruptionMode).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.DisruptionMode.All).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.PriorityClassName).To(gomega.Equal(lowPC.Name))
		}, timeout, interval).Should(gomega.Succeed())

		// High-priority JobSet: same resource footprint (28 CPUs), will require
		// preemption since 28+28 > 32 allocatable CPUs.
		hpJS := &jobset.JobSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "hp-js",
				Namespace: ns.Name,
			},
			Spec: jobset.JobSetSpec{
				SuccessPolicy: &jobset.SuccessPolicy{Operator: jobset.OperatorAll},
				Scheduling: &jobset.JobSetScheduling{
					Policy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha3.GangSchedulingPolicy{},
					},
					Disruption: &schedulingv1alpha3.DisruptionMode{
						All: &schedulingv1alpha3.AllDisruptionMode{},
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
										RestartPolicy:                 corev1.RestartPolicyNever,
										Containers: []corev1.Container{{
											Name:    "worker",
											Image:   "docker.io/library/bash:latest",
											Command: []string{"bash", "-c"},
											Args:    []string{"sleep infinity"},
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													corev1.ResourceCPU: resource.MustParse("7"),
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
			var pg schedulingv1alpha3.PodGroup
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: hpJS.Name, Namespace: ns.Name,
			}, &pg)).To(gomega.Succeed())
			g.Expect(pg.Spec.DisruptionMode).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.DisruptionMode.All).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.PriorityClassName).To(gomega.Equal(highPC.Name))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("waiting for all high-priority pods to be running")
		gomega.Eventually(func(g gomega.Gomega) {
			g.Expect(podsInPhase(g, ns.Name, "hp-js", corev1.PodRunning)).To(gomega.Equal(4))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying low-priority pods were preempted (no longer all running)")
		gomega.Eventually(func(g gomega.Gomega) {
			running := podsInPhase(g, ns.Name, "lp-js", corev1.PodRunning)
			g.Expect(running).To(gomega.BeNumerically("<", 4))
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should recreate scheduling objects when ElasticJobSet changes parallelism", func() {
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
					Policy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha3.GangSchedulingPolicy{},
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
			var pg schedulingv1alpha3.PodGroup
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: js.Name, Namespace: ns.Name,
			}, &pg)).To(gomega.Succeed())
			g.Expect(pg.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(2)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("recording original Workload UID")
		var originalWorkload schedulingv1alpha3.Workload
		gomega.Eventually(func(g gomega.Gomega) {
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: js.Name, Namespace: ns.Name,
			}, &originalWorkload)).To(gomega.Succeed())
		}, timeout, interval).Should(gomega.Succeed())
		originalUID := originalWorkload.UID

		ginkgo.By("scaling parallelism and completions from 2 to 4")
		gomega.Eventually(func(g gomega.Gomega) {
			var latest jobset.JobSet
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: ns.Name}, &latest)).To(gomega.Succeed())
			latest.Spec.ReplicatedJobs[0].Template.Spec.Parallelism = int32Ptr(4)
			latest.Spec.ReplicatedJobs[0].Template.Spec.Completions = int32Ptr(4)
			g.Expect(k8sClient.Update(ctx, &latest)).To(gomega.Succeed())
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying Workload is recreated with a new UID")
		gomega.Eventually(func(g gomega.Gomega) {
			var workload schedulingv1alpha3.Workload
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: js.Name, Namespace: ns.Name,
			}, &workload)).To(gomega.Succeed())
			g.Expect(workload.UID).NotTo(gomega.Equal(originalUID))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying PodGroup has updated minCount=4")
		gomega.Eventually(func(g gomega.Gomega) {
			var pg schedulingv1alpha3.PodGroup
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: js.Name, Namespace: ns.Name,
			}, &pg)).To(gomega.Succeed())
			g.Expect(pg.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(4)))
		}, timeout, interval).Should(gomega.Succeed())

		// TODO(https://github.com/kubernetes/kubernetes/issues/140112): Uncomment once
		// the Kubernetes API server allows changing parallelism on Jobs referencing
		// gang-scheduled PodGroups. Currently the API server rejects the patch with:
		//   "cannot change parallelism for a Job referencing gang-scheduled PodGroup"
		//
		// ginkgo.By("verifying child Job parallelism and completions are updated to 4")
		// gomega.Eventually(func(g gomega.Gomega) {
		// 	var jobList batchv1.JobList
		// 	g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(ns.Name))).To(gomega.Succeed())
		// 	g.Expect(jobList.Items).To(gomega.HaveLen(1))
		// 	job := jobList.Items[0]
		// 	g.Expect(*job.Spec.Parallelism).To(gomega.Equal(int32(4)), "child Job parallelism should be updated to 4")
		// 	g.Expect(*job.Spec.Completions).To(gomega.Equal(int32(4)), "child Job completions should be updated to 4")
		// }, timeout, interval).Should(gomega.Succeed())
		//
		// ginkgo.By("verifying 4 pods are created for the scaled Job")
		// gomega.Eventually(func(g gomega.Gomega) {
		// 	var podList corev1.PodList
		// 	g.Expect(k8sClient.List(ctx, &podList, client.InNamespace(ns.Name))).To(gomega.Succeed())
		// 	runningOrPending := 0
		// 	for _, pod := range podList.Items {
		// 		if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending {
		// 			runningOrPending++
		// 		}
		// 	}
		// 	g.Expect(runningOrPending).To(gomega.Equal(4), "expected 4 running/pending pods after scaling")
		// }, timeout, interval).Should(gomega.Succeed())
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

// podsInPhase returns the number of pods in the given namespace that are in
// the specified phase and match the given JobSet label.
func podsInPhase(g gomega.Gomega, ns, jsName string, phase corev1.PodPhase) int {
	var podList corev1.PodList
	g.Expect(k8sClient.List(ctx, &podList,
		client.InNamespace(ns),
		client.MatchingLabels{jobset.JobSetNameKey: jsName},
	)).To(gomega.Succeed())
	count := 0
	for _, p := range podList.Items {
		if p.Status.Phase == phase {
			count++
		}
	}
	return count
}
