package controllers

import (
	"context"
	"strings"
	"testing"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/workqueue"
	clocktesting "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	utiltesting "sigs.k8s.io/jobset/pkg/util/testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/ktesting"
	"k8s.io/utils/ptr"

	jobsetv1alpha2 "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

func TestTTLAfterFinishedReconciler_Run(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)

	tests := []struct {
		name           string
		clock          *clocktesting.FakeClock
		expectDeletion bool
		ttl            int32
	}{
		{
			name:  "jobset completed 10s ago, 15s ttl",
			clock: clocktesting.NewFakeClock(time.Now().Add(10 * time.Second)),
			ttl:   15,
		},
		{
			name:           "jobset completed 10s ago, 5s ttl",
			clock:          clocktesting.NewFakeClock(time.Now().Add(10 * time.Second)),
			ttl:            5,
			expectDeletion: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			t.Cleanup(cancel)

			fakeClient, controller := startTTLAfterFinishedController(ctx, t, tc.clock)

			js := utiltesting.MakeJobSet(jobSetName, ns).TTLSecondsAfterFinished(tc.ttl).Obj()
			jobSetCompletedCondition := metav1.Condition{
				Type:               string(jobsetv1alpha2.JobSetCompleted),
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
			}
			js.Status.Conditions = append(js.Status.Conditions, jobSetCompletedCondition)
			if err := fakeClient.Create(ctx, js); err != nil {
				t.Fatalf("failed to create jobset %s/%s: %v", ns, jobSetName, err)
			}
			controller.enqueue(js)

			err := wait.PollUntilContextTimeout(
				ctx,
				10*time.Millisecond,
				100*time.Millisecond,
				false,
				func(ctx context.Context) (bool, error) {
					var fresh jobsetv1alpha2.JobSet
					err := fakeClient.Get(ctx, client.ObjectKeyFromObject(js), &fresh)
					if tc.expectDeletion {
						return errors.IsNotFound(err), nil
					} else {
						if err != nil {
							t.Fatalf("failed to get jobset %s/%s: %v", ns, jobSetName, err)
						}
						return fresh.DeletionTimestamp.IsZero(), nil
					}
				},
			)
			if err != nil {
				t.Fatalf("failed to wait for jobset %s/%s to be deleted: %v", ns, jobSetName, err)
			}
		})
	}
}

func TestTimeLeft(t *testing.T) {
	now := metav1.Now()

	testCases := []struct {
		name             string
		completionTime   metav1.Time
		failedTime       metav1.Time
		ttl              *int32
		since            *time.Time
		expectErr        bool
		expectErrStr     string
		expectedTimeLeft *time.Duration
		expectedExpireAt time.Time
	}{
		{
			name:         "Error case: JobSet unfinished",
			ttl:          ptr.To[int32](100),
			since:        &now.Time,
			expectErr:    true,
			expectErrStr: "should not be cleaned up",
		},
		{
			name:           "Error case: JobSet completed now, no TTL",
			completionTime: now,
			since:          &now.Time,
			expectErr:      true,
			expectErrStr:   "should not be cleaned up",
		},
		{
			name:             "JobSet completed now, 0s TTL",
			completionTime:   now,
			ttl:              ptr.To[int32](0),
			since:            &now.Time,
			expectedTimeLeft: ptr.To(0 * time.Second),
			expectedExpireAt: now.Time,
		},
		{
			name:             "JobSet completed now, 10s TTL",
			completionTime:   now,
			ttl:              ptr.To[int32](10),
			since:            &now.Time,
			expectedTimeLeft: ptr.To(10 * time.Second),
			expectedExpireAt: now.Add(10 * time.Second),
		},
		{
			name:             "JobSet completed 10s ago, 15s TTL",
			completionTime:   metav1.NewTime(now.Add(-10 * time.Second)),
			ttl:              ptr.To[int32](15),
			since:            &now.Time,
			expectedTimeLeft: ptr.To(5 * time.Second),
			expectedExpireAt: now.Add(5 * time.Second),
		},
		{
			name:         "Error case: JobSet failed now, no TTL",
			failedTime:   now,
			since:        &now.Time,
			expectErr:    true,
			expectErrStr: "should not be cleaned up",
		},
		{
			name:             "JobSet failed now, 0s TTL",
			failedTime:       now,
			ttl:              ptr.To[int32](0),
			since:            &now.Time,
			expectedTimeLeft: ptr.To(0 * time.Second),
			expectedExpireAt: now.Time,
		},
		{
			name:             "JobSet failed now, 10s TTL",
			failedTime:       now,
			ttl:              ptr.To[int32](10),
			since:            &now.Time,
			expectedTimeLeft: ptr.To(10 * time.Second),
			expectedExpireAt: now.Add(10 * time.Second),
		},
		{
			name:             "JobSet failed 10s ago, 15s TTL",
			failedTime:       metav1.NewTime(now.Add(-10 * time.Second)),
			ttl:              ptr.To[int32](15),
			since:            &now.Time,
			expectedTimeLeft: ptr.To(5 * time.Second),
			expectedExpireAt: now.Add(5 * time.Second),
		},
	}

	for _, tc := range testCases {

		jobSet := newJobSet(tc.completionTime, tc.failedTime, tc.ttl)
		_, ctx := ktesting.NewTestContext(t)
		logger := klog.FromContext(ctx)
		gotTimeLeft, gotExpireAt, gotErr := timeLeft(logger, jobSet, tc.since)
		if tc.expectErr != (gotErr != nil) {
			t.Errorf("%s: expected error is %t, got %t, error: %v", tc.name, tc.expectErr, gotErr != nil, gotErr)
		}
		if tc.expectErr && len(tc.expectErrStr) == 0 {
			t.Errorf("%s: invalid test setup; error message must not be empty for error cases", tc.name)
		}
		if tc.expectErr && !strings.Contains(gotErr.Error(), tc.expectErrStr) {
			t.Errorf("%s: expected error message contains %q, got %v", tc.name, tc.expectErrStr, gotErr)
		}
		if !tc.expectErr {
			if *gotTimeLeft != *tc.expectedTimeLeft {
				t.Errorf("%s: expected time left %v, got %v", tc.name, tc.expectedTimeLeft, gotTimeLeft)
			}
			if *gotExpireAt != tc.expectedExpireAt {
				t.Errorf("%s: expected expire at %v, got %v", tc.name, tc.expectedExpireAt, *gotExpireAt)
			}
		}
	}
}

func newJobSet(completionTime, failedTime metav1.Time, ttl *int32) *jobsetv1alpha2.JobSet {
	js := &jobsetv1alpha2.JobSet{
		TypeMeta: metav1.TypeMeta{Kind: "JobSet"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foobar",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: jobsetv1alpha2.JobSetSpec{
			ReplicatedJobs: []jobsetv1alpha2.ReplicatedJob{
				{
					Name: "foobar-job",
					Template: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"foo": "bar"},
							},
							Template: corev1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{
										"foo": "bar",
									},
								},
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{Image: "foo/bar"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if !completionTime.IsZero() {
		c := metav1.Condition{Type: string(jobsetv1alpha2.JobSetCompleted), Status: metav1.ConditionTrue, LastTransitionTime: completionTime}
		js.Status.Conditions = append(js.Status.Conditions, c)
	}

	if !failedTime.IsZero() {
		c := metav1.Condition{Type: string(jobsetv1alpha2.JobSetFailed), Status: metav1.ConditionTrue, LastTransitionTime: failedTime}
		js.Status.Conditions = append(js.Status.Conditions, c)
	}

	if ttl != nil {
		js.Spec.TTLSecondsAfterFinished = ttl
	}

	return js
}

func startTTLAfterFinishedController(ctx context.Context, t *testing.T, clock *clocktesting.FakeClock) (client.WithWatch, *TTLAfterFinishedReconciler) {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add client-go scheme: %v", err)
	}
	if err := jobsetv1alpha2.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add resource scheme: %v", err)
	}

	queueConfig := workqueue.RateLimitingQueueConfig{Name: "ttl_jobsets_to_delete"}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	controller := TTLAfterFinishedReconciler{
		Client:       fakeClient,
		Scheme:       scheme,
		listerSynced: func() bool { return true },
		queue:        workqueue.NewRateLimitingQueueWithConfig(workqueue.DefaultControllerRateLimiter(), queueConfig),
		clock:        clock,
		log:          ctrl.Log.WithValues("controller", "TTLAfterFinished"),
	}

	go controller.Run(ctx, 1)

	return fakeClient, &controller
}
