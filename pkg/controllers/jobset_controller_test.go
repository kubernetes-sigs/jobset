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
	"context"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
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
			finished, conditionType := JobFinished(&batchv1.Job{
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

func TestConstructJobsFromTemplate(t *testing.T) {
	var (
		jobSetName        = "test-jobset"
		replicatedJobName = "replicated-job"
		jobName           = "test-job"
		ns                = "default"
		topologyDomain    = "test-topology-domain"
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
					jobIdx:            0}).
					Suspend(false).Obj(),
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName,
					jobName:           "test-jobset-replicated-job-1",
					ns:                ns,
					replicas:          2,
					jobIdx:            1}).
					Suspend(false).Obj(),
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
					jobIdx:            1}).
					Suspend(false).Obj(),
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
					jobIdx:            1}).
					Suspend(false).Obj(),
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
					jobIdx:            1}).
					Suspend(false).Obj(),
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
					jobIdx:            1}).
					Suspend(false).Obj(),
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
						jobIdx:            0}).
						Suspend(false).Obj(),
				},
			},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: "replicated-job-A",
					jobName:           "test-jobset-replicated-job-A-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0}).
					Suspend(false).Obj(),
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: "replicated-job-B",
					jobName:           "test-jobset-replicated-job-B-1",
					ns:                ns,
					replicas:          2,
					jobIdx:            1}).
					Suspend(false).Obj(),
			},
		},
		{
			name: "exclusive placement for a ReplicatedJob",
			js: testutils.MakeJobSet(jobSetName, ns).
				// Replicated Job A has exclusive placement annotation.
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName + "-A").
					Job(testutils.MakeJobTemplate(jobName, ns).
						SetAnnotations(map[string]string{jobset.ExclusiveKey: topologyDomain}).
						Obj()).
					Replicas(1).
					Obj()).
				// Replicated Job B has no exclusive placement annotation.
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName + "-B").
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).
				Obj(),
			ownedJobs: &childJobs{},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName + "-A",
					jobName:           "test-jobset-replicated-job-A-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0,
					topology:          topologyDomain}).
					Suspend(false).Obj(),
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName + "-B",
					jobName:           "test-jobset-replicated-job-B-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0}).
					Suspend(false).Obj(),
			},
		},
		{
			name: "exclusive placement using nodeSelectorStrategy for a ReplicatedJob",
			js: testutils.MakeJobSet(jobSetName, ns).
				// Replicated Job A has exclusive placement annotation.
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName + "-A").
					Job(testutils.MakeJobTemplate(jobName, ns).
						SetAnnotations(map[string]string{
							jobset.ExclusiveKey:            topologyDomain,
							jobset.NodeSelectorStrategyKey: "true"}).
						Obj()).
					Replicas(1).
					Obj()).
				// Replicated Job B has no exclusive placement annotation.
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName + "-B").
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).
				Obj(),
			ownedJobs: &childJobs{},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:           jobSetName,
					replicatedJobName:    replicatedJobName + "-A",
					jobName:              "test-jobset-replicated-job-A-0",
					ns:                   ns,
					replicas:             1,
					jobIdx:               0,
					topology:             topologyDomain,
					nodeSelectorStrategy: true}).
					Suspend(false).
					NodeSelector(map[string]string{
						jobset.NamespacedJobKey: namespacedJobName(ns, "test-jobset-replicated-job-A-0"),
					}).
					Tolerations([]corev1.Toleration{
						{
							Key:      jobset.NoScheduleTaintKey,
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					}).
					Obj(),
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName + "-B",
					jobName:           "test-jobset-replicated-job-B-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0}).
					Suspend(false).Obj(),
			},
		},
		{
			name: "exclusive placement for entire JobSet",
			js: testutils.MakeJobSet(jobSetName, ns).
				SetAnnotations(map[string]string{jobset.ExclusiveKey: topologyDomain}).
				// Replicated Job A has.
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName + "-A").
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).
				// Replicated Job B.
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName + "-B").
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).
				Obj(),
			ownedJobs: &childJobs{},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName + "-A",
					jobName:           "test-jobset-replicated-job-A-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0,
					topology:          topologyDomain}).
					Suspend(false).Obj(),
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName + "-B",
					jobName:           "test-jobset-replicated-job-B-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0,
					topology:          topologyDomain}).
					Suspend(false).Obj(),
			},
		},
		{
			name: "exclusive placement using nodeSelectorStrategy for entire JobSet",
			js: testutils.MakeJobSet(jobSetName, ns).
				SetAnnotations(map[string]string{
					jobset.ExclusiveKey:            topologyDomain,
					jobset.NodeSelectorStrategyKey: "true",
				}).
				// Replicated Job A has.
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName + "-A").
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).
				// Replicated Job B.
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName + "-B").
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).
				Obj(),
			ownedJobs: &childJobs{},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:           jobSetName,
					replicatedJobName:    replicatedJobName + "-A",
					jobName:              "test-jobset-replicated-job-A-0",
					ns:                   ns,
					replicas:             1,
					jobIdx:               0,
					topology:             topologyDomain,
					nodeSelectorStrategy: true}).
					Suspend(false).
					NodeSelector(map[string]string{
						jobset.NamespacedJobKey: namespacedJobName(ns, "test-jobset-replicated-job-A-0"),
					}).
					Tolerations([]corev1.Toleration{
						{
							Key:      jobset.NoScheduleTaintKey,
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					}).
					Obj(),
				makeJob(&makeJobArgs{
					jobSetName:           jobSetName,
					replicatedJobName:    replicatedJobName + "-B",
					jobName:              "test-jobset-replicated-job-B-0",
					ns:                   ns,
					replicas:             1,
					jobIdx:               0,
					topology:             topologyDomain,
					nodeSelectorStrategy: true}).
					Suspend(false).
					NodeSelector(map[string]string{
						jobset.NamespacedJobKey: namespacedJobName(ns, "test-jobset-replicated-job-B-0"),
					}).
					Tolerations([]corev1.Toleration{
						{
							Key:      jobset.NoScheduleTaintKey,
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					}).
					Obj(),
			},
		},
		{
			name: "pod dns hostnames enabled",
			js: testutils.MakeJobSet(jobSetName, ns).
				EnableDNSHostnames(true).
				NetworkSubdomain(jobSetName).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Subdomain(jobSetName).
					Replicas(1).
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
					jobIdx:            0}).
					Suspend(false).
					Subdomain(jobSetName).Obj(),
			},
		},
		{
			name: "suspend job set",
			js: testutils.MakeJobSet(jobSetName, ns).
				Suspend(true).
				EnableDNSHostnames(true).
				NetworkSubdomain(jobSetName).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Subdomain(jobSetName).
					Replicas(1).
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
					jobIdx:            0}).
					Suspend(true).
					Subdomain(jobSetName).Obj(),
			},
		},
		{
			name: "resume job set",
			js: testutils.MakeJobSet(jobSetName, ns).
				Suspend(false).
				EnableDNSHostnames(true).
				NetworkSubdomain(jobSetName).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Subdomain(jobSetName).
					Replicas(1).
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
					jobIdx:            0}).
					Suspend(false).
					Subdomain(jobSetName).Obj(),
			},
		},
		{
			name: "node selector exclusive placement strategy enabled",
			js: testutils.MakeJobSet(jobSetName, ns).
				EnableDNSHostnames(true).
				NetworkSubdomain(jobSetName).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).
						SetAnnotations(map[string]string{
							jobset.ExclusiveKey:            topologyDomain,
							jobset.NodeSelectorStrategyKey: "true",
						}).
						Obj()).
					Subdomain(jobSetName).
					Replicas(1).
					Obj()).
				Obj(),
			ownedJobs: &childJobs{},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:           jobSetName,
					replicatedJobName:    replicatedJobName,
					jobName:              "test-jobset-replicated-job-0",
					ns:                   ns,
					replicas:             1,
					jobIdx:               0,
					topology:             topologyDomain,
					nodeSelectorStrategy: true}).
					Suspend(false).
					Subdomain(jobSetName).
					NodeSelector(map[string]string{
						jobset.NamespacedJobKey: namespacedJobName(ns, "test-jobset-replicated-job-0"),
					}).
					Tolerations([]corev1.Toleration{
						{
							Key:      jobset.NoScheduleTaintKey,
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					}).Obj(),
			},
		},
		{
			name: "startup-policy",
			js: testutils.MakeJobSet(jobSetName, ns).
				StartupPolicy(&jobset.StartupPolicy{
					StartupPolicyOrder: jobset.InOrder,
				}).
				EnableDNSHostnames(true).
				NetworkSubdomain(jobSetName).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Subdomain(jobSetName).
					Replicas(1).
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
					jobIdx:            0}).
					Suspend(false).
					Subdomain(jobSetName).Obj(),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []*batchv1.Job
			for _, rjob := range tc.js.Spec.ReplicatedJobs {
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

func TestUpdateConditions(t *testing.T) {
	var (
		jobSetName        = "test-jobset"
		replicatedJobName = "replicated-job"
		jobName           = "test-job"
		ns                = "default"
	)

	tests := []struct {
		name           string
		js             *jobset.JobSet
		conditions     []metav1.Condition
		newCondition   metav1.Condition
		forceUpdate    bool
		expectedUpdate bool
	}{
		{
			name: "no condition",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			newCondition:   metav1.Condition{},
			conditions:     []metav1.Condition{},
			expectedUpdate: false,
		},
		{
			name: "do not update if false",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			newCondition:   metav1.Condition{Status: metav1.ConditionFalse, Type: string(jobset.JobSetSuspended), Reason: "JobsResumed"},
			conditions:     []metav1.Condition{},
			expectedUpdate: false,
		},
		{
			name: "force update if false",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			newCondition:   metav1.Condition{Status: metav1.ConditionFalse, Type: string(jobset.JobSetStartupPolicyCompleted), Reason: "StartupPolicy"},
			conditions:     []metav1.Condition{},
			expectedUpdate: true,
			forceUpdate:    true,
		},
		{
			name: "update if condition is true",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			newCondition:   metav1.Condition{Status: metav1.ConditionTrue, Type: string(jobset.JobSetSuspended), Reason: "JobsResumed"},
			conditions:     []metav1.Condition{},
			expectedUpdate: true,
		},

		{
			name: "suspended",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			newCondition:   metav1.Condition{Status: metav1.ConditionTrue, Type: string(jobset.JobSetSuspended), Reason: "JobsSuspended"},
			conditions:     []metav1.Condition{},
			expectedUpdate: true,
		},
		{
			name: "resumed",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			newCondition:   metav1.Condition{Type: string(jobset.JobSetSuspended), Reason: "JobsResumed", Status: metav1.ConditionStatus(corev1.ConditionFalse)},
			conditions:     []metav1.Condition{{Type: string(jobset.JobSetSuspended), Reason: "JobsSuspended", Status: metav1.ConditionStatus(corev1.ConditionTrue)}},
			expectedUpdate: true,
		},
		{
			name: "duplicateComplete",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			newCondition:   metav1.Condition{Type: string(jobset.JobSetCompleted), Message: "Jobs completed", Reason: "JobsCompleted", Status: metav1.ConditionTrue},
			conditions:     []metav1.Condition{{Type: string(jobset.JobSetCompleted), Message: "Jobs completed", Reason: "JobsCompleted", Status: metav1.ConditionTrue}},
			expectedUpdate: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			jsWithConditions := tc.js
			jsWithConditions.Status.Conditions = tc.conditions
			gotUpdate := updateCondition(jsWithConditions, tc.newCondition, tc.forceUpdate)
			if gotUpdate != tc.expectedUpdate {
				t.Errorf("updateCondition return mismatch")
			}
		})
	}
}

func TestCalculateReplicatedJobStatuses(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)
	tests := []struct {
		name     string
		js       *jobset.JobSet
		jobs     childJobs
		expected []jobset.ReplicatedJobStatus
	}{
		{
			name: "partial jobs are ready, no succeeded jobs",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-2").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(3).
					Obj()).Obj(),
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-0",
						ns:                ns,
						replicas:          1,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).
						Ready(1).
						Succeeded(1).Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-2",
						jobName:           "test-jobset-replicated-job-2-test-job-0",
						ns:                ns,
						replicas:          3,
						jobIdx:            0}).
						Parallelism(5).
						Ready(2).
						Succeeded(3).Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-2",
						jobName:           "test-jobset-replicated-job-2-test-job-1",
						ns:                ns,
						replicas:          3,
						jobIdx:            0}).
						Parallelism(3).
						Completions(2).
						Ready(1).
						Succeeded(1).Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-2",
						jobName:           "test-jobset-replicated-job-2-test-job-2",
						ns:                ns,
						replicas:          3,
						jobIdx:            0}).
						Parallelism(2).
						Completions(3).
						Ready(2).
						Succeeded(1).Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-2",
						jobName:           "test-jobset-replicated-job-2-test-job-3",
						ns:                ns,
						replicas:          3,
						jobIdx:            0}).
						Parallelism(4).
						Completions(5).
						Ready(2).
						Succeeded(1).Obj(),
				},
			},
			expected: []jobset.ReplicatedJobStatus{
				{
					Name:      "replicated-job-1",
					Ready:     1,
					Succeeded: 0,
				},
				{
					Name:      "replicated-job-2",
					Ready:     3,
					Succeeded: 0,
				},
			},
		},
		{
			name: "no jobs created",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-2").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(3).
					Obj()).Obj(),
			expected: []jobset.ReplicatedJobStatus{
				{
					Name:      "replicated-job-1",
					Ready:     0,
					Succeeded: 0,
				},
				{
					Name:      "replicated-job-2",
					Ready:     0,
					Succeeded: 0,
				},
			},
		},
		{
			name: "partial jobs created",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-2").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(3).
					Obj()).Obj(),
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-2",
						jobName:           "test-jobset-replicated-job-2-test-job-0",
						ns:                ns,
						replicas:          3,
						jobIdx:            0}).
						Parallelism(5).
						Ready(2).
						Succeeded(3).Obj(),
				},
			},
			expected: []jobset.ReplicatedJobStatus{
				{
					Name:      "replicated-job-1",
					Ready:     0,
					Succeeded: 0,
				},
				{
					Name:      "replicated-job-2",
					Ready:     1,
					Succeeded: 0,
				},
			},
		},
		{
			name: "no ready jobs, only succeeded jobs",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-2").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(3).
					Obj()).Obj(),
			jobs: childJobs{
				successful: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-2",
						jobName:           "test-jobset-replicated-job-2-test-job-0"}).Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-0"}).Obj(),
				},
			},
			expected: []jobset.ReplicatedJobStatus{
				{
					Name:      "replicated-job-1",
					Ready:     0,
					Succeeded: 1,
				},
				{
					Name:      "replicated-job-2",
					Ready:     0,
					Succeeded: 1,
				},
			},
		},
		{
			name: "no ready jobs, only failed jobs",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-2").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(3).
					Obj()).Obj(),
			jobs: childJobs{
				failed: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-2",
						jobName:           "test-jobset-replicated-job-2-test-job-0"}).Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-0"}).Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-1"}).Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-2"}).Obj(),
				},
			},
			expected: []jobset.ReplicatedJobStatus{
				{
					Name:   "replicated-job-1",
					Ready:  0,
					Failed: 3,
				},
				{
					Name:   "replicated-job-2",
					Ready:  0,
					Failed: 1,
				},
			},
		},
		{
			name: "active jobs",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-2").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(3).
					Obj()).Obj(),
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-2-test-job-0"}).
						Parallelism(5).
						Active(1).
						Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-2",
						jobName:           "test-jobset-replicated-job-2-test-job-0"}).
						Parallelism(5).
						Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-2",
						jobName:           "test-jobset-replicated-job-2-test-job-1"}).
						Parallelism(1).
						Active(1).
						Obj(),
				},
			},
			expected: []jobset.ReplicatedJobStatus{
				{
					Name:   "replicated-job-1",
					Ready:  0,
					Active: 1,
				},
				{
					Name:   "replicated-job-2",
					Ready:  0,
					Active: 1,
				},
			},
		},
		{
			name: "suspended jobs",
			js: testutils.MakeJobSet(jobSetName, ns).Suspend(true).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-2").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(3).
					Obj()).Obj(),
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-2-test-job-0"}).
						Parallelism(5).
						Suspend(true).
						Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-2",
						jobName:           "test-jobset-replicated-job-2-test-job-0"}).
						Parallelism(5).
						Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-2",
						jobName:           "test-jobset-replicated-job-2-test-job-1"}).
						Parallelism(1).
						Suspend(true).
						Obj(),
				},
			},
			expected: []jobset.ReplicatedJobStatus{
				{
					Name:      "replicated-job-1",
					Ready:     0,
					Suspended: 1,
				},
				{
					Name:      "replicated-job-2",
					Ready:     0,
					Suspended: 1,
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := JobSetReconciler{Client: (fake.NewClientBuilder()).Build()}
			statuses := r.calculateReplicatedJobStatuses(context.TODO(), tc.js, &tc.jobs)
			var less interface{} = func(a, b jobset.ReplicatedJobStatus) bool {
				return a.Name < b.Name
			}
			if diff := cmp.Diff(tc.expected, statuses, cmpopts.SortSlices(less)); diff != "" {
				t.Errorf("calculateReplicatedJobStatuses() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

type makeJobArgs struct {
	jobSetName           string
	replicatedJobName    string
	jobName              string
	ns                   string
	replicas             int
	jobIdx               int
	restarts             int
	topology             string
	nodeSelectorStrategy bool
}

func makeJob(args *makeJobArgs) *testutils.JobWrapper {
	labels := map[string]string{
		jobset.JobSetNameKey:         args.jobSetName,
		jobset.ReplicatedJobNameKey:  args.replicatedJobName,
		jobset.ReplicatedJobReplicas: strconv.Itoa(args.replicas),
		jobset.JobIndexKey:           strconv.Itoa(args.jobIdx),
		RestartsKey:                  strconv.Itoa(args.restarts),
		jobset.JobKey:                jobHashKey(args.ns, args.jobName),
	}
	annotations := map[string]string{
		jobset.JobSetNameKey:         args.jobSetName,
		jobset.ReplicatedJobNameKey:  args.replicatedJobName,
		jobset.ReplicatedJobReplicas: strconv.Itoa(args.replicas),
		jobset.JobIndexKey:           strconv.Itoa(args.jobIdx),
		RestartsKey:                  strconv.Itoa(args.restarts),
		jobset.JobKey:                jobHashKey(args.ns, args.jobName),
	}
	// Only set exclusive key if we are using exclusive placement per topology.
	if args.topology != "" {
		annotations[jobset.ExclusiveKey] = args.topology
		// Exclusive placement topology domain must be set in order to use the node selector strategy.
		if args.nodeSelectorStrategy {
			annotations[jobset.NodeSelectorStrategyKey] = "true"
		}
	}
	jobWrapper := testutils.MakeJob(args.jobName, args.ns).
		JobLabels(labels).
		JobAnnotations(annotations).
		PodLabels(labels).
		PodAnnotations(annotations)
	return jobWrapper
}

func makeJobSetReconciler(initObjs ...client.Object) *JobSetReconciler {
	scheme := runtime.NewScheme()
	utilruntime.Must(jobset.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(batchv1.AddToScheme(scheme))
	eventBroadcaster := record.NewBroadcaster()
	recorder := eventBroadcaster.NewRecorder(scheme, corev1.EventSource{Component: "jobset-test-reconciler"})
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(initObjs...).WithStatusSubresource(initObjs...).Build()
	return &JobSetReconciler{
		Client: c,
		Scheme: scheme,
		Record: recorder,
	}
}

func makeJobSetReconcilerWithJobIndexer(js *jobset.JobSet, job *batchv1.Job) *JobSetReconciler {
	scheme := runtime.NewScheme()
	utilruntime.Must(jobset.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(batchv1.AddToScheme(scheme))
	eventBroadcaster := record.NewBroadcaster()
	recorder := eventBroadcaster.NewRecorder(scheme, corev1.EventSource{Component: "jobset-test-reconciler"})
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithIndex(job, ".metadata.controller", func(o client.Object) []string {
			return []string{js.GetName()}
		}).WithObjects(js, job).WithStatusSubresource(js, job).Build()
	return &JobSetReconciler{
		Client: c,
		Scheme: scheme,
		Record: recorder,
	}
}

func TestCreateJobs(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)
	tests := []struct {
		name                string
		js                  *jobset.JobSet
		jobs                childJobs
		replicatedJobStatus []jobset.ReplicatedJobStatus
		expected            error
	}{
		{
			name: "create jobs",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-0",
						ns:                ns,
						replicas:          1,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).
						Ready(1).
						Succeeded(1).Obj(),
				},
			},
			replicatedJobStatus: []jobset.ReplicatedJobStatus{
				{
					Name:      "replicated-job-1",
					Ready:     1,
					Succeeded: 0,
				},
			},
		},
		{
			name: "create jobs with network enableDNSHostnames is true",
			js: testutils.MakeJobSet(jobSetName, ns).EnableDNSHostnames(true).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-0",
						ns:                ns,
						replicas:          1,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).
						Ready(1).
						Succeeded(1).Obj(),
				},
			},
			replicatedJobStatus: []jobset.ReplicatedJobStatus{
				{
					Name:      "replicated-job-1",
					Ready:     1,
					Succeeded: 0,
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := makeJobSetReconciler()
			actual := r.createJobs(context.TODO(), tc.js, &tc.jobs, tc.replicatedJobStatus)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("createJobs() run error: %s", actual)
			}
		})
	}
}

func TestResumeJob(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)
	tests := []struct {
		name     string
		jobs     childJobs
		expected error
	}{
		{
			name: "resume job",
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job",
						ns:                ns,
						replicas:          1,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).Suspend(true).
						Ready(1).
						Succeeded(1).Obj(),
				},
			},
		},
		{
			name: "resume job with job status is not nil",
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job",
						ns:                ns,
						replicas:          1,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).
						Suspend(true).
						StartTime().
						Ready(1).
						Succeeded(1).Obj(),
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := makeJobSetReconciler(tc.jobs.active[0])
			actual := r.resumeJob(context.TODO(), tc.jobs.active[0], nil)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("resumeJob() run error: %s", actual)
			}
		})
	}
}

func TestDeleteJob(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)
	tests := []struct {
		name     string
		jobs     childJobs
		expected error
	}{
		{
			name: "delete jobs",
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job",
						ns:                ns,
						replicas:          1,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).Suspend(true).
						Ready(1).
						Succeeded(1).Obj(),
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := makeJobSetReconciler(tc.jobs.active[0])
			actual := r.deleteJobs(context.TODO(), tc.jobs.active)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("deleteJobs() run error: %s", actual)
			}
		})
	}
}

func TestSuspendJobSet(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)
	tests := []struct {
		name     string
		js       *jobset.JobSet
		jobs     childJobs
		expected error
	}{
		{
			name: "test suspendJobSet",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-0",
						ns:                ns,
						replicas:          1,
						restarts:          2,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).
						Ready(1).
						Succeeded(1).Obj(),
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := makeJobSetReconciler(tc.js, tc.jobs.active[0])
			actual := r.suspendJobSet(context.TODO(), tc.js, &tc.jobs)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("suspendJobSet() run error: %s", actual)
			}
		})
	}
}

func TestExecuteFailurePolicy(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)
	tests := []struct {
		name     string
		js       *jobset.JobSet
		jobs     childJobs
		expected error
	}{
		{
			name: "test executeFailurePolicy",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-0",
						ns:                ns,
						replicas:          1,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).
						Ready(1).
						Succeeded(0).Obj(),
				},
			},
		},
		{
			name: "test executeFailurePolicy with failurePolicy is not nil",
			js: testutils.MakeJobSet(jobSetName, ns).FailurePolicy(&jobset.FailurePolicy{
				MaxRestarts: 0,
			}).ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-2").
				Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
				Replicas(1).
				Obj()).Obj(),
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-0",
						ns:                ns,
						replicas:          1,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).
						Ready(1).
						Succeeded(0).Obj(),
				},
			},
		},
		{
			name: "test executeFailurePolicy with status.restarts < failurePolicy.maxRestarts",
			js: testutils.MakeJobSet(jobSetName, ns).FailurePolicy(&jobset.FailurePolicy{
				MaxRestarts: 3,
			}).StatusRestarts(1).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-2").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-0",
						ns:                ns,
						replicas:          1,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).
						Ready(1).
						Succeeded(0).Obj(),
				},
			},
		},
		{
			name: "test executeFailurePolicy with status.restarts > failurePolicy.maxRestarts",
			js: testutils.MakeJobSet(jobSetName, ns).FailurePolicy(&jobset.FailurePolicy{
				MaxRestarts: 1,
			}).StatusRestarts(3).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-0",
						ns:                ns,
						replicas:          1,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).
						Ready(1).
						Succeeded(0).Obj(),
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := makeJobSetReconciler(tc.js)
			actual := r.executeFailurePolicy(context.TODO(), tc.js, &tc.jobs)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("executeFailurePolicy() run error: %s", actual)
			}
		})
	}
}

func TestExecuteSuccessPolicy(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)
	tests := []struct {
		name                string
		js                  *jobset.JobSet
		jobs                childJobs
		replicatedJobStatus []jobset.ReplicatedJobStatus
		expected            error
	}{
		{
			name: "test executeSuccessPolicy with spec.successPolicy is All",
			js: testutils.MakeJobSet(jobSetName, ns).SuccessPolicy(&jobset.SuccessPolicy{
				Operator:             jobset.OperatorAll,
				TargetReplicatedJobs: []string{},
			}).StartupPolicy(&jobset.StartupPolicy{StartupPolicyOrder: jobset.InOrder}).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			jobs: childJobs{
				successful: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-0",
						ns:                ns,
						replicas:          1,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).
						Ready(1).
						Succeeded(0).Obj(),
				},
			},
		},
		{
			name: "test executeSuccessPolicy with spec.successPolicy is Any",
			js: testutils.MakeJobSet(jobSetName, ns).SuccessPolicy(&jobset.SuccessPolicy{
				Operator:             jobset.OperatorAny,
				TargetReplicatedJobs: []string{},
			}).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			jobs: childJobs{
				successful: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-0",
						ns:                ns,
						replicas:          1,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).
						Ready(1).
						Succeeded(0).Obj(),
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := makeJobSetReconciler(tc.js)
			_, actual := r.executeSuccessPolicy(context.TODO(), tc.js, &tc.jobs)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("executeSuccessPolicy() run error: %s", actual)
			}
		})
	}
}

func TestResumeJobSetIfNecessary(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)
	tests := []struct {
		name                string
		js                  *jobset.JobSet
		jobs                childJobs
		replicatedJobStatus []jobset.ReplicatedJobStatus
		expected            error
	}{
		{
			name: "test resumeJobSetIfNecessary",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-0",
						ns:                ns,
						replicas:          1,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).
						Ready(1).
						Succeeded(0).Obj(),
				},
			},
			replicatedJobStatus: []jobset.ReplicatedJobStatus{
				{
					Name:      "replicated-job-2",
					Ready:     1,
					Succeeded: 0,
				},
			},
		},
		{
			name: "test resumeJobSetIfNecessary with startupPolicy is not nil",
			js: testutils.MakeJobSet(jobSetName, ns).
				StartupPolicy(&jobset.StartupPolicy{StartupPolicyOrder: jobset.InOrder}).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-0",
						ns:                ns,
						replicas:          1,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).
						Ready(1).
						Succeeded(0).Obj(),
				},
				successful: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-0",
						ns:                ns,
						replicas:          1,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).
						Ready(1).
						Succeeded(1).Obj(),
				},
			},
			replicatedJobStatus: []jobset.ReplicatedJobStatus{
				{
					Name:      "replicated-job-2",
					Ready:     1,
					Succeeded: 0,
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := makeJobSetReconciler(tc.js)
			actual := r.resumeJobSetIfNecessary(context.TODO(), tc.js, &tc.jobs, tc.replicatedJobStatus)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("resumeJobSetIfNecessary() run error: %s", actual)
			}
		})
	}
}

func TestUpdateReplicatedJobsStatuses(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)
	tests := []struct {
		name                string
		js                  *jobset.JobSet
		jobs                childJobs
		replicatedJobStatus []jobset.ReplicatedJobStatus
		expected            error
	}{
		{
			name: "test updateReplicatedJobsStatuses",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-0",
						ns:                ns,
						replicas:          1,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).
						Ready(1).
						Succeeded(0).Obj(),
				},
			},
			replicatedJobStatus: []jobset.ReplicatedJobStatus{
				{
					Name:      "replicated-job-1",
					Ready:     1,
					Succeeded: 0,
				},
			},
		},
		{
			name: "test updateReplicatedJobsStatuses with status ReplicatedJobsStatus has changed",
			js: testutils.MakeJobSet(jobSetName, ns).
				StartupPolicy(&jobset.StartupPolicy{StartupPolicyOrder: jobset.InOrder}).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-0",
						ns:                ns,
						replicas:          1,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).
						Ready(1).
						Succeeded(0).Obj(),
				},
			},
			replicatedJobStatus: []jobset.ReplicatedJobStatus{
				{
					Name:      "replicated-job-1",
					Ready:     1,
					Succeeded: 0,
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := makeJobSetReconciler(tc.js, tc.jobs.active[0])
			actual := r.updateReplicatedJobsStatuses(context.TODO(), tc.js, &tc.jobs, tc.replicatedJobStatus)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("updateReplicatedJobsStatuses() run error: %s", actual)
			}
		})
	}
}

func TestGetChildJobs(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)
	tests := []struct {
		name     string
		js       *jobset.JobSet
		jobs     childJobs
		expected error
	}{
		{
			name: "test getChildJobs",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-0",
						ns:                ns,
						replicas:          1,
						restarts:          2,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).
						Ready(1).
						Succeeded(1).Obj(),
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := makeJobSetReconcilerWithJobIndexer(tc.js, tc.jobs.active[0])
			_, actual := r.getChildJobs(context.TODO(), tc.js)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("getChildJobs() run error: %s", actual)
			}
		})
	}
}

func TestJobSetFinished(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)
	tests := []struct {
		name     string
		js       *jobset.JobSet
		expected bool
	}{
		{
			name: "test jobSetFinished with not finished",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
		},
		{
			name: "test jobSetFinished with finished",
			js: testutils.MakeJobSet(jobSetName, ns).
				SetConditions([]metav1.Condition{{Type: string(jobset.JobSetCompleted), Status: metav1.ConditionTrue}}).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-1").
					Job(testutils.MakeJobTemplate("test-job", ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			expected: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := jobSetFinished(tc.js)
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("unexpected finished value (+got/-want): %s", diff)
			}
		})
	}
}
