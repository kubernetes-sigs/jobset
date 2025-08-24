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
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/tools/record"

	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2/ktesting"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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
		jobAnnotations    = map[string]string{
			"job-annotation-key1": "job-annotation-value1",
			"job-annotation-key2": "job-annotation-value2",
		}
		jobLabels = map[string]string{
			"job-label-key1": "job-label-value1",
			"job-label-key2": "job-label-value2",
		}
		podAnnotations = map[string]string{
			"pod-annotation-key1": "pod-annotation-value1",
			"pod-annotation-key2": "pod-annotation-value2",
		}
		podLabels = map[string]string{
			"pod-label-key1": "pod-label-value1",
			"pod-label-key2": "pod-label-value2",
		}
		topologyDomain      = "test-topology-domain"
		coordinatorKeyValue = map[string]string{
			jobset.CoordinatorKey: fmt.Sprintf("%s-%s-%d-%d.%s", jobSetName, replicatedJobName, 0, 0, jobSetName),
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
					GroupName("default").
					Replicas(2).
					Obj()).Obj(),
			ownedJobs: &childJobs{},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName,
					groupName:         "default",
					jobName:           "test-jobset-replicated-job-0",
					ns:                ns,
					replicas:          2,
					jobIdx:            0}).
					Suspend(false).Obj(),
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName,
					groupName:         "default",
					jobName:           "test-jobset-replicated-job-1",
					ns:                ns,
					replicas:          2,
					jobIdx:            1}).
					Suspend(false).Obj(),
			},
		},
		{
			name: "all jobs/pods created with labels and annotations",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).
						SetLabels(jobLabels).
						SetPodLabels(podLabels).
						SetAnnotations(jobAnnotations).
						SetPodAnnotations(podAnnotations).
						Obj()).
					GroupName("default").
					Replicas(2).
					Obj()).Obj(),
			ownedJobs: &childJobs{},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName,
					groupName:         "default",
					jobName:           "test-jobset-replicated-job-0",
					ns:                ns,
					jobLabels:         jobLabels,
					jobAnnotations:    jobAnnotations,
					podLabels:         podLabels,
					podAnnotations:    podAnnotations,
					replicas:          2,
					jobIdx:            0}).
					Suspend(false).Obj(),
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName,
					groupName:         "default",
					jobName:           "test-jobset-replicated-job-1",
					ns:                ns,
					jobLabels:         jobLabels,
					jobAnnotations:    jobAnnotations,
					podLabels:         podLabels,
					podAnnotations:    podAnnotations,
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
					GroupName("default").
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
					groupName:         "default",
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
					GroupName("default").
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
					groupName:         "default",
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
					GroupName("default").
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
					groupName:         "default",
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
					GroupName("default").
					Obj()).Obj(),
			ownedJobs: &childJobs{
				previous: []*batchv1.Job{
					testutils.MakeJob("test-jobset-replicated-job-0", ns).Obj(),
				},
			},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName,
					groupName:         "default",
					jobName:           "test-jobset-replicated-job-1",
					ns:                ns,
					replicas:          2,
					jobIdx:            1}).
					Suspend(false).Obj(),
			},
		},
		{
			name: "job creation blocked until all previous jobs no longer exist",
			js: testutils.MakeJobSet(jobSetName, ns).
				FailurePolicy(&jobset.FailurePolicy{
					RestartStrategy: jobset.BlockingRecreate,
				}).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(2).
					Obj()).Obj(),
			ownedJobs: &childJobs{
				previous: []*batchv1.Job{
					testutils.MakeJob("test-jobset-replicated-job-0", ns).Obj(),
				},
			},
			want: nil,
		},
		{
			name: "multiple replicated jobs",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-A").
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					GroupName("default").
					Obj()).
				ReplicatedJob(testutils.MakeReplicatedJob("replicated-job-B").
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(2).
					GroupName("default").
					Obj()).
				Obj(),
			ownedJobs: &childJobs{
				active: []*batchv1.Job{
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-B",
						groupName:         "default",
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
					groupName:         "default",
					jobName:           "test-jobset-replicated-job-A-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0}).
					Suspend(false).Obj(),
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: "replicated-job-B",
					groupName:         "default",
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
					GroupName("default").
					Obj()).
				// Replicated Job B has no exclusive placement annotation.
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName + "-B").
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					GroupName("default").
					Obj()).
				Obj(),
			ownedJobs: &childJobs{},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName + "-A",
					groupName:         "default",
					jobName:           "test-jobset-replicated-job-A-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0,
					topology:          topologyDomain}).
					Suspend(false).Obj(),
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName + "-B",
					groupName:         "default",
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
							jobset.NodeSelectorStrategyKey: "true",
							jobset.GroupNameKey:            "default",
						}).
						Obj()).
					Replicas(1).
					GroupName("default").
					Obj()).
				// Replicated Job B has no exclusive placement annotation.
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName + "-B").
					Job(testutils.MakeJobTemplate(jobName, ns).
						SetAnnotations(map[string]string{
							jobset.GroupNameKey: "default",
						}).
						Obj()).
					Replicas(1).
					GroupName("default").
					Obj()).
				Obj(),
			ownedJobs: &childJobs{},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:           jobSetName,
					replicatedJobName:    replicatedJobName + "-A",
					groupName:            "default",
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
					groupName:         "default",
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
					GroupName("default").
					Obj()).
				// Replicated Job B.
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName + "-B").
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					GroupName("default").
					Obj()).
				Obj(),
			ownedJobs: &childJobs{},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName + "-A",
					groupName:         "default",
					jobName:           "test-jobset-replicated-job-A-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0,
					topology:          topologyDomain}).
					Suspend(false).Obj(),
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName + "-B",
					groupName:         "default",
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
					//					GroupName("default").
					Obj()).
				// Replicated Job B.
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName + "-B").
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					//					GroupName("default").
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
					GroupName("default").
					Obj()).
				Obj(),
			ownedJobs: &childJobs{},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName,
					groupName:         "default",
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
					GroupName("default").
					Obj()).
				Obj(),
			ownedJobs: &childJobs{},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName,
					groupName:         "default",
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
					GroupName("default").
					Obj()).
				Obj(),
			ownedJobs: &childJobs{},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName,
					groupName:         "default",
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
					GroupName("default").
					Obj()).
				Obj(),
			ownedJobs: &childJobs{},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:           jobSetName,
					replicatedJobName:    replicatedJobName,
					groupName:            "default",
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
			name: "coordinator",
			js: testutils.MakeJobSet(jobSetName, ns).
				Coordinator(&jobset.Coordinator{
					ReplicatedJob: replicatedJobName,
					JobIndex:      0,
					PodIndex:      0,
				}).
				EnableDNSHostnames(true).
				NetworkSubdomain(jobSetName).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Subdomain(jobSetName).
					Replicas(1).
					GroupName("default").
					Obj()).
				Obj(),
			ownedJobs: &childJobs{},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName,
					groupName:         "default",
					jobName:           "test-jobset-replicated-job-0",
					ns:                ns,
					replicas:          1,
					jobIdx:            0}).
					JobAnnotations(coordinatorKeyValue).
					JobLabels(coordinatorKeyValue).
					PodAnnotations(coordinatorKeyValue).
					PodLabels(coordinatorKeyValue).
					Suspend(false).
					Subdomain(jobSetName).Obj(),
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
					GroupName("default").
					Obj()).
				Obj(),
			ownedJobs: &childJobs{},
			want: []*batchv1.Job{
				makeJob(&makeJobArgs{
					jobSetName:        jobSetName,
					replicatedJobName: replicatedJobName,
					groupName:         "default",
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
			// Here we update the expected Jobs with certain features which require
			// direct access to the JobSet object itself to calculate. For example,
			// the `jobset.sigs.k8s.io/job-global-index` annotation requires access to the
			// full JobSet spec to calculate a unique ID for each Job.
			for _, expectedJob := range tc.want {
				addJobGlobalIndex(t, tc.js, expectedJob)
				addGlobalReplicas(t, tc.js, expectedJob)
				addJobGroupIndex(t, tc.js, expectedJob)
				addGroupReplicas(t, tc.js, expectedJob)
			}

			// Now get the actual output of constructJobsFromTemplate, and diff the results.
			var got []*batchv1.Job
			for _, rjob := range tc.js.Spec.ReplicatedJobs {
				jobs := constructJobsFromTemplate(tc.js, &rjob, tc.ownedJobs)
				got = append(got, jobs...)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("constructJobsFromTemplate() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// addJobGlobalIndex modifies the Job object in place by adding
// the `jobset.sigs.k8s.io/job-global-index` label/annotation to both the
// Job itself and the Job template spec.`
func addJobGlobalIndex(t *testing.T, js *jobset.JobSet, job *batchv1.Job) {
	t.Helper()

	rjobName := job.Annotations[jobset.ReplicatedJobNameKey]
	jobIdx, err := strconv.Atoi(job.Annotations[jobset.JobIndexKey])
	if err != nil {
		t.Fatalf("invalid test case: %v", err)
	}

	// Job label/annotation
	job.Labels[jobset.JobGlobalIndexKey] = globalJobIndex(js, rjobName, jobIdx)
	job.Annotations[jobset.JobGlobalIndexKey] = globalJobIndex(js, rjobName, jobIdx)

	// Job template spec label/annotation
	job.Spec.Template.Labels[jobset.JobGlobalIndexKey] = globalJobIndex(js, rjobName, jobIdx)
	job.Spec.Template.Annotations[jobset.JobGlobalIndexKey] = globalJobIndex(js, rjobName, jobIdx)
}

func addGlobalReplicas(t *testing.T, js *jobset.JobSet, job *batchv1.Job) {
	t.Helper()

	// Job label/annotation
	job.Labels[jobset.GlobalReplicasKey] = globalReplicas(js)
	job.Annotations[jobset.GlobalReplicasKey] = globalReplicas(js)

	// Job template spec label/annotation
	job.Spec.Template.Labels[jobset.GlobalReplicasKey] = globalReplicas(js)
	job.Spec.Template.Annotations[jobset.GlobalReplicasKey] = globalReplicas(js)
}

func addJobGroupIndex(t *testing.T, js *jobset.JobSet, job *batchv1.Job) {
	t.Helper()

	rjobName := job.Annotations[jobset.ReplicatedJobNameKey]
	groupName := job.Annotations[jobset.GroupNameKey]
	jobIdx, err := strconv.Atoi(job.Annotations[jobset.JobIndexKey])
	if err != nil {
		t.Fatalf("invalid test case: %v", err)
	}

	// Job label/annotation
	job.Labels[jobset.JobGroupIndexKey] = groupJobIndex(js, groupName, rjobName, jobIdx)
	job.Annotations[jobset.JobGroupIndexKey] = groupJobIndex(js, groupName, rjobName, jobIdx)

	// Job template spec label/annotation
	job.Spec.Template.Labels[jobset.JobGroupIndexKey] = groupJobIndex(js, groupName, rjobName, jobIdx)
	job.Spec.Template.Annotations[jobset.JobGroupIndexKey] = groupJobIndex(js, groupName, rjobName, jobIdx)
}

func addGroupReplicas(t *testing.T, js *jobset.JobSet, job *batchv1.Job) {
	t.Helper()

	groupName := job.Annotations[jobset.GroupNameKey]

	// Job label/annotation
	job.Labels[jobset.GroupReplicasKey] = groupReplicas(js, groupName)
	job.Annotations[jobset.GroupReplicasKey] = groupReplicas(js, groupName)

	// Job template
	job.Spec.Template.Labels[jobset.GroupReplicasKey] = groupReplicas(js, groupName)
	job.Spec.Template.Annotations[jobset.GroupReplicasKey] = groupReplicas(js, groupName)
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
		opts           *conditionOpts
		expectedUpdate bool
	}{
		{
			name: "no existing conditions, not adding conditions",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			opts:           &conditionOpts{},
			expectedUpdate: false,
		},
		{
			name: "no existing conditions, add a condition",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			opts:           makeCompletedConditionsOpts(),
			expectedUpdate: true,
		},
		{
			name: "suspended",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).Obj(),
			opts:           makeSuspendedConditionOpts(),
			expectedUpdate: true,
		},
		{
			name: "resume (update suspended condition type in-place)",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).
				Conditions([]metav1.Condition{
					// JobSet is currrently suspended.
					{
						Type:    string(jobset.JobSetSuspended),
						Reason:  constants.JobSetSuspendedReason,
						Message: constants.JobSetSuspendedMessage,
						Status:  metav1.ConditionTrue,
					},
				}).
				Obj(),
			opts:           makeResumedConditionOpts(),
			expectedUpdate: true,
		},
		{
			name: "existing conditions, attempt to add duplicate",
			js: testutils.MakeJobSet(jobSetName, ns).
				ReplicatedJob(testutils.MakeReplicatedJob(replicatedJobName).
					Job(testutils.MakeJobTemplate(jobName, ns).Obj()).
					Replicas(1).
					Obj()).TerminalState(jobset.JobSetCompleted).
				Conditions([]metav1.Condition{
					// JobSet is completed..
					{
						Type:    string(jobset.JobSetCompleted),
						Reason:  constants.AllJobsCompletedReason,
						Message: constants.AllJobsCompletedMessage,
						Status:  metav1.ConditionTrue,
					},
				}).Obj(),
			opts:           makeCompletedConditionsOpts(),
			expectedUpdate: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotUpdate := updateCondition(tc.js, tc.opts)
			if gotUpdate != tc.expectedUpdate {
				t.Errorf("updateCondition return mismatch (want: %v, got %v)", tc.expectedUpdate, gotUpdate)
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
						groupName:         "default",
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
						groupName:         "default",
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
						groupName:         "default",
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
						groupName:         "default",
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
						groupName:         "default",
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
						groupName:         "default",
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
						groupName:         "default",
						jobName:           "test-jobset-replicated-job-2-test-job-0"}).Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						groupName:         "default",
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
						groupName:         "default",
						jobName:           "test-jobset-replicated-job-2-test-job-0"}).Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						groupName:         "default",
						jobName:           "test-jobset-replicated-job-1-test-job-0"}).Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						groupName:         "default",
						jobName:           "test-jobset-replicated-job-1-test-job-1"}).Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-1",
						groupName:         "default",
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
						groupName:         "default",
						jobName:           "test-jobset-replicated-job-2-test-job-0"}).
						Parallelism(5).
						Active(1).
						Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-2",
						groupName:         "default",
						jobName:           "test-jobset-replicated-job-2-test-job-0"}).
						Parallelism(5).
						Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-2",
						groupName:         "default",
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
						groupName:         "default",
						jobName:           "test-jobset-replicated-job-2-test-job-0"}).
						Parallelism(5).
						Suspend(true).
						Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-2",
						groupName:         "default",
						jobName:           "test-jobset-replicated-job-2-test-job-0"}).
						Parallelism(5).
						Obj(),
					makeJob(&makeJobArgs{
						jobSetName:        jobSetName,
						replicatedJobName: "replicated-job-2",
						groupName:         "default",
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
			less := func(a, b jobset.ReplicatedJobStatus) bool {
				return a.Name < b.Name
			}
			if diff := cmp.Diff(tc.expected, statuses, cmpopts.SortSlices(less)); diff != "" {
				t.Errorf("calculateReplicatedJobStatuses() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Helper function to create a job object with a failed condition
func jobWithFailedCondition(name string, failureTime time.Time) *batchv1.Job {
	return jobWithFailedConditionAndOpts(name, failureTime, nil)
}

type failJobOptions struct {
	reason                  *string
	parentReplicatedJobName *string
}

func parseFailJobOpts(opts *failJobOptions) (reason string, labels map[string]string) {
	if opts == nil {
		return "", nil
	}

	if opts.parentReplicatedJobName != nil {
		labels = make(map[string]string)
		labels[jobset.ReplicatedJobNameKey] = *opts.parentReplicatedJobName
	}

	if opts.reason != nil {
		reason = *opts.reason
	}

	return reason, labels
}

// Helper function to create a job object with a failed condition
func jobWithFailedConditionAndOpts(name string, failureTime time.Time, opts *failJobOptions) *batchv1.Job {
	reason, labels := parseFailJobOpts(opts)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{
				{
					Type:               batchv1.JobFailed,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.NewTime(failureTime),
					Reason:             reason,
				},
			},
		},
	}

	return job
}

type makeJobArgs struct {
	jobSetName           string
	jobSetUID            string
	replicatedJobName    string
	groupName            string
	jobName              string
	ns                   string
	jobLabels            map[string]string
	jobAnnotations       map[string]string
	podLabels            map[string]string
	podAnnotations       map[string]string
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
		jobset.JobSetUIDKey:          args.jobSetUID,
		jobset.ReplicatedJobNameKey:  args.replicatedJobName,
		jobset.GroupNameKey:          args.groupName,
		jobset.ReplicatedJobReplicas: strconv.Itoa(args.replicas),
		jobset.JobIndexKey:           strconv.Itoa(args.jobIdx),
		constants.RestartsKey:        strconv.Itoa(args.restarts),
		jobset.JobKey:                jobHashKey(args.ns, args.jobName),
	}
	annotations := map[string]string{
		jobset.JobSetNameKey:         args.jobSetName,
		jobset.JobSetUIDKey:          args.jobSetUID,
		jobset.ReplicatedJobNameKey:  args.replicatedJobName,
		jobset.GroupNameKey:          args.groupName,
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
		JobLabels(args.jobLabels).
		JobAnnotations(annotations).
		JobAnnotations(args.jobAnnotations).
		PodLabels(labels).
		PodLabels(args.podLabels).
		PodAnnotations(args.podAnnotations).
		PodAnnotations(annotations)
	return jobWrapper
}

func TestCreateHeadlessSvcIfNecessary(t *testing.T) {
	var (
		jobSetName = "test-jobset"
		ns         = "default"
	)

	tests := []struct {
		name                           string
		jobSet                         *jobset.JobSet
		existingService                bool
		expectServiceCreate            bool
		expectServiceName              string
		expectPublishNotReadyAddresses bool
		expectErr                      bool
		expectErrStr                   string
	}{
		{
			name:            "headless service exists and should not be created",
			jobSet:          testutils.MakeJobSet(jobSetName, ns).EnableDNSHostnames(true).Obj(),
			existingService: true,
		},
		{
			name:            "headless service creation fails with unexpected error",
			jobSet:          testutils.MakeJobSet(jobSetName, ns).EnableDNSHostnames(true).Obj(),
			existingService: false,
			expectErr:       true,
			expectErrStr:    "unexpected error",
		},
		{
			name:                           "service does not exist and should be created, subdomain not set",
			jobSet:                         testutils.MakeJobSet(jobSetName, ns).EnableDNSHostnames(true).Obj(),
			expectServiceCreate:            true,
			expectServiceName:              "test-jobset",
			expectPublishNotReadyAddresses: true,
		},
		{
			name:                           "service does not exist and should be created, subdomain set",
			jobSet:                         testutils.MakeJobSet(jobSetName, ns).EnableDNSHostnames(true).NetworkSubdomain("test-subdomain").Obj(),
			expectServiceCreate:            true,
			expectServiceName:              "test-subdomain",
			expectPublishNotReadyAddresses: true,
		},
		{
			name:                           "service does not exist and should be created, publishNotReadyAddresses is false",
			jobSet:                         testutils.MakeJobSet(jobSetName, ns).EnableDNSHostnames(true).PublishNotReadyAddresses(false).Obj(),
			expectServiceCreate:            true,
			expectServiceName:              "test-jobset",
			expectPublishNotReadyAddresses: false,
		},
		{
			name:                           "service does not exist and should be created, publishNotReadyAddresses is true",
			jobSet:                         testutils.MakeJobSet(jobSetName, ns).EnableDNSHostnames(true).PublishNotReadyAddresses(true).Obj(),
			expectServiceCreate:            true,
			expectServiceName:              "test-jobset",
			expectPublishNotReadyAddresses: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var servicesCreated int
			_, ctx := ktesting.NewTestContext(t)
			scheme := runtime.NewScheme()
			utilruntime.Must(jobset.AddToScheme(scheme))
			utilruntime.Must(corev1.AddToScheme(scheme))
			fakeClientBuilder := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
				Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if tc.expectErr {
						return errors.New("unexpected error")
					}
					if !tc.expectServiceCreate {
						t.Fatal("unexpected service creation")
					}
					svc := obj.(*corev1.Service)
					if svc.Name != tc.expectServiceName {
						t.Errorf("expected service name to be %q, got %q", tc.expectServiceName, svc.Name)
					}
					if len(svc.OwnerReferences) != 1 {
						t.Error("expected service to have owner reference set")
					}
					expectedOwnerRef := metav1.OwnerReference{
						APIVersion:         "jobset.x-k8s.io/v1alpha2",
						Kind:               "JobSet",
						Name:               "test-jobset",
						Controller:         ptr.To(true),
						BlockOwnerDeletion: ptr.To(true),
					}
					if diff := cmp.Diff(expectedOwnerRef, svc.OwnerReferences[0]); diff != "" {
						t.Errorf("unexpected service owner reference value (+got/-want): %s", diff)
					}
					if svc.Spec.ClusterIP != corev1.ClusterIPNone {
						t.Errorf("expected service to have ClusterIP None, got %s", svc.Spec.ClusterIP)
					}
					selectorValue, ok := svc.Spec.Selector[jobset.JobSetNameKey]
					if !ok {
						t.Errorf("expected service selector to contain %q key", jobset.JobSetNameKey)
					}
					if selectorValue != tc.jobSet.Name {
						t.Errorf("expected service selector to be %q, got %q", tc.jobSet.Name, selectorValue)
					}
					if svc.Spec.PublishNotReadyAddresses != tc.expectPublishNotReadyAddresses {
						t.Errorf("expected PublishNotReadyAddresses to be %t, got %t", tc.expectPublishNotReadyAddresses, svc.Spec.PublishNotReadyAddresses)
					}
					servicesCreated++
					return nil
				},
			})
			if tc.existingService {
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-jobset",
						Namespace: ns,
					},
				}
				fakeClientBuilder.WithObjects(svc)
			}
			fakeClient := fakeClientBuilder.Build()

			eventBroadcaster := record.NewBroadcaster()
			recorder := eventBroadcaster.NewRecorder(scheme, corev1.EventSource{Component: "jobset-test-reconciler"})

			// Create a JobSetReconciler instance with the fake client
			r := &JobSetReconciler{
				Client: fakeClient,
				Scheme: scheme,
				Record: recorder,
			}

			// Execute the function under test
			gotErr := r.createHeadlessSvcIfNecessary(ctx, tc.jobSet)
			if tc.expectErr != (gotErr != nil) {
				t.Errorf("expected error is %t, got %t, error: %v", tc.expectErr, gotErr != nil, gotErr)
			}
			if tc.expectErr && len(tc.expectErrStr) == 0 {
				t.Error("invalid test setup; error message must not be empty for error cases")
			}
			if tc.expectErr && !strings.Contains(gotErr.Error(), tc.expectErrStr) {
				t.Errorf("expected error message contains %q, got %v", tc.expectErrStr, gotErr)
			}
			if !tc.expectServiceCreate && servicesCreated != 0 {
				t.Errorf("expected no service to be created, got %d created services", servicesCreated)
			}
			if tc.expectServiceCreate && servicesCreated != 1 {
				t.Errorf("expected 1 service to be created, got %d created services", servicesCreated)
			}
		})
	}
}

func TestGlobalJobIndex(t *testing.T) {
	tests := []struct {
		name                   string
		jobSet                 *jobset.JobSet
		replicatedJob          string
		jobIdx                 int
		expectedJobGlobalIndex string
	}{
		{
			name: "single replicated job",
			jobSet: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "rjob", Replicas: 3},
					},
				},
			},
			replicatedJob:          "rjob",
			jobIdx:                 1,
			expectedJobGlobalIndex: "1",
		},
		{
			name: "multiple replicated jobs",
			jobSet: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "rjob1", Replicas: 2},
						{Name: "rjob2", Replicas: 4},
						{Name: "rjob3", Replicas: 1},
					},
				},
			},
			replicatedJob:          "rjob2",
			jobIdx:                 3,
			expectedJobGlobalIndex: "5",
		},
		{
			name: "replicated job not found",
			jobSet: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "rjob1", Replicas: 2},
					},
				},
			},
			replicatedJob:          "rjob2",
			jobIdx:                 0,
			expectedJobGlobalIndex: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actualJobGlobalIndex := globalJobIndex(tc.jobSet, tc.replicatedJob, tc.jobIdx)
			if diff := cmp.Diff(tc.expectedJobGlobalIndex, actualJobGlobalIndex); diff != "" {
				t.Errorf("unexpected global job index (-want/+got): %s", diff)
			}
		})
	}
}

func TestGlobalReplicas(t *testing.T) {
	tests := []struct {
		name                   string
		jobSet                 *jobset.JobSet
		expectedGlobalReplicas string
	}{
		{
			name: "empty jobset",
			jobSet: &jobset.JobSet{
				Spec: jobset.JobSetSpec{},
			},
			expectedGlobalReplicas: "0",
		},
		{
			name: "single replicated job",
			jobSet: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Replicas: 3,
						},
					},
				},
			},
			expectedGlobalReplicas: "3",
		},
		{
			name: "multiple replicated jobs",
			jobSet: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Replicas: 2,
						},
						{
							Replicas: 5,
						},
					},
				},
			},
			expectedGlobalReplicas: "7",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actualGlobalReplicas := globalReplicas(tc.jobSet)
			if diff := cmp.Diff(tc.expectedGlobalReplicas, actualGlobalReplicas); diff != "" {
				t.Errorf("unexpected global replicas (-want/+got): %s", diff)
			}
		})
	}
}

func TestGroupJobIndex(t *testing.T) {
	tests := []struct {
		name                  string
		jobSet                *jobset.JobSet
		groupName             string
		replicatedJob         string
		jobIdx                int
		expectedGroupJobIndex string
	}{
		{
			name: "single replicated job in group",
			jobSet: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "rjob1", GroupName: "groupA", Replicas: 3},
					},
				},
			},
			groupName:             "groupA",
			replicatedJob:         "rjob1",
			jobIdx:                1,
			expectedGroupJobIndex: "1",
		},
		{
			name: "multiple replicated jobs in group",
			jobSet: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "rjob1", GroupName: "groupA", Replicas: 1},
						{Name: "rjob2", GroupName: "groupB", Replicas: 2},
						{Name: "rjob3", GroupName: "groupA", Replicas: 3},
					},
				},
			},
			groupName:             "groupA",
			replicatedJob:         "rjob3",
			jobIdx:                1,
			expectedGroupJobIndex: "2",
		},
		{
			name: "replicated job not found in group",
			jobSet: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "rjob1", GroupName: "groupA", Replicas: 2},
						{Name: "rjob2", GroupName: "groupB", Replicas: 4},
					},
				},
			},
			groupName:             "groupA",
			replicatedJob:         "rjob2",
			jobIdx:                0,
			expectedGroupJobIndex: "",
		},
		{
			name: "replicated job doesn't exist",
			jobSet: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "rjob1", GroupName: "groupA", Replicas: 2},
						{Name: "rjob2", GroupName: "groupB", Replicas: 4},
					},
				},
			},
			groupName:             "groupA",
			replicatedJob:         "rjob3",
			jobIdx:                0,
			expectedGroupJobIndex: "",
		},
		{
			name: "group doesn't exit",
			jobSet: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "rjob1", GroupName: "groupA", Replicas: 2},
						{Name: "rjob2", GroupName: "groupB", Replicas: 4},
					},
				},
			},
			groupName:             "groupC",
			replicatedJob:         "rjob1",
			jobIdx:                0,
			expectedGroupJobIndex: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actualGroupJobIndex := groupJobIndex(tc.jobSet, tc.groupName, tc.replicatedJob, tc.jobIdx)
			if diff := cmp.Diff(tc.expectedGroupJobIndex, actualGroupJobIndex); diff != "" {
				t.Errorf("unexpected group job index (-want/+got): %s", diff)
			}
		})
	}
}

func TestGroupReplicas(t *testing.T) {
	tests := []struct {
		name                  string
		jobSet                *jobset.JobSet
		groupName             string
		expectedGroupReplicas string
	}{
		{
			name: "empty jobset",
			jobSet: &jobset.JobSet{
				Spec: jobset.JobSetSpec{},
			},
			groupName:             "groupA",
			expectedGroupReplicas: "0",
		},
		{
			name: "single replicated job in group",
			jobSet: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{GroupName: "groupA", Replicas: 1},
					},
				},
			},
			groupName:             "groupA",
			expectedGroupReplicas: "1",
		},
		{
			name: "multiple replicated jobs in group",
			jobSet: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{GroupName: "groupA", Replicas: 1},
						{GroupName: "groupA", Replicas: 2},
					},
				},
			},
			groupName:             "groupA",
			expectedGroupReplicas: "3",
		},
		{
			name: "multiple groups, replicas in target group",
			jobSet: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{GroupName: "groupA", Replicas: 1},
						{GroupName: "groupB", Replicas: 2},
					},
				},
			},
			groupName:             "groupB",
			expectedGroupReplicas: "2",
		},
		{
			name: "multiple groups, no replicas in target group",
			jobSet: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{GroupName: "groupA", Replicas: 1},
						{GroupName: "groupB", Replicas: 2},
					},
				},
			},
			groupName:             "groupC",
			expectedGroupReplicas: "0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actualGroupReplicas := groupReplicas(tc.jobSet, tc.groupName)
			if diff := cmp.Diff(tc.expectedGroupReplicas, actualGroupReplicas); diff != "" {
				t.Errorf("unexpected group replicas (-want/+got): %s", diff)
			}
		})
	}
}
