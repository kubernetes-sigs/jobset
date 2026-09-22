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

package controllertest

import (
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
	"sigs.k8s.io/jobset/pkg/features"
	testutil "sigs.k8s.io/jobset/test/util"
)

// shortDuration is used with Consistently to confirm a state holds for a while.
const shortDuration = 3 * time.Second

var _ = ginkgo.Describe("JobSet activeDeadlineSeconds", func() {
	var ns *corev1.Namespace

	ginkgo.BeforeEach(func() {
		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "jobset-adls-ns-"}}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed())
	})

	ginkgo.AfterEach(func() {
		gomega.Expect(testutil.DeleteNamespace(ctx, k8sClient, ns)).To(gomega.Succeed())
	})

	get := func(js *jobset.JobSet) *jobset.JobSet {
		var fetched jobset.JobSet
		gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: js.Name, Namespace: js.Namespace}, &fetched)).To(gomega.Succeed())
		return &fetched
	}

	ginkgo.It("fails the JobSet with DeadlineExceeded once the deadline passes", func() {
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.JobSetActiveDeadlineSeconds, true)

		js := testJobSet(ns).ActiveDeadlineSeconds(1).Obj()
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

		ginkgo.By("checking the JobSet is failed with reason DeadlineExceeded")
		gomega.Eventually(func() bool {
			cond := apimeta.FindStatusCondition(get(js).Status.Conditions, string(jobset.JobSetFailed))
			return cond != nil && cond.Status == metav1.ConditionTrue && cond.Reason == constants.DeadlineExceededReason
		}, timeout, interval).Should(gomega.BeTrue())

		ginkgo.By("checking the active child jobs are marked for deletion")
		gomega.Eventually(func() int {
			var jobList batchv1.JobList
			gomega.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace))).To(gomega.Succeed())
			active := 0
			for i := range jobList.Items {
				if jobList.Items[i].DeletionTimestamp == nil {
					active++
				}
			}
			return active
		}, timeout, interval).Should(gomega.Equal(0))
	})

	ginkgo.It("populates .status.startTime while active", func() {
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.JobSetActiveDeadlineSeconds, true)

		js := testJobSet(ns).Obj() // no deadline; startTime is still maintained
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

		gomega.Eventually(func() bool {
			return get(js).Status.StartTime != nil
		}, timeout, interval).Should(gomega.BeTrue())
	})

	ginkgo.It("does not enforce the deadline while suspended", func() {
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.JobSetActiveDeadlineSeconds, true)

		js := testJobSet(ns).Suspend(true).ActiveDeadlineSeconds(1).Obj()
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

		testutil.JobSetSuspended(ctx, k8sClient, js, timeout)

		ginkgo.By("checking the suspended JobSet is not failed and has no startTime")
		gomega.Consistently(func() bool {
			fetched := get(js)
			failed := apimeta.IsStatusConditionTrue(fetched.Status.Conditions, string(jobset.JobSetFailed))
			return !failed && fetched.Status.StartTime == nil
		}, shortDuration, interval).Should(gomega.BeTrue())
	})

	ginkgo.It("does not fail a JobSet that completes before the deadline", func() {
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.JobSetActiveDeadlineSeconds, true)

		js := testJobSet(ns).ActiveDeadlineSeconds(3600).Obj()
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

		gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(testutil.NumExpectedJobs(js)))

		var jobList batchv1.JobList
		gomega.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace))).To(gomega.Succeed())
		completeAllJobs(&jobList)

		testutil.JobSetCompleted(ctx, k8sClient, js, timeout)
	})

	ginkgo.It("recomputes the deadline from startTime when activeDeadlineSeconds is lowered on a running JobSet", func() {
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.JobSetActiveDeadlineSeconds, true)

		// Start with a long deadline so the JobSet runs normally and populates startTime.
		js := testJobSet(ns).ActiveDeadlineSeconds(3600).Obj()
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		gomega.Eventually(func() bool {
			return get(js).Status.StartTime != nil
		}, timeout, interval).Should(gomega.BeTrue())

		ginkgo.By("lowering activeDeadlineSeconds below the already-elapsed active time")
		gomega.Eventually(func() error {
			fetched := get(js)
			fetched.Spec.ActiveDeadlineSeconds = ptr.To(int64(1))
			return k8sClient.Update(ctx, fetched)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("checking the JobSet is failed with reason DeadlineExceeded without a restart")
		gomega.Eventually(func() bool {
			fetched := get(js)
			cond := apimeta.FindStatusCondition(fetched.Status.Conditions, string(jobset.JobSetFailed))
			return cond != nil && cond.Status == metav1.ConditionTrue && cond.Reason == constants.DeadlineExceededReason
		}, timeout, interval).Should(gomega.BeTrue())
		gomega.Expect(get(js).Status.Restarts).To(gomega.Equal(int32(0)))
	})

	ginkgo.It("does not maintain startTime or fail the JobSet when the feature gate is disabled", func() {
		// Gate intentionally left disabled (default false). Use a plain JobSet: the
		// webhook rejects activeDeadlineSeconds while the gate is off, so a valid
		// gate-off fixture cannot set the field. The gated deadline check with the
		// field present is covered by the TestExecuteActiveDeadlinePolicy unit test.
		js := testJobSet(ns).Obj()
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

		ginkgo.By("checking the gate-off JobSet is never failed and has no startTime side effect")
		gomega.Consistently(func() bool {
			fetched := get(js)
			failed := apimeta.IsStatusConditionTrue(fetched.Status.Conditions, string(jobset.JobSetFailed))
			return !failed && fetched.Status.StartTime == nil
		}, shortDuration, interval).Should(gomega.BeTrue())
	})

	ginkgo.It("begins startTime maintenance when the gate is enabled on a running JobSet", func() {
		// Gate starts disabled (suite default). startTime maintenance is gate-driven,
		// not field-driven, so no activeDeadlineSeconds is set here (that also keeps the
		// setup consistent with the webhook, which rejects the field while the gate is off).
		js := testJobSet(ns).Obj()
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

		ginkgo.By("confirming startTime stays nil while the gate is off")
		gomega.Consistently(func() *metav1.Time {
			return get(js).Status.StartTime
		}, shortDuration, interval).Should(gomega.BeNil())

		ginkgo.By("enabling the gate and nudging a reconcile")
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.JobSetActiveDeadlineSeconds, true)
		gomega.Eventually(func() error {
			fetched := get(js)
			if fetched.Annotations == nil {
				fetched.Annotations = map[string]string{}
			}
			fetched.Annotations["test.jobset.sigs.k8s.io/nudge"] = "1"
			return k8sClient.Update(ctx, fetched)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("checking startTime is now maintained (no persisted value, so it starts at enable)")
		gomega.Eventually(func() *metav1.Time {
			return get(js).Status.StartTime
		}, timeout, interval).ShouldNot(gomega.BeNil())
	})

	ginkgo.It("preserves and resumes from the persisted startTime across a gate off->on toggle", func() {
		// Gate on: a long-deadline JobSet becomes active and records startTime.
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.JobSetActiveDeadlineSeconds, true)
		js := testJobSet(ns).ActiveDeadlineSeconds(3600).Obj()
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

		var persisted *metav1.Time
		gomega.Eventually(func() *metav1.Time {
			persisted = get(js).Status.StartTime
			return persisted
		}, timeout, interval).ShouldNot(gomega.BeNil())

		ginkgo.By("disabling the gate leaves the existing startTime untouched")
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.JobSetActiveDeadlineSeconds, false)
		gomega.Eventually(func() error {
			fetched := get(js)
			if fetched.Annotations == nil {
				fetched.Annotations = map[string]string{}
			}
			fetched.Annotations["test.jobset.sigs.k8s.io/nudge"] = "off"
			return k8sClient.Update(ctx, fetched)
		}, timeout, interval).Should(gomega.Succeed())
		gomega.Consistently(func() bool {
			st := get(js).Status.StartTime
			return st != nil && st.Equal(persisted)
		}, shortDuration, interval).Should(gomega.BeTrue())

		ginkgo.By("re-enabling the gate resumes from the persisted startTime (not reset to now)")
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.JobSetActiveDeadlineSeconds, true)
		gomega.Eventually(func() error {
			fetched := get(js)
			if fetched.Annotations == nil {
				fetched.Annotations = map[string]string{}
			}
			fetched.Annotations["test.jobset.sigs.k8s.io/nudge"] = "on"
			return k8sClient.Update(ctx, fetched)
		}, timeout, interval).Should(gomega.Succeed())
		gomega.Consistently(func() bool {
			st := get(js).Status.StartTime
			return st != nil && st.Equal(persisted)
		}, shortDuration, interval).Should(gomega.BeTrue())
	})

	ginkgo.It("does not set startTime or enforce the deadline for an externally managed JobSet", func() {
		// Gate on and deadline elapsed: proves the managedBy early-return in Reconcile
		// sits above both startTime maintenance and the deadline check, so the built-in
		// controller touches neither for an externally managed JobSet.
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.JobSetActiveDeadlineSeconds, true)

		js := testJobSet(ns).ManagedBy("example.com/external-controller").ActiveDeadlineSeconds(1).Obj()
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())

		ginkgo.By("checking the built-in controller neither sets startTime nor fails the JobSet")
		gomega.Consistently(func() bool {
			fetched := get(js)
			failed := apimeta.IsStatusConditionTrue(fetched.Status.Conditions, string(jobset.JobSetFailed))
			return !failed && fetched.Status.StartTime == nil
		}, shortDuration, interval).Should(gomega.BeTrue())
	})

	ginkgo.It("rejects activeDeadlineSeconds of zero at the API server (Minimum=1)", func() {
		// Schema validation is independent of the feature gate.
		js := testJobSet(ns).ActiveDeadlineSeconds(0).Obj()
		gomega.Expect(k8sClient.Create(ctx, js)).ToNot(gomega.Succeed())
	})

	ginkgo.It("restarts the deadline timer on resume (does not fail on pre-suspend elapsed time)", func() {
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.JobSetActiveDeadlineSeconds, true)

		// Long deadline so the JobSet runs normally and populates startTime.
		js := testJobSet(ns).ActiveDeadlineSeconds(3600).Obj()
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		gomega.Eventually(func() *metav1.Time {
			return get(js).Status.StartTime
		}, timeout, interval).ShouldNot(gomega.BeNil())
		beforeSuspend := get(js).Status.StartTime
		// metav1.Time is second-granular once round-tripped through the API server, so
		// wait a full second to make the resumed anchor strictly later than this one.
		time.Sleep(time.Second)

		ginkgo.By("suspending the JobSet clears startTime")
		gomega.Eventually(func() error {
			fetched := get(js)
			fetched.Spec.Suspend = ptr.To(true)
			return k8sClient.Update(ctx, fetched)
		}, timeout, interval).Should(gomega.Succeed())
		gomega.Eventually(func() *metav1.Time {
			return get(js).Status.StartTime
		}, timeout, interval).Should(gomega.BeNil())

		ginkgo.By("resuming the JobSet repopulates startTime with a later anchor, not the pre-suspend one")
		gomega.Eventually(func() error {
			fetched := get(js)
			fetched.Spec.Suspend = ptr.To(false)
			return k8sClient.Update(ctx, fetched)
		}, timeout, interval).Should(gomega.Succeed())
		gomega.Eventually(func(g gomega.Gomega) {
			st := get(js).Status.StartTime
			g.Expect(st).ToNot(gomega.BeNil())
			g.Expect(st.After(beforeSuspend.Time)).To(gomega.BeTrue())
		}, timeout, interval).Should(gomega.Succeed())
		gomega.Consistently(func() bool {
			return apimeta.IsStatusConditionTrue(get(js).Status.Conditions, string(jobset.JobSetFailed))
		}, shortDuration, interval).Should(gomega.BeFalse())
	})

	ginkgo.It("resets the deadline timer on a global RestartJobSet restart", func() {
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.JobSetActiveDeadlineSeconds, true)

		// Long deadline (won't fire) + a global restart policy. A failed child triggers
		// RestartJobSet, which begins a fresh run and resets startTime.
		js := testJobSet(ns).
			ActiveDeadlineSeconds(3600).
			FailurePolicy(&jobset.FailurePolicy{MaxRestarts: 1}).
			Obj()
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(testutil.NumExpectedJobs(js)))

		ginkgo.By("recording the pre-restart startTime anchor")
		gomega.Eventually(func() *metav1.Time {
			return get(js).Status.StartTime
		}, timeout, interval).ShouldNot(gomega.BeNil())
		beforeRestart := get(js).Status.StartTime
		// metav1.Time is second-granular once round-tripped through the API server, so
		// wait a full second to make a genuine reset produce a strictly later anchor.
		time.Sleep(time.Second)

		ginkgo.By("failing a child job to drive a global restart")
		var jobList batchv1.JobList
		gomega.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace))).To(gomega.Succeed())
		gomega.Expect(jobList.Items).ToNot(gomega.BeEmpty())
		failJob(&jobList.Items[0])

		ginkgo.By("checking the global restart replaced the startTime anchor with a later one (timer reset, not retained)")
		gomega.Eventually(func(g gomega.Gomega) {
			fetched := get(js)
			g.Expect(fetched.Status.Restarts).To(gomega.Equal(int32(1)))
			g.Expect(fetched.Status.StartTime).ToNot(gomega.BeNil())
			g.Expect(fetched.Status.StartTime.After(beforeRestart.Time)).To(gomega.BeTrue())
		}, timeout, interval).Should(gomega.Succeed())
		gomega.Expect(apimeta.IsStatusConditionTrue(get(js).Status.Conditions, string(jobset.JobSetFailed))).To(gomega.BeFalse())
	})

	ginkgo.It("does not reset startTime on a single-Job RestartJob restart", func() {
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.JobSetActiveDeadlineSeconds, true)
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.RestartJob, true)

		// Long deadline (won't fire) + a RestartJob rule so a single failed Job is
		// recreated without a global restart. startTime must be left unchanged.
		js := testJobSet(ns).
			ActiveDeadlineSeconds(3600).
			FailurePolicy(&jobset.FailurePolicy{
				MaxRestarts: 1,
				Rules: []jobset.FailurePolicyRule{
					{Action: jobset.RestartJob, OnJobFailureReasons: []string{batchv1.JobReasonPodFailurePolicy}},
				},
			}).
			Obj()
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(testutil.NumExpectedJobs(js)))

		gomega.Eventually(func() *metav1.Time {
			return get(js).Status.StartTime
		}, timeout, interval).ShouldNot(gomega.BeNil())
		startTimeBefore := get(js).Status.StartTime

		ginkgo.By("failing a replicated-job-a job to drive a single-Job RestartJob")
		var jobList batchv1.JobList
		gomega.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace))).To(gomega.Succeed())
		failFirstMatchingJobWithOptions(&jobList, "replicated-job-a", &failJobOptions{reason: ptr.To(batchv1.JobReasonPodFailurePolicy)})

		ginkgo.By("checking the single Job restarted with no global restart and an unchanged startTime")
		matchJobRestarts(js, []int32{1})
		gomega.Expect(get(js).Status.Restarts).To(gomega.Equal(int32(0)))
		gomega.Expect(get(js).Status.StartTime.Equal(startTimeBefore)).To(gomega.BeTrue())
		gomega.Expect(apimeta.IsStatusConditionTrue(get(js).Status.Conditions, string(jobset.JobSetFailed))).To(gomega.BeFalse())
	})

	ginkgo.It("extends the deadline when activeDeadlineSeconds is raised on a running JobSet", func() {
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.JobSetActiveDeadlineSeconds, true)

		js := testJobSet(ns).ActiveDeadlineSeconds(3600).Obj()
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		gomega.Eventually(func() *metav1.Time {
			return get(js).Status.StartTime
		}, timeout, interval).ShouldNot(gomega.BeNil())

		ginkgo.By("raising activeDeadlineSeconds on the running JobSet")
		gomega.Eventually(func() error {
			fetched := get(js)
			fetched.Spec.ActiveDeadlineSeconds = ptr.To(int64(7200))
			return k8sClient.Update(ctx, fetched)
		}, timeout, interval).Should(gomega.Succeed())

		ginkgo.By("checking the JobSet keeps running and is not failed by the deadline")
		gomega.Consistently(func() bool {
			return apimeta.IsStatusConditionTrue(get(js).Status.Conditions, string(jobset.JobSetFailed))
		}, shortDuration, interval).Should(gomega.BeFalse())
	})

	ginkgo.It("resets the deadline timer on RestartJobSetAndIgnoreMaxRestarts, unbounded by maxRestarts", func() {
		features.SetFeatureGateDuringTest(ginkgo.GinkgoTB(), features.JobSetActiveDeadlineSeconds, true)

		// MaxRestarts=0 would normally fail the JobSet on the first child failure, but
		// the ignore action restarts it anyway and begins a fresh run (startTime reset).
		js := testJobSet(ns).
			ActiveDeadlineSeconds(3600).
			FailurePolicy(&jobset.FailurePolicy{
				MaxRestarts: 0,
				Rules: []jobset.FailurePolicyRule{
					{Action: jobset.RestartJobSetAndIgnoreMaxRestarts, OnJobFailureReasons: []string{}},
				},
			}).
			Obj()
		gomega.Expect(k8sClient.Create(ctx, js)).To(gomega.Succeed())
		gomega.Eventually(testutil.NumJobs, timeout, interval).WithArguments(ctx, k8sClient, js).Should(gomega.Equal(testutil.NumExpectedJobs(js)))

		ginkgo.By("recording the pre-restart startTime anchor")
		gomega.Eventually(func() *metav1.Time {
			return get(js).Status.StartTime
		}, timeout, interval).ShouldNot(gomega.BeNil())
		beforeRestart := get(js).Status.StartTime
		time.Sleep(time.Second) // second-granular metav1.Time: ensure a reset yields a later anchor

		ginkgo.By("failing a child job to drive a global restart that ignores maxRestarts")
		var jobList batchv1.JobList
		gomega.Expect(k8sClient.List(ctx, &jobList, client.InNamespace(js.Namespace))).To(gomega.Succeed())
		gomega.Expect(jobList.Items).ToNot(gomega.BeEmpty())
		failJob(&jobList.Items[0])

		ginkgo.By("checking the restart replaced the startTime anchor with a later one (fresh per-attempt timer)")
		gomega.Eventually(func(g gomega.Gomega) {
			fetched := get(js)
			g.Expect(fetched.Status.Restarts).To(gomega.Equal(int32(1)))
			g.Expect(fetched.Status.StartTime).ToNot(gomega.BeNil())
			g.Expect(fetched.Status.StartTime.After(beforeRestart.Time)).To(gomega.BeTrue())
		}, timeout, interval).Should(gomega.Succeed())
		gomega.Expect(apimeta.IsStatusConditionTrue(get(js).Status.Conditions, string(jobset.JobSetFailed))).To(gomega.BeFalse())
	})
})
