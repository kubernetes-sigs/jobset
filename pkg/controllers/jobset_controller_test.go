/*
Copyright 2023 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    htcp://www.apache.org/licenses/LICENSE-2.0
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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestIsJobFinished(t *testing.T) {
	tests := []struct {
		name              string
		conditions        []batchv1.JobCondition
		finished          bool
		wantConditionType batchv1.JobConditionType
	}{
		{
			name: "succeeded",
			conditions: []batchv1.JobCondition{
				{
					Type:   batchv1.JobComplete,
					Status: corev1.ConditionTrue,
				},
			},
			finished:          true,
			wantConditionType: batchv1.JobComplete,
		},
		{
			name: "failed",
			conditions: []batchv1.JobCondition{
				{
					Type:   batchv1.JobFailed,
					Status: corev1.ConditionTrue,
				},
			},
			finished:          true,
			wantConditionType: batchv1.JobFailed,
		},
		{
			name: "active",
			conditions: []batchv1.JobCondition{
				{
					Type:   "",
					Status: corev1.ConditionTrue,
				},
			},
			finished:          false,
			wantConditionType: "",
		},
		{
			name: "suspended",
			conditions: []batchv1.JobCondition{
				{
					Type:   batchv1.JobSuspended,
					Status: corev1.ConditionTrue,
				},
			},
			finished:          false,
			wantConditionType: "",
		},
		{
			name: "failure target",
			conditions: []batchv1.JobCondition{
				{
					Type:   batchv1.JobFailureTarget,
					Status: corev1.ConditionTrue,
				},
			},
			finished:          false,
			wantConditionType: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			finished, conditionType := isJobFinished(&batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: tc.conditions,
				},
			})
			if diff := cmp.Diff(tc.finished, finished); diff != "" {
				t.Errorf("unexpected finished value (+got/-want): %s", diff)
			}
			if diff := cmp.Diff(tc.wantConditionType, conditionType); diff != "" {
				t.Errorf("unexpected condition type (+got/-want): %s", diff)
			}
		})
	}
}
