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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
	jobsetv1alpha "sigs.k8s.io/jobset/api/v1alpha1"
)

// JobSetReconciler reconciles a JobSet object
type JobSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
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

// 1.444µs
// 1.022188934s
// 3.145383021s
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
	var jobSet jobsetv1alpha.JobSet
	if err := r.Get(ctx, req.NamespacedName, &jobSet); err != nil {
		klog.Error(err, "unable to fetch JobSet")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

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

func (r *JobSetReconciler) constructJobFromTemplate(jobSet *jobsetv1alpha.JobSet, jobTemplate *jobsetv1alpha.ReplicatedJob) (*batchv1.Job, error) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      make(map[string]string),
			Annotations: make(map[string]string),
			Name:        generateJobName(jobSet, jobTemplate),
			Namespace:   jobSet.Namespace,
		},
		Spec: *jobTemplate.Template.Spec.DeepCopy(),
	}

	// If enableDNSHostnames is set, update job spec to set subdomain as
	// job name (a headless service with same name as job will be created later).
	if jobTemplate.Network.EnableDNSHostnames != nil && *jobTemplate.Network.EnableDNSHostnames {
		job.Spec.Template.Spec.Subdomain = job.Name
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
		skip, err := r.shouldSkipJob(ctx, job)
		if err != nil {
			return err
		}
		if skip {
			klog.Infof("skipping job %s", job.Name)
			continue
		}

		klog.Infof("creating job %s", job.Name)

		// Job must be created before headless service so that the job.spec.selector
		// field is populated.
		if err := r.Create(ctx, job); err != nil {
			klog.Error(err, "unable to create Job for JobSet", "job", job)
			return err
		}

		// Next, create headless service if specified for this job.
		if jobTemplate.Network.EnableDNSHostnames != nil && *jobTemplate.Network.EnableDNSHostnames {
			if err := r.createHeadlessSvcIfNotExist(ctx, req, jobSet, job); err != nil {
				klog.Infof("error creating headless service: %v", err)
				return err
			}
		}

		klog.Info("created Job for JobSet", "job", job)
	}
	return nil
}

// TODO: look into adopting service and updating the selector
// if it is not matching the job selector.
func (r *JobSetReconciler) createHeadlessSvcIfNotExist(ctx context.Context, req ctrl.Request, jobSet *jobsetv1alpha.JobSet, job *batchv1.Job) error {
	// Check if service already exists. Service name is same as job name.
	var headlessSvc corev1.Service
	namespacedSvc := types.NamespacedName{Namespace: jobSet.Namespace, Name: job.Name}

	if err := r.Get(ctx, namespacedSvc, &headlessSvc); err != nil {

		// Get updated job object from apiserver which will have the selector populated.
		namespacedJob := types.NamespacedName{Name: job.Name, Namespace: jobSet.Namespace}

		// Need to retry in case the freshly created job object has not been persisted quite yet.
		if err := retry.OnError(
			defaultRetry,
			func(error) bool { return true }, // always retry
			func() error {
				if err := r.Get(ctx, namespacedJob, job); err != nil {
					return err
				}
				return nil
			},
		); err != nil {
			return err
		}

		// Construct headless service defintion.
		selectorMap, err := metav1.LabelSelectorAsMap(job.Spec.Selector)
		if err != nil {
			return err
		}
		headlessSvc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      job.Name,
				Namespace: req.Namespace,
			},
			Spec: corev1.ServiceSpec{
				ClusterIP: "None",
				Selector:  selectorMap,
			},
		}

		// set controller owner reference for garbage collection and reconcilation
		if err := ctrl.SetControllerReference(jobSet, &headlessSvc, r.Scheme); err != nil {
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

func (r *JobSetReconciler) shouldSkipJob(ctx context.Context, job *batchv1.Job) (bool, error) {
	// Get updated job status
	namespacedJob := types.NamespacedName{Namespace: job.Namespace, Name: job.Name}
	if err := r.Get(ctx, namespacedJob, job); err != nil {
		// if job does not exist yet, do not skip it, since it needs to be created
		if strings.Contains(err.Error(), "not found") {
			return false, nil
		}
		return false, err
	}
	_, finishedType := IsJobFinished(job)

	switch finishedType {
	// If job is already active, skip it
	case "":
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
