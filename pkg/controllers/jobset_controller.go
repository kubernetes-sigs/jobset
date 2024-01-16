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
	"strconv"
	"sync"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
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
	"sigs.k8s.io/jobset/pkg/util/collections"
	"sigs.k8s.io/jobset/pkg/util/placement"
)

const (
	RestartsKey    string = "jobset.sigs.k8s.io/restart-attempt"
	maxParallelism int    = 50

	// Event types broadcasted when a failure policy action is being excecuted.
	FailJobEventType    = "FailJob"
	IgnoreEventType     = "IgnoringFailedJob"
	FailJobSetEventType = "FailJobSet"
	RestartJobEventType = "RestartJob"
)

var (
	jobOwnerKey = ".metadata.controller"
	apiGVStr    = jobset.GroupVersion.String()
)

// JobSetReconciler reconciles a JobSet object
type JobSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Record record.EventRecorder
}

type childJobs struct {
	// Only jobs with jobset.sigs.k8s.io/restart-attempt == jobset.status.restarts are included
	// in active, successful, and failed jobs. These jobs are part of the current JobSet run.
	active     []*batchv1.Job
	successful []*batchv1.Job
	failed     []*batchv1.Job

	// Jobs marked for deletion are mutually exclusive with the set of jobs in active, successful, and failed.
	delete []*batchv1.Job
}

func NewJobSetReconciler(client client.Client, scheme *runtime.Scheme, record record.EventRecorder) *JobSetReconciler {
	return &JobSetReconciler{Client: client, Scheme: scheme, Record: record}
}

//+kubebuilder:rbac:groups="",resources=events,verbs=create;watch;update;patch
//+kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get;update;patch
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

	log := ctrl.LoggerFrom(ctx).WithValues("jobset", klog.KObj(&js))
	ctx = ctrl.LoggerInto(ctx, log)
	log.V(2).Info("Reconciling JobSet")

	// Get Jobs owned by JobSet.
	ownedJobs, err := r.getChildJobs(ctx, &js)
	if err != nil {
		log.Error(err, "getting jobs owned by jobset")
		return ctrl.Result{}, err
	}

	// Calculate JobsReady and update statuses for each ReplicatedJob
	if err := r.calculateAndUpdateReplicatedJobsStatuses(ctx, &js, ownedJobs); err != nil {
		log.Error(err, "updating replicated jobs statuses")
		return ctrl.Result{}, err
	}

	// If JobSet is already completed or failed, clean up active child jobs.
	if jobSetFinished(&js) {
		if err := r.deleteJobs(ctx, ownedJobs.active); err != nil {
			log.Error(err, "deleting jobs")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Delete any jobs marked for deletion.
	if err := r.deleteJobs(ctx, ownedJobs.delete); err != nil {
		log.Error(err, "deleting jobs")
		return ctrl.Result{}, err
	}

	// If any jobs have failed, execute the JobSet failure policy (if any).
	if len(ownedJobs.failed) > 0 {
		if err := r.executeFailurePolicy(ctx, &js, ownedJobs); err != nil {
			log.Error(err, "executing failure policy")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// If any jobs have succeeded, execute the JobSet success policy.
	if len(ownedJobs.successful) > 0 {
		completed, err := r.executeSuccessPolicy(ctx, &js, ownedJobs)
		if err != nil {
			log.Error(err, "executing success policy")
			return ctrl.Result{}, err
		}
		if completed {
			return ctrl.Result{}, nil
		}
	}

	// If job has not failed or succeeded, continue creating any
	// jobs that are ready to be started.
	if err := r.createJobs(ctx, &js, ownedJobs); err != nil {
		log.Error(err, "creating jobs")
		return ctrl.Result{}, err
	}

	// Handle suspending a jobset or resuming a suspended jobset.
	jobsetSuspended := js.Spec.Suspend != nil && *js.Spec.Suspend
	if jobsetSuspended {
		if err := r.suspendJobSet(ctx, &js, ownedJobs); err != nil {
			log.Error(err, "suspending jobset")
			return ctrl.Result{}, err
		}
	} else {
		if err := r.resumeJobSetIfNecessary(ctx, &js, ownedJobs); err != nil {
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
	return indexer.IndexField(ctx, &batchv1.Job{}, jobOwnerKey, func(obj client.Object) []string {
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

// getChildJobs gets jobs owned by the JobSet then categorizes them by status (active, successful, failed).
// Another list (`delete`) is also added which tracks jobs marked for deletion.
func (r *JobSetReconciler) getChildJobs(ctx context.Context, js *jobset.JobSet) (*childJobs, error) {
	log := ctrl.LoggerFrom(ctx)

	// Get all active jobs owned by JobSet.
	var childJobList batchv1.JobList
	if err := r.List(ctx, &childJobList, client.InNamespace(js.Namespace), client.MatchingFields{jobOwnerKey: js.Name}); err != nil {
		return nil, err
	}

	// Categorize each job into a bucket: active, successful, failed, or delete.
	ownedJobs := childJobs{}
	for i, job := range childJobList.Items {
		// Jobs with jobset.sigs.k8s.io/restart-attempt < jobset.status.restarts are marked for
		// deletion, as they were part of the previous JobSet run.
		jobRestarts, err := strconv.Atoi(job.Labels[RestartsKey])
		if err != nil {
			log.Error(err, fmt.Sprintf("invalid value for label %s, must be integer", RestartsKey))
			ownedJobs.delete = append(ownedJobs.delete, &childJobList.Items[i])
			return nil, err
		}
		if int32(jobRestarts) < js.Status.Restarts {
			ownedJobs.delete = append(ownedJobs.delete, &childJobList.Items[i])
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

func (r *JobSetReconciler) calculateAndUpdateReplicatedJobsStatuses(ctx context.Context, js *jobset.JobSet, jobs *childJobs) error {
	status := r.calculateReplicatedJobStatuses(ctx, js, jobs)
	// Check if status ReplicatedJobsStatus has changed
	if apiequality.Semantic.DeepEqual(js.Status.ReplicatedJobsStatus, status) {
		return nil
	}
	js.Status.ReplicatedJobsStatus = status
	return r.Status().Update(ctx, js)
}

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
		if job.Labels == nil || (job.Labels != nil && job.Labels[jobset.ReplicatedJobNameKey] == "") {
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
		if job.Spec.Suspend != nil && *job.Spec.Suspend {
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

func (r *JobSetReconciler) suspendJobSet(ctx context.Context, js *jobset.JobSet, ownedJobs *childJobs) error {
	for _, job := range ownedJobs.active {
		if !ptr.Deref(job.Spec.Suspend, false) {
			job.Spec.Suspend = ptr.To(true)
			if err := r.Update(ctx, job); err != nil {
				return err
			}
		}
	}
	return r.ensureCondition(ctx, js, corev1.EventTypeNormal, metav1.Condition{
		Type:               string(jobset.JobSetSuspended),
		Status:             metav1.ConditionStatus(corev1.ConditionTrue),
		LastTransitionTime: metav1.Now(),
		Reason:             "SuspendedJobs",
		Message:            "jobset is suspended",
	})
}

func (r *JobSetReconciler) resumeJobSetIfNecessary(ctx context.Context, js *jobset.JobSet, ownedJobs *childJobs) error {
	log := ctrl.LoggerFrom(ctx)

	nodeAffinities := map[string]map[string]string{}
	for _, replicatedJob := range js.Spec.ReplicatedJobs {
		nodeAffinities[replicatedJob.Name] = replicatedJob.Template.Spec.Template.Spec.NodeSelector
	}

	// If JobSpec is unsuspended, ensure all active child Jobs are also
	// unsuspended and update the suspend condition to true.
	for _, job := range ownedJobs.active {
		if ptr.Deref(job.Spec.Suspend, false) {
			if job.Status.StartTime != nil {
				job.Status.StartTime = nil
				if err := r.Status().Update(ctx, job); err != nil {
					return err
				}
			}
			if job.Labels != nil && job.Labels[jobset.ReplicatedJobNameKey] != "" {
				// When resuming a job, its nodeSelectors should match that of the replicatedJob template
				// that it was created from, which may have been updated while it was suspended.
				job.Spec.Template.Spec.NodeSelector = nodeAffinities[job.Labels[jobset.ReplicatedJobNameKey]]
			} else {
				log.Error(nil, "job missing ReplicatedJobName label")
			}
			job.Spec.Suspend = ptr.To(false)
			if err := r.Update(ctx, job); err != nil {
				return err
			}
		}
	}
	return r.ensureCondition(ctx, js, corev1.EventTypeNormal, metav1.Condition{
		Type:               string(jobset.JobSetSuspended),
		Status:             metav1.ConditionStatus(corev1.ConditionFalse),
		LastTransitionTime: metav1.Now(),
		Reason:             "ResumeJobs",
		Message:            "jobset is resumed",
	})
}

func (r *JobSetReconciler) createJobs(ctx context.Context, js *jobset.JobSet, ownedJobs *childJobs) error {
	log := ctrl.LoggerFrom(ctx)

	// If pod DNS hostnames are enabled, create a headless service for the JobSet
	if dnsHostnamesEnabled(js) {

		// Don't try to create a headless service without a subdomain
		if js.Spec.Network.Subdomain == "" {
			return fmt.Errorf("a subdomain should be set if dns hostnames are enabled")
		}
		if err := r.createHeadlessSvcIfNotExist(ctx, js); err != nil {
			return err
		}
	}

	var lock sync.Mutex
	var finalErrs []error

	for _, rjob := range js.Spec.ReplicatedJobs {
		jobs, err := constructJobsFromTemplate(js, &rjob, ownedJobs)
		if err != nil {
			return err
		}
		workqueue.ParallelizeUntil(ctx, maxParallelism, len(jobs), func(i int) {
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
				finalErrs = append(finalErrs, err)
				return
			}
			log.V(2).Info("successfully created job", "job", klog.KObj(job))
		})
	}
	return errors.Join(finalErrs...)
}

// TODO: look into adopting service and updating the selector
// if it is not matching the job selector.
func (r *JobSetReconciler) createHeadlessSvcIfNotExist(ctx context.Context, js *jobset.JobSet) error {
	log := ctrl.LoggerFrom(ctx)

	// Check if service already exists. The service name should match the subdomain specified in
	// Spec.Network.Subdomain, with default of <jobSetName> set by the webhook.
	// If the service doesn't exist in the same namespace, create it.
	var headlessSvc corev1.Service
	if err := r.Get(ctx, types.NamespacedName{Name: js.Spec.Network.Subdomain, Namespace: js.Namespace}, &headlessSvc); err != nil {
		headlessSvc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      js.Spec.Network.Subdomain,
				Namespace: js.Namespace,
			},
			Spec: corev1.ServiceSpec{
				ClusterIP: "None",
				Selector: map[string]string{
					jobset.JobSetNameKey: js.Name,
				},
			},
		}

		// Set controller owner reference for garbage collection and reconcilation.
		if err := ctrl.SetControllerReference(js, &headlessSvc, r.Scheme); err != nil {
			return err
		}

		// Create headless service.
		if err := r.Create(ctx, &headlessSvc); err != nil {
			return err
		}
		log.V(2).Info("successfully created headless service", "service", klog.KObj(&headlessSvc))
	}
	return nil
}

// executeSuccessPolicy checks the completed jobs against the jobset success policy
// and updates the jobset status to completed if the success policy conditions are met.
// Returns a boolean value indicating if the jobset was completed or not.
func (r *JobSetReconciler) executeSuccessPolicy(ctx context.Context, js *jobset.JobSet, ownedJobs *childJobs) (bool, error) {
	if numJobsMatchingSuccessPolicy(js, ownedJobs.successful) >= numJobsExpectedToSucceed(js) {
		if err := r.ensureCondition(ctx, js, corev1.EventTypeNormal, metav1.Condition{
			Type:    string(jobset.JobSetCompleted),
			Status:  metav1.ConditionStatus(corev1.ConditionTrue),
			Reason:  "AllJobsCompleted",
			Message: "jobset completed successfully",
		}); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (r *JobSetReconciler) executeFailurePolicy(ctx context.Context, js *jobset.JobSet, ownedJobs *childJobs) error {
	log := ctrl.LoggerFrom(ctx)

	// If no failure policy is defined, the default failure policy is to mark the JobSet
	// as failed if any of its jobs have failed.
	if js.Spec.FailurePolicy == nil {
		log.Info("no failure policy, failing JobSet")
		return r.failJobSet(ctx, js)
	}

	// Map each unique job failure reason to the rule which should be executed for it.
	failureReasonToRuleMap := constructFailureReasonToRuleMap(js)

	// For each failed job, we check if a failure policy rule exists for the failure reason.
	// If so, we perform the action specified in that rule and return.
	for _, job := range ownedJobs.failed {
		if rule := matchingFailurePolicyRule(job, failureReasonToRuleMap); rule != nil {
			switch rule.Action {
			case jobset.FailJob:
				// Take no action, thus ensuring this job remains in a failed state.
				r.Record.Eventf(js, corev1.EventTypeNormal, FailJobEventType, fmt.Sprintf("job failure reason matched failure policy rule with action: %q", jobset.FailJob))
				continue
			case jobset.FailJobSet:
				return r.failJobSet(ctx, js)
			case jobset.Ignore:
				// To recreate JobSet and not count failure against maxRestarts, we simply
				// delete all the failed jobs and allow the reconciliation process to recreate them.
				r.Record.Eventf(js, corev1.EventTypeNormal, IgnoreEventType, fmt.Sprintf("job failure reason matched failure policy rule with action: %q", jobset.Ignore))
				return r.deleteJobs(ctx, ownedJobs.failed)
			case jobset.RestartJob:
				// To restart onlythe target failed job, we simply delete it and allow the
				// next reconciliation loop to recreate it.
				r.Record.Eventf(js, corev1.EventTypeNormal, RestartJobEventType, fmt.Sprintf("job failure reason matched failure policy rule with action: %q", jobset.RestartJob))
				return r.deleteJobs(ctx, []*batchv1.Job{job})
			}
		}
		// Otherwise, if there are no matching rules for this failed job, fall back to default
		// behavior of recreating the entire JobSet, and counting the failure against maxRestarts.
		return r.restartPolicyRecreateAll(ctx, js, ownedJobs)
	}

	// We can only reach this point if every failed job matched a rule with an "Ignore" action,
	// in which case we simply do nothing.
	return nil
}

// matchingFailurePolicyRule returns a failure policy rule for the failure reason of the given job,
// if one exists. Otherwise, it returns nil.
func matchingFailurePolicyRule(job *batchv1.Job, failureReasonToRuleMap map[string]*jobset.FailurePolicyRule) *jobset.FailurePolicyRule {
	// Find the job failure condition.
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			// If a rule exists for this failure reason, return it.
			if rule, exists := failureReasonToRuleMap[cond.Reason]; exists {
				return rule
			}
		}
	}
	return nil
}

// restartPolicyRecreateAll will increment the
func (r *JobSetReconciler) restartPolicyRecreateAll(ctx context.Context, js *jobset.JobSet, ownedJobs *childJobs) error {
	log := ctrl.LoggerFrom(ctx)

	// If JobSet has reached max number of restarts, mark the JobSet as failed and return.
	if js.Status.Restarts >= js.Spec.FailurePolicy.MaxRestarts {
		return r.ensureCondition(ctx, js, corev1.EventTypeWarning, metav1.Condition{
			Type:    string(jobset.JobSetFailed),
			Status:  metav1.ConditionStatus(corev1.ConditionTrue),
			Reason:  "ReachedMaxRestarts",
			Message: "jobset failed due to reaching max number of restarts",
		})
	}

	// Increment JobSet restarts. This status update will trigger reconciliation and
	// result in deletions of old jobs not part of the current jobSet run.
	js.Status.Restarts += 1
	if err := r.updateStatus(ctx, js, corev1.EventTypeWarning, "Restarting", fmt.Sprintf("restarting jobset, attempt %d", js.Status.Restarts)); err != nil {
		return err
	}
	log.V(2).Info("attempting restart", "restart attempt", js.Status.Restarts)
	return nil
}

func (r *JobSetReconciler) deleteJobs(ctx context.Context, jobsForDeletion []*batchv1.Job) error {
	log := ctrl.LoggerFrom(ctx)
	lock := &sync.Mutex{}
	var finalErrs []error
	workqueue.ParallelizeUntil(ctx, maxParallelism, len(jobsForDeletion), func(i int) {
		targetJob := jobsForDeletion[i]
		// Delete job. This deletion event will trigger another reconciliation,
		// where the jobs are recreated.
		backgroundPolicy := metav1.DeletePropagationBackground
		if err := r.Delete(ctx, targetJob, &client.DeleteOptions{PropagationPolicy: &backgroundPolicy}); client.IgnoreNotFound(err) != nil {
			lock.Lock()
			defer lock.Unlock()
			finalErrs = append(finalErrs, err)
			return
		}
		log.V(2).Info("successfully deleted job", "job", klog.KObj(targetJob), "restart attempt", targetJob.Labels[targetJob.Labels[RestartsKey]])
	})
	return errors.Join(finalErrs...)
}

// updateStatus updates the status of a JobSet.
func (r *JobSetReconciler) updateStatus(ctx context.Context, js *jobset.JobSet, eventType, eventReason, eventMsg string) error {
	if err := r.Status().Update(ctx, js); err != nil {
		return err
	}
	r.Record.Eventf(js, eventType, eventReason, eventMsg)
	return nil
}

func (r *JobSetReconciler) ensureCondition(ctx context.Context, js *jobset.JobSet, eventType string, condition metav1.Condition) error {
	if !updateCondition(js, condition) {
		return nil
	}
	if err := r.Status().Update(ctx, js); err != nil {
		return err
	}

	r.Record.Eventf(js, eventType, condition.Type, condition.Reason)
	return nil
}

func (r *JobSetReconciler) failJobSet(ctx context.Context, js *jobset.JobSet) error {
	return r.ensureCondition(ctx, js, corev1.EventTypeWarning, metav1.Condition{
		Type:    string(jobset.JobSetFailed),
		Status:  metav1.ConditionStatus(corev1.ConditionTrue),
		Reason:  "FailedJobs",
		Message: "jobset failed due to one or more job failures",
	})
}

func updateCondition(js *jobset.JobSet, condition metav1.Condition) bool {
	condition.LastTransitionTime = metav1.Now()
	for i, val := range js.Status.Conditions {
		if condition.Type == val.Type && condition.Status != val.Status {
			js.Status.Conditions[i] = condition
			// Condition found but different status so we should update
			return true
		} else if condition.Type == val.Type && condition.Status == val.Status {
			// Duplicate condition so no update
			return false
		}
	}
	// condition doesn't exist, update only if the status is true
	if condition.Status == metav1.ConditionTrue {
		js.Status.Conditions = append(js.Status.Conditions, condition)
		return true
	}
	return false
}
func constructJobsFromTemplate(js *jobset.JobSet, rjob *jobset.ReplicatedJob, ownedJobs *childJobs) ([]*batchv1.Job, error) {
	var jobs []*batchv1.Job
	for jobIdx := 0; jobIdx < int(rjob.Replicas); jobIdx++ {
		jobName := placement.GenJobName(js.Name, rjob.Name, jobIdx)
		if create := shouldCreateJob(jobName, ownedJobs); !create {
			continue
		}
		job, err := constructJob(js, rjob, jobIdx)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func constructJob(js *jobset.JobSet, rjob *jobset.ReplicatedJob, jobIdx int) (*batchv1.Job, error) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      collections.CloneMap(rjob.Template.Labels),
			Annotations: collections.CloneMap(rjob.Template.Annotations),
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
		job.Spec.Template.Spec.Subdomain = js.Spec.Network.Subdomain
	}

	// If this job should be exclusive per topology, configure the scheduling constraints accordingly.
	if _, ok := js.Annotations[jobset.ExclusiveKey]; ok {
		// If user has set the nodeSelectorStrategy annotation flag, add the job name label as a
		// nodeSelector, and add a toleration for the no schedule taint.
		// The node label and node taint must be added separately by a user/script.
		if _, exists := js.Annotations[jobset.NodeSelectorStrategyKey]; exists {
			addNamespacedJobNodeSelector(job)
			addTaintToleration(job)
		}
		// Otherwise, we fall back to the default strategy of the pod webhook assigning exclusive affinities
		// to the leader pod, preventing follower pod creation until leader pod is scheduled, then
		// assigning the follower pods to the same node as the leader pods.
	}

	// if Suspend is set, then we assume all jobs will be suspended also.
	jobsetSuspended := js.Spec.Suspend != nil && *js.Spec.Suspend
	job.Spec.Suspend = ptr.To(jobsetSuspended)

	return job, nil
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
	for _, job := range collections.Concat(ownedJobs.active, ownedJobs.successful, ownedJobs.failed, ownedJobs.delete) {
		if jobName == job.Name {
			return false
		}
	}
	return true
}

func labelAndAnnotateObject(obj metav1.Object, js *jobset.JobSet, rjob *jobset.ReplicatedJob, jobIdx int) {
	jobName := placement.GenJobName(js.Name, rjob.Name, jobIdx)
	labels := collections.CloneMap(obj.GetLabels())
	labels[jobset.JobSetNameKey] = js.Name
	labels[jobset.ReplicatedJobNameKey] = rjob.Name
	labels[RestartsKey] = strconv.Itoa(int(js.Status.Restarts))
	labels[jobset.ReplicatedJobReplicas] = strconv.Itoa(int(rjob.Replicas))
	labels[jobset.JobIndexKey] = strconv.Itoa(jobIdx)
	labels[jobset.JobKey] = jobHashKey(js.Namespace, jobName)

	annotations := collections.CloneMap(obj.GetAnnotations())
	annotations[jobset.JobSetNameKey] = js.Name
	annotations[jobset.ReplicatedJobNameKey] = rjob.Name
	annotations[RestartsKey] = strconv.Itoa(int(js.Status.Restarts))
	annotations[jobset.ReplicatedJobReplicas] = strconv.Itoa(int(rjob.Replicas))
	annotations[jobset.JobIndexKey] = strconv.Itoa(jobIdx)
	annotations[jobset.JobKey] = jobHashKey(js.Namespace, jobName)

	// Only set exclusive key label/annotation on jobs/pods if the parent JobSet
	// is using exclusive placement.
	if _, exists := js.Annotations[jobset.ExclusiveKey]; exists {
		annotations[jobset.ExclusiveKey] = js.Annotations[jobset.ExclusiveKey]
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

func GenSubdomain(js *jobset.JobSet) string {
	// If we have selected an explicit network name, use it
	if js.Spec.Network.Subdomain != "" {
		return js.Spec.Network.Subdomain
	}
	return js.Name
}

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

func dnsHostnamesEnabled(js *jobset.JobSet) bool {
	return js.Spec.Network.EnableDNSHostnames != nil && *js.Spec.Network.EnableDNSHostnames
}

func jobMatchesSuccessPolicy(js *jobset.JobSet, job *batchv1.Job) bool {
	return len(js.Spec.SuccessPolicy.TargetReplicatedJobs) == 0 || collections.Contains(js.Spec.SuccessPolicy.TargetReplicatedJobs, job.ObjectMeta.Labels[jobset.ReplicatedJobNameKey])
}

func replicatedJobMatchesSuccessPolicy(js *jobset.JobSet, rjob *jobset.ReplicatedJob) bool {
	return len(js.Spec.SuccessPolicy.TargetReplicatedJobs) == 0 || collections.Contains(js.Spec.SuccessPolicy.TargetReplicatedJobs, rjob.Name)
}

func numJobsMatchingSuccessPolicy(js *jobset.JobSet, jobs []*batchv1.Job) int {
	total := 0
	for _, job := range jobs {
		if jobMatchesSuccessPolicy(js, job) {
			total += 1
		}
	}
	return total
}

func numJobsExpectedToSucceed(js *jobset.JobSet) int {
	total := 0
	switch js.Spec.SuccessPolicy.Operator {
	case jobset.OperatorAny:
		total = 1
	case jobset.OperatorAll:
		for _, rjob := range js.Spec.ReplicatedJobs {
			if replicatedJobMatchesSuccessPolicy(js, &rjob) {
				total += int(rjob.Replicas)
			}
		}
	}
	return total
}

// constructFailureReasonToRuleMap
func constructFailureReasonToRuleMap(js *jobset.JobSet) map[string]*jobset.FailurePolicyRule {
	m := make(map[string]*jobset.FailurePolicyRule)
	for _, rule := range js.Spec.FailurePolicy.Rules {
		for _, reason := range rule.OnJobFailureReasons {
			m[reason] = &rule
		}
	}
	return m
}
