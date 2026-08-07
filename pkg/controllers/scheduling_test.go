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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	schedulingv1alpha3 "k8s.io/api/scheduling/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/features"
)

func TestBuildWorkload(t *testing.T) {
	tests := map[string]struct {
		js                *jobset.JobSet
		wantTemplates     int
		wantName          string
		wantControllerRef *schedulingv1alpha3.TypedLocalObjectReference
	}{
		"single ReplicatedJob defaults to Gang": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-js", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "workers",
							Replicas: 4,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To[int32](2),
								},
							},
						},
					},
				},
			},
			wantTemplates: 1,
			wantName:      "test-js",
			wantControllerRef: &schedulingv1alpha3.TypedLocalObjectReference{
				APIGroup: "jobset.x-k8s.io",
				Kind:     "JobSet",
				Name:     "test-js",
			},
		},
		"top-level gang with multiple ReplicatedJobs creates single template": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "multi-js", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "driver",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To[int32](1),
								},
							},
						},
						{
							Name:     "workers",
							Replicas: 8,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To[int32](4),
								},
							},
						},
					},
				},
			},
			wantTemplates: 1,
			wantName:      "multi-js",
		},
		"per-RJ policies create one template per ReplicatedJob": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "per-rj-js", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{
						ReplicatedJobPolicies: []jobset.ReplicatedJobSchedulingPolicy{
							{
								TargetReplicatedJob: []string{"driver"},
								SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
									Basic: &schedulingv1alpha3.BasicSchedulingPolicy{},
								},
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "driver",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To[int32](1),
								},
							},
						},
						{
							Name:     "workers",
							Replicas: 4,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{
									Parallelism: ptr.To[int32](2),
								},
							},
						},
					},
				},
			},
			wantTemplates: 2,
			wantName:      "per-rj-js",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			workload, err := buildWorkload(tc.js)
			require.NoError(t, err)
			assert.Equal(t, tc.wantName, workload.Name)
			assert.Equal(t, tc.js.Namespace, workload.Namespace)
			assert.Len(t, workload.Spec.PodGroupTemplates, tc.wantTemplates)
			if tc.wantControllerRef != nil {
				require.NotNil(t, workload.Spec.ControllerRef)
				assert.Equal(t, tc.wantControllerRef.Kind, workload.Spec.ControllerRef.Kind)
				assert.Equal(t, tc.wantControllerRef.Name, workload.Spec.ControllerRef.Name)
			}
		})
	}
}

// TestBuildWorkloadGroupNameCollision verifies that buildWorkload rejects a
// JobSet whose generated PodGroupTemplate names collide, rather than silently
// producing a Workload with duplicate template names that the API server
// would reject on every reconcile with no way to recover, since
// spec.scheduling and spec.replicatedJobs are immutable.
func TestBuildWorkloadGroupNameCollision(t *testing.T) {
	js := &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{Name: "collide-js", Namespace: "default"},
		Spec: jobset.JobSetSpec{
			Scheduling: &jobset.JobSetScheduling{
				ReplicatedJobPolicies: []jobset.ReplicatedJobSchedulingPolicy{
					{
						TargetReplicatedJob: []string{"leader", "worker"},
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Basic: &schedulingv1alpha3.BasicSchedulingPolicy{},
						},
					},
				},
			},
			ReplicatedJobs: []jobset.ReplicatedJob{
				{
					Name:     "leader",
					Replicas: 1,
					Template: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](1)},
					},
				},
				{
					Name:     "worker",
					Replicas: 4,
					Template: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](2)},
					},
				},
				// This ReplicatedJob's own name collides with the generated
				// group name for the "leader"+"worker" grouping above
				// (SchedulingGroupName joins targets with "-").
				{
					Name:     "leader-worker",
					Replicas: 1,
					Template: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](1)},
					},
				},
			},
		},
	}

	_, err := buildWorkload(js)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "leader-worker")
}

// TestPolicyResolutionThroughBuildWorkload tests the policy resolution logic
// (leaf > global > default) by exercising it through buildWorkload, which
// delegates to the workloadbuilder library.
func TestPolicyResolutionThroughBuildWorkload(t *testing.T) {
	tests := map[string]struct {
		js        *jobset.JobSet
		wantGang  bool
		wantBasic bool
		wantMin   int32
		// templateIdx selects which PodGroupTemplate to check (default 0).
		templateIdx int
	}{
		"default Gang with computed minCount when no global or leaf": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "workers", Replicas: 4, Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](8)},
						}},
					},
				},
			},
			wantGang: true,
			wantMin:  32, // 8 * 4
		},
		"falls back to global policy when no leaf override": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 5},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "workers", Replicas: 4, Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](2)},
						}},
					},
				},
			},
			wantGang: true,
			wantMin:  5,
		},
		"falls back to global policy with zero minCount defaults to computed": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "workers", Replicas: 4, Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](2)},
						}},
					},
				},
			},
			wantGang: true,
			wantMin:  8, // 2 * 4
		},
		"sequenced startup fallback ignores explicit global gang minCount": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					StartupPolicy: &jobset.StartupPolicy{StartupPolicyOrder: jobset.InOrder},
					Scheduling: &jobset.JobSetScheduling{
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 99},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "driver", Replicas: 1, Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](1)},
						}},
						{Name: "workers", Replicas: 2, Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](3)},
						}},
					},
				},
			},
			templateIdx: 1, // check "workers" template
			wantGang:    true,
			wantMin:     6,
		},
		"falls back to global Basic policy when no leaf override": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{
						ReplicatedJobPolicies: []jobset.ReplicatedJobSchedulingPolicy{}, // force per-RJ mode
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Basic: &schedulingv1alpha3.BasicSchedulingPolicy{},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "workers", Replicas: 4, Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](2)},
						}},
					},
				},
			},
			wantBasic: true,
		},
		"leaf override takes priority over global": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 10},
						},
						ReplicatedJobPolicies: []jobset.ReplicatedJobSchedulingPolicy{
							{
								TargetReplicatedJob: []string{"driver"},
								SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
									Basic: &schedulingv1alpha3.BasicSchedulingPolicy{},
								},
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "driver", Replicas: 1, Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](1)},
						}},
					},
				},
			},
			wantBasic: true,
		},
		"explicit Gang with custom minCount from leaf": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{
						ReplicatedJobPolicies: []jobset.ReplicatedJobSchedulingPolicy{
							{
								TargetReplicatedJob: []string{"workers"},
								SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
									Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 16},
								},
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "workers", Replicas: 4, Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](8)},
						}},
					},
				},
			},
			wantGang: true,
			wantMin:  16,
		},
		"explicit Gang without minCount from leaf defaults to computed": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{
						ReplicatedJobPolicies: []jobset.ReplicatedJobSchedulingPolicy{
							{
								TargetReplicatedJob: []string{"workers"},
								SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
									Gang: &schedulingv1alpha3.GangSchedulingPolicy{},
								},
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "workers", Replicas: 2, Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](3)},
						}},
					},
				},
			},
			wantGang: true,
			wantMin:  6, // 3 * 2
		},
		"nil parallelism defaults to 1": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "workers", Replicas: 3, Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{},
						}},
					},
				},
			},
			wantGang: true,
			wantMin:  3, // 1 * 3
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			workload, err := buildWorkload(tc.js)
			require.NoError(t, err)
			require.True(t, len(workload.Spec.PodGroupTemplates) > tc.templateIdx,
				"expected at least %d templates, got %d", tc.templateIdx+1, len(workload.Spec.PodGroupTemplates))
			policy := workload.Spec.PodGroupTemplates[tc.templateIdx].SchedulingPolicy
			if tc.wantGang {
				require.NotNil(t, policy.Gang)
				assert.Nil(t, policy.Basic)
				assert.Equal(t, tc.wantMin, policy.Gang.MinCount)
			}
			if tc.wantBasic {
				require.NotNil(t, policy.Basic)
				assert.Nil(t, policy.Gang)
			}
		})
	}
}

func TestComputeMinCount(t *testing.T) {
	tests := map[string]struct {
		rjob *jobset.ReplicatedJob
		want int32
	}{
		"parallelism 4, replicas 8": {
			rjob: &jobset.ReplicatedJob{
				Replicas: 8,
				Template: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](4)},
				},
			},
			want: 32,
		},
		"nil parallelism defaults to 1": {
			rjob: &jobset.ReplicatedJob{
				Replicas: 5,
				Template: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{},
				},
			},
			want: 5,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, computeMinCount(tc.rjob))
		})
	}
}

// TestPodGroupTemplatesThroughBuildWorkload tests the PodGroupTemplate construction
// (constraints, disruption, naming) by exercising it through buildWorkload.
func TestPodGroupTemplatesThroughBuildWorkload(t *testing.T) {
	tests := map[string]struct {
		js             *jobset.JobSet
		wantName       string
		wantGang       bool
		hasConstraints bool
		hasDisruption  bool
		wantMinCount   int32
		templateIdx    int
	}{
		"default no leaf policy": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					Scheduling:     &jobset.JobSetScheduling{},
					ReplicatedJobs: []jobset.ReplicatedJob{{Name: "workers", Replicas: 2, Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](4)}}}},
				},
			},
			wantName:       "test", // top-level gang uses jobset name
			wantGang:       true,
			hasConstraints: false,
			hasDisruption:  false,
			wantMinCount:   8,
		},
		"with topology constraints and disruption from leaf": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{
						ReplicatedJobPolicies: []jobset.ReplicatedJobSchedulingPolicy{
							{
								TargetReplicatedJob: []string{"workers"},
								SchedulingConstraints: &schedulingv1alpha3.PodGroupSchedulingConstraints{
									Topology: []schedulingv1alpha3.TopologyConstraint{{Key: "topology.kubernetes.io/rack"}},
								},
								DisruptionMode: &schedulingv1alpha3.DisruptionMode{All: &schedulingv1alpha3.AllDisruptionMode{}},
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{{Name: "workers", Replicas: 2, Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](4)}}}},
				},
			},
			wantName:       "workers",
			wantGang:       true,
			hasConstraints: true,
			hasDisruption:  true,
			wantMinCount:   8,
		},
		"global constraints and disruption used as fallback": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{
						SchedulingConstraints: &schedulingv1alpha3.PodGroupSchedulingConstraints{
							Topology: []schedulingv1alpha3.TopologyConstraint{{Key: "topology.kubernetes.io/zone"}},
						},
						DisruptionMode: &schedulingv1alpha3.DisruptionMode{All: &schedulingv1alpha3.AllDisruptionMode{}},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{{Name: "workers", Replicas: 2, Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](4)}}}},
				},
			},
			wantName:       "test", // top-level gang
			wantGang:       true,
			hasConstraints: true,
			hasDisruption:  true,
			wantMinCount:   8,
		},
		"leaf constraints override global constraints": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{
						SchedulingConstraints: &schedulingv1alpha3.PodGroupSchedulingConstraints{
							Topology: []schedulingv1alpha3.TopologyConstraint{{Key: "topology.kubernetes.io/zone"}},
						},
						ReplicatedJobPolicies: []jobset.ReplicatedJobSchedulingPolicy{
							{
								TargetReplicatedJob: []string{"workers"},
								SchedulingConstraints: &schedulingv1alpha3.PodGroupSchedulingConstraints{
									Topology: []schedulingv1alpha3.TopologyConstraint{{Key: "topology.kubernetes.io/rack"}},
								},
							},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{{Name: "workers", Replicas: 2, Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](4)}}}},
				},
			},
			wantName:       "workers",
			wantGang:       true,
			hasConstraints: true,
			hasDisruption:  false,
			wantMinCount:   8,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			workload, err := buildWorkload(tc.js)
			require.NoError(t, err)
			require.True(t, len(workload.Spec.PodGroupTemplates) > tc.templateIdx)
			template := workload.Spec.PodGroupTemplates[tc.templateIdx]
			assert.Equal(t, tc.wantName, template.Name)
			if tc.wantGang {
				require.NotNil(t, template.SchedulingPolicy.Gang)
				assert.Equal(t, tc.wantMinCount, template.SchedulingPolicy.Gang.MinCount)
			}
			if tc.hasConstraints {
				require.NotNil(t, template.SchedulingConstraints)
				assert.Len(t, template.SchedulingConstraints.Topology, 1)
			} else {
				assert.Nil(t, template.SchedulingConstraints)
			}
			if tc.hasDisruption {
				require.NotNil(t, template.DisruptionMode)
			} else {
				assert.Nil(t, template.DisruptionMode)
			}
		})
	}
}

func TestPodGroupName(t *testing.T) {
	assert.Equal(t, "my-jobset-workers", podGroupName("my-jobset", "workers"))
	assert.Equal(t, "js-driver", podGroupName("js", "driver"))

	jobSetName := strings.Repeat("j", 63)
	replicatedJobName := strings.Repeat("r", 63)
	name := podGroupName(jobSetName, replicatedJobName)
	assert.Len(t, name, maxPodGroupNameLength)
	assert.Equal(t, name, podGroupName(jobSetName, replicatedJobName))
	assert.NotEqual(t, name, podGroupName(jobSetName, strings.Repeat("x", 63)))
}

func TestSchedulingPodGroupName(t *testing.T) {
	js := &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{Name: "jobset"},
		Spec: jobset.JobSetSpec{
			Scheduling: &jobset.JobSetScheduling{
				ReplicatedJobPolicies: []jobset.ReplicatedJobSchedulingPolicy{{TargetReplicatedJob: []string{"workers"}}},
			},
		},
	}
	assert.Equal(t, "jobset-workers", schedulingPodGroupName(js, "workers"))

	js.Spec.Scheduling = &jobset.JobSetScheduling{}
	assert.Equal(t, "jobset", schedulingPodGroupName(js, "jobset"))

	js.Spec.StartupPolicy = &jobset.StartupPolicy{StartupPolicyOrder: jobset.InOrder}
	assert.Equal(t, "jobset-workers", schedulingPodGroupName(js, "workers"))
}

func TestSingleReplicatedJobPerRJReferencesTheCreatedPodGroup(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.JobSetWorkloadAwareSchedulingAPI, true)
	js := &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{Name: "jobset", Namespace: "default"},
		Spec: jobset.JobSetSpec{
			Scheduling: &jobset.JobSetScheduling{
				ReplicatedJobPolicies: []jobset.ReplicatedJobSchedulingPolicy{{TargetReplicatedJob: []string{"workers"}}},
			},
			ReplicatedJobs: []jobset.ReplicatedJob{{
				Name: "workers", Replicas: 1,
				Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](1)}},
			}},
		},
	}

	job := constructJob(js, &js.Spec.ReplicatedJobs[0], 0)
	require.NotNil(t, job.Spec.Template.Spec.SchedulingGroup)
	require.NotNil(t, job.Spec.Template.Spec.SchedulingGroup.PodGroupName)
	assert.Equal(t, schedulingPodGroupName(js, "workers"), *job.Spec.Template.Spec.SchedulingGroup.PodGroupName)
}

func TestJobSchedulingItemName(t *testing.T) {
	assert.Equal(t, "workers-0", jobSchedulingItemName("workers", 0))
	assert.Equal(t, "workers-3", jobSchedulingItemName("workers", 3))
}

func TestSchedulingGroupTemplateName(t *testing.T) {
	js := &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{Name: "jobset"},
		Spec: jobset.JobSetSpec{
			Scheduling: &jobset.JobSetScheduling{
				ReplicatedJobPolicies: []jobset.ReplicatedJobSchedulingPolicy{
					{TargetReplicatedJob: []string{"driver", "aux"}},
					{
						TargetReplicatedJob: []string{"workers"},
						JobSchedulingPolicy: &jobset.JobSchedulingPolicy{},
					},
				},
			},
		},
	}

	// Ordinary group policy: joined group name, independent of jobIdx.
	assert.Equal(t, "driver-aux", schedulingGroupTemplateName(js, "driver", 0))
	assert.Equal(t, "driver-aux", schedulingGroupTemplateName(js, "aux", 7))

	// jobSchedulingPolicy: per-Job item name, varying with jobIdx.
	assert.Equal(t, "workers-0", schedulingGroupTemplateName(js, "workers", 0))
	assert.Equal(t, "workers-2", schedulingGroupTemplateName(js, "workers", 2))

	// Not covered by any policy: falls back to the ReplicatedJob's own name.
	assert.Equal(t, "solo", schedulingGroupTemplateName(js, "solo", 5))
}

// TestBuildWorkloadWithJobSchedulingPolicy exercises the Gang-of-Gangs
// per-Job scheduling model: jobSchedulingPolicy compiles one PodGroupTemplate
// per Job (replica) of the targeted ReplicatedJob, sized to that Job's own
// parallelism, instead of one PodGroupTemplate shared by every replica.
func TestBuildWorkloadWithJobSchedulingPolicy(t *testing.T) {
	js := &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{Name: "per-ij-js", Namespace: "default"},
		Spec: jobset.JobSetSpec{
			Scheduling: &jobset.JobSetScheduling{
				ReplicatedJobPolicies: []jobset.ReplicatedJobSchedulingPolicy{
					{
						TargetReplicatedJob: []string{"launcher"},
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{},
						},
					},
					{
						TargetReplicatedJob: []string{"worker"},
						JobSchedulingPolicy: &jobset.JobSchedulingPolicy{
							DisruptionMode: &schedulingv1alpha3.DisruptionMode{All: &schedulingv1alpha3.AllDisruptionMode{}},
						},
					},
				},
			},
			ReplicatedJobs: []jobset.ReplicatedJob{
				{Name: "launcher", Replicas: 1, Template: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](1)},
				}},
				{Name: "worker", Replicas: 2, Template: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](2)},
				}},
			},
		},
	}

	workload, err := buildWorkload(js)
	require.NoError(t, err)
	// 1 template for "launcher" + 2 templates (one per worker replica).
	require.Len(t, workload.Spec.PodGroupTemplates, 3)

	templatesByName := make(map[string]schedulingv1alpha3.PodGroupTemplate, len(workload.Spec.PodGroupTemplates))
	for _, tmpl := range workload.Spec.PodGroupTemplates {
		templatesByName[tmpl.Name] = tmpl
	}

	launcherTmpl, ok := templatesByName["launcher"]
	require.True(t, ok, "expected a shared launcher template")
	require.NotNil(t, launcherTmpl.SchedulingPolicy.Gang)
	assert.Equal(t, int32(1), launcherTmpl.SchedulingPolicy.Gang.MinCount)

	for jobIdx := 0; jobIdx < 2; jobIdx++ {
		name := jobSchedulingItemName("worker", jobIdx)
		tmpl, ok := templatesByName[name]
		require.True(t, ok, "expected per-Job template %s", name)
		require.NotNil(t, tmpl.SchedulingPolicy.Gang)
		// minCount is the Job's own parallelism (2), not summed across
		// both worker replicas (which would be 4).
		assert.Equal(t, int32(2), tmpl.SchedulingPolicy.Gang.MinCount)
		require.NotNil(t, tmpl.DisruptionMode)
		require.NotNil(t, tmpl.DisruptionMode.All)
	}
}

// TestBuildWorkloadJobSchedulingPolicyIgnoresGlobalGangMinCount is a
// regression test: the composite/global Gang minCount (e.g. auto-defaulted by
// the webhook to the JobSet's total pod count) must never leak into a
// jobSchedulingPolicy per-Job PodGroup, since that total is never the correct
// minCount for a single Job's own PodGroup. Each per-Job PodGroup must
// compute its own default from that Job's own parallelism instead.
func TestBuildWorkloadJobSchedulingPolicyIgnoresGlobalGangMinCount(t *testing.T) {
	js := &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{Name: "js", Namespace: "default"},
		Spec: jobset.JobSetSpec{
			Scheduling: &jobset.JobSetScheduling{
				// Simulates the webhook's defaultSchedulingGangMinCounts: an
				// explicit composite Gang policy sized to the JobSet's total pod
				// count (launcher: 1*1=1, worker: 2*2=4, total=5).
				SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
					Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 5},
				},
				ReplicatedJobPolicies: []jobset.ReplicatedJobSchedulingPolicy{
					{
						TargetReplicatedJob: []string{"launcher"},
					},
					{
						TargetReplicatedJob: []string{"worker"},
						// jobSchedulingPolicy sets no schedulingPolicy of its own,
						// so each worker Job's PodGroup must fall back to its own
						// computed parallelism (2), not the composite total (5).
						JobSchedulingPolicy: &jobset.JobSchedulingPolicy{},
					},
				},
			},
			ReplicatedJobs: []jobset.ReplicatedJob{
				{Name: "launcher", Replicas: 1, Template: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](1)},
				}},
				{Name: "worker", Replicas: 2, Template: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](2)},
				}},
			},
		},
	}

	workload, err := buildWorkload(js)
	require.NoError(t, err)
	require.Len(t, workload.Spec.PodGroupTemplates, 3)

	for _, tmpl := range workload.Spec.PodGroupTemplates {
		switch tmpl.Name {
		case "launcher":
			require.NotNil(t, tmpl.SchedulingPolicy.Gang)
			// launcher has no jobSchedulingPolicy, so it legitimately falls
			// back to (and inherits) the composite minCount.
			assert.Equal(t, int32(5), tmpl.SchedulingPolicy.Gang.MinCount)
		case jobSchedulingItemName("worker", 0), jobSchedulingItemName("worker", 1):
			require.NotNil(t, tmpl.SchedulingPolicy.Gang)
			assert.Equal(t, int32(2), tmpl.SchedulingPolicy.Gang.MinCount,
				"per-Job PodGroup %s must not inherit the composite minCount (5)", tmpl.Name)
		default:
			t.Fatalf("unexpected PodGroupTemplate name %q", tmpl.Name)
		}
	}
}

// TestJobSchedulingPolicyConstructJobReferencesPerJobPodGroup verifies that
// each Job (replica) of a ReplicatedJob using jobSchedulingPolicy is wired up
// to its own independent PodGroup, matching that Job's own generated name.
func TestJobSchedulingPolicyConstructJobReferencesPerJobPodGroup(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.JobSetWorkloadAwareSchedulingAPI, true)
	js := &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{Name: "jobset", Namespace: "default"},
		Spec: jobset.JobSetSpec{
			Scheduling: &jobset.JobSetScheduling{
				ReplicatedJobPolicies: []jobset.ReplicatedJobSchedulingPolicy{
					{
						TargetReplicatedJob: []string{"worker"},
						JobSchedulingPolicy: &jobset.JobSchedulingPolicy{},
					},
				},
			},
			ReplicatedJobs: []jobset.ReplicatedJob{{
				Name: "worker", Replicas: 2,
				Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](2)}},
			}},
		},
	}

	podGroupNames := make(map[string]bool)
	for jobIdx := 0; jobIdx < 2; jobIdx++ {
		job := constructJob(js, &js.Spec.ReplicatedJobs[0], jobIdx)
		require.NotNil(t, job.Spec.Template.Spec.SchedulingGroup)
		require.NotNil(t, job.Spec.Template.Spec.SchedulingGroup.PodGroupName)
		pgName := *job.Spec.Template.Spec.SchedulingGroup.PodGroupName
		// The per-Job PodGroup name must equal the Job's own generated name,
		// since both are derived from "<jsName>-<rjobName>-<jobIdx>".
		assert.Equal(t, job.Name, pgName)
		assert.False(t, podGroupNames[pgName], "PodGroup name %s must be unique per Job", pgName)
		podGroupNames[pgName] = true
	}
	assert.Len(t, podGroupNames, 2)
}

func TestUseTopLevelGang(t *testing.T) {
	tests := map[string]struct {
		scheduling *jobset.JobSetScheduling
		want       bool
	}{
		"nil scheduling": {
			scheduling: nil,
			want:       false,
		},
		"empty scheduling defaults to gang": {
			scheduling: &jobset.JobSetScheduling{},
			want:       true,
		},
		"explicit gang policy with no leaf overrides": {
			scheduling: &jobset.JobSetScheduling{
				SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
					Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 10},
				},
			},
			want: true,
		},
		"explicit basic policy at top level": {
			scheduling: &jobset.JobSetScheduling{
				SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
					Basic: &schedulingv1alpha3.BasicSchedulingPolicy{},
				},
			},
			want: false,
		},
		"gang policy with leaf overrides": {
			scheduling: &jobset.JobSetScheduling{
				SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
					Gang: &schedulingv1alpha3.GangSchedulingPolicy{},
				},
				ReplicatedJobPolicies: []jobset.ReplicatedJobSchedulingPolicy{
					{TargetReplicatedJob: []string{"workers"}},
				},
			},
			want: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, UseTopLevelGang(tc.scheduling))
		})
	}
}

func TestTotalMinCount(t *testing.T) {
	tests := map[string]struct {
		js   *jobset.JobSet
		want int32
	}{
		"sums across all replicated jobs": {
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "driver",
							Replicas: 1,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](1)},
							},
						},
						{
							Name:     "workers",
							Replicas: 4,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](8)},
							},
						},
					},
				},
			},
			want: 33, // 1*1 + 8*4
		},
		"explicit minCount from top-level policy": {
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 10},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{
							Name:     "workers",
							Replicas: 4,
							Template: batchv1.JobTemplateSpec{
								Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](8)},
							},
						},
					},
				},
			},
			want: 10,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, totalMinCount(tc.js))
		})
	}
}

func TestHasSequencedStartup(t *testing.T) {
	tests := map[string]struct {
		js   *jobset.JobSet
		want bool
	}{
		"no startup policy or depends on": {
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "workers"},
					},
				},
			},
			want: false,
		},
		"InOrder startup policy": {
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					StartupPolicy: &jobset.StartupPolicy{
						StartupPolicyOrder: jobset.InOrder,
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "driver"},
						{Name: "workers"},
					},
				},
			},
			want: true,
		},
		"AnyOrder startup policy": {
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					StartupPolicy: &jobset.StartupPolicy{
						StartupPolicyOrder: jobset.AnyOrder,
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "workers"},
					},
				},
			},
			want: false,
		},
		"DependsOn on a ReplicatedJob": {
			js: &jobset.JobSet{
				Spec: jobset.JobSetSpec{
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "driver"},
						{
							Name: "workers",
							DependsOn: []jobset.DependsOn{
								{Name: "driver", Status: jobset.DependencyReady},
							},
						},
					},
				},
			},
			want: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, HasSequencedStartup(tc.js))
		})
	}
}

func TestWorkloadNeedsRecreation(t *testing.T) {
	tests := map[string]struct {
		existing *schedulingv1alpha3.Workload
		desired  *schedulingv1alpha3.Workload
		want     bool
	}{
		"identical workloads": {
			existing: &schedulingv1alpha3.Workload{
				Spec: schedulingv1alpha3.WorkloadSpec{
					PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 4},
						}},
					},
				},
			},
			desired: &schedulingv1alpha3.Workload{
				Spec: schedulingv1alpha3.WorkloadSpec{
					PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 4},
						}},
					},
				},
			},
			want: false,
		},
		"minCount changed due to elastic scaling": {
			// Gang.MinCount is mutable in the upstream API, so a minCount-only
			// change is patched in place (see TestWorkloadWithPatchedMinCounts)
			// rather than triggering a delete/recreate.
			existing: &schedulingv1alpha3.Workload{
				Spec: schedulingv1alpha3.WorkloadSpec{
					PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 4},
						}},
					},
				},
			},
			desired: &schedulingv1alpha3.Workload{
				Spec: schedulingv1alpha3.WorkloadSpec{
					PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 8},
						}},
					},
				},
			},
			want: false,
		},
		"template count changed": {
			existing: &schedulingv1alpha3.Workload{
				Spec: schedulingv1alpha3.WorkloadSpec{
					PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{
						{Name: "js"},
					},
				},
			},
			desired: &schedulingv1alpha3.Workload{
				Spec: schedulingv1alpha3.WorkloadSpec{
					PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{
						{Name: "driver"},
						{Name: "workers"},
					},
				},
			},
			want: true,
		},
		"policy type changed from gang to basic": {
			existing: &schedulingv1alpha3.Workload{
				Spec: schedulingv1alpha3.WorkloadSpec{
					PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 4},
						}},
					},
				},
			},
			desired: &schedulingv1alpha3.Workload{
				Spec: schedulingv1alpha3.WorkloadSpec{
					PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1alpha3.PodGroupSchedulingPolicy{
							Basic: &schedulingv1alpha3.BasicSchedulingPolicy{},
						}},
					},
				},
			},
			want: true,
		},
		"priority class changed": {
			existing: &schedulingv1alpha3.Workload{
				Spec: schedulingv1alpha3.WorkloadSpec{PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{{
					Name: "js", PriorityClassName: "low",
					SchedulingPolicy: schedulingv1alpha3.PodGroupSchedulingPolicy{Basic: &schedulingv1alpha3.BasicSchedulingPolicy{}},
				}}},
			},
			desired: &schedulingv1alpha3.Workload{
				Spec: schedulingv1alpha3.WorkloadSpec{PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{{
					Name: "js", PriorityClassName: "high",
					SchedulingPolicy: schedulingv1alpha3.PodGroupSchedulingPolicy{Basic: &schedulingv1alpha3.BasicSchedulingPolicy{}},
				}}},
			},
			want: true,
		},
		"template name changed": {
			existing: &schedulingv1alpha3.Workload{
				Spec: schedulingv1alpha3.WorkloadSpec{
					PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{
						{Name: "old-name"},
					},
				},
			},
			desired: &schedulingv1alpha3.Workload{
				Spec: schedulingv1alpha3.WorkloadSpec{
					PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{
						{Name: "new-name"},
					},
				},
			},
			want: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, workloadNeedsRecreation(tc.existing, tc.desired))
		})
	}
}

func TestWorkloadWithPatchedMinCounts(t *testing.T) {
	tests := map[string]struct {
		existing    *schedulingv1alpha3.Workload
		desired     *schedulingv1alpha3.Workload
		wantChanged bool
		wantCount   int32
	}{
		"minCount increased by scale up": {
			existing: &schedulingv1alpha3.Workload{
				Spec: schedulingv1alpha3.WorkloadSpec{
					PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 4},
						}},
					},
				},
			},
			desired: &schedulingv1alpha3.Workload{
				Spec: schedulingv1alpha3.WorkloadSpec{
					PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 8},
						}},
					},
				},
			},
			wantChanged: true,
			wantCount:   8,
		},
		"minCount unchanged": {
			existing: &schedulingv1alpha3.Workload{
				Spec: schedulingv1alpha3.WorkloadSpec{
					PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 4},
						}},
					},
				},
			},
			desired: &schedulingv1alpha3.Workload{
				Spec: schedulingv1alpha3.WorkloadSpec{
					PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 4},
						}},
					},
				},
			},
			wantChanged: false,
			wantCount:   4,
		},
		"basic policy has no minCount to patch": {
			existing: &schedulingv1alpha3.Workload{
				Spec: schedulingv1alpha3.WorkloadSpec{
					PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1alpha3.PodGroupSchedulingPolicy{
							Basic: &schedulingv1alpha3.BasicSchedulingPolicy{},
						}},
					},
				},
			},
			desired: &schedulingv1alpha3.Workload{
				Spec: schedulingv1alpha3.WorkloadSpec{
					PodGroupTemplates: []schedulingv1alpha3.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1alpha3.PodGroupSchedulingPolicy{
							Basic: &schedulingv1alpha3.BasicSchedulingPolicy{},
						}},
					},
				},
			},
			wantChanged: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			patched, changed := workloadWithPatchedMinCounts(tc.existing, tc.desired)
			assert.Equal(t, tc.wantChanged, changed)
			if tc.wantChanged {
				require.NotNil(t, patched.Spec.PodGroupTemplates[0].SchedulingPolicy.Gang)
				assert.Equal(t, tc.wantCount, patched.Spec.PodGroupTemplates[0].SchedulingPolicy.Gang.MinCount)
			}
			// The original existing object must never be mutated in place.
			if tc.existing.Spec.PodGroupTemplates[0].SchedulingPolicy.Gang != nil {
				assert.NotSame(t, &tc.existing.Spec.PodGroupTemplates[0], &patched.Spec.PodGroupTemplates[0])
			}
		})
	}
}

func TestBuildWorkloadWithSequencedStartup(t *testing.T) {
	tests := map[string]struct {
		js               *jobset.JobSet
		wantTemplates    int
		wantTemplateMins []int32
	}{
		"DependsOn forces per-RJ templates even with top-level gang": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "depends-js", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "driver", Replicas: 1, Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](1)},
						}},
						{Name: "workers", Replicas: 4, DependsOn: []jobset.DependsOn{
							{Name: "driver", Status: jobset.DependencyReady},
						}, Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](2)},
						}},
					},
				},
			},
			wantTemplates:    2,
			wantTemplateMins: []int32{1, 8},
		},
		"InOrder StartupPolicy forces per-RJ templates": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "startup-js", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					StartupPolicy: &jobset.StartupPolicy{
						StartupPolicyOrder: jobset.InOrder,
					},
					Scheduling: &jobset.JobSetScheduling{},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "driver", Replicas: 1, Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](1)},
						}},
						{Name: "workers", Replicas: 2, Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](4)},
						}},
					},
				},
			},
			wantTemplates:    2,
			wantTemplateMins: []int32{1, 8},
		},
		"sequenced startup does not inherit explicit top-level gang minCount": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "startup-mincount-js", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					StartupPolicy: &jobset.StartupPolicy{StartupPolicyOrder: jobset.InOrder},
					Scheduling: &jobset.JobSetScheduling{
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 99},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "driver", Replicas: 1, Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](1)},
						}},
						{Name: "workers", Replicas: 2, Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](4)},
						}},
					},
				},
			},
			wantTemplates:    2,
			wantTemplateMins: []int32{1, 8},
		},
		"AnyOrder StartupPolicy allows top-level gang": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "anyorder-js", Namespace: "default"},
				Spec: jobset.JobSetSpec{
					StartupPolicy: &jobset.StartupPolicy{
						StartupPolicyOrder: jobset.AnyOrder,
					},
					Scheduling: &jobset.JobSetScheduling{},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "driver", Replicas: 1, Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](1)},
						}},
						{Name: "workers", Replicas: 2, Template: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](4)},
						}},
					},
				},
			},
			wantTemplates:    1, // top-level gang is allowed
			wantTemplateMins: []int32{9},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			workload, err := buildWorkload(tc.js)
			require.NoError(t, err)
			assert.Len(t, workload.Spec.PodGroupTemplates, tc.wantTemplates)
			if tc.wantTemplateMins != nil {
				var got []int32
				for _, template := range workload.Spec.PodGroupTemplates {
					if template.SchedulingPolicy.Gang != nil {
						got = append(got, template.SchedulingPolicy.Gang.MinCount)
					}
				}
				assert.ElementsMatch(t, tc.wantTemplateMins, got)
			}
		})
	}
}

// TestBuildTopLevelGangThroughBuildWorkload tests top-level gang template construction
// through buildWorkload.
func TestBuildTopLevelGangThroughBuildWorkload(t *testing.T) {
	tests := map[string]struct {
		js             *jobset.JobSet
		wantName       string
		wantMinCount   int32
		hasConstraints bool
		hasDisruption  bool
	}{
		"default gang across all RJs": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "my-js"},
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "driver", Replicas: 1, Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](1)}}},
						{Name: "workers", Replicas: 4, Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](2)}}},
					},
				},
			},
			wantName:     "my-js",
			wantMinCount: 9, // 1*1 + 2*4
		},
		"explicit minCount from policy": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-js"},
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{MinCount: 5},
						},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "workers", Replicas: 4, Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](8)}}},
					},
				},
			},
			wantName:     "custom-js",
			wantMinCount: 5,
		},
		"with global constraints and disruption": {
			js: &jobset.JobSet{
				ObjectMeta: metav1.ObjectMeta{Name: "constrained-js"},
				Spec: jobset.JobSetSpec{
					Scheduling: &jobset.JobSetScheduling{
						SchedulingConstraints: &schedulingv1alpha3.PodGroupSchedulingConstraints{
							Topology: []schedulingv1alpha3.TopologyConstraint{{Key: "topology.kubernetes.io/zone"}},
						},
						DisruptionMode: &schedulingv1alpha3.DisruptionMode{All: &schedulingv1alpha3.AllDisruptionMode{}},
					},
					ReplicatedJobs: []jobset.ReplicatedJob{
						{Name: "workers", Replicas: 2, Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To[int32](3)}}},
					},
				},
			},
			wantName:       "constrained-js",
			wantMinCount:   6,
			hasConstraints: true,
			hasDisruption:  true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			workload, err := buildWorkload(tc.js)
			require.NoError(t, err)
			require.Len(t, workload.Spec.PodGroupTemplates, 1)
			template := workload.Spec.PodGroupTemplates[0]
			assert.Equal(t, tc.wantName, template.Name)
			require.NotNil(t, template.SchedulingPolicy.Gang)
			assert.Equal(t, tc.wantMinCount, template.SchedulingPolicy.Gang.MinCount)
			if tc.hasConstraints {
				require.NotNil(t, template.SchedulingConstraints)
			} else {
				assert.Nil(t, template.SchedulingConstraints)
			}
			if tc.hasDisruption {
				require.NotNil(t, template.DisruptionMode)
			} else {
				assert.Nil(t, template.DisruptionMode)
			}
		})
	}
}
