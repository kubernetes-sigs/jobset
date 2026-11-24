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

package e2e

import (
	"context"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/util/testing"
	"sigs.k8s.io/jobset/test/util"
)

// These tests cover the v1alpha2 fields that v1 drops or renames, so they keep
// working while v1alpha2 is served: startupPolicy, failurePolicy.restartStrategy,
// the Completed condition and terminal state, and replicatedJobsStatus[].succeeded.
var _ = ginkgo.Describe("JobSet v1alpha2", func() {
	var ns *corev1.Namespace

	ginkgo.BeforeEach(func() {
		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "e2e-v1alpha2-"}}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())
	})

	ginkgo.AfterEach(func() {
		gomega.Expect(k8sClient.Delete(ctx, ns)).To(gomega.Succeed())
	})

	ginkgo.When("startupPolicy is InOrder", func() {
		ginkgo.It("should start each ReplicatedJob only after the previous one is ready", func() {
			ctx := context.Background()

			// The leader pod becomes ready only after its startup probe passes, so
			// the workers must not be created for at least that long.
			leaderStartupProbe := &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					Exec: &corev1.ExecAction{Command: []string{"true"}},
				},
				InitialDelaySeconds: 20,
				PeriodSeconds:       1,
			}
			js := testing.MakeJobSet("in-order", ns.Name).
				StartupPolicy(&jobset.StartupPolicy{StartupPolicyOrder: jobset.InOrder}).
				ReplicatedJob(dependsOnTestReplicatedJob(ns, "leader", 1, leaderStartupProbe, 40, nil)).
				ReplicatedJob(dependsOnTestReplicatedJob(ns, "workers", 2, nil, 5, nil)).
				Obj()

			ginkgo.By("creating the jobset")
			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			ginkgo.By("checking the startupPolicy is stored as InOrder")
			created := &jobset.JobSet{}
			gomega.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(js), created)).Should(gomega.Succeed())
			gomega.Expect(created.Spec.StartupPolicy).NotTo(gomega.BeNil())
			gomega.Expect(created.Spec.StartupPolicy.StartupPolicyOrder).To(gomega.Equal(jobset.InOrder))

			ginkgo.By("checking the startup policy is in progress")
			util.JobSetStartupPolicyNotFinished(ctx, k8sClient, js, timeout)

			ginkgo.By("checking the leader job is created")
			gomega.Eventually(numReplicatedJobJobs, timeout, interval).WithArguments(ctx, js, "leader").Should(gomega.Equal(1))

			ginkgo.By("checking the worker jobs are not created while the leader is not ready")
			gomega.Consistently(numReplicatedJobJobs, 5*time.Second, interval).WithArguments(ctx, js, "workers").Should(gomega.Equal(0))

			ginkgo.By("checking the worker jobs are created once the leader is ready")
			gomega.Eventually(numReplicatedJobJobs, timeout, interval).WithArguments(ctx, js, "workers").Should(gomega.Equal(2))

			util.JobSetStartupPolicyComplete(ctx, k8sClient, js, timeout)
			util.JobSetCompleted(ctx, k8sClient, js, timeout)
		})
	})

	ginkgo.When("failurePolicy sets a restartStrategy", func() {
		ginkgo.DescribeTable("should recreate the jobs and succeed",
			func(restartStrategy jobset.JobSetRestartStrategy) {
				ctx := context.Background()

				js := restartJobSetFailurePolicyTestJobSet(ns)
				js.Spec.FailurePolicy.RestartStrategy = restartStrategy

				ginkgo.By("creating the jobset")
				gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

				ginkgo.By("checking the restartStrategy is stored")
				created := &jobset.JobSet{}
				gomega.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(js), created)).Should(gomega.Succeed())
				gomega.Expect(created.Spec.FailurePolicy.RestartStrategy).To(gomega.Equal(restartStrategy))

				util.JobSetCompleted(ctx, k8sClient, js, timeout)

				ginkgo.By("checking the jobset restarted once and only the restarted jobs remain")
				gomega.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(js), created)).Should(gomega.Succeed())
				gomega.Expect(created.Status.Restarts).To(gomega.Equal(int32(1)))
				gomega.Eventually(util.NumJobsByRestartAttempt, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(map[int]int{1: 2}))
			},
			ginkgo.Entry("Recreate", jobset.Recreate),
			ginkgo.Entry("BlockingRecreate", jobset.BlockingRecreate),
		)

		ginkgo.It("should default the restartStrategy to Recreate", func() {
			ctx := context.Background()

			js := restartJobSetFailurePolicyTestJobSet(ns)
			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			created := &jobset.JobSet{}
			gomega.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(js), created)).Should(gomega.Succeed())
			gomega.Expect(created.Spec.FailurePolicy.RestartStrategy).To(gomega.Equal(jobset.Recreate))
		})
	})

	ginkgo.When("the jobset completes", func() {
		ginkgo.It("should report the Completed condition and succeeded jobs", func() {
			ctx := context.Background()

			js := sleepTestJobSet(ns, 1).Obj()

			ginkgo.By("creating the jobset")
			gomega.Expect(k8sClient.Create(ctx, js)).Should(gomega.Succeed())

			// Checks the Completed condition and terminal state.
			util.JobSetCompleted(ctx, k8sClient, js, timeout)

			ginkgo.By("checking the replicatedJobsStatus reports the succeeded jobs")
			got := &jobset.JobSet{}
			gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, got)).Should(gomega.Succeed())
			gomega.Expect(got.Status.TerminalState).To(gomega.Equal(string(jobset.JobSetCompleted)))
			gomega.Expect(got.Status.ReplicatedJobsStatus).To(gomega.HaveLen(1))
			gomega.Expect(got.Status.ReplicatedJobsStatus[0].Succeeded).To(gomega.Equal(js.Spec.ReplicatedJobs[0].Replicas))
		})
	})
})

// numReplicatedJobJobs returns the number of jobs created for the
// ReplicatedJob rjobName of js.
func numReplicatedJobJobs(ctx context.Context, js *jobset.JobSet, rjobName string) (int, error) {
	var jobs batchv1.JobList
	if err := k8sClient.List(ctx, &jobs, client.InNamespace(js.Namespace), client.MatchingLabels{
		jobset.JobSetNameKey:        js.Name,
		jobset.ReplicatedJobNameKey: rjobName,
	}); err != nil {
		return -1, err
	}
	return len(jobs.Items), nil
}
