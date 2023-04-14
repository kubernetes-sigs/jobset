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
	"strconv"
	"sync"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/v1alpha1"
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

//+kubebuilder:rbac:groups="",resources=events,verbs=create;watch;update
//+kubebuilder:rbac:groups=batch.x-k8s.io,resources=jobsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch.x-k8s.io,resources=jobsets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=batch.x-k8s.io,resources=jobsets/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get

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

	// If JobSet is already completed or failed, we don't need to reconcile anything.
	if isJobSetFinished(&js) {
		return ctrl.Result{}, nil
	}

	// Get Jobs owned by JobSet.
	ownedJobs, err := r.getChildJobs(ctx, &js)
	if err != nil {
		log.Error(err, "getting jobs owned by jobset")
		return ctrl.Result{}, nil
	}

	// Delete any jobs marked for deletion.
	if err := r.deleteJobs(ctx, &js, ownedJobs.delete); err != nil {
		log.Error(err, "deleting jobs")
		return ctrl.Result{}, nil
	}

	// If any jobs have failed, execute the JobSet failure policy (if any).
	if len(ownedJobs.failed) > 0 {
		if err := r.executeFailurePolicy(ctx, &js, ownedJobs); err != nil {
			log.Error(err, "executing failure policy")
		}
		return ctrl.Result{}, nil
	}

	// If all jobs have succeeded, JobSet has succeeded.
	if len(ownedJobs.successful) == len(js.Spec.Jobs) {
		if err := r.updateStatusWithCondition(ctx, &js, corev1.EventTypeNormal, metav1.Condition{
			Type:    string(jobset.JobSetCompleted),
			Status:  metav1.ConditionStatus(corev1.ConditionTrue),
			Reason:  "AllJobsCompleted",
			Message: "jobset completed successfully",
		}); err != nil {
			log.Error(err, "updating jobset status")
			return ctrl.Result{}, nil
		}
	}

	// If job has not failed or succeeded, continue creating any
	// jobs that are ready to be started.
	if err := r.createJobs(ctx, &js, ownedJobs); err != nil {
		log.Error(err, "creating jobs")
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *JobSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &batchv1.Job{}, jobOwnerKey, func(rawObj client.Object) []string {
		// grab the job object, extract the owner...
		job := rawObj.(*batchv1.Job)
		owner := metav1.GetControllerOf(job)
		if owner == nil {
			return nil
		}
		// ...make sure it's a JobSet...
		if owner.APIVersion != apiGVStr || owner.Kind != "JobSet" {
			return nil
		}

		// ...and if so, return it
		return []string{owner.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&jobset.JobSet{}).
		Owns(&batchv1.Job{}).
		Complete(r)
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
		jobRestarts, err := strconv.Atoi(job.Labels[jobset.RestartsLabel])
		if err != nil {
			log.Error(err, fmt.Sprintf("invalid value for label %s, must be integer", jobset.RestartsLabel))
			ownedJobs.delete = append(ownedJobs.delete, &childJobList.Items[i])
			return nil, err
		}
		if jobRestarts < js.Status.Restarts {
			ownedJobs.delete = append(ownedJobs.delete, &childJobList.Items[i])
			continue
		}

		// Jobs with jobset.sigs.k8s.io/restart-attempt == jobset.status.restarts are part of
		// the current JobSet run, and marked either active, successful, or failed.
		_, finishedType := isJobFinished(&job)
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

func (r *JobSetReconciler) createJobs(ctx context.Context, js *jobset.JobSet, ownedJobs *childJobs) error {
	log := ctrl.LoggerFrom(ctx)

	for _, rjob := range js.Spec.Jobs {
		jobs, err := r.constructJobsFromTemplate(js, &rjob, ownedJobs)
		if err != nil {
			return err
		}

		for _, job := range jobs {
			// Create headless service if specified for this job.
			if dnsHostnamesEnabled(&rjob) {
				if err := r.createHeadlessSvcIfNotExist(ctx, js, job); err != nil {
					return err
				}
			}

			// Create the job.
			// TODO(#18): Deal with the case where the job exists but is not owned by the jobset.
			if err := r.Create(ctx, job); err != nil {
				return err
			}
			log.V(2).Info("successfully created job", "job", klog.KObj(job))
		}
	}
	return nil
}

// TODO: look into adopting service and updating the selector
// if it is not matching the job selector.
func (r *JobSetReconciler) createHeadlessSvcIfNotExist(ctx context.Context, js *jobset.JobSet, job *batchv1.Job) error {
	log := ctrl.LoggerFrom(ctx)

	// Check if service already exists. Service name is same as job name.
	// If the service does not exist, create it.
	var headlessSvc corev1.Service
	if err := r.Get(ctx, types.NamespacedName{Name: job.Name, Namespace: js.Namespace}, &headlessSvc); err != nil {
		headlessSvc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      job.Name,
				Namespace: js.Namespace,
			},
			Spec: corev1.ServiceSpec{
				ClusterIP: "None",
				Selector: map[string]string{
					// TODO: Migrate to the fully qualified label name.
					"job-name": job.Name,
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
		log.V(2).Info("successfully created headless service", "service", klog.KObj(job))
	}
	return nil
}

func (r *JobSetReconciler) executeFailurePolicy(ctx context.Context, js *jobset.JobSet, ownedJobs *childJobs) error {
	// If no failure policy is defined, the default failure policy is to mark the JobSet
	// as failed if any of its jobs have failed.
	if js.Spec.FailurePolicy == nil {
		return r.failJobSet(ctx, js)
	}

	// Handle different types of failure policy targets.
	switch js.Spec.FailurePolicy.Operator {
	case jobset.TerminationPolicyTargetAny:
		// To reach this point a job must have failed, and TerminationPolicyTargetAny applies to any job
		// in the JobSet, so we can skip directly to executing the restart policy.
		return r.executeRestartPolicy(ctx, js, ownedJobs)
	default:
		return fmt.Errorf("invalid termination policy target %s", js.Spec.FailurePolicy.Operator)
	}
}

func (r *JobSetReconciler) executeRestartPolicy(ctx context.Context, js *jobset.JobSet, ownedJobs *childJobs) error {
	log := ctrl.LoggerFrom(ctx)

	switch js.Spec.FailurePolicy.RestartPolicy {
	case jobset.RestartPolicyRecreateAll:
		return r.restartPolicyRecreateAll(ctx, js, ownedJobs)
	case jobset.RestartPolicyNone:
		return r.failJobSet(ctx, js)
	default:
		log.Error(fmt.Errorf("invalid restart policy: %s", js.Spec.FailurePolicy.RestartPolicy), "invalid restart policy, defaulting to None")
		return r.failJobSet(ctx, js)
	}
}

func (r *JobSetReconciler) restartPolicyRecreateAll(ctx context.Context, js *jobset.JobSet, ownedJobs *childJobs) error {
	log := ctrl.LoggerFrom(ctx)

	// If JobSet has reached max number of restarts, mark it as failed and return.
	if js.Status.Restarts == js.Spec.FailurePolicy.MaxRestarts {
		return r.updateStatusWithCondition(ctx, js, corev1.EventTypeWarning, metav1.Condition{
			Type:    string(jobset.JobSetFailed),
			Status:  metav1.ConditionStatus(corev1.ConditionTrue),
			Reason:  "ReachedMaxRestarts",
			Message: "jobset failed due to reaching max number of restarts",
		})
	}

	// Increment JobSet restarts. This will trigger reconciliation and result in deletions
	// of old jobs not part of the current jobSet run.
	js.Status.Restarts += 1
	if err := r.updateStatus(ctx, js, corev1.EventTypeWarning, "Restarting", fmt.Sprintf("restarting jobset, attempt %d", js.Status.Restarts)); err != nil {
		return err
	}
	log.V(2).Info("attempting restart", "restart attempt", js.Status.Restarts)
	return nil
}

func (r *JobSetReconciler) deleteJobs(ctx context.Context, js *jobset.JobSet, jobsForDeletion []*batchv1.Job) error {
	log := ctrl.LoggerFrom(ctx)

	var wg sync.WaitGroup
	var finalErr error

	// Delete all jobs in parallel.
	// TODO: limit number of goroutines used here.
	for _, job := range jobsForDeletion {
		job := job
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Delete job. This deletion event will trigger another reconcilliation,
			// where the jobs are recreated.
			backgroundPolicy := metav1.DeletePropagationBackground
			if err := r.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &backgroundPolicy}); client.IgnoreNotFound(err) != nil {
				finalErr = err
				return
			}
			log.V(2).Info("successfully deleted job", "job", klog.KObj(job), "restart attempt", job.Labels[job.Labels[jobset.RestartsLabel]])
		}()
	}
	wg.Wait()
	return finalErr
}

func (r *JobSetReconciler) constructJobsFromTemplate(js *jobset.JobSet, rjob *jobset.ReplicatedJob, ownedJobs *childJobs) ([]*batchv1.Job, error) {
	var jobs []*batchv1.Job
	// Construct jobs.
	for i := 0; i < rjob.Replicas; i++ {
		// Check if we need to create this job. If not, skip it.
		jobName := genJobName(js, rjob, i)

		if create := r.shouldCreateJob(jobName, ownedJobs); !create {
			continue
		}

		// Copy labels/annotations to avoid modifying the template itself.
		labels := make(map[string]string)
		for k, v := range rjob.Template.Labels {
			labels[k] = v
		}
		annotations := make(map[string]string)
		for k, v := range rjob.Template.Annotations {
			annotations[k] = v
		}

		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      labels,
				Annotations: annotations,
				Name:        genJobName(js, rjob, i),
				Namespace:   js.Namespace,
			},
			Spec: *rjob.Template.Spec.DeepCopy(),
		}

		// Add restart-attempt count label, it should be equal to jobSet restarts
		// to indicate is part of the current jobSet run.
		job.Labels[jobset.RestartsLabel] = strconv.Itoa(js.Status.Restarts)

		// Add job index as a label and annotation.
		job.Labels[jobset.JobIndexLabel] = strconv.Itoa(i)
		job.Annotations[jobset.JobIndexLabel] = strconv.Itoa(i)

		// If enableDNSHostnames is set, update job spec to set subdomain as
		// job name (a headless service with same name as job will be created later).
		if dnsHostnamesEnabled(rjob) {
			job.Spec.Template.Spec.Subdomain = job.Name
		}

		// Set controller owner reference for garbage collection and reconcilation.
		if err := ctrl.SetControllerReference(js, job, r.Scheme); err != nil {
			return nil, err
		}

		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (r *JobSetReconciler) shouldCreateJob(jobName string, ownedJobs *childJobs) bool {
	// Check if this job exists already.
	// TODO: maybe we can use a job map here so we can do O(1) lookups
	// to check if the job already exists, rather than a linear scan
	// through all the jobs owned by the jobset.
	for _, activeJob := range ownedJobs.active {
		if activeJob.Name == jobName {
			return false
		}
	}
	for _, successfulJob := range ownedJobs.successful {
		if successfulJob.Name == jobName {
			return false
		}
	}
	for _, failedJob := range ownedJobs.failed {
		if failedJob.Name == jobName {
			return false
		}
	}
	return true
}

// updateStatus updates the status of a JobSet.
func (r *JobSetReconciler) updateStatus(ctx context.Context, js *jobset.JobSet, eventType, eventReason, eventMsg string) error {
	if err := r.Status().Update(ctx, js); err != nil {
		return err
	}
	r.Record.Eventf(js, eventType, eventReason, eventMsg)
	return nil
}

// TODO: update condition in place if it exists.
func (r *JobSetReconciler) updateStatusWithCondition(ctx context.Context, js *jobset.JobSet, eventType string, condition metav1.Condition) error {
	condition.LastTransitionTime = metav1.Now()
	js.Status.Conditions = append(js.Status.Conditions, condition)

	if err := r.Status().Update(ctx, js); err != nil {
		return err
	}

	r.Record.Eventf(js, eventType, condition.Type, condition.Reason)
	return nil
}

func (r *JobSetReconciler) failJobSet(ctx context.Context, js *jobset.JobSet) error {
	return r.updateStatusWithCondition(ctx, js, corev1.EventTypeWarning, metav1.Condition{
		Type:    string(jobset.JobSetFailed),
		Status:  metav1.ConditionStatus(corev1.ConditionTrue),
		Reason:  "FailedJobs",
		Message: "jobset failed due to one or more job failures",
	})
}

func isJobFinished(job *batchv1.Job) (bool, batchv1.JobConditionType) {
	for _, c := range job.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
			return true, c.Type
		}
	}
	return false, ""
}

func genJobName(js *jobset.JobSet, rjob *jobset.ReplicatedJob, jobIndex int) string {
	return fmt.Sprintf("%s-%s-%d", js.Name, rjob.Name, jobIndex)
}

func isJobSetFinished(js *jobset.JobSet) bool {
	for _, c := range js.Status.Conditions {
		if (c.Type == string(jobset.JobSetCompleted) || c.Type == string(jobset.JobSetFailed)) && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func isIndexedJob(job *batchv1.Job) bool {
	return job.Spec.CompletionMode != nil && *job.Spec.CompletionMode == batchv1.IndexedCompletion
}

func dnsHostnamesEnabled(rjob *jobset.ReplicatedJob) bool {
	return rjob.Network.EnableDNSHostnames != nil && *rjob.Network.EnableDNSHostnames
}
