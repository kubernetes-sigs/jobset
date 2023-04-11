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
	"fmt"

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

// Obj returns the inner JobSet.
func (j *JobSetWrapper) Obj() *jobset.JobSet {
	return &j.JobSet
}

// AddReplicatedJob adds a single ReplicatedJob to the JobSet.
func (j *JobSetWrapper) AddReplicatedJob(job jobset.ReplicatedJob) *JobSetWrapper {
	j.JobSet.Spec.Jobs = append(j.JobSet.Spec.Jobs, job)
	return j
}

// AddReplicatedJobs adds `count` number of copies of a ReplicatedJob to the JobSet.
// Suffixes are added to the names to distinguish them.
// Used for easily adding multiple similar ReplicatedJobs to a JobSet for testing.
func (j *JobSetWrapper) AddReplicatedJobs(job jobset.ReplicatedJob, count int) *JobSetWrapper {
	for i := 0; i < count; i++ {
		// Add suffixes to distinguish jobs.
		job.Name += fmt.Sprintf("-%d", i)
		job.Template.Name += fmt.Sprintf("-%d", i)
		j.JobSet.Spec.Jobs = append(j.JobSet.Spec.Jobs, job)
	}
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

// SetJob sets the Job spec for the ReplicatedJob template.
func (r *ReplicatedJobWrapper) SetJob(jobSpec batchv1.JobTemplateSpec) *ReplicatedJobWrapper {
	r.Template = jobSpec
	return r
}

// SetEnableDNSHostnames sets the value of ReplicatedJob.Network.EnableDNSHostnames.
func (r *ReplicatedJobWrapper) SetEnableDNSHostnames(val bool) *ReplicatedJobWrapper {
	r.ReplicatedJob.Network.EnableDNSHostnames = pointer.Bool(val)
	return r
}

// SetReplicas sets the value of the ReplicatedJob.Replicas.
func (r *ReplicatedJobWrapper) SetReplicas(val int) *ReplicatedJobWrapper {
	r.ReplicatedJob.Replicas = pointer.Int(val)
	return r
}

// Obj returns the inner ReplicatedJob.
func (r *ReplicatedJobWrapper) Obj() jobset.ReplicatedJob {
	return r.ReplicatedJob
}

// JobWrapper wraps a Job.
type JobWrapper struct {
	batchv1.JobTemplateSpec
}

// MakeJob creates a wrapper for a Job.
func MakeJob(name, ns string) *JobWrapper {
	return &JobWrapper{
		batchv1.JobTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
			Spec: batchv1.JobSpec{},
		},
	}
}

// SetCompletionMode sets the value of job.spec.completionMode
func (j *JobWrapper) SetCompletionMode(mode batchv1.CompletionMode) *JobWrapper {
	j.Spec.CompletionMode = &mode
	return j
}

// SetCompletions sets the value of job.spec.completions
func (j *JobWrapper) SetCompletions(completions int) *JobWrapper {
	j.Spec.Completions = pointer.Int32(int32(completions))
	return j
}

// SetParallelism sets the value of job.spec.parallelism
func (j *JobWrapper) SetParallelism(parallelism int) *JobWrapper {
	j.Spec.Parallelism = pointer.Int32(int32(parallelism))
	return j
}

// SetTEmplate sets the value of the pod template spec (job.spec.template)
func (j *JobWrapper) SetTemplate(template corev1.PodTemplateSpec) *JobWrapper {
	j.Spec.Template = template
	return j
}

// Obj returns the inner batchv1.JobTemplateSpec
func (j *JobWrapper) Obj() batchv1.JobTemplateSpec {
	return j.JobTemplateSpec
}
