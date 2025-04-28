package controllers

import (
	"context"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2/ktesting"
	"k8s.io/utils/clock"
	clocktesting "k8s.io/utils/clock/testing"
	"k8s.io/utils/ptr"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	testutils "sigs.k8s.io/jobset/pkg/util/testing"
)

func TestExecuteTTLAfterFinishedPolicy(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)

	now := metav1.Now()

	tests := []struct {
		name               string
		jobset             *jobset.JobSet
		expectRequeueAfter time.Duration
		expectErr          bool
		expectErrStr       string
		expectDeletion     bool
	}{
		{
			name:               "jobset not finished, TTL not set",
			jobset:             testutils.MakeJobSet(jobSetName, ns).Obj(),
			expectRequeueAfter: 0,
		},
		{
			name:               "jobset not finished, TTL 5s",
			jobset:             testutils.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(5).Obj(),
			expectRequeueAfter: 0,
			expectErr:          true,
			expectErrStr:       "error checking if ttl expired: unable to find the status of the finished JobSet default/test-jobset",
		},
		{
			name:               "jobset completed now, TTL is not set",
			jobset:             testutils.MakeJobSet(jobSetName, ns).CompletedCondition(now).Obj(),
			expectRequeueAfter: 0,
		},
		{
			name:               "jobset completed 10s ago, TTL 5s",
			jobset:             testutils.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(5).CompletedCondition(metav1.NewTime(now.Time.Add(-10 * time.Second))).Obj(),
			expectRequeueAfter: 0 * time.Second,
			expectDeletion:     true,
		},
		{
			name:               "jobset completed 2s ago, TTL 5s",
			jobset:             testutils.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(5).CompletedCondition(metav1.NewTime(now.Time.Add(-2 * time.Second))).Obj(),
			expectRequeueAfter: 3 * time.Second,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, ctx := ktesting.NewTestContext(t)
			scheme := runtime.NewScheme()
			utilruntime.Must(jobset.AddToScheme(scheme))
			utilruntime.Must(corev1.AddToScheme(scheme))
			utilruntime.Must(batchv1.AddToScheme(scheme))
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.jobset).Build()
			fakeClock := clocktesting.NewFakeClock(now.Time)
			gotRequeueAfter, gotErr := executeTTLAfterFinishedPolicy(ctx, fakeClient, fakeClock, tc.jobset)
			if tc.expectErr != (gotErr != nil) {
				t.Errorf("expected error is %t, got %t, error: %v", tc.expectErr, gotErr != nil, gotErr)
			}
			if tc.expectErr && len(tc.expectErrStr) == 0 {
				t.Error("invalid test setup; error message must not be empty for error cases")
			}
			if tc.expectErr && !strings.Contains(gotErr.Error(), tc.expectErrStr) {
				t.Errorf("expected error message contains %q, got %v", tc.expectErrStr, gotErr)
			}
			if gotRequeueAfter != tc.expectRequeueAfter {
				t.Errorf("expected requeueAfter to be %v, got %v", tc.expectRequeueAfter, gotRequeueAfter)
			}
			if tc.expectDeletion {
				expectJobSetDeleted(ctx, t, fakeClient, tc.jobset)
			} else {
				expectJobSetNotDeleted(ctx, t, fakeClient, tc.jobset)
			}
		})
	}
}

func expectJobSetDeleted(ctx context.Context, t *testing.T, c client.Client, js *jobset.JobSet) {
	t.Helper()

	var fetched jobset.JobSet
	if err := c.Get(ctx, client.ObjectKeyFromObject(js), &fetched); !apierrors.IsNotFound(err) {
		t.Error("expected jobset to be deleted, but it was not")
	}
}

func expectJobSetNotDeleted(ctx context.Context, t *testing.T, c client.Client, js *jobset.JobSet) {
	t.Helper()

	var fetched jobset.JobSet
	if err := c.Get(ctx, client.ObjectKeyFromObject(js), &fetched); err != nil {
		t.Error("expected jobset not to be deleted, but it was")
	}
}

func TestCheckIfTTLExpired(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)

	now := metav1.Now()

	tests := []struct {
		name          string
		jobset        *jobset.JobSet
		clock         clock.Clock
		expectExpired bool
		expectErr     bool
		expectErrStr  string
	}{
		{
			name:          "jobset completed, TTL not set",
			jobset:        testutils.MakeJobSet(jobSetName, ns).CompletedCondition(now).Obj(),
			clock:         clocktesting.NewFakeClock(now.Time),
			expectExpired: false,
			expectErr:     false,
		},
		{
			name:          "jobset completed, TTL not expired",
			jobset:        testutils.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(10).CompletedCondition(now).Obj(),
			clock:         clocktesting.NewFakeClock(now.Time),
			expectExpired: false,
		},
		{
			name:          "jobset completed, TTL expired",
			jobset:        testutils.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(10).CompletedCondition(now).Obj(),
			clock:         clocktesting.NewFakeClock(now.Add(15 * time.Second)),
			expectExpired: true,
		},
		{
			name:          "jobset running, TTL set",
			jobset:        testutils.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(10).Obj(),
			clock:         clocktesting.NewFakeClock(now.Time),
			expectExpired: false,
			expectErr:     true,
			expectErrStr:  "unable to find the status of the finished JobSet default/test-jobset",
		},
		{
			name:          "jobset running, TTL not set",
			jobset:        testutils.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(10).Obj(),
			clock:         clocktesting.NewFakeClock(now.Time),
			expectExpired: false,
			expectErr:     true,
			expectErrStr:  "unable to find the status of the finished JobSet default/test-jobset",
		},
		{
			name:          "jobset completed, deletion in progress",
			jobset:        testutils.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(10).CompletedCondition(now).DeletionTimestamp(&now).Obj(),
			clock:         clocktesting.NewFakeClock(now.Time),
			expectExpired: false,
			expectErr:     false,
		},
		{
			name:          "invalid jobset completion time",
			jobset:        testutils.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(10).CompletedCondition(metav1.Time{}).Obj(),
			clock:         clocktesting.NewFakeClock(now.Time),
			expectExpired: false,
			expectErr:     true,
			expectErrStr:  "unable to find the time when the JobSet default/test-jobset finished",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, ctx := ktesting.NewTestContext(t)
			r := &JobSetReconciler{clock: tc.clock}
			expired, err := checkIfTTLExpired(ctx, r.clock, tc.jobset)
			if tc.expectErr != (err != nil) {
				t.Errorf("expected error: %v, got: %v", tc.expectErr, err)
			}
			if tc.expectErr && !strings.Contains(err.Error(), tc.expectErrStr) {
				t.Errorf("expected error string '%s' to be in '%v'", tc.expectErrStr, err)
			}
			if expired != tc.expectExpired {
				t.Errorf("expected expired to be %v, got %v", tc.expectExpired, expired)
			}
		})
	}
}

func TestTimeLeft(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)

	now := metav1.Now()

	tests := []struct {
		name             string
		jobset           *jobset.JobSet
		now              *time.Time
		expectErr        bool
		expectErrStr     string
		expectedTimeLeft *time.Duration
	}{
		{
			name:         "jobset not finished",
			jobset:       testutils.MakeJobSet(jobSetName, ns).Obj(),
			expectErr:    true,
			expectErrStr: "unable to find the status of the finished JobSet default/test-jobset",
		},
		{
			name:         "jobset completed now, nil now",
			jobset:       testutils.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(0).CompletedCondition(now).Obj(),
			now:          nil,
			expectErr:    true,
			expectErrStr: "calculated invalid expiration time, jobset cleanup will be deferred",
		},
		{
			name:             "jobset completed now, 0s TTL",
			jobset:           testutils.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(0).CompletedCondition(now).Obj(),
			now:              &now.Time,
			expectedTimeLeft: ptr.To(0 * time.Second),
		},
		{
			name:             "jobset completed now, 10s TTL",
			jobset:           testutils.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(10).CompletedCondition(now).Obj(),
			now:              &now.Time,
			expectedTimeLeft: ptr.To(10 * time.Second),
		},
		{
			name:             "jobset completed 10s ago, 15s TTL",
			jobset:           testutils.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(15).CompletedCondition(metav1.NewTime(now.Add(-10 * time.Second))).Obj(),
			now:              &now.Time,
			expectedTimeLeft: ptr.To(5 * time.Second),
		},
		{
			name:             "jobset failed now, 0s TTL",
			jobset:           testutils.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(0).FailedCondition(now).Obj(),
			now:              &now.Time,
			expectedTimeLeft: ptr.To(0 * time.Second),
		},
		{
			name:             "jobset failed now, 10s TTL",
			jobset:           testutils.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(10).FailedCondition(now).Obj(),
			now:              &now.Time,
			expectedTimeLeft: ptr.To(10 * time.Second),
		},
		{
			name:             "jobset failed 10s ago, 15s TTL",
			jobset:           testutils.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(15).FailedCondition(metav1.NewTime(now.Add(-10 * time.Second))).Obj(),
			now:              &now.Time,
			expectedTimeLeft: ptr.To(5 * time.Second),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, ctx := ktesting.NewTestContext(t)
			gotTimeLeft, gotErr := timeLeft(ctx, tc.jobset, tc.now)
			if tc.expectErr != (gotErr != nil) {
				t.Errorf("expected error is %t, got %t, error: %v", tc.expectErr, gotErr != nil, gotErr)
			}
			if tc.expectErr && len(tc.expectErrStr) == 0 {
				t.Error("invalid test setup; error message must not be empty for error cases")
			}
			if tc.expectErr && !strings.Contains(gotErr.Error(), tc.expectErrStr) {
				t.Errorf("expected error message contains %q, got %v", tc.expectErrStr, gotErr)
			}
			if !tc.expectErr {
				if gotTimeLeft != nil && *gotTimeLeft != *tc.expectedTimeLeft {
					t.Errorf("expected time left %v, got %v", tc.expectedTimeLeft, gotTimeLeft)
				}
			}
		})
	}
}

func TestRequeueJobSetAfter(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)

	now := time.Now()

	tests := []struct {
		name               string
		js                 *jobset.JobSet
		expectRequeueAfter time.Duration
		expectErr          bool
		expectErrStr       string
	}{
		{
			name:               "jobset not finished, TTL not set",
			js:                 testutils.MakeJobSet(jobSetName, ns).Obj(),
			expectRequeueAfter: 0,
		},
		{
			name:               "jobset not finished, TTL 5s",
			js:                 testutils.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(5).Obj(),
			expectRequeueAfter: 0,
			expectErr:          true,
			expectErrStr:       "unable to find the status of the finished JobSet default/test-jobset",
		},
		{
			name:               "jobset completed now, TTL is not set",
			js:                 testutils.MakeJobSet(jobSetName, ns).CompletedCondition(metav1.NewTime(now)).Obj(),
			expectRequeueAfter: 0,
		},
		{
			name:               "jobset completed now, TTL 5s",
			js:                 testutils.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(5).CompletedCondition(metav1.NewTime(now)).Obj(),
			expectRequeueAfter: 5 * time.Second,
		},
		{
			name:               "jobset completed 2s ago, TTL 5s",
			js:                 testutils.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(5).CompletedCondition(metav1.NewTime(now.Add(-2 * time.Second))).Obj(),
			expectRequeueAfter: 3 * time.Second,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotRequeueAfter, gotErr := requeueJobSetAfter(tc.js, now)
			if tc.expectErr != (gotErr != nil) {
				t.Errorf("expected error is %t, got %t, error: %v", tc.expectErr, gotErr != nil, gotErr)
			}
			if tc.expectErr && len(tc.expectErrStr) == 0 {
				t.Error("invalid test setup; error message must not be empty for error cases")
			}
			if tc.expectErr && !strings.Contains(gotErr.Error(), tc.expectErrStr) {
				t.Errorf("expected error message contains %q, got %v", tc.expectErrStr, gotErr)
			}
			if gotRequeueAfter != tc.expectRequeueAfter {
				t.Errorf("expected requeueAfter to be %v, got %v", tc.expectRequeueAfter, gotRequeueAfter)
			}
		})
	}
}
