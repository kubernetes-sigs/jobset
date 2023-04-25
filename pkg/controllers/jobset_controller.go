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

const RestartsKey string = "jobset.sigs.k8s.io/restart-attempt"

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
	if len(ownedJobs.successful) == len(js.Spec.ReplicatedJobs) {
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
	return ctrl.NewControllerManagedBy(mgr).
		For(&jobset.JobSet{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}

func SetupIndexes(indexer client.FieldIndexer) error {
	return indexer.IndexField(context.Background(), &batchv1.Job{}, jobOwnerKey, func(obj client.Object) []string {
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

	for _, rjob := range js.Spec.ReplicatedJobs {
		jobs, err := constructJobsFromTemplate(js, &rjob, ownedJobs)
		if err != nil {
			return err
		}

		// If pod DNS hostnames are enabled, create a headless service per repliactedJob.
		if dnsHostnamesEnabled(&rjob) {
			if err := r.createHeadlessSvcIfNotExist(ctx, js, &rjob); err != nil {
				return err
			}
		}

		for _, job := range jobs {
			// Set jobset controller as owner of the job for garbage collection and reconcilation.
			if err := ctrl.SetControllerReference(js, job, r.Scheme); err != nil {
				return err
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
func (r *JobSetReconciler) createHeadlessSvcIfNotExist(ctx context.Context, js *jobset.JobSet, rjob *jobset.ReplicatedJob) error {
	log := ctrl.LoggerFrom(ctx)

	// Check if service already exists. Service name is <jobSetName>-<replicatedJobName>.
	// If the service does not exist, create it.
	var headlessSvc corev1.Service
	subdomain := genSubdomain(js, rjob)
	if err := r.Get(ctx, types.NamespacedName{Name: subdomain, Namespace: js.Namespace}, &headlessSvc); err != nil {
		headlessSvc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      subdomain,
				Namespace: js.Namespace,
			},
			Spec: corev1.ServiceSpec{
				ClusterIP: "None",
				Selector: map[string]string{
					jobset.JobSetNameKey:        js.Name,
					jobset.ReplicatedJobNameKey: rjob.Name,
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

func (r *JobSetReconciler) executeFailurePolicy(ctx context.Context, js *jobset.JobSet, ownedJobs *childJobs) error {
	// If no failure policy is defined, the default failure policy is to mark the JobSet
	// as failed if any of its jobs have failed.
	if js.Spec.FailurePolicy == nil {
		return r.failJobSet(ctx, js)
	}

	// To reach this point a job must have failed.
	return r.executeRestartPolicy(ctx, js, ownedJobs)
}

func (r *JobSetReconciler) executeRestartPolicy(ctx context.Context, js *jobset.JobSet, ownedJobs *childJobs) error {
	if js.Spec.FailurePolicy.MaxRestarts == 0 {
		return r.failJobSet(ctx, js)
	}
	return r.restartPolicyRecreateAll(ctx, js, ownedJobs)
}

func (r *JobSetReconciler) restartPolicyRecreateAll(ctx context.Context, js *jobset.JobSet, ownedJobs *childJobs) error {
	log := ctrl.LoggerFrom(ctx)

	// If JobSet has reached max number of restarts, mark it as failed and return.
	if js.Status.Restarts >= js.Spec.FailurePolicy.MaxRestarts {
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
			log.V(2).Info("successfully deleted job", "job", klog.KObj(job), "restart attempt", job.Labels[job.Labels[RestartsKey]])
		}()
	}
	wg.Wait()
	return finalErr
}

// updateStatus updates the status of a JobSet.
func (r *JobSetReconciler) updateStatus(ctx context.Context, js *jobset.JobSet, eventType, eventReason, eventMsg string) error {
	if err := r.Status().Update(ctx, js); err != nil {
		return err
	}
	r.Record.Eventf(js, eventType, eventReason, eventMsg)
	return nil
}

// TODO(#39): update condition in place if it exists.
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

func constructJobsFromTemplate(js *jobset.JobSet, rjob *jobset.ReplicatedJob, ownedJobs *childJobs) ([]*batchv1.Job, error) {
	var jobs []*batchv1.Job
	for jobIdx := 0; jobIdx < rjob.Replicas; jobIdx++ {
		jobName := genJobName(js, rjob, jobIdx)
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
			Labels:      cloneMap(rjob.Template.Labels),
			Annotations: cloneMap(rjob.Template.Annotations),
			Name:        genJobName(js, rjob, jobIdx),
			Namespace:   js.Namespace,
		},
		Spec: *rjob.Template.Spec.DeepCopy(),
	}
	// Label and annotate both job and pod template spec.
	labelAndAnnotateObject(job, js, rjob, jobIdx)
	labelAndAnnotateObject(&job.Spec.Template, js, rjob, jobIdx)

	// If enableDNSHostnames is set, update job spec to set subdomain as
	// job name (a headless service with same name as job will be created later).
	if dnsHostnamesEnabled(rjob) {
		job.Spec.Template.Spec.Subdomain = genSubdomain(js, rjob)
	}

	// If this job should be exclusive per topology, set the pod affinities/anti-affinities accordingly.
	if rjob.Exclusive != nil {
		setExclusiveAffinities(job, rjob.Exclusive.TopologyKey, rjob.Exclusive.NamespaceSelector)
	}

	return job, nil
}

// Appends pod affinity/anti-affinity terms to the job pod template spec,
// ensuring that exclusively one job runs per topology domain and that all pods
// from each job land on the same topology domain.
func setExclusiveAffinities(job *batchv1.Job, topologyKey string, nsSelector *metav1.LabelSelector) {
	if job.Spec.Template.Spec.Affinity == nil {
		job.Spec.Template.Spec.Affinity = &corev1.Affinity{}
	}
	if job.Spec.Template.Spec.Affinity.PodAffinity == nil {
		job.Spec.Template.Spec.Affinity.PodAffinity = &corev1.PodAffinity{}
	}
	if job.Spec.Template.Spec.Affinity.PodAntiAffinity == nil {
		job.Spec.Template.Spec.Affinity.PodAntiAffinity = &corev1.PodAntiAffinity{}
	}

	// Pod affinity ensures the pods of this job land on the same topology domain.
	job.Spec.Template.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution = append(job.Spec.Template.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
		corev1.PodAffinityTerm{
			LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      jobset.JobNameKey,
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{job.Name},
				},
			}},
			TopologyKey:       topologyKey,
			NamespaceSelector: nsSelector,
		})

	// Pod anti-affinity ensures exclusively this job lands on the topology, preventing multiple jobs per topology domain.
	job.Spec.Template.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution = append(job.Spec.Template.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
		corev1.PodAffinityTerm{
			LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      jobset.JobNameKey,
					Operator: metav1.LabelSelectorOpExists,
				},
				{
					Key:      jobset.JobNameKey,
					Operator: metav1.LabelSelectorOpNotIn,
					Values:   []string{job.Name},
				},
			}},
			TopologyKey:       topologyKey,
			NamespaceSelector: nsSelector,
		})
}

func shouldCreateJob(jobName string, ownedJobs *childJobs) bool {
	// Check if this job exists already.
	// TODO: maybe we can use a job map here so we can do O(1) lookups
	// to check if the job already exists, rather than a linear scan
	// through all the jobs owned by the jobset.
	for _, job := range concat(ownedJobs.active, ownedJobs.successful, ownedJobs.failed, ownedJobs.delete) {
		if jobName == job.Name {
			return false
		}
	}
	return true
}

func labelAndAnnotateObject(obj metav1.Object, js *jobset.JobSet, rjob *jobset.ReplicatedJob, jobIdx int) {
	labels := cloneMap(obj.GetLabels())
	labels[jobset.JobSetNameKey] = js.Name
	labels[jobset.ReplicatedJobNameKey] = rjob.Name
	labels[RestartsKey] = strconv.Itoa(js.Status.Restarts)
	labels[jobset.ReplicatedJobReplicas] = strconv.Itoa(rjob.Replicas)
	labels[jobset.JobIndexKey] = strconv.Itoa(jobIdx)

	annotations := cloneMap(obj.GetAnnotations())
	annotations[jobset.ReplicatedJobReplicas] = strconv.Itoa(rjob.Replicas)
	annotations[jobset.JobIndexKey] = strconv.Itoa(jobIdx)

	obj.SetLabels(labels)
	obj.SetAnnotations(annotations)
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

func genSubdomain(js *jobset.JobSet, rjob *jobset.ReplicatedJob) string {
	return fmt.Sprintf("%s-%s", js.Name, rjob.Name)
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

func concat[T any](slices ...[]T) []T {
	var result []T
	for _, slice := range slices {
		result = append(result, slice...)
	}
	return result
}

func cloneMap[K, V comparable](m map[K]V) map[K]V {
	copy := make(map[K]V)
	for k, v := range m {
		copy[k] = v
	}
	return copy
}
