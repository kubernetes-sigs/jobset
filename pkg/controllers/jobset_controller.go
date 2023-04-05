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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeclientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	jobsetv1alpha "sigs.k8s.io/jobset/api/v1alpha1"
)

// JobSetReconciler reconciles a JobSet object
type JobSetReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	KubeClientSet *kubeclientset.Clientset
	Clock
}

type childJobs struct {
	active     []*batchv1.Job
	successful []*batchv1.Job
	failed     []*batchv1.Job
}

var (
	jobOwnerKey = ".metadata.controller"
	apiGVStr    = jobsetv1alpha.GroupVersion.String()
)

//+kubebuilder:rbac:groups=batch.x-k8s.io,resources=jobsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch.x-k8s.io,resources=jobsets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=batch.x-k8s.io,resources=jobsets/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *JobSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var jobSet jobsetv1alpha.JobSet
	if err := r.Get(ctx, req.NamespacedName, &jobSet); err != nil {
		klog.Error(err, "unable to fetch JobSet")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	jobs, err := r.getChildJobs(ctx, &jobSet, req)
	if err != nil {
		klog.Errorf("error getting child jobs: %v", err)
		return ctrl.Result{}, nil
	}

	// if err := r.updateStatus(ctx, &jobSet, jobs); err != nil {
	// 	klog.Errorf("error updating status: %v", err)
	// 	return ctrl.Result{}, nil
	// }

	r.cleanUpOldJobs(ctx, jobs)

	if err := r.createReadyJobs(ctx, req, &jobSet); err != nil {
		klog.Errorf("error creating ready jobs: %v", err)
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *JobSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// set up a real clock, since we're not in a test
	if r.Clock == nil {
		r.Clock = realClock{}
	}

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
		For(&jobsetv1alpha.JobSet{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}

func (r *JobSetReconciler) constructJobFromTemplate(jobSet *jobsetv1alpha.JobSet, jobTemplate *jobsetv1alpha.JobTemplate) (*batchv1.Job, error) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      make(map[string]string),
			Annotations: make(map[string]string),
			Name:        jobTemplate.Template.Name,
			Namespace:   jobSet.Namespace,
		},
		Spec: *jobTemplate.Template.Spec.DeepCopy(),
	}
	// Set controller owner reference for garbage collection and reconcilation.
	if err := ctrl.SetControllerReference(jobSet, job, r.Scheme); err != nil {
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
func (r *JobSetReconciler) getChildJobs(ctx context.Context, jobSet *jobsetv1alpha.JobSet, req ctrl.Request) (*childJobs, error) {
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

func (r *JobSetReconciler) createReadyJobs(ctx context.Context, req ctrl.Request, jobSet *jobsetv1alpha.JobSet) error {
	for _, jobTemplate := range jobSet.Spec.Jobs {
		job, err := r.constructJobFromTemplate(jobSet, &jobTemplate)
		if err != nil {
			klog.Error(err, "unable to construct job from template", "jobTemplate", jobTemplate)
			return err
		}

		// Skip the job if it is already active or succeeded.
		skip, err := r.shouldSkipJob(job)
		if err != nil {
			return err
		}
		if skip {
			klog.Infof("skipping job %s", job.Name)
			continue
		}

		klog.Infof("creating job %s", job.Name)

		// First create headless service if specified for this job.
		if jobTemplate.Network.EnableDNSHostnames != nil && *jobTemplate.Network.EnableDNSHostnames {
			if err := r.createHeadlessSvcIfNotExist(ctx, req, jobSet, job); err != nil {
				return err
			}
			// Update job spec to set subdomain as headless service name (will always be same as job name)
			job.Spec.Template.Spec.Subdomain = job.Name
		}

		if err := r.Create(ctx, job); err != nil {
			klog.Error(err, "unable to create Job for JobSet", "job", job)
			return err
		}
		klog.V(1).Info("created Job for JobSet run", "job", job)
	}
	return nil
}

func (r *JobSetReconciler) createHeadlessSvcIfNotExist(ctx context.Context, req ctrl.Request, jobSet *jobsetv1alpha.JobSet, job *batchv1.Job) error {
	// Check if service already exists. Service name is same as job name.
	if _, err := r.KubeClientSet.CoreV1().Services(req.Namespace).Get(ctx, job.Name, metav1.GetOptions{}); err != nil {
		headlessSvc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      job.Name,
				Namespace: req.Namespace,
			},
			Spec: corev1.ServiceSpec{
				ClusterIP: "None",
				Ports: []corev1.ServicePort{
					{
						Port: 8443,
					},
				},
				Selector: map[string]string{
					"job-name": job.Name,
				},
			},
		}
		// set controller owner reference for garbage collection and reconcilation
		if err := ctrl.SetControllerReference(jobSet, &headlessSvc, r.Scheme); err != nil {
			klog.Error(err, "error setting controller owner reference for headless service", "service", headlessSvc)
			return err
		}
		if _, err := r.KubeClientSet.CoreV1().Services(req.Namespace).Create(ctx, &headlessSvc, metav1.CreateOptions{}); err != nil {
			klog.Error(err, "unable to create headless service", "service", headlessSvc)
			return err
		}
	}
	return nil
}

func (r *JobSetReconciler) shouldSkipJob(job *batchv1.Job) (bool, error) {
	// Get updated job status
	job, err := r.KubeClientSet.BatchV1().Jobs(job.Namespace).Get(context.Background(), job.Name, metav1.GetOptions{})
	if err != nil {
		// if job does not exist yet, do not skip it, since it needs to be created
		if strings.Contains(err.Error(), "not found") {
			return false, nil
		}
		return false, err
	}
	_, finishedType := IsJobFinished(job)

	switch finishedType {
	case "":
		// If job is already active, skip it
		return true, nil
	// If job has failed, don't skip (recreate it)
	case batchv1.JobFailed:
		return false, nil
	// If job is complete, don't restart it
	case batchv1.JobComplete:
		return true, nil
	// Skip suspended jobs (TODO: is this the correct behavior?)
	case batchv1.JobSuspended:
		return true, nil
	// Skip if job is about to fail, we will restart it once it actually fails
	case batchv1.JobFailureTarget:
		return true, nil
	// This default should never be reached but is here for future proofing against new conditions
	default:
		return true, nil
	}
}

func (r *JobSetReconciler) isJobReady(job *batchv1.Job) (bool, error) {
	// Get updated job status
	job, err := r.KubeClientSet.BatchV1().Jobs(job.Namespace).Get(context.Background(), job.Name, metav1.GetOptions{})
	if err != nil {
		// if job does not exist yet, it is not ready
		if strings.Contains(err.Error(), "not found") {
			return false, nil
		}
		return false, err
	}
	_, finishedType := IsJobFinished(job)

	switch finishedType {
	case "":
		// Job is ready if it is active and number of ready pods == paralellism
		if job.Status.Ready != nil && job.Spec.Parallelism != nil && *job.Status.Ready == *job.Spec.Parallelism {
			return true, nil
		}
		return false, nil
	// Failed job is not ready
	case batchv1.JobFailed:
		return false, nil
	// Complete job is considered ready
	case batchv1.JobComplete:
		return true, nil
	// Suspended job is not ready
	case batchv1.JobSuspended:
		return false, nil
	// Job that is about to fail is not ready
	case batchv1.JobFailureTarget:
		return false, nil
	// This default should never be reached but is here for future proofing against new conditions
	default:
		return false, nil
	}
}
