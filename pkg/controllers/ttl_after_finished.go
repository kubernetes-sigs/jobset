package controllers

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha1"
)

// executeTTLAfterFinishedPolicy checks if the JobSet has a TTLSecondsAfterFinished set.
// If the JobSet has expired, it deletes the JobSet.
// If the JobSet has not expired, it returns the time after which the JobSet should be requeued.
// If the JobSet does not have a TTLSecondsAfterFinished set, it returns 0.
func executeTTLAfterFinishedPolicy(ctx context.Context, client client.Client, clock clock.Clock, js *jobset.JobSet) (time.Duration, error) {
	log := ctrl.LoggerFrom(ctx)

	if js.Spec.TTLSecondsAfterFinished != nil {
		expired, err := checkIfTTLExpired(ctx, clock, js)
		if err != nil {
			return 0, fmt.Errorf("error checking if ttl expired: %w", err)
		}
		// if expired is true, that means the TTL has expired, and we should delete the JobSet
		// otherwise, we requeue it for the remaining TTL duration.
		if expired {
			log.V(2).Info("JobSet TTL expired, deleting")
			if err := deleteJobSet(ctx, client, js); err != nil {
				return 0, err
			}
		} else {
			return requeueJobSetAfter(js, clock.Now())
		}
	}
	return 0, nil
}

// checkIfTTLExpired checks whether a given JobSet's TTL has expired.
func checkIfTTLExpired(ctx context.Context, clock clock.Clock, js *jobset.JobSet) (bool, error) {
	// We don't care about the JobSets that don't have a TTL configured or are going to be deleted
	if js.Spec.TTLSecondsAfterFinished == nil || js.DeletionTimestamp != nil {
		return false, nil
	}

	now := clock.Now()
	remaining, err := timeLeft(ctx, js, &now)
	if err != nil {
		return false, err
	}

	// TTL has expired
	ttlExpired := remaining != nil && *remaining <= 0
	return ttlExpired, nil
}

// timeLeft returns the time left until the JobSet's TTL expires and the time when it will expire.
func timeLeft(ctx context.Context, js *jobset.JobSet, now *time.Time) (*time.Duration, error) {
	log := ctrl.LoggerFrom(ctx)

	finishAt, expireAt, err := getJobSetFinishAndExpireTime(js)
	if err != nil {
		return nil, err
	}
	// The following check does sanity checking for nil pointers in case of changes to the above function.
	// This logic should never be executed.
	if now == nil || finishAt == nil || expireAt == nil {
		return nil, fmt.Errorf("calculated invalid expiration time, jobset cleanup will be deferred")
	}

	if finishAt.After(*now) {
		log.V(5).Info("Found JobSet finished in the future. This is likely due to time skew in the cluster.")
	}
	remaining := expireAt.Sub(*now)
	log.V(5).Info("Found JobSet finished", "finishTime", finishAt.UTC(), "remainingTTL", remaining, "startTime", now.UTC(), "deadlineTTL", expireAt.UTC())
	return &remaining, nil
}

func getJobSetFinishAndExpireTime(js *jobset.JobSet) (finishAt, expireAt *time.Time, err error) {
	finishTime, err := jobSetFinishTime(js)
	if err != nil {
		return nil, nil, err
	}

	finishAt = &finishTime.Time
	expiration := finishAt.Add(time.Duration(*js.Spec.TTLSecondsAfterFinished) * time.Second)
	expireAt = ptr.To(expiration)
	return finishAt, expireAt, nil
}

// jobSetFinishTime takes an already finished JobSet and returns the time it finishes.
func jobSetFinishTime(finishedJobSet *jobset.JobSet) (metav1.Time, error) {
	for _, c := range finishedJobSet.Status.Conditions {
		if (c.Type == string(jobset.JobSetCompleted) || c.Type == string(jobset.JobSetFailed)) && c.Status == metav1.ConditionTrue {
			finishAt := c.LastTransitionTime
			if finishAt.IsZero() {
				return metav1.Time{}, fmt.Errorf("unable to find the time when the JobSet %s/%s finished", finishedJobSet.Namespace, finishedJobSet.Name)
			}
			return finishAt, nil
		}
	}

	// This should never happen if the JobSets have finished
	return metav1.Time{}, fmt.Errorf("unable to find the status of the finished JobSet %s/%s", finishedJobSet.Namespace, finishedJobSet.Name)
}

// requeueJobSetAfter returns the duration after which the JobSet should be requeued if TTLSecondsAfterFinished is set, otherwise returns 0.
func requeueJobSetAfter(js *jobset.JobSet, now time.Time) (time.Duration, error) {
	var requeueAfter time.Duration = 0
	if js.Spec.TTLSecondsAfterFinished != nil {
		finishedAt, err := jobSetFinishTime(js)
		if err != nil {
			return 0, err
		}
		ttl := time.Duration(*js.Spec.TTLSecondsAfterFinished) * time.Second
		requeueAfter = finishedAt.Add(ttl).Sub(now)
	}
	return requeueAfter, nil
}

func deleteJobSet(ctx context.Context, c client.Client, js *jobset.JobSet) error {
	log := ctrl.LoggerFrom(ctx)

	policy := metav1.DeletePropagationForeground
	options := []client.DeleteOption{client.PropagationPolicy(policy)}
	log.V(2).Info("Cleaning up JobSet", "jobset", klog.KObj(js))

	return c.Delete(ctx, js, options...)
}
