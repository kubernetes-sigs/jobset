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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestSetExclusiveAffinities(t *testing.T) {
	var (
		topologyKey = "test-topology-key"
		jobName     = "test-job"
	)
	tests := []struct {
		name         string
		job          *batchv1.Job
		wantAffinity corev1.Affinity
	}{
		{
			name: "no existing affinities",
			job:  testJob(jobName),
			wantAffinity: corev1.Affinity{
				PodAffinity: &corev1.PodAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						{
							LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "job-name",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{jobName},
								},
							}},
							TopologyKey: topologyKey,
						},
					},
				},
				PodAntiAffinity: &corev1.PodAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						{
							LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "job-name",
									Operator: metav1.LabelSelectorOpNotIn,
									Values:   []string{jobName, ""},
								},
							}},
							TopologyKey: topologyKey,
						},
					},
				},
			},
		},
		{
			name: "existing affinities should be appended to, not replaced",
			job:  testJobWithAffinities(jobName),
			wantAffinity: corev1.Affinity{
				PodAffinity: &corev1.PodAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						{
							LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "label-foo",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"value-foo"},
								},
							}},
							TopologyKey: "topology.kubernetes.io/zone",
						},
						{
							LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "job-name",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{jobName},
								},
							}},
							TopologyKey: topologyKey,
						},
					},
				},
				PodAntiAffinity: &corev1.PodAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						{
							LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "label-bar",
									Operator: metav1.LabelSelectorOpNotIn,
									Values:   []string{"value-bar"},
								},
							}},
							TopologyKey: "topology.kubernetes.io/zone",
						},
						{
							LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "job-name",
									Operator: metav1.LabelSelectorOpNotIn,
									Values:   []string{jobName, ""},
								},
							}},
							TopologyKey: topologyKey,
						},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setExclusiveAffinities(tc.job, topologyKey)
			if diff := cmp.Diff(tc.wantAffinity.PodAffinity, tc.job.Spec.Template.Spec.Affinity.PodAffinity); diff != "" {
				t.Errorf("unexpected diff in pod affinity (-want/+got): %s", diff)
			}
			if diff := cmp.Diff(tc.wantAffinity.PodAntiAffinity, tc.job.Spec.Template.Spec.Affinity.PodAntiAffinity); diff != "" {
				t.Errorf("unexpected diff in pod anti-affinity (-want/+got): %s", diff)
			}
		})
	}
}

func testJob(jobName string) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: jobName,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{},
			},
		},
	}
}

func testJobWithAffinities(jobName string) *batchv1.Job {
	job := testJob(jobName)
	job.Spec.Template.Spec.Affinity = &corev1.Affinity{
		PodAffinity: &corev1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "label-foo",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"value-foo"},
						},
					}},
					TopologyKey: "topology.kubernetes.io/zone",
				},
			},
		},
		PodAntiAffinity: &corev1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "label-bar",
							Operator: metav1.LabelSelectorOpNotIn,
							Values:   []string{"value-bar"},
						},
					}},
					TopologyKey: "topology.kubernetes.io/zone",
				},
			},
		},
	}
	return job
}
