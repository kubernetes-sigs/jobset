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
	"math"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/utils/ptr"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

func TestReplicatedJobPodCountSaturatesOverflow(t *testing.T) {
	rjob := &jobset.ReplicatedJob{
		Replicas: 2,
		Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](math.MaxInt32)}},
	}
	if got := ReplicatedJobPodCount(rjob); got != math.MaxInt32 {
		t.Fatalf("ReplicatedJobPodCount() = %d, want %d", got, math.MaxInt32)
	}
}

func TestTotalReplicatedJobPodCountSaturatesOverflow(t *testing.T) {
	rjobs := []jobset.ReplicatedJob{
		{Replicas: 1, Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](math.MaxInt32)}}},
		{Replicas: 1, Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](math.MaxInt32)}}},
	}
	if got := TotalReplicatedJobPodCount(rjobs); got != math.MaxInt32 {
		t.Fatalf("TotalReplicatedJobPodCount() = %d, want %d", got, math.MaxInt32)
	}
}
