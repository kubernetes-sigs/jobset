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
	schedulingv1alpha3 "k8s.io/api/scheduling/v1alpha3"
	schedulingv1beta1 "k8s.io/api/scheduling/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	testutil "sigs.k8s.io/jobset/test/util"
)

var _ = ginkgo.Describe("Workload-Aware Scheduling E2E", func() {

	// Each test runs in a separate namespace.
	var ns *corev1.Namespace

	ginkgo.BeforeEach(func() {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "e2e-sched-"},
		}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())
	})

	ginkgo.AfterEach(func() {
		gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
	})

	ginkgo.It("should complete a simple JobSet on a WAS-enabled cluster", func() {
		ginkgo.By("creating a simple JobSet")
		js := makeJobSet("simple-was", ns.Name, 1, 1, 1, nil)
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

		ginkgo.By("waiting for JobSet to complete")
		testutil.JobSetCompleted(ctx, k8sClient, js, timeout)
	})

	ginkgo.Context("Gang Scheduling", func() {
		// Follows: site/content/en/docs/workload-aware-scheduling/gang_scheduling.md
		// Creates a Workload + PodGroup + JobSet with gang scheduling.
		// All pods must be schedulable before any are admitted.

		ginkgo.It("should gang-schedule all pods in a JobSet", func() {
			jsName := "gang-js"
			workloadName := "gang-wl"
			pgName := "gang-pg"
			pgTemplateName := "workers"
			replicas := int32(2)
			completions := int32(2)
			totalPods := replicas * completions // 4

			ginkgo.By("creating the Workload")
			workload := &schedulingv1alpha3.Workload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workloadName,
					Namespace: ns.Name,
				},
				Spec: schedulingv1alpha3.WorkloadSpec{
					ControllerRef: &schedulingv1alpha3.TypedLocalObjectReference{
						APIGroup: jobset.GroupVersion.Group,
						Kind:     "JobSet",
						Name:     jsName,
					},
					PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{
						{
							Name: pgTemplateName,
							SchedulingPolicy: schedulingv1alpha3.PodGroupSchedulingPolicy{
								Gang: &schedulingv1alpha3.GangSchedulingPolicy{
									MinCount: totalPods,
								},
							},
						},
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, workload)).To(gomega.Succeed())

			ginkgo.By("creating the PodGroup")
			pg := &schedulingv1alpha3.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pgName,
					Namespace: ns.Name,
				},
				Spec: schedulingv1alpha3.PodGroupSpec{
					WorkloadRef: &schedulingv1alpha3.WorkloadReference{
						WorkloadName: workloadName,
						TemplateName: pgTemplateName,
					},
					SchedulingPolicy: schedulingv1alpha3.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha3.GangSchedulingPolicy{
							MinCount: totalPods,
						},
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, pg)).To(gomega.Succeed())

			ginkgo.By("creating the JobSet with pods referencing the PodGroup")
			js := makeJobSet(jsName, ns.Name, replicas, completions, completions, &pgName)
			gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

			ginkgo.By("verifying all pods are scheduled (gang semantics)")
			gomega.Eventually(func(g gomega.Gomega) {
				pods := &corev1.PodList{}
				g.Expect(k8sClient.List(ctx, pods,
					client.InNamespace(ns.Name),
					client.MatchingLabels{jobset.JobSetNameKey: jsName},
				)).To(gomega.Succeed())
				scheduledCount := 0
				for _, pod := range pods.Items {
					if pod.Spec.NodeName != "" {
						scheduledCount++
					}
				}
				g.Expect(int32(scheduledCount)).To(gomega.Equal(totalPods),
					fmt.Sprintf("expected %d pods scheduled, got %d", totalPods, scheduledCount))
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("verifying the Workload exists")
			gomega.Eventually(func(g gomega.Gomega) {
				var wl schedulingv1alpha3.Workload
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: workloadName, Namespace: ns.Name,
				}, &wl)).To(gomega.Succeed())
				g.Expect(wl.Spec.ControllerRef).NotTo(gomega.BeNil())
				g.Expect(wl.Spec.ControllerRef.Name).To(gomega.Equal(jsName))
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("verifying the PodGroup exists with gang policy")
			gomega.Eventually(func(g gomega.Gomega) {
				var podGroup schedulingv1alpha3.PodGroup
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: pgName, Namespace: ns.Name,
				}, &podGroup)).To(gomega.Succeed())
				g.Expect(podGroup.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
				g.Expect(podGroup.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(totalPods))
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("waiting for JobSet to complete")
			testutil.JobSetCompleted(ctx, k8sClient, js, timeout)
		})
	})

	ginkgo.Context("Topology Aware Scheduling", func() {
		// Follows: site/content/en/docs/workload-aware-scheduling/tas.md
		// All pods must land on nodes within the same topology domain (rack).

		ginkgo.It("should co-locate all pods within the same rack", func() {
			jsName := "tas-js"
			workloadName := "tas-wl"
			pgName := "tas-pg"
			pgTemplateName := "workers"
			replicas := int32(2)
			completions := int32(2)
			totalPods := replicas * completions // 4

			ginkgo.By("creating the Workload with gang policy")
			workload := &schedulingv1alpha3.Workload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workloadName,
					Namespace: ns.Name,
				},
				Spec: schedulingv1alpha3.WorkloadSpec{
					ControllerRef: &schedulingv1alpha3.TypedLocalObjectReference{
						APIGroup: jobset.GroupVersion.Group,
						Kind:     "JobSet",
						Name:     jsName,
					},
					PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{
						{
							Name: pgTemplateName,
							SchedulingPolicy: schedulingv1alpha3.PodGroupSchedulingPolicy{
								Gang: &schedulingv1alpha3.GangSchedulingPolicy{
									MinCount: totalPods,
								},
							},
						},
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, workload)).To(gomega.Succeed())

			ginkgo.By("creating the PodGroup with topology constraints")
			pg := &schedulingv1alpha3.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pgName,
					Namespace: ns.Name,
				},
				Spec: schedulingv1alpha3.PodGroupSpec{
					WorkloadRef: &schedulingv1alpha3.WorkloadReference{
						WorkloadName: workloadName,
						TemplateName: pgTemplateName,
					},
					SchedulingPolicy: schedulingv1alpha3.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha3.GangSchedulingPolicy{
							MinCount: totalPods,
						},
					},
					SchedulingConstraints: &schedulingv1alpha3.PodGroupSchedulingConstraints{
						Topology: []schedulingv1alpha3.TopologyConstraint{
							{Key: "topology.kubernetes.io/rack"},
						},
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, pg)).To(gomega.Succeed())

			ginkgo.By("creating the JobSet with pods referencing the PodGroup")
			js := makeJobSet(jsName, ns.Name, replicas, completions, completions, &pgName)
			gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

			ginkgo.By("verifying all pods land on nodes in the same rack")
			gomega.Eventually(func(g gomega.Gomega) {
				pods := &corev1.PodList{}
				g.Expect(k8sClient.List(ctx, pods,
					client.InNamespace(ns.Name),
					client.MatchingLabels{jobset.JobSetNameKey: jsName},
				)).To(gomega.Succeed())

				// Collect the rack labels from each pod's assigned node.
				racks := map[string]bool{}
				scheduledCount := 0
				for _, pod := range pods.Items {
					if pod.Spec.NodeName == "" {
						continue
					}
					scheduledCount++
					var node corev1.Node
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod.Spec.NodeName}, &node)).To(gomega.Succeed())
					rack, ok := node.Labels["topology.kubernetes.io/rack"]
					g.Expect(ok).To(gomega.BeTrue(),
						fmt.Sprintf("node %s missing topology.kubernetes.io/rack label", pod.Spec.NodeName))
					racks[rack] = true
				}
				g.Expect(int32(scheduledCount)).To(gomega.Equal(totalPods),
					fmt.Sprintf("expected %d pods scheduled, got %d", totalPods, scheduledCount))
				g.Expect(racks).To(gomega.HaveLen(1),
					fmt.Sprintf("expected all pods on 1 rack, found %d: %v", len(racks), racks))
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("verifying PodGroup has topology constraints")
			var podGroup schedulingv1alpha3.PodGroup
			gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: pgName, Namespace: ns.Name,
			}, &podGroup)).To(gomega.Succeed())
			gomega.Expect(podGroup.Spec.SchedulingConstraints).NotTo(gomega.BeNil())
			gomega.Expect(podGroup.Spec.SchedulingConstraints.Topology).To(gomega.HaveLen(1))
			gomega.Expect(podGroup.Spec.SchedulingConstraints.Topology[0].Key).To(gomega.Equal("topology.kubernetes.io/rack"))

			ginkgo.By("waiting for JobSet to complete")
			testutil.JobSetCompleted(ctx, k8sClient, js, timeout)
		})
	})

	ginkgo.Context("Job-Level Scheduling via Delegated PodGroup", func() {
		// Follows: site/content/en/docs/workload-aware-scheduling/job_level_scheduling.md
		//
		// Covers the "delegated" WorkloadWithJob model: a JobSet-owned child Job
		// is not a root workload (it has a controller owner, the JobSet), so the
		// upstream Job controller will not auto-create a Workload/PodGroup for it
		// on its own (see getManagementMode in kubernetes/pkg/controller/job).
		// Instead, the JobSet author:
		//   1. Creates a parent Workload whose spec.controllerRef names the
		//      JobSet (by apiGroup/kind/name) and defines a named PodGroupTemplate
		//      with the desired scheduling policy (e.g. gang).
		//   2. Sets the groupTemplateNameAnnotation on the JobSet's ReplicatedJob
		//      pod template metadata, naming that PodGroupTemplate.
		//
		// JobSet's controller copies template.metadata.annotations verbatim
		// onto the Job it creates (see constructJob in jobset_controller.go),
		// so no JobSet code changes are required to make this work: the Job
		// controller discovers the annotation, finds the parent Workload by
		// its controllerRef, and materializes a runtime PodGroup for the Job.
		//
		// The parent Workload must exist *before* the JobSet creates the Job,
		// otherwise the Job controller's one-shot "is this a new Job" check
		// will already have failed by the time the Workload appears (it only
		// creates delegated PodGroups for Jobs that have not started any pods
		// yet). Suspending the JobSet does not help: a Job that has ever
		// carried a Suspended condition is treated as not-new indefinitely.

		ginkgo.It("should create a delegated PodGroup for a JobSet-owned Job", func() {
			jsName := "delegated-js"
			workloadName := jsName
			pgTemplateName := "workers"
			replicas := int32(1)
			completions := int32(2)
			parallelism := int32(2)
			totalPods := completions

			ginkgo.By("creating the parent Workload before the JobSet exists")
			workload := &schedulingv1beta1.Workload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workloadName,
					Namespace: ns.Name,
				},
				Spec: schedulingv1beta1.WorkloadSpec{
					ControllerRef: &schedulingv1beta1.TypedLocalObjectReference{
						APIGroup: jobset.GroupVersion.Group,
						Kind:     "JobSet",
						Name:     jsName,
					},
					PodGroupTemplates: []schedulingv1beta1.PodGroupTemplate{
						{
							Name: pgTemplateName,
							SchedulingPolicy: schedulingv1beta1.PodGroupSchedulingPolicy{
								Gang: &schedulingv1beta1.GangSchedulingPolicy{
									MinCount: totalPods,
								},
							},
						},
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, workload)).To(gomega.Succeed())

			ginkgo.By("creating the JobSet with the delegation annotation on the Job template")
			js := makeJobSet(jsName, ns.Name, replicas, completions, parallelism, nil)
			js.Spec.ReplicatedJobs[0].Template.Annotations = map[string]string{
				groupTemplateNameAnnotation: pgTemplateName,
			}
			gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

			ginkgo.By("verifying the child Job received the delegation annotation")
			jobName := fmt.Sprintf("%s-%s-0", jsName, js.Spec.ReplicatedJobs[0].Name)
			gomega.Eventually(func(g gomega.Gomega) {
				var job batchv1.Job
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: jobName, Namespace: ns.Name,
				}, &job)).To(gomega.Succeed())
				g.Expect(job.Annotations).To(gomega.HaveKeyWithValue(groupTemplateNameAnnotation, pgTemplateName))
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("verifying a delegated PodGroup was created for the Job")
			gomega.Eventually(func(g gomega.Gomega) {
				pgs := &schedulingv1beta1.PodGroupList{}
				g.Expect(k8sClient.List(ctx, pgs, client.InNamespace(ns.Name))).To(gomega.Succeed())

				var owned *schedulingv1beta1.PodGroup
				for i := range pgs.Items {
					for _, ref := range pgs.Items[i].OwnerReferences {
						if ref.Kind == "Job" && ref.Name == jobName {
							owned = &pgs.Items[i]
						}
					}
				}
				g.Expect(owned).NotTo(gomega.BeNil(), "expected a PodGroup owned by Job %s", jobName)
				g.Expect(owned.Spec.WorkloadRef).NotTo(gomega.BeNil())
				g.Expect(owned.Spec.WorkloadRef.WorkloadName).To(gomega.Equal(workloadName))
				g.Expect(owned.Spec.WorkloadRef.TemplateName).To(gomega.Equal(pgTemplateName))
				g.Expect(owned.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
				g.Expect(owned.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(totalPods))
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("waiting for JobSet to complete")
			testutil.JobSetCompleted(ctx, k8sClient, js, timeout)
		})
	})

})

// groupTemplateNameAnnotation maps a JobSet-owned child Job to the named
// PodGroupTemplate on its parent Workload, delegating runtime PodGroup
// creation to the upstream Job controller. This mirrors the unexported
// k8s.io/kubernetes/pkg/apis/scheduling.GroupTemplateNameAnnotation constant,
// which is an internal (non-vendorable) package, so JobSet users must set
// this well-known annotation key directly.
const groupTemplateNameAnnotation = "scheduling.k8s.io/group-template-name"

// makeJobSet creates a JobSet with a single ReplicatedJob. If podGroupName is
// non-nil, the pod template references it via schedulingGroup.podGroupName.
func makeJobSet(name, namespace string, replicas, completions, parallelism int32, podGroupName *string) *jobset.JobSet {
	podSpec := corev1.PodSpec{
		RestartPolicy:                 corev1.RestartPolicyNever,
		TerminationGracePeriodSeconds: ptr.To(int64(0)),
		Containers: []corev1.Container{
			{
				Name:    "worker",
				Image:   "busybox",
				Command: []string{"sh", "-c", "sleep 5"},
			},
		},
	}

	if podGroupName != nil {
		podSpec.SchedulingGroup = &corev1.PodSchedulingGroup{
			PodGroupName: podGroupName,
		}
	}

	return &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: jobset.JobSetSpec{
			ReplicatedJobs: []jobset.ReplicatedJob{
				{
					Name:     "rj",
					Replicas: replicas,
					Template: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{
							Completions:  ptr.To(completions),
							Parallelism:  ptr.To(parallelism),
							BackoffLimit: ptr.To(int32(10)),
							Template: corev1.PodTemplateSpec{
								Spec: podSpec,
							},
						},
					},
				},
			},
		},
	}
}
