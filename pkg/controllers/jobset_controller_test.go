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
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
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

func TestFindFirstFailedJob(t *testing.T) {
	testCases := []struct {
		name       string
		failedJobs []*batchv1.Job
		expected   *batchv1.Job
	}{
		{
			name:       "No failed jobs",
			failedJobs: []*batchv1.Job{},
			expected:   nil,
		},
		{
			name: "Single failed job",
			failedJobs: []*batchv1.Job{
				jobWithFailedCondition("job1", time.Now().Add(-1*time.Hour)),
			},
			expected: jobWithFailedCondition("job1", time.Now().Add(-1*time.Hour)),
		},
		{
			name: "Multiple failed jobs, earliest first",
			failedJobs: []*batchv1.Job{
				jobWithFailedCondition("job1", time.Now().Add(-3*time.Hour)),
				jobWithFailedCondition("job2", time.Now().Add(-5*time.Hour)),
			},
			expected: jobWithFailedCondition("job2", time.Now().Add(-5*time.Hour)),
		},
		{
			name: "Jobs without failed condition",
			failedJobs: []*batchv1.Job{
				{ObjectMeta: metav1.ObjectMeta{Name: "job1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "job2"}},
			},
			expected: nil,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			result := findFirstFailedJob(tc.failedJobs)
			if result != nil && tc.expected != nil {
				assert.Equal(t, result.Name, tc.expected.Name)
			} else if result != nil && tc.expected == nil || result == nil && tc.expected != nil {
				t.Errorf("Expected: %v, got: %v)", result, tc.expected)
			}
		})
	}
}

// Helper function to create a job object with a failed condition
func jobWithFailedCondition(name string, failureTime time.Time) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{
				{
					Type:               batchv1.JobFailed,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.NewTime(failureTime),
				},
			},
		},
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
		expectedErr         error
		errorFlag           bool
		expectedJobsCreated []*batchv1.Job
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
						jobIdx:            0,
					}).Parallelism(1).
						Completions(2).
						Ready(1).
						Active(1).Obj(),
				},
			},
			expectedJobsCreated: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: "replicated-job-1",
					jobName:           "test-jobset-replicated-job-1-test-job-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0,
				}).Parallelism(1).
					Completions(2).
					Ready(1).
					Active(1).Obj(),
			},
		},
		{
			name: "create jobs with DNSHostname",
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
						jobIdx:            0,
					}).Parallelism(1).
						Completions(2).
						Ready(1).
						Active(1).Obj(),
				},
			},
			expectedJobsCreated: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: "replicated-job-1",
					jobName:           "test-jobset-replicated-job-1-test-job-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0,
				}).Parallelism(1).
					Completions(2).
					Ready(1).
					Active(1).Obj(),
			},
		},
		{
			name: "create jobs with StartupPolicyOrder is InOrder",
			js: testutils.MakeJobSet(jobSetName, ns).EnableDNSHostnames(true).StartupPolicy(&jobset.StartupPolicy{
				StartupPolicyOrder: jobset.InOrder,
			}).
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
						jobIdx:            0,
					}).Parallelism(1).
						Completions(2).
						Ready(1).
						Active(1).Obj(),
				},
			},
			expectedJobsCreated: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: "replicated-job-1",
					jobName:           "test-jobset-replicated-job-1-test-job-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0,
				}).Parallelism(1).
					Completions(2).
					Ready(1).
					Active(1).Obj(),
			},
		},
		{
			name: "create jobs with create error",
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
						jobIdx:            0,
					}).Parallelism(1).
						Completions(2).
						Ready(1).
						Active(1).Obj(),
				},
			},
			errorFlag:           true,
			expectedErr:         errors.Join(errors.New("job \"test-jobset-replicated-job-1-0\" creation failed with error: create pod error")),
			expectedJobsCreated: nil,
		},
		{
			name: "create jobs without replicatedJobs",
			js: testutils.MakeJobSet(jobSetName, ns).StartupPolicy(&jobset.StartupPolicy{
				StartupPolicyOrder: jobset.InOrder,
			}).Obj(),
			jobs: childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						jobName:           "test-jobset-replicated-job-1-test-job-0",
						ns:                ns,
						replicas:          1,
						jobIdx:            0,
					}).Parallelism(1).
						Completions(2).
						Ready(1).
						Active(1).Obj(),
				},
			},
			expectedJobsCreated: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var jobsCreated []*batchv1.Job
			fc := makeClient(interceptor.Funcs{
				Create: func(ctx context.Context, client client.WithWatch,
					obj client.Object, opts ...client.CreateOption) error {
					job, ok := obj.(*batchv1.Job)
					if !ok {
						return nil
					}
					if tc.errorFlag || job == nil {
						return errors.New("create pod error")
					}
					job = tc.jobs.active[0]
					jobsCreated = append(jobsCreated, job)
					return nil
				},
			}, tc.js)
			r := &JobSetReconciler{
				Client: fc.client,
				Scheme: fc.scheme,
				Record: fc.record,
			}

			actual := r.createJobs(context.TODO(), tc.js, &tc.jobs, tc.js.Status.ReplicatedJobsStatus)
			assert.Equal(t, actual, tc.expectedErr)

			sort.Slice(jobsCreated, func(i, j int) bool {
				return jobsCreated[i].Name < jobsCreated[j].Name
			})
			sort.Slice(tc.expectedJobsCreated, func(i, j int) bool {
				return tc.expectedJobsCreated[i].Name < tc.expectedJobsCreated[j].Name
			})

			if !reflect.DeepEqual(jobsCreated, tc.expectedJobsCreated) {
				t.Errorf("createJobs() did not make the expected job creation calls")
			}
		})
	}
}

func TestDeleteJobs(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)
	tests := []struct {
		name                string
		jobs                []*batchv1.Job
		expectedJobsDeleted []*batchv1.Job
		errorFlag           bool
		expectedErr         error
	}{
		{
			name: "delete jobs",
			jobs: []*batchv1.Job{
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
					Active(1).Obj(),
			},
			expectedJobsDeleted: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: "replicated-job-1",
					jobName:           "test-jobset-replicated-job-1-test-job",
					ns:                ns,
					replicas:          1,
					jobIdx:            0,
				}).Parallelism(1).
					Completions(2).Suspend(true).
					Ready(1).
					Active(1).Obj(),
			},
		},
		{
			name: "delete jobs with DeletionTimestamp",
			jobs: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: "replicated-job-1",
					jobName:           "test-jobset-replicated-job-1-test-job",
					ns:                ns,
					replicas:          1,
					jobIdx:            0}).
					Parallelism(1).DelTimestamp(&metav1.Time{Time: time.Now()}).
					Completions(2).Suspend(true).
					Ready(1).
					Active(1).Obj(),
			},
			expectedJobsDeleted: nil,
		},
		{
			name: "delete jobs with error",
			jobs: []*batchv1.Job{
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
					Active(1).Obj(),
			},
			expectedJobsDeleted: nil,
			errorFlag:           true,
			expectedErr:         errors.Join(errors.New("delete pod error")),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var jobsDeleted []*batchv1.Job
			fc := makeClient(interceptor.Funcs{
				Delete: func(ctx context.Context, client client.WithWatch,
					obj client.Object, opts ...client.DeleteOption) error {
					job, ok := obj.(*batchv1.Job)
					if !ok {
						return nil
					}
					if tc.errorFlag || job == nil {
						return errors.New("delete pod error")
					}
					job = tc.jobs[0]
					jobsDeleted = append(jobsDeleted, job)
					return nil
				},
			})
			r := &JobSetReconciler{
				Client: fc.client,
				Scheme: fc.scheme,
				Record: fc.record,
			}
			actual := r.deleteJobs(context.TODO(), tc.jobs)
			assert.Equal(t, actual, tc.expectedErr)

			sort.Slice(jobsDeleted, func(i, j int) bool {
				return jobsDeleted[i].Name < jobsDeleted[j].Name
			})
			sort.Slice(tc.expectedJobsDeleted, func(i, j int) bool {
				return tc.expectedJobsDeleted[i].Name < tc.expectedJobsDeleted[j].Name
			})

			if !reflect.DeepEqual(jobsDeleted, tc.expectedJobsDeleted) {
				t.Errorf("deleteJobs() did not make the expected job deletion calls")
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
		name                string
		js                  *jobset.JobSet
		jobs                childJobs
		expectedJobsUpdated []*batchv1.Job
		errorFlag           bool
		expectedErr         error
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
						restarts:          0,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).
						Ready(1).
						Active(1).Obj(),
				},
			},
			expectedJobsUpdated: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: "replicated-job-1",
					jobName:           "test-jobset-replicated-job-1-test-job-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0,
				}).Parallelism(1).
					Completions(2).Suspend(true).
					ReVersion("999").
					Ready(1).
					Active(1).Obj(),
			},
		},
		{
			name: "test suspendJobSet with update error",
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
						restarts:          0,
						jobIdx:            0}).
						Parallelism(1).
						Completions(2).
						Ready(1).
						Active(1).Obj(),
				},
			},
			expectedJobsUpdated: nil,
			errorFlag:           true,
			expectedErr:         errors.Join(errors.New("update pod error")),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var jobsUpdated []*batchv1.Job
			fc := makeClient(interceptor.Funcs{
				Update: func(ctx context.Context, client client.WithWatch,
					obj client.Object, opts ...client.UpdateOption) error {
					job, ok := obj.(*batchv1.Job)
					if !ok {
						return nil
					}
					if tc.errorFlag || job == nil {
						return errors.Join(errors.New("update pod error"))
					}
					jobsUpdated = append(jobsUpdated, job)
					return nil
				},
			}, tc.js, tc.jobs.active[0])
			r := &JobSetReconciler{
				Client: fc.client,
				Scheme: fc.scheme,
				Record: fc.record,
			}
			actual := r.suspendJobSet(context.TODO(), tc.js, &tc.jobs)
			assert.Equal(t, actual, tc.expectedErr)
			sort.Slice(jobsUpdated, func(i, j int) bool {
				return jobsUpdated[i].Name < jobsUpdated[j].Name
			})
			sort.Slice(tc.expectedJobsUpdated, func(i, j int) bool {
				return tc.expectedJobsUpdated[i].Name < tc.expectedJobsUpdated[j].Name
			})
			if !reflect.DeepEqual(jobsUpdated, tc.expectedJobsUpdated) {
				t.Errorf("suspendJobSet() did not make the expected job update calls")
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
			assert.Equal(t, actual, tc.expected)
		})
	}
}

func TestResumeJob(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)
	tests := []struct {
		name                string
		jobs                []*batchv1.Job
		expectedJobsUpdated []*batchv1.Job
		errorFlag           bool
		expectedErr         error
	}{
		{
			name: "resume job",
			jobs: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: "replicated-job-1",
					jobName:           "test-jobset-replicated-job-1-test-job-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0}).
					Parallelism(1).
					Completions(2).Suspend(true).
					Ready(1).
					Active(1).Obj(),
			},
			expectedJobsUpdated: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: "replicated-job-1",
					jobName:           "test-jobset-replicated-job-1-test-job-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0,
				}).Parallelism(1).
					Completions(2).Suspend(false).
					ReVersion("999").
					Ready(1).
					Active(1).Obj(),
			},
		},
		{
			name: "resume job with job status startTimestamp is not nil",
			jobs: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: "replicated-job-1",
					jobName:           "test-jobset-replicated-job-1-test-job-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0}).
					Parallelism(1).
					Completions(2).Suspend(true).
					Ready(1).StartTimestamp(&metav1.Time{Time: time.Now()}).
					Active(1).Obj(),
			},
			expectedJobsUpdated: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: "replicated-job-1",
					jobName:           "test-jobset-replicated-job-1-test-job-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0,
				}).Parallelism(1).
					Completions(2).Suspend(false).
					ReVersion("999").
					Ready(1).
					Active(1).Obj(),
			},
		},
		{
			name: "resume job with update error",
			jobs: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: "replicated-job-1",
					jobName:           "test-jobset-replicated-job-1-test-job-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0}).
					Parallelism(1).
					Completions(2).Suspend(true).
					Ready(1).StartTimestamp(&metav1.Time{Time: time.Now()}).
					Active(1).Obj(),
			},
			expectedJobsUpdated: nil,
			errorFlag:           true,
			expectedErr:         errors.Join(errors.New("update pod error")),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var jobsUpdated []*batchv1.Job
			fc := makeClient(interceptor.Funcs{
				Update: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					job, ok := obj.(*batchv1.Job)
					if !ok {
						return nil
					}
					if tc.errorFlag || job == nil {
						return errors.Join(errors.New("update pod error"))
					}
					jobsUpdated = append(jobsUpdated, job)
					return nil
				},
				SubResourceUpdate: func(ctx context.Context, client client.Client, subResourceName string,
					obj client.Object, opts ...client.SubResourceUpdateOption) error {
					job, ok := obj.(*batchv1.Job)
					if !ok {
						return nil
					}
					if tc.errorFlag || job == nil {
						return errors.Join(errors.New("update pod error"))
					}
					return nil
				},
			}, tc.jobs[0])
			r := &JobSetReconciler{
				Client: fc.client,
				Scheme: fc.scheme,
				Record: fc.record,
			}
			actual := r.resumeJob(context.TODO(), tc.jobs[0], nil)
			assert.Equal(t, actual, tc.expectedErr)
			sort.Slice(jobsUpdated, func(i, j int) bool {
				return jobsUpdated[i].Name < jobsUpdated[j].Name
			})
			sort.Slice(tc.expectedJobsUpdated, func(i, j int) bool {
				return tc.expectedJobsUpdated[i].Name < tc.expectedJobsUpdated[j].Name
			})

			if !reflect.DeepEqual(jobsUpdated, tc.expectedJobsUpdated) {
				t.Errorf("resumeJob() did not make the expected job update calls")
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
		name        string
		js          *jobset.JobSet
		jobs        childJobs
		errorFlag   bool
		expectedErr error
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
			var jobsetUpdated *jobset.JobSet
			fc := makeClient(interceptor.Funcs{
				SubResourceUpdate: func(ctx context.Context, client client.Client, subResourceName string,
					obj client.Object, opts ...client.SubResourceUpdateOption) error {
					jt, ok := obj.(*jobset.JobSet)
					if !ok {
						return nil
					}
					if tc.errorFlag || jt == nil {
						return errors.Join(errors.New("update pod error"))
					}
					jobsetUpdated = jt
					return nil
				},
			}, tc.js)
			r := &JobSetReconciler{
				Client: fc.client,
				Scheme: fc.scheme,
				Record: fc.record,
			}
			_, actual := r.executeSuccessPolicy(context.TODO(), tc.js, &tc.jobs)
			assert.Equal(t, actual, tc.expectedErr)
			if !reflect.DeepEqual(jobsetUpdated, tc.js) {
				t.Errorf("executeSuccessPolicy() did not make the expected job update calls")
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
		errorFlag           bool
		expectedErr         error
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
						Ready(0).
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
			var jobsetUpdated *jobset.JobSet
			fc := makeClient(interceptor.Funcs{
				SubResourceUpdate: func(ctx context.Context, client client.Client, subResourceName string,
					obj client.Object, opts ...client.SubResourceUpdateOption) error {
					jt, ok := obj.(*jobset.JobSet)
					if !ok {
						return nil
					}
					if tc.errorFlag || jt == nil {
						return errors.Join(errors.New("update pod error"))
					}
					jobsetUpdated = jt
					return nil
				},
			}, tc.js)
			r := &JobSetReconciler{
				Client: fc.client,
				Scheme: fc.scheme,
				Record: fc.record,
			}
			actual := r.updateReplicatedJobsStatuses(context.TODO(), tc.js, &tc.jobs, tc.replicatedJobStatus)
			assert.Equal(t, actual, tc.expectedErr)
			if !reflect.DeepEqual(jobsetUpdated, tc.js) {
				t.Errorf("executeSuccessPolicy() did not make the expected job update calls")
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
		expectedErr         error
	}{
		{
			name: "test resumeJobSetIfNecessary",
			js: testutils.MakeJobSet(jobSetName, ns).
				SetConditions([]metav1.Condition{{Type: string(jobset.JobSetSuspended), Status: metav1.ConditionTrue}}).
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
			var jobsetUpdated *jobset.JobSet
			fc := makeClient(interceptor.Funcs{
				Update: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					jt, ok := obj.(*jobset.JobSet)
					if !ok {
						return nil
					}
					fmt.Println("aaa")
					jobsetUpdated = jt
					return nil
				},
				SubResourceUpdate: func(ctx context.Context, client client.Client, subResourceName string,
					obj client.Object, opts ...client.SubResourceUpdateOption) error {
					jt, ok := obj.(*jobset.JobSet)
					if !ok {
						return nil
					}
					fmt.Println("vvv")
					jobsetUpdated = jt
					return nil
				},
			}, tc.js)
			r := &JobSetReconciler{
				Client: fc.client,
				Scheme: fc.scheme,
				Record: fc.record,
			}
			actual := r.resumeJobSetIfNecessary(context.TODO(), tc.js, &tc.jobs, tc.replicatedJobStatus)
			assert.Equal(t, actual, tc.expectedErr)
			if !reflect.DeepEqual(jobsetUpdated, tc.js) {
				t.Errorf("resumeJobSetIfNecessary() did not make the expected job update calls")
				fmt.Println(jobsetUpdated)
				fmt.Println(tc.js)
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
		name         string
		js           *jobset.JobSet
		jobs         childJobs
		expectedJobs childJobs
		expectedErr  error
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
			expectedJobs: childJobs{
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
						Completions(2).ReVersion("999").
						Ready(1).
						Succeeded(1).Obj(),
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			utilruntime.Must(jobset.AddToScheme(scheme))
			utilruntime.Must(corev1.AddToScheme(scheme))
			utilruntime.Must(batchv1.AddToScheme(scheme))
			eventBroadcaster := record.NewBroadcaster()
			recorder := eventBroadcaster.NewRecorder(scheme, corev1.EventSource{Component: "jobset-test-reconciler"})
			fc := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.js, tc.jobs.active[0]).
				WithIndex(tc.jobs.active[0], ".metadata.controller", func(o client.Object) []string {
					return []string{tc.js.GetName()}
				}).
				WithStatusSubresource(tc.js, tc.jobs.active[0]).
				Build()
			r := &JobSetReconciler{
				Client: fc,
				Scheme: scheme,
				Record: recorder,
			}
			actualJobs, actual := r.getChildJobs(context.TODO(), tc.js)
			assert.Equal(t, actual, tc.expectedErr)
			if !reflect.DeepEqual(actualJobs.active, tc.expectedJobs.active) {
				t.Errorf("getChildJobs() did not make the expected job list calls")
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

// Helper function to create a Job for unit testing.
func makeJob(args *makeJobArgs) *testutils.JobWrapper {
	labels := map[string]string{
		jobset.JobSetNameKey:         args.jobSetName,
		jobset.ReplicatedJobNameKey:  args.replicatedJobName,
		jobset.ReplicatedJobReplicas: strconv.Itoa(args.replicas),
		jobset.JobIndexKey:           strconv.Itoa(args.jobIdx),
		constants.RestartsKey:        strconv.Itoa(args.restarts),
		jobset.JobKey:                jobHashKey(args.ns, args.jobName),
	}
	annotations := map[string]string{
		jobset.JobSetNameKey:         args.jobSetName,
		jobset.ReplicatedJobNameKey:  args.replicatedJobName,
		jobset.ReplicatedJobReplicas: strconv.Itoa(args.replicas),
		jobset.JobIndexKey:           strconv.Itoa(args.jobIdx),
		constants.RestartsKey:        strconv.Itoa(args.restarts),
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

type fakeClient struct {
	client client.WithWatch
	scheme *runtime.Scheme
	record record.EventRecorder
}

func makeClient(interceptor interceptor.Funcs, initObjs ...client.Object) *fakeClient {
	scheme := runtime.NewScheme()
	utilruntime.Must(jobset.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(batchv1.AddToScheme(scheme))

	eventBroadcaster := record.NewBroadcaster()
	recorder := eventBroadcaster.NewRecorder(scheme, corev1.EventSource{Component: "jobset-test-reconciler"})
	fc := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(initObjs...).
		WithInterceptorFuncs(interceptor).
		WithStatusSubresource(initObjs...).
		Build()
	return &fakeClient{
		client: fc,
		scheme: scheme,
		record: recorder,
	}
}
