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

package controllers

import (
	"testing"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// member builds a *ReplicatedJob with the given pod count (replicas*parallelism)
// and optional DependsOn, reusing the package rjob(name, replicas, parallelism)
// helper.
func member(name string, replicas, parallelism int32, dependsOn ...jobset.DependsOn) *jobset.ReplicatedJob {
	rj := rjob(name, replicas, parallelism)
	rj.DependsOn = dependsOn
	return &rj
}

func TestSharedGroupMinCount(t *testing.T) {
	ready := func(name string) jobset.DependsOn {
		return jobset.DependsOn{Name: name, Status: jobset.DependencyReady}
	}
	cases := map[string]struct {
		members []*jobset.ReplicatedJob
		want    int32
	}{
		"no dependsOn sums all members": {
			members: []*jobset.ReplicatedJob{member("a", 2, 2), member("b", 1, 3)},
			want:    7, // 2*2 + 1*3
		},
		"MPI launcher depends on node excludes launcher": {
			// launcher (1 pod) dependsOn node (49 pods) -> minCount 49.
			members: []*jobset.ReplicatedJob{
				member("launcher", 1, 1, ready("node")),
				member("node", 1, 49),
			},
			want: 49,
		},
		"dependsOn outside the group is not excluded": {
			members: []*jobset.ReplicatedJob{
				member("a", 1, 2),
				member("b", 1, 3, ready("other")),
			},
			want: 5, // 2 + 3
		},
		"chain excludes only the dependent member": {
			members: []*jobset.ReplicatedJob{
				member("node", 1, 10),
				member("worker", 1, 4, ready("node")),
			},
			want: 10,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := sharedGroupMinCount(tc.members); got != tc.want {
				t.Fatalf("sharedGroupMinCount = %d, want %d", got, tc.want)
			}
		})
	}
}
