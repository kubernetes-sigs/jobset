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

package testing

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// TestPodSpec is the default pod spec used for testing.
var TestPodSpec = corev1.PodSpec{
	RestartPolicy: "Never",
	Containers: []corev1.Container{
		{
			Name:  "test-container",
			Image: "busybox:latest",
		},
	},
}

// JobSetWrapper wraps a JobSet.
type JobSetWrapper struct {
	jobset.JobSet
}

// MakeJobSet creates a wrapper for a JobSet.
func MakeJobSet(name, ns string) *JobSetWrapper {
	return &JobSetWrapper{
		jobset.JobSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
			Spec: jobset.JobSetSpec{
				ReplicatedJobs: []jobset.ReplicatedJob{},
				Network:        &jobset.Network{},
			},
		},
	}
}

// SuccessPolicy sets the value of jobSet.spec.successPolicy
func (j *JobSetWrapper) SuccessPolicy(policy *jobset.SuccessPolicy) *JobSetWrapper {
	j.Spec.SuccessPolicy = policy
	return j
}

// FailurePolicy sets the value of jobSet.spec.failurePolicy
func (j *JobSetWrapper) FailurePolicy(policy *jobset.FailurePolicy) *JobSetWrapper {
	j.Spec.FailurePolicy = policy
	return j
}

// SetAnnotations sets the value of the jobSet.metadata.annotations.
func (j *JobSetWrapper) SetAnnotations(annotations map[string]string) *JobSetWrapper {
	j.Annotations = annotations
	return j
}

// Subdomain sets the JobSet Network subdomain.
func (j *JobSetWrapper) Subdomain(subdomain string) *JobSetWrapper {
	j.Spec.Network.Subdomain = subdomain
	return j
}

// Obj returns the inner JobSet.
func (j *JobSetWrapper) Obj() *jobset.JobSet {
	return &j.JobSet
}

// ReplicatedJob adds a single ReplicatedJob to the JobSet.
func (j *JobSetWrapper) ReplicatedJob(job jobset.ReplicatedJob) *JobSetWrapper {
	j.JobSet.Spec.ReplicatedJobs = append(j.JobSet.Spec.ReplicatedJobs, job)
	return j
}

// Suspend adds a suspend flag to JobSet
func (j *JobSetWrapper) Suspend(suspend bool) *JobSetWrapper {
	j.JobSet.Spec.Suspend = pointer.Bool(suspend)
	return j
}

// NetworkSubdomain sets the value of JobSet.Network.Subdomain
func (j *JobSetWrapper) NetworkSubdomain(val string) *JobSetWrapper {
	j.JobSet.Spec.Network.Subdomain = val
	return j
}

// EnableDNSHostnames sets the value of ReplicatedJob.Network.EnableDNSHostnames.
func (j *JobSetWrapper) EnableDNSHostnames(val bool) *JobSetWrapper {
	j.JobSet.Spec.Network.EnableDNSHostnames = pointer.Bool(val)
	return j
}

// ReplicatedJobWrapper wraps a ReplicatedJob.
type ReplicatedJobWrapper struct {
	jobset.ReplicatedJob
}

// MakeReplicatedJob creates a wrapper for a ReplicatedJob.
func MakeReplicatedJob(name string) *ReplicatedJobWrapper {
	return &ReplicatedJobWrapper{
		jobset.ReplicatedJob{
			Name: name,
		},
	}
}

// Job sets the Job spec for the ReplicatedJob template.
func (r *ReplicatedJobWrapper) Job(jobSpec batchv1.JobTemplateSpec) *ReplicatedJobWrapper {
	r.Template = jobSpec
	return r
}

// Replicas sets the value of the ReplicatedJob.Replicas.
func (r *ReplicatedJobWrapper) Replicas(val int) *ReplicatedJobWrapper {
	r.ReplicatedJob.Replicas = val
	return r
}

// Subdomain sets the subdomain on the PodSpec
// We artificially do this because the webhook does not work in testing
func (r *ReplicatedJobWrapper) Subdomain(subdomain string) *ReplicatedJobWrapper {
	r.Template.Spec.Template.Spec.Subdomain = subdomain
	return r
}

// Obj returns the inner ReplicatedJob.
func (r *ReplicatedJobWrapper) Obj() jobset.ReplicatedJob {
	return r.ReplicatedJob
}

// JobTemplateWrapper wraps a JobTemplateSpec.
type JobTemplateWrapper struct {
	batchv1.JobTemplateSpec
}

// MakeJobTemplate creates a wrapper for a JobTemplateSpec.
func MakeJobTemplate(name, ns string) *JobTemplateWrapper {
	return &JobTemplateWrapper{
		batchv1.JobTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{},
				},
			},
		},
	}
}

// CompletionMode sets the value of job.spec.completionMode
func (j *JobTemplateWrapper) CompletionMode(mode batchv1.CompletionMode) *JobTemplateWrapper {
	j.Spec.CompletionMode = &mode
	return j
}

// Containers sets the pod template spec containers.
func (j *JobTemplateWrapper) PodSpec(podSpec corev1.PodSpec) *JobTemplateWrapper {
	j.Spec.Template.Spec = podSpec
	return j
}

// Obj returns the inner batchv1.JobTemplateSpec
func (j *JobTemplateWrapper) Obj() batchv1.JobTemplateSpec {
	return j.JobTemplateSpec
}

// JobWrapper wraps a Job.
type JobWrapper struct {
	batchv1.Job
}

// MakeJobWrapper creates a wrapper for a Job.
func MakeJob(jobName, ns string) *JobWrapper {
	return &JobWrapper{
		batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: ns,
			},
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{},
				},
			},
		},
	}
}

// Affinity sets the pod affinities/anti-affinities for the pod template spec.
func (j *JobWrapper) Affinity(affinity *corev1.Affinity) *JobWrapper {
	j.Spec.Template.Spec.Affinity = affinity
	return j
}

// JobLabels sets the Job labels.
func (j *JobWrapper) JobLabels(labels map[string]string) *JobWrapper {
	j.Labels = labels
	return j
}

// JobAnnotations sets the Job annotations.
func (j *JobWrapper) JobAnnotations(annotations map[string]string) *JobWrapper {
	j.Annotations = annotations
	return j
}

// PodLabels sets the pod template spec labels.
func (j *JobWrapper) PodLabels(labels map[string]string) *JobWrapper {
	j.Spec.Template.Labels = labels
	return j
}

// Suspend sets suspend in the job spec.
func (j *JobWrapper) Suspend(suspend bool) *JobWrapper {
	j.Spec.Suspend = pointer.Bool(suspend)
	return j
}

// PodAnnotations sets the pod template spec annotations.
func (j *JobWrapper) PodAnnotations(annotations map[string]string) *JobWrapper {
	j.Spec.Template.Annotations = annotations
	return j
}

// PodSpec sets the pod template spec.
func (j *JobWrapper) PodSpec(podSpec corev1.PodSpec) *JobWrapper {
	j.Spec.Template.Spec = podSpec
	return j
}

// Subdomain sets the pod template spec subdomain.
func (j *JobWrapper) Subdomain(subdomain string) *JobWrapper {
	j.Spec.Template.Spec.Subdomain = subdomain
	return j
}

// Parallelism sets the job spec parallelism.
func (j *JobWrapper) Parallelism(parallelism int32) *JobWrapper {
	j.Spec.Parallelism = pointer.Int32(parallelism)
	return j
}

// Completions sets the job spec completions.
func (j *JobWrapper) Completions(completions int32) *JobWrapper {
	j.Spec.Completions = pointer.Int32(completions)
	return j
}

// Succeeded sets the job status succeeded.
func (j *JobWrapper) Succeeded(succeeded int32) *JobWrapper {
	j.Status.Succeeded = succeeded
	return j
}

// Ready sets the job status ready.
func (j *JobWrapper) Ready(ready int32) *JobWrapper {
	j.Status.Ready = pointer.Int32(ready)
	return j
}

// Obj returns the wrapped Job.
func (j *JobWrapper) Obj() *batchv1.Job {
	return &j.Job
}
