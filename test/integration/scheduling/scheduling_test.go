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

package schedulingtest

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	schedulingv1alpha3 "k8s.io/api/scheduling/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/controllers"
	"sigs.k8s.io/jobset/pkg/features"
	testingutil "sigs.k8s.io/jobset/pkg/util/testing"
	testutil "sigs.k8s.io/jobset/test/util"
)

const (
	timeout  = 10 * time.Second
	interval = time.Millisecond * 250
)

// makeSchedulingJobSet creates a valid JobSet with the given name/namespace and scheduling config.
func makeSchedulingJobSet(name, ns string, rjobs []jobset.ReplicatedJob, scheduling *jobset.JobSetScheduling) *jobset.JobSet {
	w := testingutil.MakeJobSet(name, ns).
		EnableDNSHostnames(true).
		SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll})
	for _, rj := range rjobs {
		w = w.ReplicatedJob(rj)
	}
	js := w.Obj()
	js.Spec.Scheduling = scheduling
	return js
}

// makeRJob creates a ReplicatedJob with a valid pod spec.
func makeRJob(name string, replicas, parallelism, completions int32) jobset.ReplicatedJob {
	return testingutil.MakeReplicatedJob(name).
		Replicas(replicas).
		Job(testingutil.MakeJobTemplate("", "").
			PodSpec(testingutil.TestPodSpec).
			Parallelism(parallelism).
			Completions(completions).
			CompletionMode(batchv1.IndexedCompletion).
			Obj()).
		Obj()
}

var _ = ginkgo.Describe("Workload-Aware Scheduling integration", func() {

	ginkgo.BeforeEach(func() {
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.JobSetWorkloadAwareSchedulingAPI, true)
	})

	ginkgo.It("should create Workload and per-RJ PodGroups when leaf overrides are present", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "sched-create-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("sched-create", nsObj.Name,
			[]jobset.ReplicatedJob{
				makeRJob("driver", 1, 1, 1),
				makeRJob("workers", 4, 2, 2),
			},
			&jobset.JobSetScheduling{
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{
						TargetReplicatedJobs: []string{"driver"},
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Basic: &schedulingv1alpha3.BasicSchedulingPolicy{},
						},
					},
					{
						TargetReplicatedJobs: []string{"workers"},
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 1},
						},
					},
				},
			},
		)

		ginkgo.By("creating the JobSet with scheduling")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying Workload is created with per-RJ templates")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadForJobSet(ctx, g, nsObj.Name)
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(2))
			g.Expect(workload.Spec.ControllerRef).NotTo(gomega.BeNil())
			g.Expect(workload.Spec.ControllerRef.Kind).To(gomega.Equal("JobSet"))
			g.Expect(workload.Spec.ControllerRef.Name).To(gomega.Equal(js.Name))

			templateNames := []string{
				workload.Spec.PodGroupTemplates[0].Name,
				workload.Spec.PodGroupTemplates[1].Name,
			}
			g.Expect(templateNames).To(gomega.ConsistOf(gomega.HavePrefix("driver-"), gomega.HavePrefix("workers-")))

			g.Expect(workload.OwnerReferences).To(gomega.HaveLen(1))
			g.Expect(workload.OwnerReferences[0].Name).To(gomega.Equal(js.Name))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying per-RJ PodGroups are created")
		gomega.Eventually(func(g gomega.Gomega) {
			driverPG := getPodGroupByPrefix(ctx, g, nsObj.Name, fmt.Sprintf("%s-driver", js.Name))
			g.Expect(driverPG.Spec.SchedulingPolicy.Basic).NotTo(gomega.BeNil())
			g.Expect(driverPG.Spec.SchedulingPolicy.Gang).To(gomega.BeNil())

			workersPG := getPodGroupByPrefix(ctx, g, nsObj.Name, fmt.Sprintf("%s-workers", js.Name))
			g.Expect(workersPG.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(workersPG.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(1)))
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should generate distinct Workloads and PodGroups for two JobSets whose readable names collide", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "sched-collide-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		perRJGang := func(target string) *jobset.JobSetScheduling {
			return &jobset.JobSetScheduling{
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{
						TargetReplicatedJobs: []string{target},
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 1},
						},
					},
				},
			}
		}

		// JobSet "a-b" / ReplicatedJob "c" and JobSet "a" / ReplicatedJob "b-c"
		// both read as "a-b-c" before hashing (PodGroup name is
		// "<jobSet>-<replicatedJob>"), so without the source-identity hash their
		// per-RJ PodGroups would collide in the same namespace.
		js1 := makeSchedulingJobSet("a-b", nsObj.Name,
			[]jobset.ReplicatedJob{makeRJob("c", 1, 1, 1)}, perRJGang("c"))
		js2 := makeSchedulingJobSet("a", nsObj.Name,
			[]jobset.ReplicatedJob{makeRJob("b-c", 1, 1, 1)}, perRJGang("b-c"))

		ginkgo.By("creating both JobSets in the same namespace")
		gomega.Expect(k8sClient.Create(ctx, js1)).To(gomega.Succeed())
		gomega.Expect(k8sClient.Create(ctx, js2)).To(gomega.Succeed())

		ginkgo.By("verifying two distinct PodGroups exist, both reading as a-b-c-*")
		gomega.Eventually(func(g gomega.Gomega) {
			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.HaveLen(2))
			names := []string{pgList.Items[0].Name, pgList.Items[1].Name}
			for _, n := range names {
				g.Expect(n).To(gomega.HavePrefix("a-b-c-"))
			}
			g.Expect(names[0]).NotTo(gomega.Equal(names[1]),
				"PodGroups with identical readable names must be disambiguated by the hash")
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying two distinct Workloads exist")
		gomega.Eventually(func(g gomega.Gomega) {
			var wlList schedulingv1alpha3.WorkloadList
			g.Expect(k8sClient.List(ctx, &wlList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(wlList.Items).To(gomega.HaveLen(2))
			g.Expect(wlList.Items[0].Name).NotTo(gomega.Equal(wlList.Items[1].Name))
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should annotate child Jobs with per-RJ template name when leaf overrides are present", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "sched-annot-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("sched-annot", nsObj.Name,
			[]jobset.ReplicatedJob{makeRJob("workers", 2, 1, 1)},
			&jobset.JobSetScheduling{
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{
						TargetReplicatedJobs: []string{"workers"},
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 1},
						},
					},
				},
			},
		)

		ginkgo.By("creating the JobSet")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying child Jobs have per-RJ scheduling annotations")
		gomega.Eventually(func(g gomega.Gomega) {
			var jobList batchv1.JobList
			g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(jobList.Items).To(gomega.HaveLen(2))
			for _, job := range jobList.Items {
				g.Expect(job.Annotations).To(gomega.HaveKeyWithValue(
					controllers.SchedulingGroupTemplateNameKey, gomega.HavePrefix("workers-")))
			}
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should not create scheduling objects when scheduling is nil", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "no-sched-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("no-sched", nsObj.Name,
			[]jobset.ReplicatedJob{makeRJob("workers", 1, 1, 1)},
			nil, // no scheduling
		)

		ginkgo.By("creating the JobSet without scheduling")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying Jobs are created")
		gomega.Eventually(func(g gomega.Gomega) {
			var jobList batchv1.JobList
			g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(jobList.Items).To(gomega.HaveLen(1))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying no Workload is created")
		gomega.Consistently(func(g gomega.Gomega) {
			var workloadList schedulingv1alpha3.WorkloadList
			g.Expect(k8sClient.List(ctx, &workloadList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(workloadList.Items).To(gomega.BeEmpty())
		}, 2*time.Second, interval).Should(gomega.Succeed())

		ginkgo.By("verifying no PodGroups are created")
		gomega.Consistently(func(g gomega.Gomega) {
			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.BeEmpty())
		}, 2*time.Second, interval).Should(gomega.Succeed())

		ginkgo.By("verifying child Jobs do NOT have scheduling annotations")
		var jobList batchv1.JobList
		gomega.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
		for _, job := range jobList.Items {
			gomega.Expect(job.Annotations).NotTo(gomega.HaveKey(controllers.SchedulingGroupTemplateNameKey))
		}
	})

	ginkgo.It("should create PodGroups with Basic policy when overridden per ReplicatedJob", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "basic-ovr-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("basic-ovr", nsObj.Name,
			[]jobset.ReplicatedJob{
				makeRJob("driver", 1, 1, 1),
				makeRJob("workers", 4, 2, 2),
			},
			&jobset.JobSetScheduling{
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{
						TargetReplicatedJobs: []string{"driver"},
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Basic: &schedulingv1alpha3.BasicSchedulingPolicy{},
						},
					},
					{
						TargetReplicatedJobs: []string{"workers"},
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 1},
						},
						SchedulingConstraints: &schedulingv1alpha3.PodGroupSchedulingConstraints{
							Topology: []schedulingv1alpha3.TopologyConstraint{
								{Key: "topology.kubernetes.io/rack"},
							},
						},
						// Note: Disruption mode requires WorkloadAwarePreemption feature gate
						// on the API server, which may not be available in all test environments.
					},
				},
			},
		)

		ginkgo.By("creating the JobSet")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying driver PodGroup has Basic policy")
		gomega.Eventually(func(g gomega.Gomega) {
			driverPG := getPodGroupByPrefix(ctx, g, nsObj.Name, fmt.Sprintf("%s-driver", js.Name))
			g.Expect(driverPG.Spec.SchedulingPolicy.Basic).NotTo(gomega.BeNil())
			g.Expect(driverPG.Spec.SchedulingPolicy.Gang).To(gomega.BeNil())
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying workers PodGroup has Gang + topology + disruption")
		gomega.Eventually(func(g gomega.Gomega) {
			workersPG := getPodGroupByPrefix(ctx, g, nsObj.Name, fmt.Sprintf("%s-workers", js.Name))
			g.Expect(workersPG.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(workersPG.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(1)))
			g.Expect(workersPG.Spec.SchedulingConstraints).NotTo(gomega.BeNil())
			g.Expect(workersPG.Spec.SchedulingConstraints.Topology).To(gomega.HaveLen(1))
			g.Expect(workersPG.Spec.SchedulingConstraints.Topology[0].Key).To(gomega.Equal("topology.kubernetes.io/rack"))
			// DisruptionMode is gated by WorkloadAwarePreemption and may be stripped by the API server.
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should create one Basic PodGroup per ReplicatedJob when the top-level policy is Basic", func() {
		// UseTopLevelGang only collapses the JobSet into a single PodGroup for
		// an effective composite Gang policy (see the KEP's Controller
		// Integration section, "effective composite policy is Gang"). A
		// top-level Basic policy with no replicatedJobs overrides falls
		// through to per-ReplicatedJob mode instead, with each ReplicatedJob's
		// PodGroup inheriting Basic from the global policy.
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "basic-toplevel-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("basic-toplevel", nsObj.Name,
			[]jobset.ReplicatedJob{
				makeRJob("driver", 1, 1, 1),
				makeRJob("workers", 2, 2, 2),
			},
			&jobset.JobSetScheduling{
				SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
					Basic: &schedulingv1alpha3.BasicSchedulingPolicy{},
				},
			},
		)

		ginkgo.By("creating the JobSet with a top-level Basic policy")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying the Workload has one PodGroupTemplate per ReplicatedJob, each Basic")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadForJobSet(ctx, g, nsObj.Name)
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(2))
			for _, tmpl := range workload.Spec.PodGroupTemplates {
				g.Expect(tmpl.SchedulingPolicy.Basic).NotTo(gomega.BeNil())
				g.Expect(tmpl.SchedulingPolicy.Gang).To(gomega.BeNil())
			}
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying a Basic PodGroup exists per ReplicatedJob, not a single JobSet-wide one")
		gomega.Eventually(func(g gomega.Gomega) {
			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.HaveLen(2))
			for _, pg := range pgList.Items {
				g.Expect(pg.Name).To(gomega.SatisfyAny(
					gomega.HavePrefix(fmt.Sprintf("%s-driver-", js.Name)),
					gomega.HavePrefix(fmt.Sprintf("%s-workers-", js.Name)),
				))
				g.Expect(pg.Spec.SchedulingPolicy.Basic).NotTo(gomega.BeNil())
			}
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should set OwnerReferences on Workload and PodGroups for GC cleanup", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "cleanup-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("sched-cleanup", nsObj.Name,
			[]jobset.ReplicatedJob{makeRJob("workers", 1, 1, 1)},
			&jobset.JobSetScheduling{
				SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
					Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 1},
				},
			},
		)

		ginkgo.By("creating the JobSet")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying scheduling objects have OwnerReferences pointing to the JobSet")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadForJobSet(ctx, g, nsObj.Name)
			g.Expect(workload.OwnerReferences).To(gomega.HaveLen(1))
			g.Expect(workload.OwnerReferences[0].Name).To(gomega.Equal(js.Name))
			g.Expect(workload.OwnerReferences[0].Kind).To(gomega.Equal("JobSet"))

			// Top-level gang with no leaf overrides → single PodGroup named after the JobSet.
			pg := getPodGroupByPrefix(ctx, g, nsObj.Name, js.Name)
			g.Expect(pg.OwnerReferences).To(gomega.HaveLen(1))
			g.Expect(pg.OwnerReferences[0].Name).To(gomega.Equal(js.Name))
			g.Expect(pg.OwnerReferences[0].Kind).To(gomega.Equal("JobSet"))
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should create a single PodGroup when top-level gang with no leaf overrides", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "top-gang-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("top-gang", nsObj.Name,
			[]jobset.ReplicatedJob{
				makeRJob("driver", 1, 1, 1),
				makeRJob("workers", 4, 2, 2),
			},
			&jobset.JobSetScheduling{
				SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
					Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 5},
				},
			},
		)

		ginkgo.By("creating the JobSet with top-level gang policy")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying Workload has a single PodGroupTemplate named after the JobSet")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadForJobSet(ctx, g, nsObj.Name)
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(1))
			g.Expect(workload.Spec.PodGroupTemplates[0].Name).To(gomega.HavePrefix(js.Name + "-"))
			g.Expect(workload.Spec.PodGroupTemplates[0].SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(workload.Spec.PodGroupTemplates[0].SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(5)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying a single PodGroup is created named after the JobSet")
		gomega.Eventually(func(g gomega.Gomega) {
			pg := getPodGroupByPrefix(ctx, g, nsObj.Name, js.Name)
			g.Expect(pg.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(5)))
			g.Expect(pg.Spec.WorkloadRef).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.WorkloadRef.WorkloadName).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.WorkloadRef.TemplateName).To(gomega.HavePrefix(js.Name + "-"))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying no per-RJ PodGroups are created")
		gomega.Consistently(func(g gomega.Gomega) {
			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.HaveLen(1))
			g.Expect(pgList.Items[0].Name).To(gomega.HavePrefix(js.Name + "-"))
		}, 2*time.Second, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should create a single PodGroup with computed minCount when top-level gang has no explicit minCount", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "top-gang-default-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("top-gang-default", nsObj.Name,
			[]jobset.ReplicatedJob{
				makeRJob("driver", 1, 1, 1),
				makeRJob("workers", 2, 3, 3),
			},
			&jobset.JobSetScheduling{}, // empty scheduling → defaults to top-level gang
		)

		ginkgo.By("creating the JobSet with default scheduling")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying single PodGroup has computed minCount = sum(parallelism*replicas)")
		gomega.Eventually(func(g gomega.Gomega) {
			pg := getPodGroupByPrefix(ctx, g, nsObj.Name, js.Name)
			g.Expect(pg.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			// driver: 1*1 = 1, workers: 3*2 = 6, total = 7
			g.Expect(pg.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(7)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying only one PodGroup exists")
		gomega.Consistently(func(g gomega.Gomega) {
			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.HaveLen(1))
		}, 2*time.Second, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should annotate child Jobs with JobSet name when top-level gang is active", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "top-gang-annot-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("top-gang-annot", nsObj.Name,
			[]jobset.ReplicatedJob{
				makeRJob("driver", 1, 1, 1),
				makeRJob("workers", 2, 1, 1),
			},
			&jobset.JobSetScheduling{
				SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
					Gang: &schedulingv1alpha3.GangSchedulingPolicy{},
				},
			},
		)

		ginkgo.By("creating the JobSet")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying all child Jobs reference the top-level (JobSet-derived) template name")
		gomega.Eventually(func(g gomega.Gomega) {
			var jobList batchv1.JobList
			g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(jobList.Items).To(gomega.HaveLen(3)) // 1 driver + 2 workers
			for _, job := range jobList.Items {
				g.Expect(job.Annotations).To(gomega.HaveKeyWithValue(
					controllers.SchedulingGroupTemplateNameKey, gomega.HavePrefix(js.Name+"-")))
			}
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should not create scheduling objects when JobSet is created suspended", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "sched-suspended-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("sched-suspended", nsObj.Name,
			[]jobset.ReplicatedJob{makeRJob("workers", 2, 2, 2)},
			&jobset.JobSetScheduling{
				SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
					Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 4},
				},
			},
		)
		js.Spec.Suspend = ptr.To(true)

		ginkgo.By("creating the JobSet in suspended state")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying no Workload is created while suspended")
		gomega.Consistently(func(g gomega.Gomega) {
			var workloadList schedulingv1alpha3.WorkloadList
			g.Expect(k8sClient.List(ctx, &workloadList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(workloadList.Items).To(gomega.BeEmpty())
		}, 2*time.Second, interval).Should(gomega.Succeed())

		ginkgo.By("verifying no PodGroups are created while suspended")
		gomega.Consistently(func(g gomega.Gomega) {
			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.BeEmpty())
		}, 2*time.Second, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should recreate scheduling objects when a suspended JobSet is resumed", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "sched-resume-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("sched-resume", nsObj.Name,
			[]jobset.ReplicatedJob{makeRJob("workers", 2, 2, 2)},
			&jobset.JobSetScheduling{
				SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
					Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 4},
				},
			},
		)
		js.Spec.Suspend = ptr.To(true)

		ginkgo.By("creating the JobSet in suspended state")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying no scheduling objects exist while suspended")
		gomega.Consistently(func(g gomega.Gomega) {
			var workloadList schedulingv1alpha3.WorkloadList
			g.Expect(k8sClient.List(ctx, &workloadList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(workloadList.Items).To(gomega.BeEmpty())
		}, 2*time.Second, interval).Should(gomega.Succeed())

		ginkgo.By("resuming the JobSet")
		gomega.Eventually(func(g gomega.Gomega) {
			var latest jobset.JobSet
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: nsObj.Name}, &latest)).To(gomega.Succeed())
			latest.Spec.Suspend = ptr.To(false)
			g.Expect(k8sClient.Update(ctx, &latest)).To(gomega.Succeed())
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying Workload is created after resume")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadForJobSet(ctx, g, nsObj.Name)
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(1))
			g.Expect(workload.Spec.ControllerRef).NotTo(gomega.BeNil())
			g.Expect(workload.Spec.ControllerRef.Kind).To(gomega.Equal("JobSet"))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying PodGroup is created after resume with correct gang policy")
		gomega.Eventually(func(g gomega.Gomega) {
			pg := getPodGroupByPrefix(ctx, g, nsObj.Name, js.Name)
			g.Expect(pg.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(4)))
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should delete scheduling objects when a running JobSet is suspended", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "sched-del-suspend-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("sched-del-suspend", nsObj.Name,
			[]jobset.ReplicatedJob{makeRJob("workers", 2, 2, 2)},
			&jobset.JobSetScheduling{
				SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
					Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 4},
				},
			},
		)

		ginkgo.By("creating the JobSet (not suspended)")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying Workload and PodGroup are created")
		gomega.Eventually(func(g gomega.Gomega) {
			getWorkloadForJobSet(ctx, g, nsObj.Name)
			getPodGroupByPrefix(ctx, g, nsObj.Name, js.Name)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("suspending the JobSet")
		gomega.Eventually(func(g gomega.Gomega) {
			var latest jobset.JobSet
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: nsObj.Name}, &latest)).To(gomega.Succeed())
			latest.Spec.Suspend = ptr.To(true)
			g.Expect(k8sClient.Update(ctx, &latest)).To(gomega.Succeed())
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying Workload is deleted after suspend")
		gomega.Eventually(func(g gomega.Gomega) {
			var workloadList schedulingv1alpha3.WorkloadList
			g.Expect(k8sClient.List(ctx, &workloadList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(workloadList.Items).To(gomega.BeEmpty())
		}, timeout, interval).Should(gomega.Succeed())

		// PodGroups carry the podgroup-protection finalizer, which is normally
		// removed by a kube-controller-manager controller that doesn't run
		// under envtest. Simulate it so the Delete issued on suspend completes.
		removePodGroupProtectionFinalizers(ctx, nsObj.Name)

		ginkgo.By("verifying PodGroups are deleted after suspend")
		gomega.Eventually(func(g gomega.Gomega) {
			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.BeEmpty())
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should delete and recreate scheduling objects across suspend-resume cycle", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "sched-cycle-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("sched-cycle", nsObj.Name,
			[]jobset.ReplicatedJob{
				makeRJob("driver", 1, 1, 1),
				makeRJob("workers", 2, 2, 2),
			},
			&jobset.JobSetScheduling{
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{
						TargetReplicatedJobs: []string{"driver"},
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Basic: &schedulingv1alpha3.BasicSchedulingPolicy{},
						},
					},
					{
						TargetReplicatedJobs: []string{"workers"},
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 1},
						},
					},
				},
			},
		)

		ginkgo.By("creating the JobSet (not suspended)")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying Workload and per-RJ PodGroups exist")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadForJobSet(ctx, g, nsObj.Name)
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(2))

			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.HaveLen(2))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("recording the Workload UID before suspend")
		originalWorkload := getWorkloadForJobSet(ctx, gomega.Default, nsObj.Name)
		originalUID := originalWorkload.UID

		ginkgo.By("suspending the JobSet")
		gomega.Eventually(func(g gomega.Gomega) {
			var latest jobset.JobSet
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: nsObj.Name}, &latest)).To(gomega.Succeed())
			latest.Spec.Suspend = ptr.To(true)
			g.Expect(k8sClient.Update(ctx, &latest)).To(gomega.Succeed())
		}, timeout, interval).Should(gomega.Succeed())

		// PodGroups carry the podgroup-protection finalizer, which is normally
		// removed by a kube-controller-manager controller that doesn't run
		// under envtest. Simulate it so the Delete issued on suspend completes.
		removePodGroupProtectionFinalizers(ctx, nsObj.Name)

		ginkgo.By("verifying all scheduling objects are deleted")
		gomega.Eventually(func(g gomega.Gomega) {
			var workloadList schedulingv1alpha3.WorkloadList
			g.Expect(k8sClient.List(ctx, &workloadList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(workloadList.Items).To(gomega.BeEmpty())

			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.BeEmpty())
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("resuming the JobSet")
		gomega.Eventually(func(g gomega.Gomega) {
			var latest jobset.JobSet
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: nsObj.Name}, &latest)).To(gomega.Succeed())
			latest.Spec.Suspend = ptr.To(false)
			g.Expect(k8sClient.Update(ctx, &latest)).To(gomega.Succeed())
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying Workload is recreated with a new UID")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadForJobSet(ctx, g, nsObj.Name)
			g.Expect(workload.UID).NotTo(gomega.Equal(originalUID))
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(2))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying per-RJ PodGroups are recreated")
		gomega.Eventually(func(g gomega.Gomega) {
			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.HaveLen(2))

			pgNames := []string{pgList.Items[0].Name, pgList.Items[1].Name}
			g.Expect(pgNames).To(gomega.ConsistOf(
				gomega.HavePrefix(fmt.Sprintf("%s-driver-", js.Name)),
				gomega.HavePrefix(fmt.Sprintf("%s-workers-", js.Name)),
			))
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should use per-RJ PodGroups when DependsOn is configured with top-level gang", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "sched-depends-on-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		// Build ReplicatedJobs where workers depend on driver being ready.
		driverRJ := makeRJob("driver", 1, 1, 1)
		workersRJ := makeRJob("workers", 2, 2, 2)
		workersRJ.DependsOn = []jobset.DependsOn{
			{Name: "driver", Status: jobset.DependencyReady},
		}

		js := makeSchedulingJobSet("sched-depends", nsObj.Name,
			[]jobset.ReplicatedJob{driverRJ, workersRJ},
			&jobset.JobSetScheduling{
				SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
					Gang: &schedulingv1alpha3.GangSchedulingPolicy{},
				},
				// No per-RJ overrides — would normally use top-level gang,
				// but DependsOn should force per-RJ PodGroups.
			},
		)

		ginkgo.By("creating the JobSet with DependsOn and top-level gang")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying Workload has per-RJ PodGroupTemplates (not a single top-level one)")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadForJobSet(ctx, g, nsObj.Name)
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(2))
			templateNames := []string{
				workload.Spec.PodGroupTemplates[0].Name,
				workload.Spec.PodGroupTemplates[1].Name,
			}
			g.Expect(templateNames).To(gomega.ConsistOf(gomega.HavePrefix("driver-"), gomega.HavePrefix("workers-")))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying per-RJ PodGroups are created")
		gomega.Eventually(func(g gomega.Gomega) {
			driverPG := getPodGroupByPrefix(ctx, g, nsObj.Name, fmt.Sprintf("%s-driver", js.Name))
			g.Expect(driverPG.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(driverPG.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(1)))

			workersPG := getPodGroupByPrefix(ctx, g, nsObj.Name, fmt.Sprintf("%s-workers", js.Name))
			g.Expect(workersPG.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(workersPG.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(4)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying child Jobs have per-RJ template name annotations")
		gomega.Eventually(func(g gomega.Gomega) {
			var jobList batchv1.JobList
			g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			// At minimum the driver job should exist.
			g.Expect(len(jobList.Items)).To(gomega.BeNumerically(">=", 1))
			for _, job := range jobList.Items {
				rjName := job.Labels[jobset.ReplicatedJobNameKey]
				g.Expect(job.Annotations).To(gomega.HaveKeyWithValue(
					controllers.SchedulingGroupTemplateNameKey, gomega.HavePrefix(rjName+"-")))
			}
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should use per-RJ PodGroups when InOrder StartupPolicy is configured with top-level gang", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "sched-startup-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("sched-startup", nsObj.Name,
			[]jobset.ReplicatedJob{
				makeRJob("driver", 1, 1, 1),
				makeRJob("workers", 2, 2, 2),
			},
			&jobset.JobSetScheduling{
				SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
					Gang: &schedulingv1alpha3.GangSchedulingPolicy{},
				},
			},
		)
		js.Spec.StartupPolicy = &jobset.StartupPolicy{
			StartupPolicyOrder: jobset.InOrder,
		}

		ginkgo.By("creating the JobSet with InOrder StartupPolicy and top-level gang")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying Workload has per-RJ PodGroupTemplates")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadForJobSet(ctx, g, nsObj.Name)
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(2))
			templateNames := []string{
				workload.Spec.PodGroupTemplates[0].Name,
				workload.Spec.PodGroupTemplates[1].Name,
			}
			g.Expect(templateNames).To(gomega.ConsistOf(gomega.HavePrefix("driver-"), gomega.HavePrefix("workers-")))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying per-RJ PodGroups with correct minCount")
		gomega.Eventually(func(g gomega.Gomega) {
			driverPG := getPodGroupByPrefix(ctx, g, nsObj.Name, fmt.Sprintf("%s-driver", js.Name))
			g.Expect(driverPG.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(driverPG.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(1)))

			workersPG := getPodGroupByPrefix(ctx, g, nsObj.Name, fmt.Sprintf("%s-workers", js.Name))
			g.Expect(workersPG.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(workersPG.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(4)))
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should create one PodGroup per Job when job is set (Gang-of-Gangs per-Job model)", func() {
		// Mirrors site/static/examples/scheduling/per-ij-job-independent.yaml:
		// "launcher" shares a single per-RJ PodGroup (its leaf policy sets gang
		// directly), while "worker" uses job so that each of its
		// replicas gets its own independent PodGroup instead of sharing one.
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "sched-per-ij-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("per-ij", nsObj.Name,
			[]jobset.ReplicatedJob{
				makeRJob("launcher", 1, 1, 1),
				makeRJob("worker", 2, 2, 2),
			},
			&jobset.JobSetScheduling{
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{
						TargetReplicatedJobs: []string{"launcher"},
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{},
						},
					},
					{
						TargetReplicatedJobs: []string{"worker"},
						Job: &jobset.JobScheduling{
							DisruptionMode: &schedulingv1alpha3.DisruptionMode{All: &schedulingv1alpha3.AllDisruptionMode{}},
						},
					},
				},
			},
		)

		ginkgo.By("creating the JobSet")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying the Workload has one shared template for launcher and one per-Job template per worker replica")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadForJobSet(ctx, g, nsObj.Name)
			// 1 launcher template + 2 per-Job worker templates.
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(3))

			templateNames := make([]string, len(workload.Spec.PodGroupTemplates))
			for i, tmpl := range workload.Spec.PodGroupTemplates {
				templateNames[i] = tmpl.Name
			}
			g.Expect(templateNames).To(gomega.ConsistOf(
				gomega.HavePrefix("launcher-"),
				gomega.HavePrefix("worker-0-"),
				gomega.HavePrefix("worker-1-"),
			))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying a PodGroup exists per worker Job, named after that Job")
		gomega.Eventually(func(g gomega.Gomega) {
			var jobList batchv1.JobList
			g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(nsObj.Name),
				client.MatchingLabels{jobset.ReplicatedJobNameKey: "worker"})).To(gomega.Succeed())
			g.Expect(jobList.Items).To(gomega.HaveLen(2))

			for _, job := range jobList.Items {
				// Each worker Job references its own dedicated per-Job PodGroup by
				// the hashed name recorded in its pod template's schedulingGroup.
				g.Expect(job.Spec.Template.Spec.SchedulingGroup).NotTo(gomega.BeNil())
				g.Expect(job.Spec.Template.Spec.SchedulingGroup.PodGroupName).NotTo(gomega.BeNil())
				var pg schedulingv1alpha3.PodGroup
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: *job.Spec.Template.Spec.SchedulingGroup.PodGroupName, Namespace: nsObj.Name,
				}, &pg)).To(gomega.Succeed())

				g.Expect(pg.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
				// minCount is the Job's own parallelism (2), not the worker
				// ReplicatedJob's total pod count across both replicas (4).
				g.Expect(pg.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(2)))
			}
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying the launcher PodGroup is shared and unaffected by the worker's per-Job policy")
		gomega.Eventually(func(g gomega.Gomega) {
			launcherPG := getPodGroupByPrefix(ctx, g, nsObj.Name, fmt.Sprintf("%s-launcher", js.Name))
			g.Expect(launcherPG.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(launcherPG.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(1)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying exactly 3 PodGroups exist in total (1 launcher + 2 per-Job workers)")
		gomega.Consistently(func(g gomega.Gomega) {
			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.HaveLen(3))
		}, 2*time.Second, interval).Should(gomega.Succeed())

		ginkgo.By("verifying worker child Jobs are annotated with their own per-Job template name")
		gomega.Eventually(func(g gomega.Gomega) {
			var jobList batchv1.JobList
			g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(nsObj.Name),
				client.MatchingLabels{jobset.ReplicatedJobNameKey: "worker"})).To(gomega.Succeed())
			g.Expect(jobList.Items).To(gomega.HaveLen(2))
			seen := map[string]bool{}
			for _, job := range jobList.Items {
				templateName := job.Annotations[controllers.SchedulingGroupTemplateNameKey]
				g.Expect(templateName).To(gomega.SatisfyAny(gomega.HavePrefix("worker-0-"), gomega.HavePrefix("worker-1-")))
				seen[templateName] = true

				// The pod template's schedulingGroup should reference this Job's
				// own dedicated per-Job PodGroup, whose name is prefixed with the
				// JobSet name and suffixed with an identity hash.
				g.Expect(job.Spec.Template.Spec.SchedulingGroup).NotTo(gomega.BeNil())
				g.Expect(job.Spec.Template.Spec.SchedulingGroup.PodGroupName).NotTo(gomega.BeNil())
				g.Expect(*job.Spec.Template.Spec.SchedulingGroup.PodGroupName).To(gomega.HavePrefix(js.Name + "-"))
			}
			g.Expect(seen).To(gomega.HaveLen(2))
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should patch Gang minCount in place when ElasticJobSet changes parallelism", func() {
		ctx := context.Background()
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.ElasticJobSet, true)

		nsObj := createTestNamespace(ctx, "sched-elastic-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("sched-elastic", nsObj.Name,
			[]jobset.ReplicatedJob{makeRJob("workers", 1, 2, 2)},
			&jobset.JobSetScheduling{
				SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
					Gang: &schedulingv1alpha3.GangSchedulingPolicy{},
				},
			},
		)

		ginkgo.By("creating the JobSet with parallelism=2")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying PodGroup has minCount=2")
		gomega.Eventually(func(g gomega.Gomega) {
			pg := getPodGroupByPrefix(ctx, g, nsObj.Name, js.Name)
			g.Expect(pg.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(2)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("recording original Workload and PodGroup UIDs")
		originalWorkload := getWorkloadForJobSet(ctx, gomega.Default, nsObj.Name)
		originalWorkloadUID := originalWorkload.UID

		originalPodGroup := getPodGroupByPrefix(ctx, gomega.Default, nsObj.Name, js.Name)
		originalPodGroupUID := originalPodGroup.UID

		ginkgo.By("scaling parallelism from 2 to 4")
		gomega.Eventually(func(g gomega.Gomega) {
			var latest jobset.JobSet
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: nsObj.Name}, &latest)).To(gomega.Succeed())
			latest.Spec.ReplicatedJobs[0].Template.Spec.Parallelism = ptr.To(int32(4))
			latest.Spec.ReplicatedJobs[0].Template.Spec.Completions = ptr.To(int32(4))
			g.Expect(k8sClient.Update(ctx, &latest)).To(gomega.Succeed())
		}, timeout, interval).Should(gomega.Succeed())

		// Gang.MinCount is mutable in the upstream API, so scaling patches it in
		// place on the existing Workload and PodGroup rather than deleting and
		// recreating them: the UIDs must stay the same and the templates keep
		// their identity throughout the scale event.
		ginkgo.By("verifying the Workload is patched in place with updated minCount")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadForJobSet(ctx, g, nsObj.Name)
			g.Expect(workload.UID).To(gomega.Equal(originalWorkloadUID))
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(1))
			g.Expect(workload.Spec.PodGroupTemplates[0].SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(workload.Spec.PodGroupTemplates[0].SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(4)))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying the PodGroup is patched in place with updated minCount")
		gomega.Eventually(func(g gomega.Gomega) {
			pg := getPodGroupByPrefix(ctx, g, nsObj.Name, js.Name)
			g.Expect(pg.UID).To(gomega.Equal(originalPodGroupUID))
			g.Expect(pg.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(4)))
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should truncate and hash the generated PodGroup name for a grouped replicatedJobs target exceeding the 63-character DNS label limit", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "sched-grp-trunc-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		// Neither ReplicatedJob name alone pushes its own generated Job name
		// (<jobset-name>-<rjob-name>-<job-index>) over the 63-character limit,
		// so this JobSet passes JobSet's pre-existing job-name-length
		// validation. But the *joined* group name used to name the shared
		// PodGroup (<jobset-name>-<rjobA-name>-<rjobB-name>) is long enough to
		// overflow, since it concatenates both ReplicatedJob names together.
		rjobA := strings.Repeat("a", 29)
		rjobB := strings.Repeat("b", 29)
		groupName := controllers.SchedulingGroupName([]string{rjobA, rjobB})

		js := makeSchedulingJobSet("sched-grp-trunc", nsObj.Name,
			[]jobset.ReplicatedJob{
				makeRJob(rjobA, 1, 1, 1),
				makeRJob(rjobB, 1, 1, 1),
			},
			&jobset.JobSetScheduling{
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{
						TargetReplicatedJobs: []string{rjobA, rjobB},
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 2},
						},
					},
				},
			},
		)

		ginkgo.By("creating the JobSet with a grouped target whose joined name overflows 63 characters")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		// The group template name is hashName(groupName): since the joined group
		// name plus the identity-hash suffix overflows 63 characters, the group
		// name portion is truncated and a 10-char hash appended, yielding a
		// DNS-1123-valid label distinct from the raw joined name.
		var generatedTemplateName string
		ginkgo.By("verifying the Workload's group template name is DNS-valid and hash-suffixed")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadForJobSet(ctx, g, nsObj.Name)
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(1))
			name := workload.Spec.PodGroupTemplates[0].Name
			g.Expect(name).NotTo(gomega.BeEmpty())
			g.Expect(len(name)).To(gomega.BeNumerically("<=", 63))
			g.Expect(validation.IsDNS1123Label(name)).To(gomega.BeEmpty())
			generatedTemplateName = name
		}, timeout, interval).Should(gomega.Succeed())

		var generatedPodGroupName string
		ginkgo.By("verifying exactly one PodGroup is created with a truncated, DNS-1123-valid name")
		gomega.Eventually(func(g gomega.Gomega) {
			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.HaveLen(1))

			pg := pgList.Items[0]
			g.Expect(pg.Name).NotTo(gomega.Equal(fmt.Sprintf("%s-%s", js.Name, groupName)))
			g.Expect(len(pg.Name)).To(gomega.BeNumerically("<=", 63))
			g.Expect(validation.IsDNS1123Label(pg.Name)).To(gomega.BeEmpty())
			g.Expect(pg.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
			g.Expect(pg.Spec.SchedulingPolicy.Gang.MinCount).To(gomega.Equal(int32(2)))
			generatedPodGroupName = pg.Name
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying both ReplicatedJobs' child Jobs reference the same truncated PodGroup name")
		gomega.Eventually(func(g gomega.Gomega) {
			var jobList batchv1.JobList
			g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(jobList.Items).To(gomega.HaveLen(2))
			for _, job := range jobList.Items {
				g.Expect(job.Annotations).To(gomega.HaveKeyWithValue(
					controllers.SchedulingGroupTemplateNameKey, generatedTemplateName))
				g.Expect(job.Spec.Template.Spec.SchedulingGroup).NotTo(gomega.BeNil())
				g.Expect(job.Spec.Template.Spec.SchedulingGroup.PodGroupName).NotTo(gomega.BeNil())
				g.Expect(*job.Spec.Template.Spec.SchedulingGroup.PodGroupName).To(gomega.Equal(generatedPodGroupName))
			}
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("triggering a reconcile and verifying the generated name is stable")
		gomega.Eventually(func(g gomega.Gomega) {
			var latest jobset.JobSet
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: nsObj.Name}, &latest)).To(gomega.Succeed())
			if latest.Labels == nil {
				latest.Labels = map[string]string{}
			}
			latest.Labels["sched-grp-trunc-touch"] = "1"
			g.Expect(k8sClient.Update(ctx, &latest)).To(gomega.Succeed())
		}, timeout, interval).Should(gomega.Succeed())

		gomega.Eventually(func(g gomega.Gomega) {
			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.HaveLen(1))
			g.Expect(pgList.Items[0].Name).To(gomega.Equal(generatedPodGroupName))
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should leave a ReplicatedJob's native batch/v1 Job scheduling untouched when no top-level spec.scheduling is set", func() {
		// Mirrors site/static/examples/scheduling/no-top-level-scheduling-per-job-gang.yaml:
		// spec.scheduling is unset on the JobSet entirely, and gang scheduling
		// is instead configured directly on the "workers" ReplicatedJob's
		// JobTemplate via the native batch/v1 Job spec.scheduling field (the
		// WorkloadWithJob feature), bypassing JobSet's own scheduling API. The
		// "driver" ReplicatedJob sets no scheduling at all.
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "sched-no-top-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		driver := makeRJob("driver", 1, 1, 1)
		workers := makeRJob("workers", 2, 2, 2)
		nativeScheduling := &batchv1.JobSchedulingConfiguration{
			SchedulingPolicy: &schedulingv1alpha3.WorkloadPodGroupSchedulingPolicy{
				Gang: &schedulingv1alpha3.WorkloadPodGroupGangSchedulingPolicy{},
			},
			DisruptionMode: &schedulingv1alpha3.WorkloadPodGroupDisruptionMode{All: &schedulingv1alpha3.WorkloadPodGroupAllDisruptionMode{}},
		}
		workers.Template.Spec.Scheduling = nativeScheduling

		js := makeSchedulingJobSet("sched-no-top", nsObj.Name,
			[]jobset.ReplicatedJob{driver, workers},
			nil, // no JobSet-level scheduling
		)

		ginkgo.By("creating the JobSet with a native Job-level scheduling field but no JobSet-level scheduling")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying all Jobs are created")
		gomega.Eventually(func(g gomega.Gomega) {
			var jobList batchv1.JobList
			g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(jobList.Items).To(gomega.HaveLen(3))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying the JobSet controller creates no Workload/PodGroup of its own")
		gomega.Consistently(func(g gomega.Gomega) {
			var workloadList schedulingv1alpha3.WorkloadList
			g.Expect(k8sClient.List(ctx, &workloadList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(workloadList.Items).To(gomega.BeEmpty())

			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.BeEmpty())
		}, 2*time.Second, interval).Should(gomega.Succeed())

		ginkgo.By("verifying the workers Jobs' native spec.scheduling is left untouched and driver carries none")
		var jobList batchv1.JobList
		gomega.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(nsObj.Name),
			client.MatchingLabels{jobset.ReplicatedJobNameKey: "workers"})).To(gomega.Succeed())
		gomega.Expect(jobList.Items).To(gomega.HaveLen(2))
		for _, job := range jobList.Items {
			gomega.Expect(job.Spec.Scheduling).To(gomega.Equal(nativeScheduling))
			gomega.Expect(job.Annotations).NotTo(gomega.HaveKey(controllers.SchedulingGroupTemplateNameKey))
			gomega.Expect(job.Spec.Template.Spec.SchedulingGroup).To(gomega.BeNil())
		}

		var driverJobs batchv1.JobList
		gomega.Expect(k8sClient.List(ctx, &driverJobs, client.InNamespace(nsObj.Name),
			client.MatchingLabels{jobset.ReplicatedJobNameKey: "driver"})).To(gomega.Succeed())
		gomega.Expect(driverJobs.Items).To(gomega.HaveLen(1))
		gomega.Expect(driverJobs.Items[0].Spec.Scheduling).To(gomega.BeNil())
		gomega.Expect(driverJobs.Items[0].Annotations).NotTo(gomega.HaveKey(controllers.SchedulingGroupTemplateNameKey))
	})
})

var _ = ginkgo.Describe("Workload-Aware Scheduling DRA ResourceClaims integration", func() {

	ginkgo.BeforeEach(func() {
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.JobSetWorkloadAwareSchedulingAPI, true)
	})

	ginkgo.It("should propagate resourceClaims from ReplicatedJobScheduling to per-RJ PodGroups", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "sched-dra-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("sched-dra", nsObj.Name,
			[]jobset.ReplicatedJob{
				makeRJob("driver", 1, 1, 1),
				makeRJob("workers", 4, 2, 2),
			},
			&jobset.JobSetScheduling{
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{
						TargetReplicatedJobs: []string{"driver"},
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Basic: &schedulingv1alpha3.BasicSchedulingPolicy{},
						},
					},
					{
						TargetReplicatedJobs: []string{"workers"},
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 1},
						},
						ResourceClaims: []schedulingv1alpha3.PodGroupResourceClaim{
							{
								Name:                      "imex-channel",
								ResourceClaimTemplateName: ptr.To("imex-channel-template"),
							},
						},
					},
				},
			},
		)

		ginkgo.By("creating the JobSet with DRA resourceClaims on workers")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying Workload is created with per-RJ PodGroupTemplates")
		gomega.Eventually(func(g gomega.Gomega) {
			workload := getWorkloadForJobSet(ctx, g, nsObj.Name)
			g.Expect(workload.Spec.PodGroupTemplates).To(gomega.HaveLen(2))

			// Find the workers template and verify it carries the resourceClaims.
			var workersTemplate *schedulingv1alpha3.PodGroupTemplate
			var driverTemplate *schedulingv1alpha3.PodGroupTemplate
			for i := range workload.Spec.PodGroupTemplates {
				name := workload.Spec.PodGroupTemplates[i].Name
				switch {
				case strings.HasPrefix(name, "workers-"):
					workersTemplate = &workload.Spec.PodGroupTemplates[i]
				case strings.HasPrefix(name, "driver-"):
					driverTemplate = &workload.Spec.PodGroupTemplates[i]
				}
			}
			g.Expect(workersTemplate).NotTo(gomega.BeNil(), "workers PodGroupTemplate not found")
			g.Expect(driverTemplate).NotTo(gomega.BeNil(), "driver PodGroupTemplate not found")

			// Workers template should have the IMEX channel resourceClaim.
			g.Expect(workersTemplate.ResourceClaims).To(gomega.HaveLen(1))
			g.Expect(workersTemplate.ResourceClaims[0].Name).To(gomega.Equal("imex-channel"))
			g.Expect(workersTemplate.ResourceClaims[0].ResourceClaimTemplateName).NotTo(gomega.BeNil())
			g.Expect(*workersTemplate.ResourceClaims[0].ResourceClaimTemplateName).To(gomega.Equal("imex-channel-template"))

			// Driver template should have no resourceClaims.
			g.Expect(driverTemplate.ResourceClaims).To(gomega.BeEmpty())
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying workers PodGroup carries the resourceClaims")
		gomega.Eventually(func(g gomega.Gomega) {
			workersPG := getPodGroupByPrefix(ctx, g, nsObj.Name, fmt.Sprintf("%s-workers", js.Name))

			// PodGroup should have the resourceClaims from the leaf policy.
			g.Expect(workersPG.Spec.ResourceClaims).To(gomega.HaveLen(1))
			g.Expect(workersPG.Spec.ResourceClaims[0].Name).To(gomega.Equal("imex-channel"))
			g.Expect(workersPG.Spec.ResourceClaims[0].ResourceClaimTemplateName).NotTo(gomega.BeNil())
			g.Expect(*workersPG.Spec.ResourceClaims[0].ResourceClaimTemplateName).To(gomega.Equal("imex-channel-template"))

			g.Expect(workersPG.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying driver PodGroup has no resourceClaims")
		gomega.Eventually(func(g gomega.Gomega) {
			driverPG := getPodGroupByPrefix(ctx, g, nsObj.Name, fmt.Sprintf("%s-driver", js.Name))

			g.Expect(driverPG.Spec.ResourceClaims).To(gomega.BeEmpty())
			g.Expect(driverPG.Spec.SchedulingPolicy.Basic).NotTo(gomega.BeNil())
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should propagate resourceClaims with direct ResourceClaimName reference", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "sched-dra-ref-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("sched-dra-ref", nsObj.Name,
			[]jobset.ReplicatedJob{
				makeRJob("workers", 2, 2, 2),
			},
			&jobset.JobSetScheduling{
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{
						TargetReplicatedJobs: []string{"workers"},
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 1},
						},
						ResourceClaims: []schedulingv1alpha3.PodGroupResourceClaim{
							{
								Name:              "shared-tpu-slice",
								ResourceClaimName: ptr.To("pre-allocated-tpu"),
							},
						},
					},
				},
			},
		)

		ginkgo.By("creating the JobSet with a direct ResourceClaimName reference")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying workers PodGroup carries the direct ResourceClaimName")
		gomega.Eventually(func(g gomega.Gomega) {
			workersPG := getPodGroupByPrefix(ctx, g, nsObj.Name, fmt.Sprintf("%s-workers", js.Name))

			g.Expect(workersPG.Spec.ResourceClaims).To(gomega.HaveLen(1))
			g.Expect(workersPG.Spec.ResourceClaims[0].Name).To(gomega.Equal("shared-tpu-slice"))
			g.Expect(workersPG.Spec.ResourceClaims[0].ResourceClaimName).NotTo(gomega.BeNil())
			g.Expect(*workersPG.Spec.ResourceClaims[0].ResourceClaimName).To(gomega.Equal("pre-allocated-tpu"))
			g.Expect(workersPG.Spec.ResourceClaims[0].ResourceClaimTemplateName).To(gomega.BeNil())
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should propagate multiple resourceClaims to a single PodGroup", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "sched-dra-multi-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("sched-dra-multi", nsObj.Name,
			[]jobset.ReplicatedJob{
				makeRJob("workers", 2, 2, 2),
			},
			&jobset.JobSetScheduling{
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{
						TargetReplicatedJobs: []string{"workers"},
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{},
						},
						SchedulingConstraints: &schedulingv1alpha3.PodGroupSchedulingConstraints{
							Topology: []schedulingv1alpha3.TopologyConstraint{
								{Key: "topology.kubernetes.io/rack"},
							},
						},
						ResourceClaims: []schedulingv1alpha3.PodGroupResourceClaim{
							{
								Name:                      "imex-channel",
								ResourceClaimTemplateName: ptr.To("imex-template"),
							},
							{
								Name:              "shared-network",
								ResourceClaimName: ptr.To("existing-network-claim"),
							},
						},
					},
				},
			},
		)

		ginkgo.By("creating the JobSet with multiple resourceClaims and topology constraints")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying workers PodGroup has both resourceClaims and topology constraints")
		gomega.Eventually(func(g gomega.Gomega) {
			workersPG := getPodGroupByPrefix(ctx, g, nsObj.Name, fmt.Sprintf("%s-workers", js.Name))

			// Verify both resourceClaims are present.
			g.Expect(workersPG.Spec.ResourceClaims).To(gomega.HaveLen(2))

			claimNames := []string{
				workersPG.Spec.ResourceClaims[0].Name,
				workersPG.Spec.ResourceClaims[1].Name,
			}
			g.Expect(claimNames).To(gomega.ConsistOf("imex-channel", "shared-network"))

			// Verify topology constraints are also present.
			g.Expect(workersPG.Spec.SchedulingConstraints).NotTo(gomega.BeNil())
			g.Expect(workersPG.Spec.SchedulingConstraints.Topology).To(gomega.HaveLen(1))
			g.Expect(workersPG.Spec.SchedulingConstraints.Topology[0].Key).To(gomega.Equal("topology.kubernetes.io/rack"))

			// Verify gang policy is inherited.
			g.Expect(workersPG.Spec.SchedulingPolicy.Gang).NotTo(gomega.BeNil())
		}, timeout, interval).Should(gomega.Succeed())
	})

	ginkgo.It("should preserve resourceClaims across suspend-resume cycle", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "sched-dra-resume-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("sched-dra-resume", nsObj.Name,
			[]jobset.ReplicatedJob{
				makeRJob("workers", 2, 2, 2),
			},
			&jobset.JobSetScheduling{
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{
						TargetReplicatedJobs: []string{"workers"},
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 1},
						},
						ResourceClaims: []schedulingv1alpha3.PodGroupResourceClaim{
							{
								Name:                      "tpu-slice",
								ResourceClaimTemplateName: ptr.To("tpu-slice-template"),
							},
						},
					},
				},
			},
		)

		ginkgo.By("creating the JobSet")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying PodGroup has resourceClaims")
		gomega.Eventually(func(g gomega.Gomega) {
			workersPG := getPodGroupByPrefix(ctx, g, nsObj.Name, fmt.Sprintf("%s-workers", js.Name))
			g.Expect(workersPG.Spec.ResourceClaims).To(gomega.HaveLen(1))
			g.Expect(workersPG.Spec.ResourceClaims[0].Name).To(gomega.Equal("tpu-slice"))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("suspending the JobSet")
		gomega.Eventually(func(g gomega.Gomega) {
			var latest jobset.JobSet
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: nsObj.Name}, &latest)).To(gomega.Succeed())
			latest.Spec.Suspend = ptr.To(true)
			g.Expect(k8sClient.Update(ctx, &latest)).To(gomega.Succeed())
		}, timeout, interval).Should(gomega.Succeed())

		// PodGroups carry the podgroup-protection finalizer, which is normally
		// removed by a kube-controller-manager controller that doesn't run
		// under envtest. Simulate it so the Delete issued on suspend completes.
		removePodGroupProtectionFinalizers(ctx, nsObj.Name)

		ginkgo.By("verifying scheduling objects are cleaned up on suspend")
		gomega.Eventually(func(g gomega.Gomega) {
			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.BeEmpty())
			var wlList schedulingv1alpha3.WorkloadList
			g.Expect(k8sClient.List(ctx, &wlList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(wlList.Items).To(gomega.BeEmpty())
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("resuming the JobSet")
		gomega.Eventually(func(g gomega.Gomega) {
			var latest jobset.JobSet
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: nsObj.Name}, &latest)).To(gomega.Succeed())
			latest.Spec.Suspend = ptr.To(false)
			g.Expect(k8sClient.Update(ctx, &latest)).To(gomega.Succeed())
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying PodGroup is recreated with resourceClaims preserved")
		gomega.Eventually(func(g gomega.Gomega) {
			workersPG := getPodGroupByPrefix(ctx, g, nsObj.Name, fmt.Sprintf("%s-workers", js.Name))

			g.Expect(workersPG.Spec.ResourceClaims).To(gomega.HaveLen(1))
			g.Expect(workersPG.Spec.ResourceClaims[0].Name).To(gomega.Equal("tpu-slice"))
			g.Expect(workersPG.Spec.ResourceClaims[0].ResourceClaimTemplateName).NotTo(gomega.BeNil())
			g.Expect(*workersPG.Spec.ResourceClaims[0].ResourceClaimTemplateName).To(gomega.Equal("tpu-slice-template"))
		}, timeout, interval).Should(gomega.Succeed())
	})
})

var _ = ginkgo.Describe("Workload-Aware Scheduling integration (feature gate disabled)", func() {

	ginkgo.BeforeEach(func() {
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.JobSetWorkloadAwareSchedulingAPI, false)
	})

	ginkgo.It("should not create scheduling objects when feature gate is disabled and scheduling is nil", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "gate-off-nil-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		js := makeSchedulingJobSet("gate-off-nil", nsObj.Name,
			[]jobset.ReplicatedJob{makeRJob("workers", 1, 1, 1)},
			nil, // no scheduling
		)

		ginkgo.By("creating the JobSet without scheduling")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying Jobs are created normally")
		gomega.Eventually(func(g gomega.Gomega) {
			var jobList batchv1.JobList
			g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(jobList.Items).To(gomega.HaveLen(1))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying no Workload is created")
		gomega.Consistently(func(g gomega.Gomega) {
			var workloadList schedulingv1alpha3.WorkloadList
			g.Expect(k8sClient.List(ctx, &workloadList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(workloadList.Items).To(gomega.BeEmpty())
		}, 2*time.Second, interval).Should(gomega.Succeed())

		ginkgo.By("verifying no PodGroups are created")
		gomega.Consistently(func(g gomega.Gomega) {
			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.BeEmpty())
		}, 2*time.Second, interval).Should(gomega.Succeed())

		ginkgo.By("verifying child Jobs do NOT have scheduling annotations")
		var jobList batchv1.JobList
		gomega.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
		for _, job := range jobList.Items {
			gomega.Expect(job.Annotations).NotTo(gomega.HaveKey(controllers.SchedulingGroupTemplateNameKey))
		}
	})

	ginkgo.It("should skip scheduling reconciliation when feature gate is disabled even if scheduling field is set", func() {
		ctx := context.Background()
		nsObj := createTestNamespace(ctx, "gate-off-set-ns-")
		defer func() {
			gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, nsObj)).To(gomega.Succeed())
		}()

		// Create a JobSet without scheduling first (so it passes webhook validation
		// which rejects spec.scheduling when the gate is off), then patch the
		// scheduling field directly into the stored object to simulate a race or
		// upgrade scenario where the field already exists.
		js := makeSchedulingJobSet("gate-off-set", nsObj.Name,
			[]jobset.ReplicatedJob{makeRJob("workers", 1, 1, 1)},
			nil,
		)

		ginkgo.By("creating the JobSet without scheduling")
		gomega.Eventually(func() error {
			return k8sClient.Create(ctx, js)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("patching scheduling into the stored JobSet")
		gomega.Eventually(func(g gomega.Gomega) {
			var latest jobset.JobSet
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: nsObj.Name}, &latest)).To(gomega.Succeed())
			latest.Spec.Scheduling = &jobset.JobSetScheduling{
				SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
					Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 1},
				},
			}
			g.Expect(k8sClient.Update(ctx, &latest)).To(gomega.Succeed())
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying Jobs are created")
		gomega.Eventually(func(g gomega.Gomega) {
			var jobList batchv1.JobList
			g.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(jobList.Items).To(gomega.HaveLen(1))
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("verifying the controller does not create any scheduling objects")
		gomega.Consistently(func(g gomega.Gomega) {
			var workloadList schedulingv1alpha3.WorkloadList
			g.Expect(k8sClient.List(ctx, &workloadList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(workloadList.Items).To(gomega.BeEmpty())

			var pgList schedulingv1alpha3.PodGroupList
			g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
			g.Expect(pgList.Items).To(gomega.BeEmpty())
		}, 2*time.Second, interval).Should(gomega.Succeed())

		ginkgo.By("verifying child Jobs do NOT have scheduling annotations")
		var jobList batchv1.JobList
		gomega.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(nsObj.Name))).To(gomega.Succeed())
		for _, job := range jobList.Items {
			gomega.Expect(job.Annotations).NotTo(gomega.HaveKey(controllers.SchedulingGroupTemplateNameKey))
		}
	})
})

// getWorkloadForJobSet returns the single Workload in the namespace. Generated
// Workload names carry an identity hash suffix (<jobset>-<hash>), so tests can
// no longer Get them by the JobSet name; each test creates exactly one JobSet
// per namespace, so listing and taking the sole item is unambiguous.
func getWorkloadForJobSet(ctx context.Context, g gomega.Gomega, ns string) schedulingv1alpha3.Workload {
	var wlList schedulingv1alpha3.WorkloadList
	g.Expect(k8sClient.List(ctx, &wlList, client.InNamespace(ns))).To(gomega.Succeed())
	g.Expect(wlList.Items).To(gomega.HaveLen(1))
	return wlList.Items[0]
}

// getPodGroupByPrefix returns the single PodGroup in the namespace whose name
// starts with prefix+"-". Generated PodGroup names carry an identity hash
// suffix (e.g. <jobset>-<rjob>-<hash>), so exact-name Gets no longer work.
func getPodGroupByPrefix(ctx context.Context, g gomega.Gomega, ns, prefix string) schedulingv1alpha3.PodGroup {
	var pgList schedulingv1alpha3.PodGroupList
	g.Expect(k8sClient.List(ctx, &pgList, client.InNamespace(ns))).To(gomega.Succeed())
	var matches []schedulingv1alpha3.PodGroup
	for i := range pgList.Items {
		if strings.HasPrefix(pgList.Items[i].Name, prefix+"-") {
			matches = append(matches, pgList.Items[i])
		}
	}
	g.Expect(matches).To(gomega.HaveLen(1), "expected exactly one PodGroup with prefix %q, got %d", prefix, len(matches))
	return matches[0]
}

// createTestNamespace creates a namespace with a generated name prefix.
func createTestNamespace(ctx context.Context, prefix string) *corev1.Namespace {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: prefix,
		},
	}
	gomega.ExpectWithOffset(1, k8sClient.Create(ctx, ns)).To(gomega.Succeed())
	return ns
}

// podGroupProtectionFinalizer is the finalizer kube-apiserver's
// PodGroupProtection admission plugin stamps onto every PodGroup at
// creation time (scheduling.k8s.io/podgroup-protection, see
// k8s.io/kubernetes/pkg/apis/scheduling.PodGroupProtectionFinalizer). It is
// normally removed by the podgroupprotection controller running in
// kube-controller-manager once no active Pods reference the PodGroup.
// envtest only runs etcd and kube-apiserver, so that controller never runs
// and PodGroups would otherwise be stuck Terminating forever. Since these
// tests never create real Pods, removing the finalizer here has the same
// effect the controller would have in a real cluster.
const podGroupProtectionFinalizer = "scheduling.k8s.io/podgroup-protection"

// removePodGroupProtectionFinalizers strips podGroupProtectionFinalizer from
// every PodGroup in the namespace that has one, simulating the
// podgroupprotection controller so that Deletes issued by the JobSet
// controller (e.g. on suspend, or when recreating a stale Workload/PodGroup)
// actually complete under envtest.
func removePodGroupProtectionFinalizers(ctx context.Context, namespace string) {
	gomega.Eventually(func() error {
		var pgList schedulingv1alpha3.PodGroupList
		if err := k8sClient.List(ctx, &pgList, client.InNamespace(namespace)); err != nil {
			return err
		}
		for i := range pgList.Items {
			pg := &pgList.Items[i]
			idx := slices.Index(pg.Finalizers, podGroupProtectionFinalizer)
			if idx == -1 {
				continue
			}
			pg.Finalizers = slices.Delete(pg.Finalizers, idx, idx+1)
			if err := k8sClient.Update(ctx, pg); err != nil {
				return err
			}
		}
		return nil
	}, timeout, interval).Should(gomega.Succeed())
}
