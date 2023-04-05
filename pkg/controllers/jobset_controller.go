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
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
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

const jobLabelKey = "jobset.sigs.k8s.io/job-name"

var (
	jobOwnerKey = ".metadata.controller"
	apiGVStr    = jobset.GroupVersion.String()
)

var defaultRetry = wait.Backoff{
	Steps:    3,
	Duration: 1 * time.Second,
	Factor:   2.0,
	Jitter:   0.1,
}

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
		klog.Error(err, "unable to fetch JobSet")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Get Jobs owned by JobSet.
	jobs, err := r.getChildJobs(ctx, &js, req)
	if err != nil {
		klog.Errorf("error getting jobs owned by jobset %s: %v", js.Name, err)
		return ctrl.Result{}, nil
	}

	// If any jobs have failed, JobSet has failed.
	if len(jobs.failed) > 0 {
		if err := r.updateStatus(ctx, &js, jobset.JobSetCondition{
			Type:    jobset.JobSetFailed,
			Status:  corev1.ConditionTrue,
			Message: "jobset failed due to one or more failed jobs",
		}); err != nil {
			klog.Errorf("error updating jobset status: %s", err)
			return ctrl.Result{}, nil
		}
	}

	// If all jobs have succeeded, JobSet has succeeded.
	if len(jobs.successful) == len(js.Spec.Jobs) {
		if err := r.updateStatus(ctx, &js, jobset.JobSetCondition{
			Type:    jobset.JobSetComplete,
			Status:  corev1.ConditionTrue,
			Message: "jobset completed successfully",
		}); err != nil {
			klog.Errorf("error updating jobset status: %s", err)
			return ctrl.Result{}, nil
		}
	}

	// If job has not failed or succeeded, continue creating any
	// jobs that are ready to be started.
	if err := r.createReadyJobs(ctx, &js); err != nil {
		klog.Errorf("error creating ready jobs: %v", err)
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

func (r *JobSetReconciler) constructJobFromTemplate(js *jobset.JobSet, jobTemplate *jobset.ReplicatedJob) (*batchv1.Job, error) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      make(map[string]string),
			Annotations: make(map[string]string),
			Name:        genJobName(js, jobTemplate),
			Namespace:   js.Namespace,
		},
		Spec: *jobTemplate.Template.Spec.DeepCopy(),
	}

	// If enableDNSHostnames is set, update job spec to set subdomain as
	// job name (a headless service with same name as job will be created later).
	if jobTemplate.Network.EnableDNSHostnames != nil && *jobTemplate.Network.EnableDNSHostnames {
		job.Spec.Template.Spec.Subdomain = job.Name

		// Add labels to pods to be selected by the headless service selector.
		job.Spec.Template.Labels = genLabelSelector(job)
	}

	// Set controller owner reference for garbage collection and reconcilation.
	if err := ctrl.SetControllerReference(js, job, r.Scheme); err != nil {
		return nil, err
	}
	return job, nil
}

// cleanUpOldJobs does "best effort" deletion of old jobs - if we fail on
// a particular one, we won't requeue just to finish the deleting.
func (r *JobSetReconciler) cleanUpOldJobs(ctx context.Context, jobs *childJobs) {
	// Clean up failed jobs
	for _, job := range jobs.failed {
		if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); client.IgnoreNotFound(err) != nil {
			klog.Error(err, "unable to delete old failed job", "job", job)
		} else {
			klog.V(0).Info("deleted old failed job", "job", job)
		}
	}
}

// getChildJobs fetches all Jobs owned by the JobSet and returns them
// categorized by status (active, successful, failed).
func (r *JobSetReconciler) getChildJobs(ctx context.Context, js *jobset.JobSet, req ctrl.Request) (*childJobs, error) {
	// Get all active jobs owned by JobSet.
	var childJobList batchv1.JobList
	if err := r.List(ctx, &childJobList, client.InNamespace(req.Namespace), client.MatchingFields{jobOwnerKey: req.Name}); err != nil {
		klog.Error(err, "unable to list child Jobs")
		return nil, err
	}

	jobs := childJobs{}
	for i, job := range childJobList.Items {
		_, finishedType := IsJobFinished(&job)
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

func (r *JobSetReconciler) createReadyJobs(ctx context.Context, js *jobset.JobSet) error {
	for _, jobTemplate := range js.Spec.Jobs {
		job, err := r.constructJobFromTemplate(js, &jobTemplate)
		if err != nil {
			klog.Error(err, "unable to construct job from template", "jobTemplate", jobTemplate)
			return err
		}

		// Check if we need to create this job.
		// If not, skip this job and continue iterating to the next job.
		create, err := r.shouldCreateJob(ctx, job)
		if err != nil {
			return err
		}
		if !create {
			klog.Infof("skipping job %s", job.Name)
			continue
		}

		// Create headless service if specified for this job.
		if jobTemplate.Network.EnableDNSHostnames != nil && *jobTemplate.Network.EnableDNSHostnames {
			klog.Infof("creating headless service: %s", job.Name)
			if err := r.createHeadlessSvcIfNotExist(ctx, js, job); err != nil {
				klog.Infof("error creating headless service: %v", err)
				return err
			}
		}

		// Create the job.
		klog.Infof("creating job %s", job.Name)
		if err := r.Create(ctx, job); err != nil {
			klog.Error(err, "unable to create Job for JobSet", "job", job)
			return err
		}
	}
	return nil
}

// TODO: look into adopting service and updating the selector
// if it is not matching the job selector.
func (r *JobSetReconciler) createHeadlessSvcIfNotExist(ctx context.Context, js *jobset.JobSet, job *batchv1.Job) error {
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
				Selector:  genLabelSelector(job),
			},
		}

		// Set controller owner reference for garbage collection and reconcilation.
		if err := ctrl.SetControllerReference(js, &headlessSvc, r.Scheme); err != nil {
			klog.Error(err, "error setting controller owner reference for headless service", "service", headlessSvc)
			return err
		}

		// Create headless service.
		if err := r.Create(ctx, &headlessSvc); err != nil {
			klog.Error(err, "unable to create headless service", "service", headlessSvc)
			return err
		}
	}
	return nil
}

func (r *JobSetReconciler) shouldCreateJob(ctx context.Context, job *batchv1.Job) (bool, error) {
	// Check if this job exists already.
	if err := r.Get(ctx, types.NamespacedName{Namespace: job.Namespace, Name: job.Name}, &batchv1.Job{}); err != nil {
		// If job does not exist yet, it needs to be created.
		if strings.Contains(err.Error(), "not found") {
			return true, nil
		}
		// If we got any error besides the resource not being found,
		// do not create the job and surface the error to the caller.
		return false, err
	}
	// If job already exists, do not create it.
	return false, nil
}

// updateStatus updates the status of a JobSet by appending
// the new condition to jobset.status.conditions.
func (r *JobSetReconciler) updateStatus(ctx context.Context, js *jobset.JobSet, condition jobset.JobSetCondition) error {
	js.Status.Conditions = append(js.Status.Conditions, condition)
	if err := r.Status().Update(ctx, js); err != nil {
		klog.Error(err, "unable to update JobSet status")
		return err
	}
	klog.Infof("jobset %s condition: %v", js.Name, condition)
	return nil
}
