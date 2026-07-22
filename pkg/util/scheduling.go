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

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// ReplicatedJobPodCount returns the number of pods represented by a
// ReplicatedJob. Invalid values that exceed the int32 range are saturated at
// MaxInt32; admission validation rejects those values separately.
func ReplicatedJobPodCount(rjob *jobset.ReplicatedJob) int32 {
	parallelism := int64(1)
	if rjob.Template.Spec.Parallelism != nil {
		parallelism = int64(*rjob.Template.Spec.Parallelism)
	}
	count := parallelism * int64(rjob.Replicas)
	if count > math.MaxInt32 {
		return math.MaxInt32
	}
	if count < math.MinInt32 {
		return math.MinInt32
	}
	return int32(count)
}

// TotalReplicatedJobPodCount returns the total number of pods represented by
// the supplied ReplicatedJobs, saturating at the int32 range.
func TotalReplicatedJobPodCount(rjobs []jobset.ReplicatedJob) int32 {
	var total int64
	for i := range rjobs {
		total += int64(ReplicatedJobPodCount(&rjobs[i]))
		if total > math.MaxInt32 {
			return math.MaxInt32
		}
		if total < math.MinInt32 {
			return math.MinInt32
		}
	}
	return int32(total)
}
