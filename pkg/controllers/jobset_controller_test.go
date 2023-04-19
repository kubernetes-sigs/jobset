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
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	jobset "sigs.k8s.io/jobset/api/v1alpha1"
	testutils "sigs.k8s.io/jobset/pkg/util/testing"
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
		ns          = "default"
	)
	tests := []struct {
		name         string
		job          *batchv1.Job
		nsSelector   *metav1.LabelSelector
		wantAffinity corev1.Affinity
	}{
		{
			name:       "no existing affinities",
			job:        testutils.MakeJob(jobName, ns).Obj(),
			nsSelector: &metav1.LabelSelector{}, // all namespaces
			wantAffinity: corev1.Affinity{
				PodAffinity: &corev1.PodAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						{
							LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      jobset.JobNameKey,
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{jobName},
								},
							}},
							TopologyKey:       topologyKey,
							NamespaceSelector: &metav1.LabelSelector{},
						},
					},
				},
				PodAntiAffinity: &corev1.PodAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						{
							LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      jobset.JobNameKey,
									Operator: metav1.LabelSelectorOpExists,
								},
								{
									Key:      jobset.JobNameKey,
									Operator: metav1.LabelSelectorOpNotIn,
									Values:   []string{jobName},
								},
							}},
							TopologyKey:       topologyKey,
							NamespaceSelector: &metav1.LabelSelector{},
						},
					},
				},
			},
		},
		{
			name: "existing affinities should be appended to, not replaced",
			job: testutils.MakeJob(jobName, ns).Affinity(&corev1.Affinity{
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
			}).Obj(),
			nsSelector: nil,
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
									Key:      jobset.JobNameKey,
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
									Key:      jobset.JobNameKey,
									Operator: metav1.LabelSelectorOpExists,
								},
								{
									Key:      jobset.JobNameKey,
									Operator: metav1.LabelSelectorOpNotIn,
									Values:   []string{jobName},
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
			setExclusiveAffinities(tc.job, topologyKey, tc.nsSelector)
			// Check pod affinities.
			if diff := cmp.Diff(tc.wantAffinity.PodAffinity, tc.job.Spec.Template.Spec.Affinity.PodAffinity); diff != "" {
				t.Errorf("unexpected diff in pod affinity (-want/+got): %s", diff)
			}
			// Check pod anti-affinities.
			if diff := cmp.Diff(tc.wantAffinity.PodAntiAffinity, tc.job.Spec.Template.Spec.Affinity.PodAntiAffinity); diff != "" {
				t.Errorf("unexpected diff in pod anti-affinity (-want/+got): %s", diff)
			}

		})
	}
}

func TestConstructJobsFromTemplate(t *testing.T) {
	var (
		jobSetName        = "test-jobset"
		replicatedJobName = "replicated-job"
		jobName           = "test-job"
		ns                = "default"
		exclusive         = &jobset.Exclusive{
			TopologyKey: "test-topology-key",
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"key": "value",
				},
			},
		}
	)

	tests := []struct {
		name      string
		js        *jobset.JobSet
		ownedJobs *childJobs
		want      []*batchv1.Job
	}{
		{
			name: "no jobs created",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			ownedJobs: &childJobs{
				active: []*batchv1.Job{
					testutils.MakeJob("test-jobset-replicated-job-0", ns).Obj(),
				},
			},
		},
		{
			name: "all jobs created",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(2).
					Obj()).Obj(),
			ownedJobs: &childJobs{},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName,
					jobName:           "test-jobset-replicated-job-0",
					ns:                ns,
					replicas:          2,
					jobIdx:            0}).Obj(),
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName,
					jobName:           "test-jobset-replicated-job-1",
					ns:                ns,
					replicas:          2,
					jobIdx:            1}).Obj(),
			},
		},
		{
			name: "one job created, one job not created (already active)",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(2).
					Obj()).Obj(),
			ownedJobs: &childJobs{
				active: []*batchv1.Job{
					testutils.MakeJob("test-jobset-replicated-job-0", ns).Obj(),
				},
			},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName,
					jobName:           "test-jobset-replicated-job-1",
					ns:                ns,
					replicas:          2,
					jobIdx:            1}).Obj(),
			},
		},
		{
			name: "one job created, one job not created (already succeeded)",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(2).
					Obj()).Obj(),
			ownedJobs: &childJobs{
				successful: []*batchv1.Job{
					testutils.MakeJob("test-jobset-replicated-job-0", ns).Obj(),
				},
			},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName,
					jobName:           "test-jobset-replicated-job-1",
					ns:                ns,
					replicas:          2,
					jobIdx:            1}).Obj(),
			},
		},
		{
			name: "one job created, one job not created (already failed)",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(2).
					Obj()).Obj(),
			ownedJobs: &childJobs{
				failed: []*batchv1.Job{
					testutils.MakeJob("test-jobset-replicated-job-0", ns).Obj(),
				},
			},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName,
					jobName:           "test-jobset-replicated-job-1",
					ns:                ns,
					replicas:          2,
					jobIdx:            1}).Obj(),
			},
		},
		{
			name: "one job created, one job not created (marked for deletion)",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(2).
					Obj()).Obj(),
			ownedJobs: &childJobs{
				delete: []*batchv1.Job{
					testutils.MakeJob("test-jobset-replicated-job-0", ns).Obj(),
				},
			},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName,
					jobName:           "test-jobset-replicated-job-1",
					ns:                ns,
					replicas:          2,
					jobIdx:            1}).Obj(),
			},
		},
		{
			name: "multiple replicated jobs",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-A").
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-B").
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(2).
					Obj()).
				Obj(),
			ownedJobs: &childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-B",
						jobName:           "test-jobset-replicated-job-B-0",
						ns:                ns,
						replicas:          2,
						jobIdx:            0}).Obj(),
				},
			},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: "replicated-job-A",
					jobName:           "test-jobset-replicated-job-A-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0}).Obj(),
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: "replicated-job-B",
					jobName:           "test-jobset-replicated-job-B-1",
					ns:                ns,
					replicas:          2,
					jobIdx:            1}).Obj(),
			},
		},
		{
			name: "exclusive affinities",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Exclusive(exclusive).
					Obj()).
				Obj(),
			ownedJobs: &childJobs{},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName,
					jobName:           "test-jobset-replicated-job-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0}).Affinity(&corev1.Affinity{
					PodAffinity: &corev1.PodAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
							{
								LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      jobset.JobNameKey,
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"test-jobset-replicated-job-0"},
									},
								}},
								TopologyKey:       exclusive.TopologyKey,
								NamespaceSelector: exclusive.NamespaceSelector,
							},
						},
					},
					PodAntiAffinity: &corev1.PodAntiAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
							{
								LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      jobset.JobNameKey,
										Operator: metav1.LabelSelectorOpExists,
									},
									{
										Key:      jobset.JobNameKey,
										Operator: metav1.LabelSelectorOpNotIn,
										Values:   []string{"test-jobset-replicated-job-0"},
									},
								}},
								TopologyKey:       exclusive.TopologyKey,
								NamespaceSelector: exclusive.NamespaceSelector,
							},
						},
					},
				}).Obj(),
			},
		},
		{
			name: "pod dns hostnames enabled",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					EnableDNSHostnames(true).
					Obj()).
				Obj(),
			ownedJobs: &childJobs{},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName,
					jobName:           "test-jobset-replicated-job-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0}).Subdomain("test-jobset-replicated-job").Obj(),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []*batchv1.Job
			for _, rjob := range tc.js.Spec.Jobs {
				jobs, err := constructJobsFromTemplate(tc.js, &rjob, tc.ownedJobs)
				if err != nil {
					t.Errorf("constructJobsFromTemplate() error = %v", err)
					return
				}
				got = append(got, jobs...)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("constructJobsFromTemplate() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

type makeJobArgs struct {
	jobSetName        string
	replicatedJobName string
	jobName           string
	ns                string
	replicas          int
	jobIdx            int
	restarts          int
}

func makeJob(args *makeJobArgs) *testutils.JobWrapper {
	jobWrapper := testutils.MakeJob(args.jobName, args.ns).
		JobLabels(map[string]string{
			jobset.JobSetKey:        args.jobSetName,
			jobset.ReplicatedJobKey: args.replicatedJobName,
			jobset.ReplicasKey:      strconv.Itoa(args.replicas),
			jobset.JobIndexKey:      strconv.Itoa(args.jobIdx),
			jobset.RestartsKey:      strconv.Itoa(args.restarts),
		}).
		JobAnnotations(map[string]string{
			jobset.ReplicasKey: strconv.Itoa(args.replicas),
			jobset.JobIndexKey: strconv.Itoa(args.jobIdx),
		}).
		PodLabels(map[string]string{
			jobset.JobSetKey:        args.jobSetName,
			jobset.ReplicatedJobKey: args.replicatedJobName,
			jobset.ReplicasKey:      strconv.Itoa(args.replicas),
			jobset.JobIndexKey:      strconv.Itoa(args.jobIdx),
			jobset.RestartsKey:      strconv.Itoa(args.restarts),
		}).
		PodAnnotations(map[string]string{
			jobset.ReplicasKey: strconv.Itoa(args.replicas),
			jobset.JobIndexKey: strconv.Itoa(args.jobIdx),
		})
	return jobWrapper
}
