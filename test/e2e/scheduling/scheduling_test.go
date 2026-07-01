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
	schedulingv1alpha2 "k8s.io/api/scheduling/v1alpha2"
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
			workload := &schedulingv1alpha2.Workload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workloadName,
					Namespace: ns.Name,
				},
				Spec: schedulingv1alpha2.WorkloadSpec{
					ControllerRef: &schedulingv1alpha2.TypedLocalObjectReference{
						APIGroup: jobset.GroupVersion.Group,
						Kind:     "JobSet",
						Name:     jsName,
					},
					PodGroupTemplates: []schedulingv1alpha2.PodGroupTemplate{
						{
							Name: pgTemplateName,
							SchedulingPolicy: schedulingv1alpha2.PodGroupSchedulingPolicy{
								Gang: &schedulingv1alpha2.GangSchedulingPolicy{
									MinCount: totalPods,
								},
							},
						},
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, workload)).To(gomega.Succeed())

			ginkgo.By("creating the PodGroup")
			pg := &schedulingv1alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pgName,
					Namespace: ns.Name,
				},
				Spec: schedulingv1alpha2.PodGroupSpec{
					PodGroupTemplateRef: &schedulingv1alpha2.PodGroupTemplateReference{
						Workload: &schedulingv1alpha2.WorkloadPodGroupTemplateReference{
							WorkloadName:         workloadName,
							PodGroupTemplateName: pgTemplateName,
						},
					},
					SchedulingPolicy: schedulingv1alpha2.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha2.GangSchedulingPolicy{
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
				var wl schedulingv1alpha2.Workload
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: workloadName, Namespace: ns.Name,
				}, &wl)).To(gomega.Succeed())
				g.Expect(wl.Spec.ControllerRef).NotTo(gomega.BeNil())
				g.Expect(wl.Spec.ControllerRef.Name).To(gomega.Equal(jsName))
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("verifying the PodGroup exists with gang policy")
			gomega.Eventually(func(g gomega.Gomega) {
				var podGroup schedulingv1alpha2.PodGroup
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
			workload := &schedulingv1alpha2.Workload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workloadName,
					Namespace: ns.Name,
				},
				Spec: schedulingv1alpha2.WorkloadSpec{
					ControllerRef: &schedulingv1alpha2.TypedLocalObjectReference{
						APIGroup: jobset.GroupVersion.Group,
						Kind:     "JobSet",
						Name:     jsName,
					},
					PodGroupTemplates: []schedulingv1alpha2.PodGroupTemplate{
						{
							Name: pgTemplateName,
							SchedulingPolicy: schedulingv1alpha2.PodGroupSchedulingPolicy{
								Gang: &schedulingv1alpha2.GangSchedulingPolicy{
									MinCount: totalPods,
								},
							},
						},
					},
				},
			}
			gomega.Expect(k8sClient.Create(ctx, workload)).To(gomega.Succeed())

			ginkgo.By("creating the PodGroup with topology constraints")
			pg := &schedulingv1alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pgName,
					Namespace: ns.Name,
				},
				Spec: schedulingv1alpha2.PodGroupSpec{
					PodGroupTemplateRef: &schedulingv1alpha2.PodGroupTemplateReference{
						Workload: &schedulingv1alpha2.WorkloadPodGroupTemplateReference{
							WorkloadName:         workloadName,
							PodGroupTemplateName: pgTemplateName,
						},
					},
					SchedulingPolicy: schedulingv1alpha2.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha2.GangSchedulingPolicy{
							MinCount: totalPods,
						},
					},
					SchedulingConstraints: &schedulingv1alpha2.PodGroupSchedulingConstraints{
						Topology: []schedulingv1alpha2.TopologyConstraint{
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
			var podGroup schedulingv1alpha2.PodGroup
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

})

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
