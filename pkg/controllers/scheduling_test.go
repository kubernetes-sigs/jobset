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
	schedulingv1beta1 "k8s.io/api/scheduling/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/features"
)

// assertHashedName verifies that name has the form "<prefix>-<hash>", where
// hash is a podGroupNameHashLength lowercase-hex identity hash. Every generated
// scheduling name (Workload, PodGroupTemplate/WorkloadItem, PodGroup) carries
// this suffix for determinism and cross-controller collision resistance, so
// tests assert the readable prefix and hash shape rather than a hardcoded hash.
func assertHashedName(t *testing.T, prefix, name string) {
	t.Helper()
	require.Truef(t, strings.HasPrefix(name, prefix+"-"), "name %q should start with %q-", name, prefix)
	hash := strings.TrimPrefix(name, prefix+"-")
	require.Lenf(t, hash, podGroupNameHashLength, "hash suffix of %q", name)
	for _, c := range hash {
		require.Truef(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"hash suffix %q of %q must be lowercase hex", hash, name)
	}
}

func TestBuildWorkload(t *testing.T) {
	tests := map[string]struct {
		js                *jobset.JobSet
		wantTemplates     int
		wantName          string
		wantControllerRef *schedulingv1beta1.TypedLocalObjectReference
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
			wantControllerRef: &schedulingv1beta1.TypedLocalObjectReference{
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
						ReplicatedJobs: []jobset.ReplicatedJobScheduling{
							{
								TargetReplicatedJobs: []string{"driver"},
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
			assertHashedName(t, tc.wantName, workload.Name)
			assert.Equal(t, workloadName(tc.js), workload.Name)
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

// TestBuildWorkloadGroupNameCollision verifies that a ReplicatedJob whose name
// matches the human-readable name of a replicatedJobs grouping
// (SchedulingGroupName joins targets with "-") no longer produces colliding
// PodGroupTemplate names: every generated name carries a hash over the source
// object's kind/namespace/identity, so the group and the ReplicatedJob compile
// into distinct templates and buildWorkload succeeds.
func TestBuildWorkloadGroupNameCollision(t *testing.T) {
	js := &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{Name: "collide-js", Namespace: "default"},
		Spec: jobset.JobSetSpec{
			Scheduling: &jobset.JobSetScheduling{
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{
						TargetReplicatedJobs: []string{"leader", "worker"},
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
				// This ReplicatedJob's own name matches the generated group name
				// for the "leader"+"worker" grouping above (SchedulingGroupName
				// joins targets with "-"), but the source-identity hash keeps the
				// two generated PodGroupTemplate names distinct.
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

	workload, err := buildWorkload(js)
	require.NoError(t, err)

	// The grouped ["leader","worker"] template and the "leader-worker"
	// ReplicatedJob template share the "leader-worker" prefix but hash to
	// distinct, non-colliding names.
	groupName := groupTemplateName(js, []string{"leader", "worker"})
	rjName := perRJTemplateName(js, "leader-worker")
	assert.NotEqual(t, groupName, rjName)

	names := make(map[string]bool, len(workload.Spec.PodGroupTemplates))
	for _, tmpl := range workload.Spec.PodGroupTemplates {
		assert.Falsef(t, names[tmpl.Name], "duplicate PodGroupTemplate name %q", tmpl.Name)
		names[tmpl.Name] = true
	}
	assert.Contains(t, names, groupName)
	assert.Contains(t, names, rjName)
}

// TestSchedulingIdentityHashResistsCollisions verifies the identity hash folded
// into every generated scheduling name is deterministic and distinguishes the
// source object's kind, namespace, and name. This is what prevents a
// JobSet-owned Workload or PodGroup from colliding with an object another
// controller derived from a different source that happens to share the same
// human-readable prefix.
func TestSchedulingIdentityHashResistsCollisions(t *testing.T) {
	// Deterministic: identical inputs always hash the same.
	base := schedulingIdentityHash(jobSetSourceKind, "default", "foo")
	assert.Equal(t, base, schedulingIdentityHash(jobSetSourceKind, "default", "foo"))
	assert.Len(t, base, podGroupNameHashLength)

	// Distinguishing: changing any component of the identity changes the hash.
	cases := map[string]struct{ kind, ns, name string }{
		"different kind":      {replicatedJobSourceKind, "default", "foo"},
		"different namespace": {jobSetSourceKind, "other", "foo"},
		"different name":      {jobSetSourceKind, "default", "bar"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.NotEqual(t, base, schedulingIdentityHash(tc.kind, tc.ns, tc.name))
		})
	}
}

// TestGeneratedNamesResistPrefixCollisions verifies that pairs of generated
// scheduling names which share the same human-readable prefix — and so would
// have collided before hashing — resolve to distinct names because the hash
// encodes the differing source identity (kind, owning JobSet, namespace).
func TestGeneratedNamesResistPrefixCollisions(t *testing.T) {
	jsFoo := &jobset.JobSet{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "default"}}
	jsA := &jobset.JobSet{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "default"}}
	jsB := &jobset.JobSet{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "default"}}
	jsFooOther := &jobset.JobSet{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "other"}}

	cases := []struct {
		name   string
		prefix string
		a, b   string
	}{
		{
			// A JobSet's top-level template (kind JobSet) vs a same-named
			// ReplicatedJob's template (kind ReplicatedJob).
			name:   "top-level template vs same-named replicatedJob template",
			prefix: "foo",
			a:      topLevelTemplateName(jsFoo),
			b:      perRJTemplateName(jsFoo, "foo"),
		},
		{
			// The same ReplicatedJob name under two different JobSets: the
			// owning JobSet name is folded into the hashed identity.
			name:   "same replicatedJob name across two JobSets",
			prefix: "worker",
			a:      perRJTemplateName(jsA, "worker"),
			b:      perRJTemplateName(jsB, "worker"),
		},
		{
			// The same JobSet name in two namespaces yields distinct Workloads.
			name:   "same JobSet name across namespaces",
			prefix: "foo",
			a:      workloadName(jsFoo),
			b:      workloadName(jsFooOther),
		},
		{
			// A grouping of ["a","b"] joins to "a-b"; a ReplicatedJob literally
			// named "a-b" shares the prefix but hashes differently.
			name:   "group name vs same-named replicatedJob",
			prefix: "a-b",
			a:      groupTemplateName(jsFoo, []string{"a", "b"}),
			b:      perRJTemplateName(jsFoo, "a-b"),
		},
		{
			// A per-Job template "worker-0" vs a ReplicatedJob literally named
			// "worker-0".
			name:   "per-Job template vs same-named replicatedJob",
			prefix: "worker-0",
			a:      perJobTemplateName(jsFoo, "worker", 0),
			b:      perRJTemplateName(jsFoo, "worker-0"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertHashedName(t, tc.prefix, tc.a)
			assertHashedName(t, tc.prefix, tc.b)
			assert.NotEqualf(t, tc.a, tc.b, "names sharing prefix %q must not collide", tc.prefix)
		})
	}
}

// TestPodGroupNamesResistCollisions verifies that PodGroup object names for
// different ReplicatedJobs across different JobSets do not collide even when
// their readable "<jobSet>-<replicatedJob>" forms are identical, because the
// PodGroupTemplate name each PodGroup is built from carries a source-identity
// hash.
func TestPodGroupNamesResistCollisions(t *testing.T) {
	jsAB := &jobset.JobSet{ObjectMeta: metav1.ObjectMeta{Name: "a-b", Namespace: "default"}}
	jsA := &jobset.JobSet{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "default"}}

	// JobSet "a-b" / ReplicatedJob "c" and JobSet "a" / ReplicatedJob "b-c"
	// both read as "a-b-c-..." but must resolve to distinct PodGroup names.
	pg1 := podGroupName(jsAB.Name, perRJTemplateName(jsAB, "c"))
	pg2 := podGroupName(jsA.Name, perRJTemplateName(jsA, "b-c"))

	assert.Truef(t, strings.HasPrefix(pg1, "a-b-c-"), "pg1 %q should read as a-b-c-*", pg1)
	assert.Truef(t, strings.HasPrefix(pg2, "a-b-c-"), "pg2 %q should read as a-b-c-*", pg2)
	assert.NotEqual(t, pg1, pg2, "PodGroups with identical readable names must not collide")
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
						ReplicatedJobs: []jobset.ReplicatedJobScheduling{}, // force per-RJ mode
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
						ReplicatedJobs: []jobset.ReplicatedJobScheduling{
							{
								TargetReplicatedJobs: []string{"driver"},
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
						ReplicatedJobs: []jobset.ReplicatedJobScheduling{
							{
								TargetReplicatedJobs: []string{"workers"},
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
						ReplicatedJobs: []jobset.ReplicatedJobScheduling{
							{
								TargetReplicatedJobs: []string{"workers"},
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
						ReplicatedJobs: []jobset.ReplicatedJobScheduling{
							{
								TargetReplicatedJobs: []string{"workers"},
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
						ReplicatedJobs: []jobset.ReplicatedJobScheduling{
							{
								TargetReplicatedJobs: []string{"workers"},
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
			assertHashedName(t, tc.wantName, template.Name)
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
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{{TargetReplicatedJobs: []string{"workers"}}},
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
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{{TargetReplicatedJobs: []string{"workers"}}},
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
	wantPodGroup := schedulingPodGroupName(js, schedulingGroupTemplateName(js, "workers", 0))
	assert.Equal(t, wantPodGroup, *job.Spec.Template.Spec.SchedulingGroup.PodGroupName)
}

func TestPerJobTemplateName(t *testing.T) {
	js := &jobset.JobSet{ObjectMeta: metav1.ObjectMeta{Name: "jobset", Namespace: "default"}}

	// Human-readable "<rjob>-<jobIdx>" prefix plus a deterministic identity hash.
	assertHashedName(t, "workers-0", perJobTemplateName(js, "workers", 0))
	assertHashedName(t, "workers-3", perJobTemplateName(js, "workers", 3))

	// Deterministic and distinct per Job index.
	assert.Equal(t, perJobTemplateName(js, "workers", 0), perJobTemplateName(js, "workers", 0))
	assert.NotEqual(t, perJobTemplateName(js, "workers", 0), perJobTemplateName(js, "workers", 1))
}

func TestSchedulingGroupTemplateName(t *testing.T) {
	js := &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{Name: "jobset"},
		Spec: jobset.JobSetSpec{
			Scheduling: &jobset.JobSetScheduling{
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{TargetReplicatedJobs: []string{"driver", "aux"}},
					{
						TargetReplicatedJobs: []string{"workers"},
						Job:                  &jobset.JobScheduling{},
					},
				},
			},
		},
	}

	// Ordinary group policy: shared group template name, independent of jobIdx.
	assert.Equal(t, groupTemplateName(js, []string{"driver", "aux"}), schedulingGroupTemplateName(js, "driver", 0))
	assert.Equal(t, groupTemplateName(js, []string{"driver", "aux"}), schedulingGroupTemplateName(js, "aux", 7))

	// job: per-Job template name, varying with jobIdx.
	assert.Equal(t, perJobTemplateName(js, "workers", 0), schedulingGroupTemplateName(js, "workers", 0))
	assert.Equal(t, perJobTemplateName(js, "workers", 2), schedulingGroupTemplateName(js, "workers", 2))

	// Not covered by any policy: falls back to the ReplicatedJob's own template.
	assert.Equal(t, perRJTemplateName(js, "solo"), schedulingGroupTemplateName(js, "solo", 5))
}

// TestBuildWorkloadWithJobScheduling exercises the Gang-of-Gangs
// per-Job scheduling model: job compiles one PodGroupTemplate
// per Job (replica) of the targeted ReplicatedJob, sized to that Job's own
// parallelism, instead of one PodGroupTemplate shared by every replica.
func TestBuildWorkloadWithJobScheduling(t *testing.T) {
	js := &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{Name: "per-ij-js", Namespace: "default"},
		Spec: jobset.JobSetSpec{
			Scheduling: &jobset.JobSetScheduling{
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{
						TargetReplicatedJobs: []string{"launcher"},
						SchedulingPolicy: &schedulingv1alpha3.PodGroupSchedulingPolicy{
							Gang: &schedulingv1alpha3.GangSchedulingPolicy{},
						},
					},
					{
						TargetReplicatedJobs: []string{"worker"},
						Job: &jobset.JobScheduling{
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

	templatesByName := make(map[string]schedulingv1beta1.PodGroupTemplate, len(workload.Spec.PodGroupTemplates))
	for _, tmpl := range workload.Spec.PodGroupTemplates {
		templatesByName[tmpl.Name] = tmpl
	}

	launcherTmpl, ok := templatesByName[groupTemplateName(js, []string{"launcher"})]
	require.True(t, ok, "expected a shared launcher template")
	require.NotNil(t, launcherTmpl.SchedulingPolicy.Gang)
	assert.Equal(t, int32(1), launcherTmpl.SchedulingPolicy.Gang.MinCount)

	for jobIdx := 0; jobIdx < 2; jobIdx++ {
		name := perJobTemplateName(js, "worker", jobIdx)
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

// TestBuildWorkloadJobSchedulingIgnoresGlobalGangMinCount is a
// regression test: the composite/global Gang minCount (e.g. auto-defaulted by
// the webhook to the JobSet's total pod count) must never leak into a
// job per-Job PodGroup, since that total is never the correct
// minCount for a single Job's own PodGroup. Each per-Job PodGroup must
// compute its own default from that Job's own parallelism instead.
func TestBuildWorkloadJobSchedulingIgnoresGlobalGangMinCount(t *testing.T) {
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
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{
						TargetReplicatedJobs: []string{"launcher"},
					},
					{
						TargetReplicatedJobs: []string{"worker"},
						// job sets no schedulingPolicy of its own,
						// so each worker Job's PodGroup must fall back to its own
						// computed parallelism (2), not the composite total (5).
						Job: &jobset.JobScheduling{},
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
		case groupTemplateName(js, []string{"launcher"}):
			require.NotNil(t, tmpl.SchedulingPolicy.Gang)
			// launcher has no job, so it legitimately falls
			// back to (and inherits) the composite minCount.
			assert.Equal(t, int32(5), tmpl.SchedulingPolicy.Gang.MinCount)
		case perJobTemplateName(js, "worker", 0), perJobTemplateName(js, "worker", 1):
			require.NotNil(t, tmpl.SchedulingPolicy.Gang)
			assert.Equal(t, int32(2), tmpl.SchedulingPolicy.Gang.MinCount,
				"per-Job PodGroup %s must not inherit the composite minCount (5)", tmpl.Name)
		default:
			t.Fatalf("unexpected PodGroupTemplate name %q", tmpl.Name)
		}
	}
}

// TestJobSchedulingConstructJobReferencesPerJobPodGroup verifies that
// each Job (replica) of a ReplicatedJob using job is wired up
// to its own independent PodGroup, matching that Job's own generated name.
func TestJobSchedulingConstructJobReferencesPerJobPodGroup(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.JobSetWorkloadAwareSchedulingAPI, true)
	js := &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{Name: "jobset", Namespace: "default"},
		Spec: jobset.JobSetSpec{
			Scheduling: &jobset.JobSetScheduling{
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{
						TargetReplicatedJobs: []string{"worker"},
						Job:                  &jobset.JobScheduling{},
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
		// The per-Job PodGroup name is derived from the Job's per-Job
		// PodGroupTemplate name (which carries the source-identity hash),
		// combined with the JobSet name.
		assert.Equal(t, schedulingPodGroupName(js, perJobTemplateName(js, "worker", jobIdx)), pgName)
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
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{TargetReplicatedJobs: []string{"workers"}},
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
		existing *schedulingv1beta1.Workload
		desired  *schedulingv1beta1.Workload
		want     bool
	}{
		"identical workloads": {
			existing: &schedulingv1beta1.Workload{
				Spec: schedulingv1beta1.WorkloadSpec{
					PodGroupTemplates: []schedulingv1beta1.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1beta1.PodGroupSchedulingPolicy{
							Gang: &schedulingv1beta1.GangSchedulingPolicy{MinCount: 4},
						}},
					},
				},
			},
			desired: &schedulingv1beta1.Workload{
				Spec: schedulingv1beta1.WorkloadSpec{
					PodGroupTemplates: []schedulingv1beta1.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1beta1.PodGroupSchedulingPolicy{
							Gang: &schedulingv1beta1.GangSchedulingPolicy{MinCount: 4},
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
			existing: &schedulingv1beta1.Workload{
				Spec: schedulingv1beta1.WorkloadSpec{
					PodGroupTemplates: []schedulingv1beta1.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1beta1.PodGroupSchedulingPolicy{
							Gang: &schedulingv1beta1.GangSchedulingPolicy{MinCount: 4},
						}},
					},
				},
			},
			desired: &schedulingv1beta1.Workload{
				Spec: schedulingv1beta1.WorkloadSpec{
					PodGroupTemplates: []schedulingv1beta1.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1beta1.PodGroupSchedulingPolicy{
							Gang: &schedulingv1beta1.GangSchedulingPolicy{MinCount: 8},
						}},
					},
				},
			},
			want: false,
		},
		"template count changed": {
			existing: &schedulingv1beta1.Workload{
				Spec: schedulingv1beta1.WorkloadSpec{
					PodGroupTemplates: []schedulingv1beta1.PodGroupTemplate{
						{Name: "js"},
					},
				},
			},
			desired: &schedulingv1beta1.Workload{
				Spec: schedulingv1beta1.WorkloadSpec{
					PodGroupTemplates: []schedulingv1beta1.PodGroupTemplate{
						{Name: "driver"},
						{Name: "workers"},
					},
				},
			},
			want: true,
		},
		"policy type changed from gang to basic": {
			existing: &schedulingv1beta1.Workload{
				Spec: schedulingv1beta1.WorkloadSpec{
					PodGroupTemplates: []schedulingv1beta1.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1beta1.PodGroupSchedulingPolicy{
							Gang: &schedulingv1beta1.GangSchedulingPolicy{MinCount: 4},
						}},
					},
				},
			},
			desired: &schedulingv1beta1.Workload{
				Spec: schedulingv1beta1.WorkloadSpec{
					PodGroupTemplates: []schedulingv1beta1.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1beta1.PodGroupSchedulingPolicy{
							Basic: &schedulingv1beta1.BasicSchedulingPolicy{},
						}},
					},
				},
			},
			want: true,
		},
		"priority class changed": {
			existing: &schedulingv1beta1.Workload{
				Spec: schedulingv1beta1.WorkloadSpec{PodGroupTemplates: []schedulingv1beta1.PodGroupTemplate{{
					Name: "js", PriorityClassName: "low",
					SchedulingPolicy: schedulingv1beta1.PodGroupSchedulingPolicy{Basic: &schedulingv1beta1.BasicSchedulingPolicy{}},
				}}},
			},
			desired: &schedulingv1beta1.Workload{
				Spec: schedulingv1beta1.WorkloadSpec{PodGroupTemplates: []schedulingv1beta1.PodGroupTemplate{{
					Name: "js", PriorityClassName: "high",
					SchedulingPolicy: schedulingv1beta1.PodGroupSchedulingPolicy{Basic: &schedulingv1beta1.BasicSchedulingPolicy{}},
				}}},
			},
			want: true,
		},
		"template name changed": {
			existing: &schedulingv1beta1.Workload{
				Spec: schedulingv1beta1.WorkloadSpec{
					PodGroupTemplates: []schedulingv1beta1.PodGroupTemplate{
						{Name: "old-name"},
					},
				},
			},
			desired: &schedulingv1beta1.Workload{
				Spec: schedulingv1beta1.WorkloadSpec{
					PodGroupTemplates: []schedulingv1beta1.PodGroupTemplate{
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
		existing    *schedulingv1beta1.Workload
		desired     *schedulingv1beta1.Workload
		wantChanged bool
		wantCount   int32
	}{
		"minCount increased by scale up": {
			existing: &schedulingv1beta1.Workload{
				Spec: schedulingv1beta1.WorkloadSpec{
					PodGroupTemplates: []schedulingv1beta1.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1beta1.PodGroupSchedulingPolicy{
							Gang: &schedulingv1beta1.GangSchedulingPolicy{MinCount: 4},
						}},
					},
				},
			},
			desired: &schedulingv1beta1.Workload{
				Spec: schedulingv1beta1.WorkloadSpec{
					PodGroupTemplates: []schedulingv1beta1.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1beta1.PodGroupSchedulingPolicy{
							Gang: &schedulingv1beta1.GangSchedulingPolicy{MinCount: 8},
						}},
					},
				},
			},
			wantChanged: true,
			wantCount:   8,
		},
		"minCount unchanged": {
			existing: &schedulingv1beta1.Workload{
				Spec: schedulingv1beta1.WorkloadSpec{
					PodGroupTemplates: []schedulingv1beta1.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1beta1.PodGroupSchedulingPolicy{
							Gang: &schedulingv1beta1.GangSchedulingPolicy{MinCount: 4},
						}},
					},
				},
			},
			desired: &schedulingv1beta1.Workload{
				Spec: schedulingv1beta1.WorkloadSpec{
					PodGroupTemplates: []schedulingv1beta1.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1beta1.PodGroupSchedulingPolicy{
							Gang: &schedulingv1beta1.GangSchedulingPolicy{MinCount: 4},
						}},
					},
				},
			},
			wantChanged: false,
			wantCount:   4,
		},
		"basic policy has no minCount to patch": {
			existing: &schedulingv1beta1.Workload{
				Spec: schedulingv1beta1.WorkloadSpec{
					PodGroupTemplates: []schedulingv1beta1.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1beta1.PodGroupSchedulingPolicy{
							Basic: &schedulingv1beta1.BasicSchedulingPolicy{},
						}},
					},
				},
			},
			desired: &schedulingv1beta1.Workload{
				Spec: schedulingv1beta1.WorkloadSpec{
					PodGroupTemplates: []schedulingv1beta1.PodGroupTemplate{
						{Name: "js", SchedulingPolicy: schedulingv1beta1.PodGroupSchedulingPolicy{
							Basic: &schedulingv1beta1.BasicSchedulingPolicy{},
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
			assertHashedName(t, tc.wantName, template.Name)
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
