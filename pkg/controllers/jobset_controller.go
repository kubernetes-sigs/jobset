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
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"sync"

	"k8s.io/utils/clock"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
	"sigs.k8s.io/jobset/pkg/metrics"
	"sigs.k8s.io/jobset/pkg/util/collections"
	"sigs.k8s.io/jobset/pkg/util/placement"
)

var apiGVStr = jobset.GroupVersion.String()

// JobSetReconciler reconciles a JobSet object
type JobSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Record record.EventRecorder
	clock  clock.Clock
}

type childJobs struct {
	// Only jobs with jobset.sigs.k8s.io/restart-attempt == jobset.status.restarts are included
	// in active, successful, and failed jobs. These jobs are part of the current JobSet run.
	active     []*batchv1.Job
	successful []*batchv1.Job
	failed     []*batchv1.Job

	// Jobs from a previous restart (marked for deletion) are mutually exclusive
	// with the set of jobs in active, successful, and failed.
	previous []*batchv1.Job
}

// statusUpdateOpts tracks if a JobSet status update should be performed at the end of the reconciliation
// attempt, as well as events that should be conditionally emitted if the status update succeeds.
type statusUpdateOpts struct {
	shouldUpdate bool
	events       []*eventParams
}

// eventParams contains parameters used for emitting a Kubernetes event.
type eventParams struct {
	// object is the object that caused the event or is for some other reason associated with the event.
	object runtime.Object
	// eventType is a machine interpretable CamelCase string like "PanicTypeFooBar" describing the type of event.
	eventType string
	// eventReason is a machine interpretable CamelCase string like "FailureReasonFooBar" describing the reason for the event.
	eventReason string
	// eventMessage is a human interpretable sentence with details about the event.
	eventMessage string
}

func NewJobSetReconciler(client client.Client, scheme *runtime.Scheme, record record.EventRecorder) *JobSetReconciler {
	return &JobSetReconciler{Client: client, Scheme: scheme, Record: record, clock: clock.RealClock{}}
}

//+kubebuilder:rbac:groups="",resources=events,verbs=create;watch;update;patch
//+kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *JobSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Get JobSet from apiserver.
	var js jobset.JobSet
	if err := r.Get(ctx, req.NamespacedName, &js); err != nil {
		// we'll ignore not-found errors, since there is nothing we can do here.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Don't reconcile JobSets marked for deletion.
	if jobSetMarkedForDeletion(&js) {
		return ctrl.Result{}, nil
	}

	// Track JobSet status updates that should be performed at the end of the reconciliation attempt.
	updateStatusOpts := statusUpdateOpts{}

	// Reconcile the JobSet.
	result, err := r.reconcile(ctx, &js, &updateStatusOpts)
	if err != nil {
		return result, err
	}

	if err := r.updateJobSetStatus(ctx, &js, &updateStatusOpts); apierrors.IsConflict(err) {
		return ctrl.Result{Requeue: true}, nil
	}
	// At the end of this Reconcile attempt, do one API call to persist all the JobSet status changes.
	return ctrl.Result{RequeueAfter: result.RequeueAfter}, err
}

// reconcile is the internal method containing the core JobSet reconciliation logic.
func (r *JobSetReconciler) reconcile(ctx context.Context, js *jobset.JobSet, updateStatusOpts *statusUpdateOpts) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithValues("jobset", klog.KObj(js))
	ctx = ctrl.LoggerInto(ctx, log)

	// Check the controller configured for the JobSet.
	// See https://github.com/kubernetes-sigs/kueue/tree/559faa1aece36d3e3e09001673278396ec28b0cb/keps/693-multikueue
	// for why a JobSet would not be controlled by the default JobSet controller.
	if manager := managedByExternalController(js); manager != nil {
		log.V(5).Info("Skipping JobSet managed by a different controller", "managed-by", manager)
		return ctrl.Result{}, nil
	}

	log.V(2).Info("Reconciling JobSet")

	// Get Jobs owned by JobSet.
	ownedJobs, err := r.getChildJobs(ctx, js)
	if err != nil {
		log.Error(err, "getting jobs owned by jobset")
		return ctrl.Result{}, err
	}

	// Calculate JobsReady and update statuses for each ReplicatedJob.
	rjobStatuses := r.calculateReplicatedJobStatuses(ctx, js, ownedJobs)
	updateReplicatedJobsStatuses(js, rjobStatuses, updateStatusOpts)

	// If JobSet is already completed or failed, clean up active child jobs and requeue if TTLSecondsAfterFinished is set.
	if jobSetFinished(js) {
		if err := r.deleteJobs(ctx, ownedJobs.active); err != nil {
			log.Error(err, "deleting jobs")
			return ctrl.Result{}, err
		}
		requeueAfter, err := executeTTLAfterFinishedPolicy(ctx, r.Client, r.clock, js)
		if err != nil {
			log.Error(err, "executing ttl after finished policy")
			return ctrl.Result{}, err
		}
		if requeueAfter > 0 {
			return ctrl.Result{RequeueAfter: requeueAfter}, nil
		}
		return ctrl.Result{}, nil
	}

	// Delete all jobs from a previous restart that are marked for deletion.
	if err := r.deleteJobs(ctx, ownedJobs.previous); err != nil {
		log.Error(err, "deleting jobs")
		return ctrl.Result{}, err
	}

	// If any jobs have failed, execute the JobSet failure policy (if any).
	if len(ownedJobs.failed) > 0 {
		if err := executeFailurePolicy(ctx, js, ownedJobs, updateStatusOpts); err != nil {
			log.Error(err, "executing failure policy")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// If any jobs have succeeded, execute the JobSet success policy.
	if len(ownedJobs.successful) > 0 {
		if completed := executeSuccessPolicy(js, ownedJobs, updateStatusOpts); completed {
			return ctrl.Result{}, nil
		}
	}

	// If pod DNS hostnames are enabled, create a headless service for the JobSet
	if err := r.createHeadlessSvcIfNecessary(ctx, js); err != nil {
		log.Error(err, "creating headless service")
		return ctrl.Result{}, err
	}

	// If job has not failed or succeeded, reconcile the state of the replicatedJobs.
	if err := r.reconcileReplicatedJobs(ctx, js, ownedJobs, rjobStatuses, updateStatusOpts); err != nil {
		log.Error(err, "creating jobs")
		return ctrl.Result{}, err
	}

	// Handle suspending a jobset or resuming a suspended jobset.
	jobsetSuspended := jobSetSuspended(js)
	if jobsetSuspended {
		if err := r.suspendJobs(ctx, js, ownedJobs.active, updateStatusOpts); err != nil {
			log.Error(err, "suspending jobset")
			return ctrl.Result{}, err
		}
	} else {
		if err := r.resumeJobsIfNecessary(ctx, js, ownedJobs.active, rjobStatuses, updateStatusOpts); err != nil {
			log.Error(err, "resuming jobset")
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *JobSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&jobset.JobSet{}).
		Owns(&batchv1.Job{}).
		Owns(&corev1.Service{}).
		Complete(r)
}

func SetupJobSetIndexes(ctx context.Context, indexer client.FieldIndexer) error {
	return indexer.IndexField(ctx, &batchv1.Job{}, constants.JobOwnerKey, func(obj client.Object) []string {
		o := obj.(*batchv1.Job)
		owner := metav1.GetControllerOf(o)
		if owner == nil {
			return nil
		}
		// ...make sure it's a JobSet...
		if owner.APIVersion != apiGVStr || owner.Kind != "JobSet" {
			return nil
		}
		return []string{owner.Name}
	})
}

// updateJobSetStatus will update the JobSet status if updateStatusOpts requires it,
// and conditionally emit events in updateStatusOpts if the status update call succeeds.
func (r *JobSetReconciler) updateJobSetStatus(ctx context.Context, js *jobset.JobSet, updateStatusOpts *statusUpdateOpts) error {
	log := ctrl.LoggerFrom(ctx)

	if updateStatusOpts.shouldUpdate {
		// Make single API call to persist the JobSet status update.
		if err := r.Status().Update(ctx, js); err != nil {
			if !apierrors.IsConflict(err) {
				log.Error(err, "updating jobset status")
			}
			return err
		}
		// If the status update was successful, emit any enqueued events.
		for _, event := range updateStatusOpts.events {
			r.Record.Eventf(event.object, event.eventType, event.eventReason, event.eventMessage)
		}
	}
	return nil
}

// getChildJobs gets jobs owned by the JobSet then categorizes them by status (active, successful, failed).
// Another list (`delete`) is also added which tracks jobs marked for deletion.
func (r *JobSetReconciler) getChildJobs(ctx context.Context, js *jobset.JobSet) (*childJobs, error) {
	log := ctrl.LoggerFrom(ctx)

	// Get all active jobs owned by JobSet.
	var childJobList batchv1.JobList
	if err := r.List(ctx, &childJobList, client.InNamespace(js.Namespace), client.MatchingFields{constants.JobOwnerKey: js.Name}); err != nil {
		return nil, err
	}

	// Categorize each job into a bucket: active, successful, failed, or delete.
	ownedJobs := childJobs{}
	for i, job := range childJobList.Items {
		// Jobs with jobset.sigs.k8s.io/restart-attempt < jobset.status.restarts are marked for
		// deletion, as they were part of the previous JobSet run.
		jobRestarts, err := strconv.Atoi(job.Labels[constants.RestartsKey])
		if err != nil {
			log.Error(err, fmt.Sprintf("invalid value for label %s, must be integer", constants.RestartsKey))
			ownedJobs.previous = append(ownedJobs.previous, &childJobList.Items[i])
			return nil, err
		}
		if int32(jobRestarts) < js.Status.Restarts {
			ownedJobs.previous = append(ownedJobs.previous, &childJobList.Items[i])
			continue
		}

		// Jobs with jobset.sigs.k8s.io/restart-attempt == jobset.status.restarts are part of
		// the current JobSet run, and marked either active, successful, or failed.
		_, finishedType := JobFinished(&job)
		switch finishedType {
		case "": // active
			ownedJobs.active = append(ownedJobs.active, &childJobList.Items[i])
		case batchv1.JobFailed:
			ownedJobs.failed = append(ownedJobs.failed, &childJobList.Items[i])
		case batchv1.JobComplete:
			ownedJobs.successful = append(ownedJobs.successful, &childJobList.Items[i])
		}
	}
	return &ownedJobs, nil
}

// updateReplicatedJobsStatuses updates the replicatedJob statuses if they have changed.
func updateReplicatedJobsStatuses(js *jobset.JobSet, statuses []jobset.ReplicatedJobStatus, updateStatusOpts *statusUpdateOpts) {
	// If replicated job statuses haven't changed, there's nothing to do here.
	if replicatedJobStatusesEqual(js.Status.ReplicatedJobsStatus, statuses) {
		return
	}
	// Add a new status update to perform at the end of the reconciliation attempt.
	js.Status.ReplicatedJobsStatus = statuses
	updateStatusOpts.shouldUpdate = true
}

// calculateReplicatedJobStatuses uses the JobSet's child jobs to update the statuses
// of each of its replicatedJobs.
func (r *JobSetReconciler) calculateReplicatedJobStatuses(ctx context.Context, js *jobset.JobSet, jobs *childJobs) []jobset.ReplicatedJobStatus {
	log := ctrl.LoggerFrom(ctx)

	// Prepare replicatedJobsReady for optimal iteration
	replicatedJobsReady := map[string]map[string]int32{}
	for _, replicatedJob := range js.Spec.ReplicatedJobs {
		replicatedJobsReady[replicatedJob.Name] = map[string]int32{
			"ready":     0,
			"succeeded": 0,
			"failed":    0,
			"active":    0,
			"suspended": 0,
		}
	}

	// Calculate jobsReady for each Replicated Job
	for _, job := range jobs.active {
		if job.Labels == nil || job.Labels[jobset.ReplicatedJobNameKey] == "" {
			log.Error(nil, fmt.Sprintf("job %s missing ReplicatedJobName label, can't update status", job.Name))
			continue
		}
		ready := ptr.Deref(job.Status.Ready, 0)
		// parallelism is always set as it is otherwise defaulted by k8s to 1
		podsCount := *(job.Spec.Parallelism)
		if job.Spec.Completions != nil && *job.Spec.Completions < podsCount {
			podsCount = *job.Spec.Completions
		}
		if job.Status.Succeeded+ready >= podsCount {
			replicatedJobsReady[job.Labels[jobset.ReplicatedJobNameKey]]["ready"]++
		}
		if job.Status.Active > 0 {
			replicatedJobsReady[job.Labels[jobset.ReplicatedJobNameKey]]["active"]++
		}
		if jobSuspended(job) {
			replicatedJobsReady[job.Labels[jobset.ReplicatedJobNameKey]]["suspended"]++
		}
	}

	// Calculate succeededJobs
	for _, job := range jobs.successful {
		replicatedJobsReady[job.Labels[jobset.ReplicatedJobNameKey]]["succeeded"]++
	}

	for _, job := range jobs.failed {
		replicatedJobsReady[job.Labels[jobset.ReplicatedJobNameKey]]["failed"]++
	}

	// Calculate ReplicatedJobsStatus
	var rjStatus []jobset.ReplicatedJobStatus
	for name, status := range replicatedJobsReady {
		rjStatus = append(rjStatus, jobset.ReplicatedJobStatus{
			Name:      name,
			Ready:     status["ready"],
			Succeeded: status["succeeded"],
			Failed:    status["failed"],
			Active:    status["active"],
			Suspended: status["suspended"],
		})
	}
	return rjStatus
}

func (r *JobSetReconciler) suspendJobs(ctx context.Context, js *jobset.JobSet, activeJobs []*batchv1.Job, updateStatusOpts *statusUpdateOpts) error {
	for _, job := range activeJobs {
		if !jobSuspended(job) {
			job.Spec.Suspend = ptr.To(true)
			if err := r.Update(ctx, job); err != nil {
				return err
			}
		}
	}
	setJobSetSuspendedCondition(js, updateStatusOpts)
	return nil
}

// resumeJobsIfNecessary iterates through each replicatedJob, resuming any suspended jobs if the JobSet
// is not suspended.
func (r *JobSetReconciler) resumeJobsIfNecessary(ctx context.Context, js *jobset.JobSet, activeJobs []*batchv1.Job, replicatedJobStatuses []jobset.ReplicatedJobStatus, updateStatusOpts *statusUpdateOpts) error {
	// Store pod template for each replicatedJob.
	replicatedJobTemplateMap := map[string]corev1.PodTemplateSpec{}
	for _, replicatedJob := range js.Spec.ReplicatedJobs {
		replicatedJobTemplateMap[replicatedJob.Name] = replicatedJob.Template.Spec.Template
	}

	// Map each replicatedJob to a list of its active jobs.
	replicatedJobToActiveJobs := map[string][]*batchv1.Job{}
	for _, job := range activeJobs {
		replicatedJobName := job.Labels[jobset.ReplicatedJobNameKey]
		replicatedJobToActiveJobs[replicatedJobName] = append(replicatedJobToActiveJobs[replicatedJobName], job)
	}

	startupPolicy := js.Spec.StartupPolicy

	// Map where key is the Job name and value is the Job replicas.
	rJobsReplicas := map[string]int32{}

	// If JobSpec is unsuspended, ensure all active child Jobs are also
	// unsuspended and update the suspend condition to true.
	for _, replicatedJob := range js.Spec.ReplicatedJobs {
		replicatedJobStatus := findReplicatedJobStatus(replicatedJobStatuses, replicatedJob.Name)

		rJobsReplicas[replicatedJob.Name] = replicatedJob.Replicas

		// For depends on, the ReplicatedJob is created only after the previous ReplicatedJob reached the status.
		if !dependencyReachedStatus(replicatedJob, rJobsReplicas, replicatedJobStatuses) {
			continue
		}

		// If this replicatedJob has already started, continue.
		if inOrderStartupPolicy(startupPolicy) && allReplicasStarted(replicatedJob.Replicas, replicatedJobStatus) {
			continue
		}

		jobsFromRJob := replicatedJobToActiveJobs[replicatedJob.Name]
		for _, job := range jobsFromRJob {
			if !jobSuspended(job) {
				continue
			}
			if err := r.resumeJob(ctx, job, replicatedJobTemplateMap); err != nil {
				return err
			}
		}
		// If in order startup policy, we need to return early and allow for
		// this replicatedJob to become ready before resuming the next.
		if inOrderStartupPolicy(startupPolicy) {
			setInOrderStartupPolicyInProgressCondition(js, updateStatusOpts)
			return nil
		}
	}

	// Finally, set the suspended condition on the JobSet to false to indicate
	// the JobSet is no longer suspended.
	setJobSetResumedCondition(js, updateStatusOpts)
	return nil
}

func (r *JobSetReconciler) resumeJob(ctx context.Context, job *batchv1.Job, replicatedJobTemplateMap map[string]corev1.PodTemplateSpec) error {
	log := ctrl.LoggerFrom(ctx)
	// Kubernetes validates that a job template is immutable
	// so if the job has started i.e., startTime != nil), we must set it to nil first.
	if job.Status.StartTime != nil {
		job.Status.StartTime = nil
		if err := r.Status().Update(ctx, job); err != nil {
			return err
		}
	}

	// Get name of parent replicated job and use it to look up the pod template.
	replicatedJobName := job.Labels[jobset.ReplicatedJobNameKey]
	replicatedJobPodTemplate := replicatedJobTemplateMap[replicatedJobName]
	if job.Labels != nil && job.Labels[jobset.ReplicatedJobNameKey] != "" {
		// Certain fields on the Job pod template may be mutated while a JobSet is suspended,
		// for integration with Kueue. Ensure these updates are propagated to the child Jobs
		// when the JobSet is resumed.
		// Merge values rather than overwriting them, since a different controller
		// (e.g., the Job controller) may have added labels/annotations/etc to the
		// Job that do not exist in the ReplicatedJob pod template.
		job.Spec.Template.Labels = collections.MergeMaps(
			job.Spec.Template.Labels,
			replicatedJobPodTemplate.Labels,
		)
		job.Spec.Template.Annotations = collections.MergeMaps(
			job.Spec.Template.Annotations,
			replicatedJobPodTemplate.Annotations,
		)
		job.Spec.Template.Spec.NodeSelector = collections.MergeMaps(
			job.Spec.Template.Spec.NodeSelector,
			replicatedJobPodTemplate.Spec.NodeSelector,
		)
		job.Spec.Template.Spec.Tolerations = collections.MergeSlices(
			job.Spec.Template.Spec.Tolerations,
			replicatedJobPodTemplate.Spec.Tolerations,
		)
		job.Spec.Template.Spec.SchedulingGates = collections.MergeSlices(
			job.Spec.Template.Spec.SchedulingGates,
			replicatedJobPodTemplate.Spec.SchedulingGates,
		)
	} else {
		log.Error(nil, "job missing ReplicatedJobName label")
	}
	job.Spec.Suspend = ptr.To(false)
	return r.Update(ctx, job)
}

func (r *JobSetReconciler) reconcileReplicatedJobs(ctx context.Context, js *jobset.JobSet, ownedJobs *childJobs, replicatedJobStatuses []jobset.ReplicatedJobStatus, updateStatusOpts *statusUpdateOpts) error {
	log := ctrl.LoggerFrom(ctx)
	startupPolicy := js.Spec.StartupPolicy

	// Map where key is the Job name and value is the Job replicas.
	rJobsReplicas := map[string]int32{}

	for _, replicatedJob := range js.Spec.ReplicatedJobs {
		jobs := constructJobsFromTemplate(js, &replicatedJob, ownedJobs)
		replicatedJobStatus := findReplicatedJobStatus(replicatedJobStatuses, replicatedJob.Name)

		rJobsReplicas[replicatedJob.Name] = replicatedJob.Replicas

		// For depends on, the ReplicatedJob is created only after the previous ReplicatedJob reached the status.
		if !dependencyReachedStatus(replicatedJob, rJobsReplicas, replicatedJobStatuses) {
			continue
		}

		// For startup policy, if the replicatedJob is started we can skip this loop.
		// Jobs have been created.
		if !jobSetSuspended(js) && inOrderStartupPolicy(startupPolicy) && allReplicasStarted(replicatedJob.Replicas, replicatedJobStatus) {
			continue
		}

		// Create jobs as necessary.
		if err := r.createJobs(ctx, js, jobs); err != nil {
			log.Error(err, "creating jobs")
			return err
		}

		// If we are using inOrder StartupPolicy, then we return to wait for jobs to be ready.
		// This updates the StartupPolicy condition and notifies that we are waiting
		// for this replicated job to start up before moving onto the next one.
		if !jobSetSuspended(js) && inOrderStartupPolicy(startupPolicy) {
			setInOrderStartupPolicyInProgressCondition(js, updateStatusOpts)
			return nil
		}
	}

	// Skip emitting a condition for StartupPolicy if JobSet is suspended
	if !jobSetSuspended(js) && inOrderStartupPolicy(startupPolicy) {
		setInOrderStartupPolicyCompletedCondition(js, updateStatusOpts)
	}
	return nil
}

func (r *JobSetReconciler) createJobs(ctx context.Context, js *jobset.JobSet, jobs []*batchv1.Job) error {
	log := ctrl.LoggerFrom(ctx)

	var lock sync.Mutex
	var finalErrs []error
	workqueue.ParallelizeUntil(ctx, constants.MaxParallelism, len(jobs), func(i int) {
		job := jobs[i]

		// Set jobset controller as owner of the job for garbage collection and reconcilation.
		if err := ctrl.SetControllerReference(js, job, r.Scheme); err != nil {
			lock.Lock()
			defer lock.Unlock()
			finalErrs = append(finalErrs, err)
			return
		}

		// Create the job.
		// TODO(#18): Deal with the case where the job exists but is not owned by the jobset.
		if err := r.Create(ctx, job); err != nil {
			lock.Lock()
			defer lock.Unlock()
			finalErrs = append(finalErrs, fmt.Errorf("job %q creation failed with error: %v", job.Name, err))
			return
		}
		log.V(2).Info("successfully created job", "job", klog.KObj(job))
	})
	allErrs := errors.Join(finalErrs...)
	return allErrs
}

func (r *JobSetReconciler) deleteJobs(ctx context.Context, jobsForDeletion []*batchv1.Job) error {
	log := ctrl.LoggerFrom(ctx)
	lock := &sync.Mutex{}
	var finalErrs []error
	workqueue.ParallelizeUntil(ctx, constants.MaxParallelism, len(jobsForDeletion), func(i int) {
		targetJob := jobsForDeletion[i]
		// Skip deleting jobs with deletion timestamp already set.
		if targetJob.DeletionTimestamp != nil {
			return
		}
		// Delete job. This deletion event will trigger another reconciliation,
		// where the jobs are recreated.
		foregroundPolicy := metav1.DeletePropagationForeground
		if err := r.Delete(ctx, targetJob, &client.DeleteOptions{PropagationPolicy: &foregroundPolicy}); client.IgnoreNotFound(err) != nil {
			lock.Lock()
			defer lock.Unlock()
			log.Error(err, fmt.Sprintf("failed to delete job: %q", targetJob.Name))
			finalErrs = append(finalErrs, err)
			return
		}
		log.V(2).Info("successfully deleted job", "job", klog.KObj(targetJob), "restart attempt", targetJob.Labels[targetJob.Labels[constants.RestartsKey]])
	})
	return errors.Join(finalErrs...)
}

// TODO: look into adopting service and updating the selector
// if it is not matching the job selector.
func (r *JobSetReconciler) createHeadlessSvcIfNecessary(ctx context.Context, js *jobset.JobSet) error {
	log := ctrl.LoggerFrom(ctx)

	// Headless service is only necessary for indexed jobs whose pods need to communicate with
	// eachother via pod hostnames.
	if !dnsHostnamesEnabled(js) {
		return nil
	}

	// Check if service already exists. The service name should match the subdomain specified in
	// Spec.Network.Subdomain, with default of <jobSetName> set by the webhook.
	// If the service doesn't exist in the same namespace, create it.
	var headlessSvc corev1.Service
	subdomain := GetSubdomain(js)
	if err := r.Get(ctx, types.NamespacedName{Name: subdomain, Namespace: js.Namespace}, &headlessSvc); err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
		headlessSvc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      subdomain,
				Namespace: js.Namespace,
			},
			Spec: corev1.ServiceSpec{
				ClusterIP: "None",
				Selector: map[string]string{
					jobset.JobSetNameKey: js.Name,
				},
				PublishNotReadyAddresses: ptr.Deref(js.Spec.Network.PublishNotReadyAddresses, true),
			},
		}

		// Set controller owner reference for garbage collection and reconcilation.
		if err := ctrl.SetControllerReference(js, &headlessSvc, r.Scheme); err != nil {
			return err
		}

		// Create headless service.
		if err := r.Create(ctx, &headlessSvc); err != nil {
			r.Record.Eventf(js, corev1.EventTypeWarning, constants.HeadlessServiceCreationFailedReason, err.Error())
			return err
		}
		log.V(2).Info("successfully created headless service", "service", klog.KObj(&headlessSvc))
	}
	return nil
}

// executeSuccessPolicy checks the completed jobs against the jobset success policy
// and updates the jobset status to completed if the success policy conditions are met.
// Returns a boolean value indicating if the jobset was completed or not.
func executeSuccessPolicy(js *jobset.JobSet, ownedJobs *childJobs, updateStatusOpts *statusUpdateOpts) bool {
	if numJobsMatchingSuccessPolicy(js, ownedJobs.successful) >= numJobsExpectedToSucceed(js) {
		setJobSetCompletedCondition(js, updateStatusOpts)
		return true
	}
	return false
}

func constructJobsFromTemplate(js *jobset.JobSet, rjob *jobset.ReplicatedJob, ownedJobs *childJobs) []*batchv1.Job {
	var jobs []*batchv1.Job
	// If the JobSet is using the BlockingRecreate failure policy, we should not create any new jobs until
	// all the jobs slated for deletion (i.e. from the last restart index) have been deleted.
	useBlockingRecreate := js.Spec.FailurePolicy != nil && js.Spec.FailurePolicy.RestartStrategy == jobset.BlockingRecreate
	if len(ownedJobs.previous) > 0 && useBlockingRecreate {
		return jobs
	}

	for jobIdx := 0; jobIdx < int(rjob.Replicas); jobIdx++ {
		jobName := placement.GenJobName(js.Name, rjob.Name, jobIdx)
		if create := shouldCreateJob(jobName, ownedJobs); !create {
			continue
		}
		job := constructJob(js, rjob, jobIdx)
		jobs = append(jobs, job)
	}
	return jobs
}

func constructJob(js *jobset.JobSet, rjob *jobset.ReplicatedJob, jobIdx int) *batchv1.Job {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      maps.Clone(rjob.Template.Labels),
			Annotations: maps.Clone(rjob.Template.Annotations),
			Name:        placement.GenJobName(js.Name, rjob.Name, jobIdx),
			Namespace:   js.Namespace,
		},
		Spec: *rjob.Template.Spec.DeepCopy(),
	}
	// Label and annotate both job and pod template spec.
	labelAndAnnotateObject(job, js, rjob, jobIdx)
	labelAndAnnotateObject(&job.Spec.Template, js, rjob, jobIdx)

	// If enableDNSHostnames is set, update job spec to set subdomain as
	// job name (a headless service with same name as job will be created later).
	if dnsHostnamesEnabled(js) {
		job.Spec.Template.Spec.Subdomain = GetSubdomain(js)
	}

	// If this job is using the nodeSelectorStrategy implementation of exclusive placement,
	// add the job name label as a nodeSelector, and add a toleration for the no schedule taint.
	// The node label and node taint must be added to the nodes separately by a user/script.
	_, exclusivePlacement := job.Annotations[jobset.ExclusiveKey]
	_, nodeSelectorStrategy := job.Annotations[jobset.NodeSelectorStrategyKey]
	if exclusivePlacement && nodeSelectorStrategy {
		addNamespacedJobNodeSelector(job)
		addTaintToleration(job)
	}

	// if Suspend is set, then we assume all jobs will be suspended also.
	jobsetSuspended := jobSetSuspended(js)
	job.Spec.Suspend = ptr.To(jobsetSuspended)

	return job
}

func addTaintToleration(job *batchv1.Job) {
	job.Spec.Template.Spec.Tolerations = append(job.Spec.Template.Spec.Tolerations,
		corev1.Toleration{
			Key:      jobset.NoScheduleTaintKey,
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
	)
}

func shouldCreateJob(jobName string, ownedJobs *childJobs) bool {
	// Check if this job exists already.
	// TODO: maybe we can use a job map here so we can do O(1) lookups
	// to check if the job already exists, rather than a linear scan
	// through all the jobs owned by the jobset.
	for _, job := range slices.Concat(ownedJobs.active, ownedJobs.successful, ownedJobs.failed, ownedJobs.previous) {
		if jobName == job.Name {
			return false
		}
	}
	return true
}

// labelAndAnnotateObjects adds standard JobSet related labels and annotations a k8s object.
// In practice it is used to label and annotate child Jobs and pods.
// The same set of labels are also as added as annotations for simplicity's sake.
// The two exceptions to this are:
//  1. "alpha.jobset.sigs.k8s.io/exclusive-topology" which is
//     a JobSet annoation optionally added by the user, so we only add it as an annotation
//     to child Jobs and pods if it is defined, and do not add it as a label.
//  2. "alpha.jobset.sigs.k8s.io/node-selector" which is another optional
//     annotation applied by the user to indicate they are using the
//     nodeSelector exclusive placement strategy, where they have manually
//     labelled the nodes ahead of time with hack/label_nodes/label_nodes.py
func labelAndAnnotateObject(obj metav1.Object, js *jobset.JobSet, rjob *jobset.ReplicatedJob, jobIdx int) {
	jobName := placement.GenJobName(js.Name, rjob.Name, jobIdx)

	// Set labels on the object.
	labels := make(map[string]string)
	maps.Copy(labels, obj.GetLabels())
	labels[jobset.JobSetNameKey] = js.Name
	labels[jobset.ReplicatedJobNameKey] = rjob.Name
	labels[constants.RestartsKey] = strconv.Itoa(int(js.Status.Restarts))
	labels[jobset.ReplicatedJobReplicas] = strconv.Itoa(int(rjob.Replicas))
	labels[jobset.GlobalReplicasKey] = globalReplicas(js)
	labels[jobset.JobIndexKey] = strconv.Itoa(jobIdx)
	labels[jobset.JobKey] = jobHashKey(js.Namespace, jobName)
	labels[jobset.JobGlobalIndexKey] = globalJobIndex(js, rjob.Name, jobIdx)

	annotations := make(map[string]string)
	maps.Copy(annotations, obj.GetAnnotations())
	annotations[jobset.JobSetNameKey] = js.Name
	annotations[jobset.ReplicatedJobNameKey] = rjob.Name
	annotations[constants.RestartsKey] = strconv.Itoa(int(js.Status.Restarts))
	annotations[jobset.ReplicatedJobReplicas] = strconv.Itoa(int(rjob.Replicas))
	annotations[jobset.GlobalReplicasKey] = globalReplicas(js)
	annotations[jobset.JobIndexKey] = strconv.Itoa(jobIdx)
	annotations[jobset.JobKey] = jobHashKey(js.Namespace, jobName)
	annotations[jobset.JobGlobalIndexKey] = globalJobIndex(js, rjob.Name, jobIdx)

	// Apply coordinator annotation/label if a coordinator is defined in the JobSet spec.
	if js.Spec.Coordinator != nil {
		labels[jobset.CoordinatorKey] = coordinatorEndpoint(js)
		annotations[jobset.CoordinatorKey] = coordinatorEndpoint(js)
	}

	// Check for JobSet level exclusive placement.
	if topologyDomain, exists := js.Annotations[jobset.ExclusiveKey]; exists {
		annotations[jobset.ExclusiveKey] = topologyDomain
		// Check if we are using nodeSelectorStrategy implementation of exclusive placement at the JobSet level.
		if value, ok := js.Annotations[jobset.NodeSelectorStrategyKey]; ok {
			annotations[jobset.NodeSelectorStrategyKey] = value
		}
	}
	// Check for ReplicatedJob level exclusive placement.
	if topologyDomain, exists := rjob.Template.Annotations[jobset.ExclusiveKey]; exists {
		annotations[jobset.ExclusiveKey] = topologyDomain
		// Check if we are using nodeSelectorStrategy implementation of exclusive placement at the ReplicatedJob level.
		if value, ok := rjob.Template.Annotations[jobset.NodeSelectorStrategyKey]; ok {
			annotations[jobset.NodeSelectorStrategyKey] = value
		}
	}

	obj.SetLabels(labels)
	obj.SetAnnotations(annotations)
}

func JobFinished(job *batchv1.Job) (bool, batchv1.JobConditionType) {
	for _, c := range job.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
			return true, c.Type
		}
	}
	return false, ""
}

func GetSubdomain(js *jobset.JobSet) string {
	// If enableDNSHostnames is set, and subdomain is unset, default the subdomain to be the JobSet name.
	// This must be done in the controller rather than in the request-time defaulting, since if a JobSet
	// uses generateName rather than setting the name explicitly, the JobSet name will still be an empty
	// string at that time.
	if js.Spec.Network.Subdomain != "" {
		return js.Spec.Network.Subdomain
	}
	return js.Name
}

// addNamespacedJobNodeSelector adds the namespaced job name as a nodeSelector for use by the
// nodeSelector exclusive job placement strategy, where the user has labeled nodes ahead of time
// with one job name label per nodepool using hack/label_nodes/label_nodes.py
func addNamespacedJobNodeSelector(job *batchv1.Job) {
	if job.Spec.Template.Spec.NodeSelector == nil {
		job.Spec.Template.Spec.NodeSelector = make(map[string]string)
	}
	job.Spec.Template.Spec.NodeSelector[jobset.NamespacedJobKey] = namespacedJobName(job.Namespace, job.Name)
}

// Human readable namespaced job name. We must use '_' to separate namespace and job instead of '/'
// since the '/' character is not allowed in label values.
func namespacedJobName(ns, jobName string) string {
	return fmt.Sprintf("%s_%s", ns, jobName)
}

// jobHashKey returns the SHA1 hash of the namespaced job name (i.e. <namespace>/<jobName>).
func jobHashKey(ns string, jobName string) string {
	return sha1Hash(fmt.Sprintf("%s/%s", ns, jobName))
}

// sha1Hash accepts an input string and returns the 40 character SHA1 hash digest of the input string.
func sha1Hash(s string) string {
	h := sha1.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func jobSetFinished(js *jobset.JobSet) bool {
	for _, c := range js.Status.Conditions {
		if (c.Type == string(jobset.JobSetCompleted) || c.Type == string(jobset.JobSetFailed)) && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func jobSetMarkedForDeletion(js *jobset.JobSet) bool {
	return js.DeletionTimestamp != nil
}

func dnsHostnamesEnabled(js *jobset.JobSet) bool {
	return js.Spec.Network.EnableDNSHostnames != nil && *js.Spec.Network.EnableDNSHostnames
}

func jobSetSuspended(js *jobset.JobSet) bool {
	return ptr.Deref(js.Spec.Suspend, false)
}

func jobSuspended(job *batchv1.Job) bool {
	return ptr.Deref(job.Spec.Suspend, false)
}

func findReplicatedJobStatus(replicatedJobStatus []jobset.ReplicatedJobStatus, replicatedJobName string) *jobset.ReplicatedJobStatus {
	for _, status := range replicatedJobStatus {
		if status.Name == replicatedJobName {
			return &status
		}
	}
	return nil
}

// managedByExternalController returns a pointer to the name of the external controller managing
// the JobSet, if one exists. Otherwise, it returns nil.
func managedByExternalController(js *jobset.JobSet) *string {
	if controllerName := js.Spec.ManagedBy; controllerName != nil && *controllerName != jobset.JobSetControllerName {
		return controllerName
	}
	return nil
}

// enqueueEvent appends a new k8s event to be emitted if and only after running the status
// update functions in the updateStatusOpts, the status update API call suceeds.
func enqueueEvent(updateStatusOpts *statusUpdateOpts, event *eventParams) {
	updateStatusOpts.events = append(updateStatusOpts.events, event)
}

// function parameters for setCondition
type conditionOpts struct {
	eventType string
	condition *metav1.Condition
}

// setCondition will add a new condition to the JobSet status (or update an existing one),
// and enqueue an event for emission if the status update succeeds at the end of the reconcile.
func setCondition(js *jobset.JobSet, condOpts *conditionOpts, updateStatusOpts *statusUpdateOpts) {
	// Return early if no status update is required for this condition.
	if !updateCondition(js, condOpts) {
		return
	}

	if updateStatusOpts == nil {
		updateStatusOpts = &statusUpdateOpts{}
	}

	// If we made some changes to the status conditions, configure updateStatusOpts
	// to persist the status update at the end of the reconciliation attempt.
	updateStatusOpts.shouldUpdate = true

	// Conditionally emit an event for each JobSet condition update if and only if
	// the status update call is successful.
	event := &eventParams{
		object:       js,
		eventType:    condOpts.eventType,
		eventReason:  condOpts.condition.Reason,
		eventMessage: condOpts.condition.Message,
	}
	enqueueEvent(updateStatusOpts, event)
}

// updateCondition accepts a given condition and does one of the following:
//  1. If an identical condition already exists, do nothing and return false (indicating
//     no change was made).
//  2. If a condition of the same type exists but with a different status, update
//     the condition in place and return true (indicating a condition change was made).
func updateCondition(js *jobset.JobSet, opts *conditionOpts) bool {
	if opts == nil || opts.condition == nil {
		return false
	}

	found := false
	shouldUpdate := false
	newCond := *opts.condition
	newCond.LastTransitionTime = metav1.Now()

	for i, currCond := range js.Status.Conditions {
		// If condition type has a status change, update it.
		if newCond.Type == currCond.Type {
			// Status change of an existing condition. Update status call will be required.
			if newCond.Status != currCond.Status {
				js.Status.Conditions[i] = newCond
				shouldUpdate = true
			}

			// If both are true or both are false, this is a duplicate condition, do nothing.
			found = true
		} else {
			// If conditions are of different types, only perform an update if they are both true
			// and they are mutually exclusive. If so, then set the existing condition status to
			// false before adding the new condition.
			if exclusiveConditions(currCond, newCond) &&
				currCond.Status == metav1.ConditionTrue &&
				newCond.Status == metav1.ConditionTrue {
				js.Status.Conditions[i].Status = metav1.ConditionFalse
				shouldUpdate = true
			}
		}
	}

	// Condition doesn't exist, add it if condition status is true.
	if !found && newCond.Status == metav1.ConditionTrue {
		js.Status.Conditions = append(js.Status.Conditions, newCond)
		shouldUpdate = true
	}
	return shouldUpdate
}

// setJobSetCompletedCondition sets a condition and terminal state on the JobSet status indicating it has completed.
func setJobSetCompletedCondition(js *jobset.JobSet, updateStatusOpts *statusUpdateOpts) {
	setCondition(js, makeCompletedConditionsOpts(), updateStatusOpts)
	js.Status.TerminalState = string(jobset.JobSetCompleted)
	// Update the metrics
	metrics.JobSetCompleted(fmt.Sprintf("%s/%s", js.Namespace, js.Name))
}

// setJobSetSuspendedCondition sets a condition on the JobSet status indicating it is currently suspended.
func setJobSetSuspendedCondition(js *jobset.JobSet, updateStatusOpts *statusUpdateOpts) {
	setCondition(js, makeSuspendedConditionOpts(), updateStatusOpts)
}

// setJobSetResumedCondition sets a condition on the JobSet status indicating it has been resumed.
// This updates the "suspended" condition type from "true" to "false."
func setJobSetResumedCondition(js *jobset.JobSet, updateStatusOpts *statusUpdateOpts) {
	setCondition(js, makeResumedConditionOpts(), updateStatusOpts)
}

// completedConditionsOpts contains the options we use to generate the JobSet completed condition.
func makeCompletedConditionsOpts() *conditionOpts {
	return &conditionOpts{
		eventType: corev1.EventTypeNormal,
		condition: &metav1.Condition{
			Type:    string(jobset.JobSetCompleted),
			Status:  metav1.ConditionTrue,
			Reason:  constants.AllJobsCompletedReason,
			Message: constants.AllJobsCompletedMessage,
		},
	}
}

// makeSuspendedConditionOpts returns the options we use to generate the JobSet suspended condition.
func makeSuspendedConditionOpts() *conditionOpts {
	return &conditionOpts{
		eventType: corev1.EventTypeNormal,
		condition: &metav1.Condition{
			Type:               string(jobset.JobSetSuspended),
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             constants.JobSetSuspendedReason,
			Message:            constants.JobSetSuspendedMessage,
		},
	}
}

// makeResumedConditionOpts returns the options we use to generate the JobSet resumed condition.
func makeResumedConditionOpts() *conditionOpts {
	return &conditionOpts{
		eventType: corev1.EventTypeNormal,
		condition: &metav1.Condition{
			Type:               string(jobset.JobSetSuspended),
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             constants.JobSetResumedReason,
			Message:            constants.JobSetResumedMessage,
		},
	}
}

// replicatedJobStatusesEqual compares two slices of replicatedJob statuses, and returns
// a boolean value indicating if they are equal. This is a semantic equality check, not
// a memory equality check.
func replicatedJobStatusesEqual(oldStatuses, newStatuses []jobset.ReplicatedJobStatus) bool {
	sort.Slice(oldStatuses, func(i, j int) bool {
		return oldStatuses[i].Name > oldStatuses[j].Name
	})
	sort.Slice(newStatuses, func(i, j int) bool {
		return newStatuses[i].Name > newStatuses[j].Name
	})
	return apiequality.Semantic.DeepEqual(oldStatuses, newStatuses)
}

// exclusiveConditions accepts 2 conditions and returns a boolean indicating if
// they are mutually exclusive.
func exclusiveConditions(cond1, cond2 metav1.Condition) bool {
	inProgressAndCompleted := cond1.Type == string(jobset.JobSetStartupPolicyInProgress) &&
		cond2.Type == string(jobset.JobSetStartupPolicyCompleted)
	completedAndInProgress := cond1.Type == string(jobset.JobSetStartupPolicyCompleted) &&
		cond2.Type == string(jobset.JobSetStartupPolicyInProgress)
	return inProgressAndCompleted || completedAndInProgress
}

// coordinatorEndpoint returns the stable network endpoint where the coordinator pod can be reached.
// This function assumes the caller has validated that jobset.Spec.Coordinator != nil.
func coordinatorEndpoint(js *jobset.JobSet) string {
	return fmt.Sprintf("%s-%s-%d-%d.%s", js.Name, js.Spec.Coordinator.ReplicatedJob, js.Spec.Coordinator.JobIndex, js.Spec.Coordinator.PodIndex, GetSubdomain(js))
}

// globalJobIndex determines the job global index for a given job. The job global index is a unique
// global index for the job, with values ranging from 0 to N-1,
// where N=total number of jobs in the jobset. The job global index is calculated by
// iterating through the replicatedJobs in the order, as defined in the JobSet
// spec, keeping a cumulative sum of total replicas seen so far, then when we
// arrive at the parent replicatedJob of the target job, we add the local job
// index to our running sum of total jobs seen so far, in order to arrive at
// the final job global index value.
//
// Below is a diagram illustrating how job global indexs differ from job indexes.
//
// |                             my-jobset                             |
// |        replicated job A         |        replicated job B         |
// |    job index 0 |   job index 1  |   job index 0  | job index 1    |
// | global index 0 | global index 2 | global index 3 | global index 4 |
//
// Returns an empty string if the parent replicated Job does not exist,
// although this should never happen in practice.
func globalJobIndex(js *jobset.JobSet, replicatedJobName string, jobIdx int) string {
	currTotalJobs := 0
	for _, rjob := range js.Spec.ReplicatedJobs {
		if rjob.Name == replicatedJobName {
			return strconv.Itoa(currTotalJobs + jobIdx)
		}
		currTotalJobs += int(rjob.Replicas)
	}
	return ""
}

// globalReplicas calculates the total number of replicas across all replicated jobs in a JobSet.
func globalReplicas(js *jobset.JobSet) string {
	currGlobalReplicas := 0
	for _, rjob := range js.Spec.ReplicatedJobs {
		currGlobalReplicas += int(rjob.Replicas)
	}
	return strconv.Itoa(currGlobalReplicas)
}
