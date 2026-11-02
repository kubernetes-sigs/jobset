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

package inplacerestarttest

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
	testingutil "sigs.k8s.io/jobset/pkg/util/testing"
	testutil "sigs.k8s.io/jobset/test/util"
)

const (
	timeout  = 10 * time.Second
	interval = time.Millisecond * 250
)

var _ = ginkgo.Describe("JobSet with InPlaceRestart strategy", func() {
	var ns *corev1.Namespace

	ginkgo.BeforeEach(func() {
		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "jobset-inplace-ns-"}}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())
	})

	ginkgo.AfterEach(func() {
		gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
	})

	type testCase struct {
		maxRestarts int32
		// podInPlaceRestartAttempt is the in-place restart attempt reported by the agent on every Pod.
		podInPlaceRestartAttempt int32
		wantFailed               bool
	}

	ginkgo.DescribeTable("a failed Job counts in-place restarts towards maxRestarts",
		func(tc testCase) {
			js := inPlaceRestartJobSet(ns.Name, tc.maxRestarts)

			ginkgo.By("creating the JobSet")
			gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
			gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(testutil.NumExpectedJobs(js)))

			ginkgo.By(fmt.Sprintf("creating Pods at in-place restart attempt %d", tc.podInPlaceRestartAttempt))
			createAnnotatedPods(js, tc.podInPlaceRestartAttempt)

			ginkgo.By("waiting for the JobSet to sync the current in-place restart attempt")
			gomega.Eventually(func(g gomega.Gomega) {
				var got jobset.JobSet
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(js), &got)).To(gomega.Succeed())
				g.Expect(got.Status.CurrentInPlaceRestartAttempt).To(gomega.Equal(ptr.To(tc.podInPlaceRestartAttempt)))
			}, timeout, interval).Should(gomega.Succeed())

			ginkgo.By("failing a Job")
			var jobList batchv1.JobList
			gomega.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace))).To(gomega.Succeed())
			gomega.Expect(jobList.Items).NotTo(gomega.BeEmpty())
			failJob(&jobList.Items[0])

			if tc.wantFailed {
				testutil.JobSetFailed(ctx, k8sClient, js, timeout)
				var got jobset.JobSet
				gomega.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(js), &got)).To(gomega.Succeed())
				failed := apimeta.FindStatusCondition(got.Status.Conditions, string(jobset.JobSetFailed))
				gomega.Expect(failed).NotTo(gomega.BeNil())
				gomega.Expect(failed.Reason).To(gomega.Equal(constants.ReachedMaxRestartsReason))
				gomega.Expect(got.Status.Restarts).To(gomega.BeZero())
				return
			}

			gomega.Eventually(func(g gomega.Gomega) {
				var got jobset.JobSet
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(js), &got)).To(gomega.Succeed())
				g.Expect(got.Status.Restarts).To(gomega.Equal(int32(1)))
				g.Expect(got.Status.RestartsCountTowardsMax).To(gomega.Equal(int32(1)))
				g.Expect(got.Status.TerminalState).To(gomega.BeEmpty())
			}, timeout, interval).Should(gomega.Succeed())
		},
		ginkgo.Entry("fails the JobSet when in-place restarts reached maxRestarts", testCase{
			maxRestarts:              1,
			podInPlaceRestartAttempt: 1,
			wantFailed:               true,
		}),
		ginkgo.Entry("restarts the JobSet when in-place restarts are below maxRestarts", testCase{
			maxRestarts:              2,
			podInPlaceRestartAttempt: 1,
			wantFailed:               false,
		}),
	)
})

// inPlaceRestartJobSet returns a JobSet with a single Job of two Pods using the InPlaceRestart strategy.
// The Job template meets the webhook requirements for in-place restart, even though the webhook is not
// installed in this suite.
func inPlaceRestartJobSet(namespace string, maxRestarts int32) *jobset.JobSet {
	jobTemplate := testingutil.MakeJobTemplate("job", namespace).
		PodSpec(testingutil.TestPodSpec).
		CompletionMode(batchv1.IndexedCompletion).
		Parallelism(2).
		Completions(2).
		Obj()
	jobTemplate.Spec.BackoffLimit = ptr.To[int32](math.MaxInt32)
	jobTemplate.Spec.PodReplacementPolicy = ptr.To(batchv1.Failed)

	return testingutil.MakeJobSet("test-js", namespace).
		SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAll, TargetReplicatedJobs: []string{}}).
		EnableDNSHostnames(true).
		FailurePolicy(&jobset.FailurePolicy{
			MaxRestarts:     maxRestarts,
			RestartStrategy: jobset.InPlaceRestart,
		}).
		ReplicatedJob(testingutil.MakeReplicatedJob("rjob").
			Job(jobTemplate).
			Replicas(1).
			Obj()).
		Obj()
}

// createAnnotatedPods creates every Pod of the JobSet with the in-place restart attempt annotation,
// emulating the in-place restart agent since envtest runs no kubelet or job controller.
func createAnnotatedPods(js *jobset.JobSet, inPlaceRestartAttempt int32) {
	for _, rjob := range js.Spec.ReplicatedJobs {
		numPods := int(rjob.Replicas * ptr.Deref(rjob.Template.Spec.Parallelism, 1))
		for i := range numPods {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-%s-%d", js.Name, rjob.Name, i),
					Namespace: js.Namespace,
					Labels: map[string]string{
						jobset.JobSetNameKey:        js.Name,
						jobset.ReplicatedJobNameKey: rjob.Name,
					},
					Annotations: map[string]string{
						jobset.InPlaceRestartAttemptKey: strconv.Itoa(int(inPlaceRestartAttempt)),
					},
				},
				Spec: testingutil.TestPodSpec,
			}
			gomega.Expect(k8sClient.Create(ctx, pod)).To(gomega.Succeed())
		}
	}
}

// failJob marks the Job as failed, emulating the kube-controller-manager job controller.
func failJob(job *batchv1.Job) {
	gomega.Eventually(func() error {
		var got batchv1.Job
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, &got); err != nil {
			return err
		}
		got.Status = batchv1.JobStatus{
			// A finished Job requires a non-empty startTime.
			StartTime: ptr.To(metav1.Now()),
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue},
				{Type: batchv1.JobFailed, Status: corev1.ConditionTrue},
			},
		}
		return k8sClient.Status().Update(ctx, &got)
	}, timeout, interval).Should(gomega.Succeed())
}
