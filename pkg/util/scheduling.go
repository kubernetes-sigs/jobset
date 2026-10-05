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
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// ReplicatedJobPodCount returns the number of pods represented by a
// ReplicatedJob (parallelism * replicas). Admission validation rejects a
// ReplicatedJob whose product exceeds the int32 range, so the result always
// fits in int32 for an admitted JobSet. The multiplication is performed in
// int64 so the intermediate product cannot overflow before the final cast.
func ReplicatedJobPodCount(rjob *jobset.ReplicatedJob) int32 {
	return int32(int64(JobParallelism(rjob)) * int64(rjob.Replicas))
}

// JobParallelism returns the number of pods represented by a single Job
// (i.e. a single replica) of a ReplicatedJob, defaulting to 1 when
// parallelism is unset. This is the per-Job pod count used by the
// Gang-of-Gangs per-Job scheduling model, as opposed to ReplicatedJobPodCount
// which sums pods across every replica.
//
// The Job controller runs at most min(parallelism, completions) pods at once,
// so the count is capped at completions when it is lower. Otherwise a gang
// minCount derived from parallelism alone (e.g. parallelism: 4, completions: 2)
// could never be met and the pods would stay Pending forever.
func JobParallelism(rjob *jobset.ReplicatedJob) int32 {
	count := int32(1)
	if rjob.Template.Spec.Parallelism != nil {
		count = *rjob.Template.Spec.Parallelism
	}
	if c := rjob.Template.Spec.Completions; c != nil && *c < count {
		count = *c
	}
	return count
}

// TotalReplicatedJobPodCount returns the total number of pods represented by
// the supplied ReplicatedJobs. This feeds the top-level gang PodGroup minCount,
// and admission validation rejects a top-level-gang JobSet whose total exceeds
// the int32 range, so the result fits in int32 for an admitted JobSet. The sum
// is accumulated in int64 so it cannot overflow before the final cast.
func TotalReplicatedJobPodCount(rjobs []jobset.ReplicatedJob) int32 {
	var total int64
	for i := range rjobs {
		total += int64(ReplicatedJobPodCount(&rjobs[i]))
	}
	return int32(total)
}
