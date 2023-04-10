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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jobset "sigs.k8s.io/jobset/api/v1alpha1"
)

// JobSetReconciler reconciles a JobSet object
type JobSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

type childJobs struct {
	active     []*batchv1.Job
	successful []*batchv1.Job
	failed     []*batchv1.Job
}

var (
	jobOwnerKey = ".metadata.controller"
	apiGVStr    = jobset.GroupVersion.String()
)

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
	ownedJobs, err := r.getChildJobs(ctx, &js, req)
	if err != nil {
		log.Error(err, "getting jobs owned by jobset")
		return ctrl.Result{}, nil
	}

	// If any jobs have failed, JobSet has failed.
	if len(ownedJobs.failed) > 0 {
		if err := r.updateStatus(ctx, &js, metav1.Condition{
			Type:               string(jobset.JobSetFailed),
			Status:             metav1.ConditionStatus(corev1.ConditionTrue),
			LastTransitionTime: metav1.Now(),
			Reason:             "FailedJobs",
			Message:            "jobset failed due to one or more failed jobs",
		}); err != nil {
			log.Error(err, "updating jobset status")
			return ctrl.Result{}, nil
		}
	}

	// If all jobs have succeeded, JobSet has succeeded.
	if len(ownedJobs.successful) == len(js.Spec.Jobs) {
		if err := r.updateStatus(ctx, &js, metav1.Condition{
			Type:               string(jobset.JobSetCompleted),
			Status:             metav1.ConditionStatus(corev1.ConditionTrue),
			LastTransitionTime: metav1.Now(),
			Reason:             "AllJobsCompleted",
			Message:            "jobset completed successfully",
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

func (r *JobSetReconciler) constructJobsFromTemplate(js *jobset.JobSet, rjob *jobset.ReplicatedJob) ([]*batchv1.Job, error) {
	var jobs []*batchv1.Job

	// Defaulting and validation.
	// TODO: Do defaulting and validation in webhook instead of here (https://github.com/kubernetes-sigs/jobset/issues/6)
	replicas := 1
	if rjob.Replicas != nil && *rjob.Replicas > 0 {
		replicas = *rjob.Replicas
	}
	labels := rjob.Template.Labels
	if labels == nil {
		labels = make(map[string]string)
	}
	annotations := rjob.Template.Annotations
	if annotations == nil {
		annotations = make(map[string]string)
	}

	// Construct jobs.
	for i := 0; i < replicas; i++ {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      labels,
				Annotations: annotations,
				Name:        genJobName(js, rjob, i),
				Namespace:   js.Namespace,
			},
			Spec: *rjob.Template.Spec.DeepCopy(),
		}

		// Add job index as a label and annotation.
		job.Labels[jobset.JobIndexLabel] = strconv.Itoa(i)
		job.Annotations[jobset.JobIndexLabel] = strconv.Itoa(i)

		// If enableDNSHostnames is set, update job spec to set subdomain as
		// job name (a headless service with same name as job will be created later).
		if rjob.Network.EnableDNSHostnames != nil && *rjob.Network.EnableDNSHostnames {
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

// getChildJobs fetches all Jobs owned by the JobSet and returns them
// categorized by status (active, successful, failed).
func (r *JobSetReconciler) getChildJobs(ctx context.Context, js *jobset.JobSet, req ctrl.Request) (*childJobs, error) {
	// Get all active jobs owned by JobSet.
	var childJobList batchv1.JobList
	if err := r.List(ctx, &childJobList, client.InNamespace(req.Namespace), client.MatchingFields{jobOwnerKey: req.Name}); err != nil {
		return nil, err
	}

	jobs := childJobs{}
	for i, job := range childJobList.Items {
		_, finishedType := isJobFinished(&job)
		switch finishedType {
		case "": // active
			jobs.active = append(jobs.active, &childJobList.Items[i])
		case batchv1.JobFailed:
			jobs.failed = append(jobs.failed, &childJobList.Items[i])
		case batchv1.JobComplete:
			jobs.successful = append(jobs.successful, &childJobList.Items[i])
		}
	}

	return &jobs, nil
}

func (r *JobSetReconciler) createJobs(ctx context.Context, js *jobset.JobSet, ownedJobs *childJobs) error {
	log := ctrl.LoggerFrom(ctx).WithValues("jobset", klog.KObj(js))
	ctx = ctrl.LoggerInto(ctx, log)

	for _, rjob := range js.Spec.Jobs {
		jobs, err := r.constructJobsFromTemplate(js, &rjob)
		if err != nil {
			return err
		}

		for _, job := range jobs {
			// Check if we need to create this job.
			// If not, skip this job and continue iterating to the next job.
			if create := r.shouldCreateJob(ctx, job, ownedJobs); !create {
				continue
			}

			// Create headless service if specified for this job.
			if rjob.Network.EnableDNSHostnames != nil && *rjob.Network.EnableDNSHostnames {
				if err := r.createHeadlessSvcIfNotExist(ctx, js, job); err != nil {
					return err
				}
			}

			// Create the job.
			// TODO: Deal with the case where the job exists but is not owned by the jobset.
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
	log := ctrl.LoggerFrom(ctx).WithValues("jobset", klog.KObj(js))
	ctx = ctrl.LoggerInto(ctx, log)

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

func (r *JobSetReconciler) shouldCreateJob(ctx context.Context, job *batchv1.Job, ownedJobs *childJobs) bool {
	// Check if this job exists already.
	// TODO: maybe we can use a job map here so we can do O(1) lookups
	// to check if the job already exists, rather than a linear scan
	// through all the jobs owned by the jobset.
	for _, activeJob := range ownedJobs.active {
		if activeJob.Name == job.Name {
			return false
		}
	}
	for _, successfulJob := range ownedJobs.successful {
		if successfulJob.Name == job.Name {
			return false
		}
	}
	for _, failedJob := range ownedJobs.failed {
		if failedJob.Name == job.Name {
			return false
		}
	}
	return true
}

// updateStatus updates the status of a JobSet by appending
// the new condition to jobset.status.conditions.
func (r *JobSetReconciler) updateStatus(ctx context.Context, js *jobset.JobSet, condition metav1.Condition) error {
	js.Status.Conditions = append(js.Status.Conditions, condition)
	if err := r.Status().Update(ctx, js); err != nil {
		return err
	}
	return nil
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
