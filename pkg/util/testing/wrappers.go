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

	jobset "sigs.k8s.io/jobset/api/v1alpha1"
)

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
				Jobs: []jobset.ReplicatedJob{},
			},
		},
	}
}

// FailurePolicy sets the value of jobSet.spec.failurePolicy
func (j *JobSetWrapper) FailurePolicy(policy *jobset.FailurePolicy) *JobSetWrapper {
	j.Spec.FailurePolicy = policy
	return j
}

// Obj returns the inner JobSet.
func (j *JobSetWrapper) Obj() *jobset.JobSet {
	return &j.JobSet
}

// ReplicatedJob adds a single ReplicatedJob to the JobSet.
func (j *JobSetWrapper) ReplicatedJob(job jobset.ReplicatedJob) *JobSetWrapper {
	j.JobSet.Spec.Jobs = append(j.JobSet.Spec.Jobs, job)
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
			Name:    name,
			Network: &jobset.Network{},
		},
	}
}

// Job sets the Job spec for the ReplicatedJob template.
func (r *ReplicatedJobWrapper) Job(jobSpec batchv1.JobTemplateSpec) *ReplicatedJobWrapper {
	r.Template = jobSpec
	return r
}

// EnableDNSHostnames sets the value of ReplicatedJob.Network.EnableDNSHostnames.
func (r *ReplicatedJobWrapper) EnableDNSHostnames(val bool) *ReplicatedJobWrapper {
	r.ReplicatedJob.Network.EnableDNSHostnames = pointer.Bool(val)
	return r
}

// Replicas sets the value of the ReplicatedJob.Replicas.
func (r *ReplicatedJobWrapper) Replicas(val int) *ReplicatedJobWrapper {
	r.ReplicatedJob.Replicas = val
	return r
}

// Exclusive sets the value of ReplicatedJob.Exclusive.
func (r *ReplicatedJobWrapper) Exclusive(e *jobset.Exclusive) *ReplicatedJobWrapper {
	r.ReplicatedJob.Exclusive = e
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

// PodAnnotations sets the pod template spec annotations.
func (j *JobWrapper) PodAnnotations(annotations map[string]string) *JobWrapper {
	j.Spec.Template.Annotations = annotations
	return j
}

// Subdomain sets the pod template spec subdomain.
func (j *JobWrapper) Subdomain(subdomain string) *JobWrapper {
	j.Spec.Template.Spec.Subdomain = subdomain
	return j
}

// Obj returns the wrapped Job.
func (j *JobWrapper) Obj() *batchv1.Job {
	return &j.Job
}
