/*
Copyright 2023 The Kubernetes Authors.
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
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobsetv1alpha2 "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	jobsetinformerv1alpha2 "sigs.k8s.io/jobset/client-go/informers/externalversions/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/controllers/metrics"
)

//+kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets,verbs=get;delete
//+kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets/status,verbs=get

// TTLAfterFinishedReconciler reconciles a Pod owned by a JobSet using exclusive placement.
type TTLAfterFinishedReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// listerSynced returns true if the JobSet store has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	listerSynced cache.InformerSynced
	// Jobs that the controller will check its TTL and attempt to delete when the TTL expires.
	queue workqueue.RateLimitingInterface
	// The clock for tracking time
	clock clock.Clock
	// log is the logger for the controller
	log logr.Logger
}

// NewTTLAfterFinishedReconciler creates an instance of Controller
func NewTTLAfterFinishedReconciler(
	client client.Client,
	scheme *runtime.Scheme,
	jobSetInformer jobsetinformerv1alpha2.JobSetInformer,
	log logr.Logger,
) *TTLAfterFinishedReconciler {
	config := workqueue.RateLimitingQueueConfig{Name: "ttl_jobsets_to_delete"}
	tc := &TTLAfterFinishedReconciler{
		Client: client,
		Scheme: scheme,
		queue:  workqueue.NewRateLimitingQueueWithConfig(workqueue.DefaultControllerRateLimiter(), config),
		log:    log,
	}

	_, _ = jobSetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			tc.addJobSet(obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			tc.updateJobSet(oldObj, newObj)
		},
	})

	tc.listerSynced = jobSetInformer.Informer().HasSynced

	tc.clock = clock.RealClock{}

	return tc
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *TTLAfterFinishedReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TTLAfterFinishedReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&jobsetv1alpha2.JobSet{}).
		Complete(r)
}

// Run starts the workers to clean up Jobs.
func (r *TTLAfterFinishedReconciler) Run(ctx context.Context, workers int) {
	defer utilruntime.HandleCrash()
	defer r.queue.ShutDown()

	r.log.V(2).Info("Starting TTL after finished controller")
	defer r.log.V(2).Info("Shutting down TTL after finished controller")

	if !cache.WaitForNamedCacheSync("TTL after finished", ctx.Done(), r.listerSynced) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, r.worker, time.Second)
	}

	<-ctx.Done()
}

func (r *TTLAfterFinishedReconciler) addJobSet(obj interface{}) {
	jobSet := obj.(*jobsetv1alpha2.JobSet)
	r.log.V(2).Info("Adding jobset", "jobset", klog.KObj(jobSet))

	if jobSet.DeletionTimestamp == nil && needsCleanup(jobSet) {
		r.enqueue(jobSet)
	}

}

func (r *TTLAfterFinishedReconciler) updateJobSet(old, cur interface{}) {
	jobSet := cur.(*jobsetv1alpha2.JobSet)
	r.log.V(2).Info("Updating jobset", "jobset", klog.KObj(jobSet))

	if jobSet.DeletionTimestamp == nil && needsCleanup(jobSet) {
		r.enqueue(jobSet)
	}
}

func (r *TTLAfterFinishedReconciler) enqueue(jobSet *jobsetv1alpha2.JobSet) {
	r.log.V(2).Info("Add jobset to cleanup", "jobset", klog.KObj(jobSet))
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(jobSet)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", jobSet, err))
		return
	}

	r.queue.Add(key)
}

func (r *TTLAfterFinishedReconciler) enqueueAfter(jobSet *jobsetv1alpha2.JobSet, after time.Duration) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(jobSet)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", jobSet, err))
		return
	}

	r.queue.AddAfter(key, after)
}

func (r *TTLAfterFinishedReconciler) worker(ctx context.Context) {
	for r.processNextWorkItem(ctx) {
	}
}

func (r *TTLAfterFinishedReconciler) processNextWorkItem(ctx context.Context) bool {
	key, quit := r.queue.Get()
	if quit {
		return false
	}
	defer r.queue.Done(key)

	err := r.processJobSet(ctx, key.(string))
	r.handleErr(err, key)

	return true
}

func (r *TTLAfterFinishedReconciler) handleErr(err error, key interface{}) {
	if err == nil {
		r.queue.Forget(key)
		return
	}

	utilruntime.HandleError(fmt.Errorf("error cleaning up JobSet %v, will retry: %v", key, err))
	r.queue.AddRateLimited(key)
}

// processJobSet will check the JobSet's state and TTL and delete the JobSet when it
// finishes and its TTL after finished has expired. If the JobSet hasn't finished or
// its TTL hasn't expired, it will be added to the queue after the TTL is expected
// to expire.
// This function is not meant to be invoked concurrently with the same key.
func (r *TTLAfterFinishedReconciler) processJobSet(ctx context.Context, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	// Ignore the JobSets that are already deleted or being deleted, or the ones that don't need to clean up.
	var jobSet jobsetv1alpha2.JobSet
	err = r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &jobSet)

	r.log.V(2).Info("Checking if JobSet is ready for cleanup", "jobset", klog.KRef(namespace, name))

	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if expiredAt, err := r.processTTL(&jobSet); err != nil {
		return err
	} else if expiredAt == nil {
		return nil
	}

	// The JobSet's TTL is assumed to have expired, but the JobSet TTL might be stale.
	// Before deleting the JobSet, do a final sanity check.
	// If TTL is modified before we do this check, we cannot be sure if the TTL truly expires.
	// The latest JobSet may have a different UID, but it's fine because the checks will be run again.
	var fresh jobsetv1alpha2.JobSet
	err = r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &fresh)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	// Use the latest JobSet TTL to see if the TTL truly expires.
	expiredAt, err := r.processTTL(&fresh)
	if err != nil {
		return err
	} else if expiredAt == nil {
		return nil
	}
	// Cascade deletes the JobSets if TTL truly expires.
	policy := metav1.DeletePropagationForeground
	options := []client.DeleteOption{client.PropagationPolicy(policy), client.Preconditions{UID: &fresh.UID}}
	r.log.V(2).Info("Cleaning up JobSet", "jobset", klog.KObj(&fresh))

	if err := r.Client.Delete(ctx, &fresh, options...); err != nil {
		return err
	}
	metrics.JobSetDeletionDurationSeconds.Observe(time.Since(*expiredAt).Seconds())
	return nil
}

// processTTL checks whether a given JobSet's TTL has expired, and add it to the queue after the TTL is expected to expire
// if the TTL will expire later.
func (r *TTLAfterFinishedReconciler) processTTL(jobSet *jobsetv1alpha2.JobSet) (expiredAt *time.Time, err error) {
	// We don't care about the JobSets that are going to be deleted, or the ones that don't need cleanup.
	if jobSet.DeletionTimestamp != nil || !needsCleanup(jobSet) {
		return nil, nil
	}

	now := r.clock.Now()
	t, e, err := timeLeft(r.log, jobSet, &now)
	if err != nil {
		return nil, err
	}

	// TTL has expired
	if *t <= 0 {
		return e, nil
	}

	r.enqueueAfter(jobSet, *t)
	return nil, nil
}

// needsCleanup checks whether a JobSet has finished and has a TTL set.
func needsCleanup(js *jobsetv1alpha2.JobSet) bool {
	return js.Spec.TTLSecondsAfterFinished != nil && jobSetFinished(js)
}

func getFinishAndExpireTime(js *jobsetv1alpha2.JobSet) (*time.Time, *time.Time, error) {
	if !needsCleanup(js) {
		return nil, nil, fmt.Errorf("jobset %s/%s should not be cleaned up", js.Namespace, js.Name)
	}
	t, err := jobSetFinishTime(js)
	if err != nil {
		return nil, nil, err
	}
	finishAt := t.Time
	expireAt := finishAt.Add(time.Duration(*js.Spec.TTLSecondsAfterFinished) * time.Second)
	return &finishAt, &expireAt, nil
}

func timeLeft(log logr.Logger, js *jobsetv1alpha2.JobSet, since *time.Time) (*time.Duration, *time.Time, error) {
	finishAt, expireAt, err := getFinishAndExpireTime(js)
	if err != nil {
		return nil, nil, err
	}

	if finishAt.After(*since) {
		log.V(2).Info("Warning: Found JobSet finished in the future. This is likely due to time skew in the cluster. JobSet cleanup will be deferred.", "jobset", klog.KObj(js))
	}
	remaining := expireAt.Sub(*since)
	log.V(2).Info("Found JobSet finished", "jobset", klog.KObj(js), "finishTime", finishAt.UTC(), "remainingTTL", remaining, "startTime", since.UTC(), "deadlineTTL", expireAt.UTC())
	return &remaining, expireAt, nil
}

// jobSetFinishTime takes an already finished JobSet and returns the time it finishes.
func jobSetFinishTime(finishedJobSet *jobsetv1alpha2.JobSet) (metav1.Time, error) {
	for _, c := range finishedJobSet.Status.Conditions {
		if (c.Type == string(jobsetv1alpha2.JobSetCompleted) || c.Type == string(jobsetv1alpha2.JobSetFailed)) && c.Status == metav1.ConditionTrue {
			finishAt := c.LastTransitionTime
			if finishAt.IsZero() {
				return metav1.Time{}, fmt.Errorf("unable to find the time when the JobSet %s/%s finished", finishedJobSet.Namespace, finishedJobSet.Name)
			}
			return c.LastTransitionTime, nil
		}
	}

	// This should never happen if the JobSets have finished
	return metav1.Time{}, fmt.Errorf("unable to find the status of the finished JobSet %s/%s", finishedJobSet.Namespace, finishedJobSet.Name)
}
