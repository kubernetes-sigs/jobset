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
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	jobsetv1alpha1 "sigs.k8s.io/jobset/api/v1alpha1"
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
			finished, conditionType := IsJobFinished(&batchv1.Job{
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

func TestJobIndex(t *testing.T) {
	jobSet := &jobsetv1alpha1.JobSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-jobset",
		},
		Spec: jobsetv1alpha1.JobSetSpec{
			Jobs: []jobsetv1alpha1.JobTemplate{
				{
					Name: "template-1",
					Template: &batchv1.JobTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Name: "job-1",
						},
					},
				},
				{
					Name: "template-2",
					Template: &batchv1.JobTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Name: "job-2",
						},
					},
				},
				{
					Name: "template-3",
					Template: &batchv1.JobTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Name: "job-3",
						},
					},
				},
			},
		},
	}
	tests := []struct {
		name      string
		jobName   string
		wantIndex int
		wantErr   string
	}{
		{
			name:      "first",
			jobName:   "job-1",
			wantIndex: 0,
			wantErr:   "",
		},
		{
			name:      "middle",
			jobName:   "job-2",
			wantIndex: 1,
			wantErr:   "",
		},
		{
			name:      "last",
			jobName:   "job-3",
			wantIndex: 2,
			wantErr:   "",
		},
		{
			name:      "not found",
			jobName:   "job-4",
			wantIndex: -1,
			wantErr:   "JobSet test-jobset does not contain Job job-4",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: tc.jobName,
				},
			}

			gotIndex, err := jobIndex(jobSet, job)

			// Handle unexpected errors.
			if err != nil {
				if tc.wantErr == "" {
					t.Errorf("expected error, got nil")
				} else if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("want error: %v, got: %v", tc.wantErr, err)
				}
				return
			}

			// Handle missing errors we were expecting.
			if tc.wantErr != "" {
				t.Errorf("want error: %s, got nil", tc.wantErr)
			}
			if gotIndex != tc.wantIndex {
				t.Errorf("want index: %d, got: %d", tc.wantIndex, gotIndex)
			}
		})
	}
}
