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
	"testing"

	"github.com/google/go-cmp/cmp"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

func TestInOrderStartupPolicy(t *testing.T) {
	tests := []struct {
		name          string
		startupPolicy *jobset.StartupPolicy
		expected      bool
	}{
		{
			name:          "startup policy AnyOrder",
			startupPolicy: &jobset.StartupPolicy{StartupPolicyOrder: jobset.AnyOrder},
			expected:      false,
		},
		{
			name:          "startup policy InOrder",
			startupPolicy: &jobset.StartupPolicy{StartupPolicyOrder: jobset.InOrder},
			expected:      true,
		},
		{
			name:          "nil startup policy",
			startupPolicy: nil,
			expected:      false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := inOrderStartupPolicy(tc.startupPolicy)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("unexpected finished value (+got/-want): %s", diff)
			}
		})
	}
}

func TestReplicatedJobStarted(t *testing.T) {
	tests := []struct {
		name                string
		replicas            int32
		replicatedJobStatus *jobset.ReplicatedJobStatus
		expected            bool
	}{
		{
			name:                "replicas 1; no replicatedJobStatus",
			replicas:            1,
			replicatedJobStatus: &jobset.ReplicatedJobStatus{},
			expected:            false,
		},
		{
			name:     "replicas 4; replicatedJobStatus all ready",
			replicas: 4,
			replicatedJobStatus: &jobset.ReplicatedJobStatus{
				Name:      "test",
				Ready:     4,
				Succeeded: 0,
				Failed:    0,
				Suspended: 0,
				Active:    0,
			},
			expected: true,
		},
		{
			name:     "replicas 4; replicatedJobStatus mix of ready, failed and succeded",
			replicas: 4,
			replicatedJobStatus: &jobset.ReplicatedJobStatus{
				Name:      "test",
				Ready:     2,
				Succeeded: 1,
				Failed:    1,
				Suspended: 0,
				Active:    0,
			},
			expected: true,
		},
		{
			name:     "replicas 4; replicatedJobStatus all active",
			replicas: 4,
			replicatedJobStatus: &jobset.ReplicatedJobStatus{
				Name:      "test",
				Ready:     0,
				Succeeded: 0,
				Failed:    0,
				Suspended: 0,
				Active:    4,
			},
			expected: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := allReplicasStarted(tc.replicas, tc.replicatedJobStatus)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("unexpected finished value (+got/-want): %s", diff)
			}
		})
	}
}
