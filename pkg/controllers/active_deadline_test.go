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

package controllers

import (
	"fmt"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2/ktesting"
	clocktesting "k8s.io/utils/clock/testing"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
	"sigs.k8s.io/jobset/pkg/features"
	"sigs.k8s.io/jobset/pkg/metrics"
	testutils "sigs.k8s.io/jobset/pkg/util/testing"
)

func TestSyncActiveDeadlineStartTime(t *testing.T) {
	const (
		jobSetName = "test-jobset"
		ns         = "default"
	)
	now := metav1.Now()
	earlier := metav1.NewTime(now.Add(-time.Hour))

	tests := []struct {
		name           string
		jobset         *jobset.JobSet
		expectStartSet bool // startTime should be non-nil after sync
		expectCleared  bool // startTime was non-nil, expect nil after sync
		expectChanged  bool // shouldUpdate expected
		expectPreserve bool // startTime should keep its original value
	}{
		{
			name:           "active jobset with no startTime gets one",
			jobset:         testutils.MakeJobSet(jobSetName, ns).Obj(),
			expectStartSet: true,
			expectChanged:  true,
		},
		{
			name:           "active jobset with existing startTime is preserved",
			jobset:         testutils.MakeJobSet(jobSetName, ns).StartTime(earlier).Obj(),
			expectStartSet: true,
			expectPreserve: true,
		},
		{
			name:          "suspended jobset with startTime gets cleared",
			jobset:        testutils.MakeJobSet(jobSetName, ns).Suspend(true).StartTime(earlier).Obj(),
			expectCleared: true,
			expectChanged: true,
		},
		{
			name:   "suspended jobset with no startTime stays nil",
			jobset: testutils.MakeJobSet(jobSetName, ns).Suspend(true).Obj(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			features.SetFeatureGateDuringTest(t, features.JobSetActiveDeadlineSeconds, true)
			r := &JobSetReconciler{clock: clocktesting.NewFakeClock(now.Time)}
			opts := &statusUpdateOpts{}
			orig := tc.jobset.Status.StartTime.DeepCopy()

			r.syncActiveDeadlineStartTime(tc.jobset, opts)

			got := tc.jobset.Status.StartTime
			if tc.expectStartSet {
				require.NotNil(t, got, "expected startTime to be set")
			}
			if tc.expectCleared {
				require.Nil(t, got, "expected startTime to be cleared")
			}
			if !tc.expectStartSet && !tc.expectCleared {
				require.Nil(t, got, "expected startTime to stay nil")
			}
			if tc.expectPreserve {
				require.NotNil(t, got)
				require.True(t, got.Equal(orig), "expected startTime %v to be preserved, got %v", orig, got)
			}
			require.Equal(t, tc.expectChanged, opts.shouldUpdate)
		})
	}
}

func TestResetStartTimeOnGlobalRestart(t *testing.T) {
	const (
		jobSetName = "test-jobset"
		ns         = "default"
	)
	now := metav1.Now()
	old := metav1.NewTime(now.Add(-time.Hour))

	tests := []struct {
		name           string
		restartsBefore int32
		restartsAfter  int32
		startTime      *metav1.Time
		gateDisabled   bool // run with the feature gate off (default: on)
		expectReset    bool
		expectChanged  bool
	}{
		{
			name:           "global restart bumps restarts: startTime reset to now",
			restartsBefore: 2,
			restartsAfter:  3,
			startTime:      &old,
			expectReset:    true,
			expectChanged:  true,
		},
		{
			name:           "single-Job restart (restarts unchanged): startTime untouched",
			restartsBefore: 2,
			restartsAfter:  2,
			startTime:      &old,
		},
		{
			name:           "restart bumped but startTime nil (suspended): stays nil",
			restartsBefore: 2,
			restartsAfter:  3,
			startTime:      nil,
		},
		{
			// Gate off: the function short-circuits, so no reset even on a global restart.
			name:           "gate disabled: global restart is a no-op",
			restartsBefore: 2,
			restartsAfter:  3,
			startTime:      &old,
			gateDisabled:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			features.SetFeatureGateDuringTest(t, features.JobSetActiveDeadlineSeconds, !tc.gateDisabled)
			r := &JobSetReconciler{clock: clocktesting.NewFakeClock(now.Time)}
			js := testutils.MakeJobSet(jobSetName, ns).Obj()
			js.Status.StartTime = tc.startTime
			js.Status.Restarts = tc.restartsAfter
			opts := &statusUpdateOpts{}

			r.resetStartTimeOnGlobalRestart(js, tc.restartsBefore, opts)

			if tc.expectReset {
				require.NotNil(t, js.Status.StartTime)
				require.True(t, js.Status.StartTime.Time.Equal(now.Time), "expected startTime reset to %v, got %v", now.Time, js.Status.StartTime)
			} else if tc.startTime == nil {
				require.Nil(t, js.Status.StartTime, "expected startTime to stay nil")
			} else {
				require.True(t, js.Status.StartTime.Equal(tc.startTime), "expected startTime unchanged %v, got %v", tc.startTime, js.Status.StartTime)
			}
			require.Equal(t, tc.expectChanged, opts.shouldUpdate)
		})
	}
}

func TestExecuteActiveDeadlinePolicy(t *testing.T) {
	const (
		jobSetName = "test-jobset"
		ns         = "default"
	)
	now := metav1.Now()

	tests := []struct {
		name                string
		gateEnabled         bool
		jobset              *jobset.JobSet
		expectExpired       bool
		expectRequeueApprox time.Duration // >0 means requeue expected roughly equal
		expectFailed        bool
	}{
		{
			name:        "gate disabled: no-op even when deadline set and elapsed",
			gateEnabled: false,
			jobset: testutils.MakeJobSet(jobSetName, ns).ActiveDeadlineSeconds(10).
				StartTime(metav1.NewTime(now.Add(-time.Hour))).Obj(),
		},
		{
			name:        "deadline unset: no-op",
			gateEnabled: true,
			jobset:      testutils.MakeJobSet(jobSetName, ns).StartTime(now).Obj(),
		},
		{
			name:        "suspended: no-op",
			gateEnabled: true,
			jobset: testutils.MakeJobSet(jobSetName, ns).Suspend(true).ActiveDeadlineSeconds(10).
				StartTime(metav1.NewTime(now.Add(-time.Hour))).Obj(),
		},
		{
			name:        "not started (nil startTime): no-op",
			gateEnabled: true,
			jobset:      testutils.MakeJobSet(jobSetName, ns).ActiveDeadlineSeconds(10).Obj(),
		},
		{
			name:        "remaining > 0: requeue, no failure",
			gateEnabled: true,
			jobset: testutils.MakeJobSet(jobSetName, ns).ActiveDeadlineSeconds(60).
				StartTime(metav1.NewTime(now.Add(-10 * time.Second))).Obj(),
			expectRequeueApprox: 50 * time.Second,
		},
		{
			name:        "deadline exceeded: fail",
			gateEnabled: true,
			jobset: testutils.MakeJobSet(jobSetName, ns).ActiveDeadlineSeconds(10).
				StartTime(metav1.NewTime(now.Add(-time.Hour))).Obj(),
			expectExpired: true,
			expectFailed:  true,
		},
		{
			name:        "future startTime (clock skew): treated as not expired",
			gateEnabled: true,
			jobset: testutils.MakeJobSet(jobSetName, ns).ActiveDeadlineSeconds(10).
				StartTime(metav1.NewTime(now.Add(time.Hour))).Obj(),
			expectRequeueApprox: time.Hour + 10*time.Second,
		},
		{
			name:        "overflow-large deadline: treated as never expiring, no failure",
			gateEnabled: true,
			jobset: testutils.MakeJobSet(jobSetName, ns).ActiveDeadlineSeconds(math.MaxInt64).
				StartTime(metav1.NewTime(now.Add(-time.Hour))).Obj(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, ctx := ktesting.NewTestContext(t)
			features.SetFeatureGateDuringTest(t, features.JobSetActiveDeadlineSeconds, tc.gateEnabled)

			r := &JobSetReconciler{clock: clocktesting.NewFakeClock(now.Time)}
			opts := &statusUpdateOpts{}
			// Mirror the reconcile loop: decide from the pure check, then act only on
			// an armed, expired deadline. No active jobs, so deleteJobs needs no client.
			armed, remaining := r.activeDeadlineRemaining(tc.jobset)
			expired := armed && remaining <= 0
			var requeueAfter time.Duration
			if expired {
				require.NoError(t, r.failJobSetOnActiveDeadline(ctx, tc.jobset, &childJobs{}, opts))
			} else if armed {
				requeueAfter = remaining
			}
			require.Equal(t, tc.expectExpired, expired)
			if tc.expectRequeueApprox > 0 {
				// Allow small slack for test execution time.
				diff := requeueAfter - tc.expectRequeueApprox
				require.True(t, diff >= -time.Second && diff <= time.Second, "expected requeueAfter ~%v, got %v", tc.expectRequeueApprox, requeueAfter)
			} else if !tc.expectExpired {
				require.Zero(t, requeueAfter, "expected no requeue")
			}
			failed := apimeta.IsStatusConditionTrue(tc.jobset.Status.Conditions, string(jobset.JobSetFailed))
			require.Equal(t, tc.expectFailed, failed)
			if tc.expectFailed {
				cond := apimeta.FindStatusCondition(tc.jobset.Status.Conditions, string(jobset.JobSetFailed))
				require.NotNil(t, cond)
				require.Equal(t, constants.DeadlineExceededReason, cond.Reason)
			}
		})
	}
}

// adlsScheme builds a scheme with the types the reconcile-level tests need.
func adlsScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(jobset.AddToScheme(s))
	utilruntime.Must(batchv1.AddToScheme(s))
	utilruntime.Must(corev1.AddToScheme(s))
	return s
}

// adlsJobIndex mirrors the controller's JobsIndexByJobSetKey index so the fake
// client resolves getChildJobs by JobSet UID.
func adlsJobIndex(obj client.Object) []string {
	owner := metav1.GetControllerOf(obj.(*batchv1.Job))
	if owner == nil || owner.Kind != "JobSet" {
		return nil
	}
	return []string{string(owner.UID)}
}

// adlsRJobName is the single ReplicatedJob name used by adlsJobSet/adlsChildJob.
const adlsRJobName = "rjob"

// adlsChildJob builds a child Job owned by js, in the current run (restart 0),
// optionally already finished with the given condition type.
func adlsChildJob(js *jobset.JobSet, idx int, finished batchv1.JobConditionType) *batchv1.Job {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-%d", js.Name, adlsRJobName, idx),
			Namespace: js.Namespace,
			Labels: map[string]string{
				constants.RestartsKey:       "0",
				jobset.ReplicatedJobNameKey: adlsRJobName,
				jobset.JobIndexKey:          strconv.Itoa(idx),
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: jobset.GroupVersion.String(),
				Kind:       "JobSet",
				Name:       js.Name,
				UID:        js.UID,
				Controller: ptr.To(true),
			}},
		},
	}
	if finished != "" {
		job.Status.Conditions = []batchv1.JobCondition{{Type: finished, Status: corev1.ConditionTrue}}
	}
	return job
}

// adlsReconcile drives r.reconcile against a fake client seeded with the JobSet
// and its child jobs, returning the mutated JobSet for assertions.
func adlsReconcile(t *testing.T, js *jobset.JobSet, jobs ...*batchv1.Job) *jobset.JobSet {
	t.Helper()
	_, ctx := ktesting.NewTestContext(t)
	features.SetFeatureGateDuringTest(t, features.JobSetActiveDeadlineSeconds, true)

	scheme := adlsScheme()
	objs := []client.Object{js}
	for _, j := range jobs {
		objs = append(objs, j)
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&jobset.JobSet{}, &batchv1.Job{}).
		WithIndex(&batchv1.Job{}, constants.JobsIndexByJobSetKey, adlsJobIndex).
		Build()

	r := &JobSetReconciler{
		Client: c,
		Scheme: scheme,
		clock:  clocktesting.NewFakeClock(time.Now()),
	}
	_, err := r.reconcile(ctx, js, &statusUpdateOpts{})
	require.NoError(t, err)
	return js
}

// adlsJobSet builds a single-ReplicatedJob JobSet with an Any success policy.
func adlsJobSet(replicas int32) *jobset.JobSet {
	js := testutils.MakeJobSet("adls-order", "default").
		SuccessPolicy(&jobset.SuccessPolicy{Operator: jobset.OperatorAny}).
		ReplicatedJob(testutils.MakeReplicatedJob(adlsRJobName).
			Job(testutils.MakeJobTemplate("job", "default").Obj()).
			Replicas(replicas).
			Obj()).
		Obj()
	js.UID = "adls-order-uid"
	return js
}

func isCompleted(js *jobset.JobSet) bool {
	return apimeta.IsStatusConditionTrue(js.Status.Conditions, string(jobset.JobSetCompleted))
}

func failedReason(js *jobset.JobSet) string {
	c := apimeta.FindStatusCondition(js.Status.Conditions, string(jobset.JobSetFailed))
	if c == nil || c.Status != metav1.ConditionTrue {
		return ""
	}
	return c.Reason
}

// TestReconcileSuccessBeatsExpiredDeadline covers KEP precedence: a JobSet that
// satisfies its success policy in the same reconcile where the deadline has
// already expired is marked Completed, not failed with DeadlineExceeded.
func TestReconcileSuccessBeatsExpiredDeadline(t *testing.T) {
	js := adlsJobSet(1)
	js.Spec.ActiveDeadlineSeconds = ptr.To[int64](10)
	js.Status.StartTime = ptr.To(metav1.NewTime(time.Now().Add(-time.Hour))) // deadline long past

	got := adlsReconcile(t, js, adlsChildJob(js, 0, batchv1.JobComplete))

	require.True(t, isCompleted(got), "expected JobSet Completed via success policy")
	require.NotEqual(t, constants.DeadlineExceededReason, failedReason(got), "must not be failed by the deadline")
}

// TestReconcileDeadlineBeatsFailurePolicy covers KEP semantics: when a child-Job
// failure and an elapsed deadline are observed in the same reconcile, the JobSet
// fails with DeadlineExceeded (checked before the failure policy) and is not
// restarted.
func TestReconcileDeadlineBeatsFailurePolicy(t *testing.T) {
	js := adlsJobSet(1)
	js.Spec.ActiveDeadlineSeconds = ptr.To[int64](10)
	js.Spec.FailurePolicy = &jobset.FailurePolicy{MaxRestarts: 3} // would restart if reached
	js.Status.StartTime = ptr.To(metav1.NewTime(time.Now().Add(-time.Hour)))

	got := adlsReconcile(t, js, adlsChildJob(js, 0, batchv1.JobFailed))

	require.Equal(t, constants.DeadlineExceededReason, failedReason(got), "expected DeadlineExceeded before failure policy")
	require.Equal(t, int32(0), got.Status.Restarts, "must not restart when the deadline wins")
}

// TestReconcileNoDeadlineKeepsOriginalOrdering covers the reorder scoping: a
// JobSet without activeDeadlineSeconds keeps the original failure -> success
// ordering even when the gate is on, so enabling the gate never flips the
// terminal outcome of a JobSet that does not use the feature. With a satisfiable
// Any success policy and a simultaneous failed job, the failure policy wins
// (original behavior), not success.
func TestReconcileNoDeadlineKeepsOriginalOrdering(t *testing.T) {
	js := adlsJobSet(2) // rjob-0 complete, rjob-1 failed
	// No ActiveDeadlineSeconds set; nil FailurePolicy => failed job fails the JobSet.
	got := adlsReconcile(t, js,
		adlsChildJob(js, 0, batchv1.JobComplete),
		adlsChildJob(js, 1, batchv1.JobFailed),
	)

	require.False(t, isCompleted(got), "success must not win: field unset keeps failure -> success ordering")
	require.Equal(t, constants.FailedJobsReason, failedReason(got), "expected failure policy to fail the JobSet")
}

// TestReconcileLiveDeadlineKeepsOriginalOrdering covers the reorder scoping for a
// JobSet that sets activeDeadlineSeconds but whose deadline has not yet expired.
// The success -> deadline reorder must not apply, so a satisfiable Any success
// policy alongside a simultaneous failed job still fails via the failure policy
// (original behavior) rather than flipping the JobSet to Completed just because a
// far-off deadline was set.
func TestReconcileLiveDeadlineKeepsOriginalOrdering(t *testing.T) {
	js := adlsJobSet(2)                                                        // rjob-0 complete, rjob-1 failed
	js.Spec.ActiveDeadlineSeconds = ptr.To[int64](3600)                        // one hour, far from expiry
	js.Status.StartTime = ptr.To(metav1.NewTime(time.Now().Add(-time.Second))) // started just now

	got := adlsReconcile(t, js,
		adlsChildJob(js, 0, batchv1.JobComplete),
		adlsChildJob(js, 1, batchv1.JobFailed),
	)

	require.False(t, isCompleted(got), "success must not win while the deadline is live")
	require.Equal(t, constants.FailedJobsReason, failedReason(got), "expected failure policy to fail the JobSet")
	require.NotEqual(t, constants.DeadlineExceededReason, failedReason(got), "deadline is not expired")
}

// TestExecuteActiveDeadlinePolicyIncrementsMetric verifies the
// jobset_active_deadline_exceeded_total metric is incremented once on expiry.
func TestExecuteActiveDeadlinePolicyIncrementsMetric(t *testing.T) {
	_, ctx := ktesting.NewTestContext(t)
	features.SetFeatureGateDuringTest(t, features.JobSetActiveDeadlineSeconds, true)

	const name, ns = "adls-metric-js", "default"
	js := testutils.MakeJobSet(name, ns).
		ActiveDeadlineSeconds(10).
		StartTime(metav1.NewTime(time.Now().Add(-time.Hour))).
		Obj()

	before := testutil.ToFloat64(metrics.ActiveDeadlineExceededTotal.WithLabelValues(name, ns))

	r := &JobSetReconciler{clock: clocktesting.NewFakeClock(time.Now())}
	armed, remaining := r.activeDeadlineRemaining(js)
	require.True(t, armed && remaining <= 0, "expected an armed, expired deadline")
	require.NoError(t, r.failJobSetOnActiveDeadline(ctx, js, &childJobs{}, &statusUpdateOpts{}))

	after := testutil.ToFloat64(metrics.ActiveDeadlineExceededTotal.WithLabelValues(name, ns))
	require.Equal(t, before+1, after, "expected active_deadline_exceeded_total to increment by 1")
}
