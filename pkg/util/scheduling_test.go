/*
Copyright The Kubernetes Authors.
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

package util

import (
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/utils/ptr"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// ReplicatedJobPodCount and TotalReplicatedJobPodCount assume admission
// validation has rejected pod counts that overflow int32 (see
// pkg/webhooks/jobset_webhook.go), so they only need to compute the in-range
// product and sum correctly.
func TestReplicatedJobPodCount(t *testing.T) {
	rjob := &jobset.ReplicatedJob{
		Replicas: 3,
		Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](4)}},
	}
	if got := ReplicatedJobPodCount(rjob); got != 12 {
		t.Fatalf("ReplicatedJobPodCount() = %d, want %d", got, 12)
	}
}

func TestTotalReplicatedJobPodCount(t *testing.T) {
	rjobs := []jobset.ReplicatedJob{
		{Replicas: 2, Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](3)}}},
		{Replicas: 4, Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](5)}}},
	}
	if got := TotalReplicatedJobPodCount(rjobs); got != 26 {
		t.Fatalf("TotalReplicatedJobPodCount() = %d, want %d", got, 26)
	}
}

func TestJobParallelism(t *testing.T) {
	tests := map[string]struct {
		rjob *jobset.ReplicatedJob
		want int32
	}{
		"explicit parallelism": {
			rjob: &jobset.ReplicatedJob{
				Replicas: 4,
				Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](3)}},
			},
			want: 3,
		},
		"nil parallelism defaults to 1": {
			rjob: &jobset.ReplicatedJob{
				Replicas: 4,
				Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{}},
			},
			want: 1,
		},
		"completions lower than parallelism caps the count": {
			rjob: &jobset.ReplicatedJob{
				Replicas: 1,
				Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{
					Parallelism: ptr.To[int32](4),
					Completions: ptr.To[int32](2),
				}},
			},
			want: 2,
		},
		"completions higher than parallelism does not raise the count": {
			rjob: &jobset.ReplicatedJob{
				Replicas: 1,
				Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{
					Parallelism: ptr.To[int32](2),
					Completions: ptr.To[int32](4),
				}},
			},
			want: 2,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := JobParallelism(tc.rjob); got != tc.want {
				t.Fatalf("JobParallelism() = %d, want %d", got, tc.want)
			}
		})
	}
}
