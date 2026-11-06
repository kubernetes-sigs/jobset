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
	"context"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	batchv1 "k8s.io/api/batch/v1"
	schedulingv1alpha3 "k8s.io/api/scheduling/v1alpha3"
	schedulingv1beta1 "k8s.io/api/scheduling/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/features"
)

func rjob(name string, replicas, parallelism int32) jobset.ReplicatedJob {
	return jobset.ReplicatedJob{
		Name:     name,
		Replicas: replicas,
		Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To(parallelism)}},
	}
}

func compositeGang() *schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy {
	return &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{}}
}

func compositeGangMin(n int32) *schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy {
	return &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{MinGroupCount: ptr.To(n)}}
}

func compositeBasic() *schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy {
	return &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{Basic: &schedulingv1alpha3.WorkloadCompositePodGroupBasicSchedulingPolicy{}}
}

func jobGang() *jobset.JobScheduling {
	return &jobset.JobScheduling{SchedulingPolicy: &schedulingv1alpha3.WorkloadPodGroupSchedulingPolicy{Gang: &schedulingv1alpha3.WorkloadPodGroupGangSchedulingPolicy{}}}
}

func compositeJobSet(rjobs []jobset.ReplicatedJob, scheduling *jobset.JobSetScheduling) *jobset.JobSet {
	return &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{Name: "js", Namespace: "ns"},
		Spec:       jobset.JobSetSpec{ReplicatedJobs: rjobs, Scheduling: scheduling},
	}
}

// treeNode is a compact, comparable view of a compiled template tree.
type treeNode struct {
	Name     string
	Policy   string // "basic", "gang/<minGroupCount>" for composites, "gang/<minCount>" for leaves
	Topology []string
	Leaves   []treeNode
	Children []treeNode
}

func describeComposite(t schedulingv1beta1.CompositePodGroupTemplate) treeNode {
	n := treeNode{Name: t.Name, Policy: "basic"}
	if g := t.SchedulingPolicy.Gang; g != nil {
		n.Policy = "gang/" + itoa(g.MinGroupCount)
	}
	if t.SchedulingConstraints != nil {
		for _, tc := range t.SchedulingConstraints.Topology {
			n.Topology = append(n.Topology, tc.Key)
		}
	}
	for _, l := range t.PodGroupTemplates {
		leaf := treeNode{Name: l.Name, Policy: "basic"}
		if g := l.SchedulingPolicy.Gang; g != nil {
			leaf.Policy = "gang/" + itoa(g.MinCount)
		}
		n.Leaves = append(n.Leaves, leaf)
	}
	for _, c := range t.CompositePodGroupTemplates {
		n.Children = append(n.Children, describeComposite(c))
	}
	return n
}

func itoa(i int32) string {
	return strconv.Itoa(int(i))
}

// TestBuildCompositeWorkloadUniformTree covers the uniform tree of KEP-969: the
// same root -> group -> leaf shape whatever levels are configured, the root
// default policy, policy placement, the fallback group, and index-prefix names.
func TestBuildCompositeWorkloadUniformTree(t *testing.T) {
	ready := func(name string) jobset.DependsOn {
		return jobset.DependsOn{Name: name, Status: jobset.DependencyReady}
	}
	rack := &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingConstraints{
		Topology: []schedulingv1alpha3.TopologyConstraint{{Key: "example.com/rack"}},
	}
	zone := &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingConstraints{
		Topology: []schedulingv1alpha3.TopologyConstraint{{Key: "topology.kubernetes.io/zone"}},
	}

	cases := map[string]struct {
		js   *jobset.JobSet
		want treeNode
	}{
		"top-level only: Gang root over one fallback group of every ReplicatedJob": {
			js: compositeJobSet(
				[]jobset.ReplicatedJob{rjob("leader", 1, 1), rjob("workers", 2, 4)},
				&jobset.JobSetScheduling{SchedulingPolicy: compositeGang()},
			),
			want: treeNode{Name: "js-cpg", Policy: "gang/1", Children: []treeNode{
				{Name: "0-leader-workers-cpg", Policy: "gang/1", Leaves: []treeNode{{Name: "0-leader-workers", Policy: "gang/9"}}},
			}},
		},
		"empty scheduling block keeps the Alpha top-level gang": {
			js: compositeJobSet(
				[]jobset.ReplicatedJob{rjob("workers", 1, 3)},
				&jobset.JobSetScheduling{},
			),
			want: treeNode{Name: "js-cpg", Policy: "gang/1", Children: []treeNode{
				{Name: "0-workers-cpg", Policy: "gang/1", Leaves: []treeNode{{Name: "0-workers", Policy: "gang/3"}}},
			}},
		},
		"replicatedJobs only: Basic root, one group per entry": {
			js: compositeJobSet(
				[]jobset.ReplicatedJob{rjob("driver", 1, 1), rjob("workers", 2, 4)},
				&jobset.JobSetScheduling{ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{TargetReplicatedJobs: []string{"driver"}, SchedulingPolicy: compositeBasic()},
					{TargetReplicatedJobs: []string{"workers"}},
				}},
			),
			want: treeNode{Name: "js-cpg", Policy: "basic", Children: []treeNode{
				{Name: "0-driver-cpg", Policy: "basic", Leaves: []treeNode{{Name: "0-driver", Policy: "basic"}}},
				{Name: "1-workers-cpg", Policy: "gang/1", Leaves: []treeNode{{Name: "1-workers", Policy: "gang/8"}}},
			}},
		},
		"replicatedJobs only with an untargeted ReplicatedJob: Basic fallback group": {
			js: compositeJobSet(
				[]jobset.ReplicatedJob{rjob("sidecar", 1, 1), rjob("workers", 1, 2)},
				&jobset.JobSetScheduling{ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{TargetReplicatedJobs: []string{"workers"}},
				}},
			),
			want: treeNode{Name: "js-cpg", Policy: "basic", Children: []treeNode{
				{Name: "0-workers-cpg", Policy: "gang/1", Leaves: []treeNode{{Name: "0-workers", Policy: "gang/2"}}},
				{Name: "1-sidecar-cpg", Policy: "basic", Leaves: []treeNode{{Name: "1-sidecar", Policy: "basic"}}},
			}},
		},
		"combined: job entry under a Basic root, untargeted leader falls back": {
			js: compositeJobSet(
				[]jobset.ReplicatedJob{rjob("leader", 1, 1), rjob("workers", 2, 4)},
				&jobset.JobSetScheduling{
					SchedulingPolicy: compositeBasic(),
					ReplicatedJobs: []jobset.ReplicatedJobScheduling{
						{TargetReplicatedJobs: []string{"workers"}, SchedulingPolicy: compositeGang(), Job: jobGang()},
					},
				},
			),
			want: treeNode{Name: "js-cpg", Policy: "basic", Children: []treeNode{
				{Name: "0-workers-cpg", Policy: "gang/2", Leaves: []treeNode{
					{Name: "workers-0", Policy: "gang/4"},
					{Name: "workers-1", Policy: "gang/4"},
				}},
				{Name: "1-leader-cpg", Policy: "basic", Leaves: []treeNode{{Name: "1-leader", Policy: "basic"}}},
			}},
		},
		"nested topology: zone on the root, rack on each group": {
			js: compositeJobSet(
				[]jobset.ReplicatedJob{rjob("leader", 1, 1), rjob("worker", 1, 4)},
				&jobset.JobSetScheduling{
					SchedulingPolicy:      compositeGang(),
					SchedulingConstraints: zone,
					ReplicatedJobs: []jobset.ReplicatedJobScheduling{
						{TargetReplicatedJobs: []string{"leader"}, SchedulingConstraints: rack},
						{TargetReplicatedJobs: []string{"worker"}, SchedulingConstraints: rack},
					},
				},
			),
			want: treeNode{Name: "js-cpg", Policy: "gang/2", Topology: []string{"topology.kubernetes.io/zone"}, Children: []treeNode{
				{Name: "0-leader-cpg", Policy: "gang/1", Topology: []string{"example.com/rack"}, Leaves: []treeNode{{Name: "0-leader", Policy: "gang/1"}}},
				{Name: "1-worker-cpg", Policy: "gang/1", Topology: []string{"example.com/rack"}, Leaves: []treeNode{{Name: "1-worker", Policy: "gang/4"}}},
			}},
		},
		"explicit root minGroupCount is kept": {
			js: compositeJobSet(
				[]jobset.ReplicatedJob{rjob("a", 1, 1), rjob("b", 1, 1)},
				&jobset.JobSetScheduling{
					SchedulingPolicy: compositeGangMin(1),
					ReplicatedJobs: []jobset.ReplicatedJobScheduling{
						{TargetReplicatedJobs: []string{"a"}},
						{TargetReplicatedJobs: []string{"b"}},
					},
				},
			),
			want: treeNode{Name: "js-cpg", Policy: "gang/1", Children: []treeNode{
				{Name: "0-a-cpg", Policy: "gang/1", Leaves: []treeNode{{Name: "0-a", Policy: "gang/1"}}},
				{Name: "1-b-cpg", Policy: "gang/1", Leaves: []treeNode{{Name: "1-b", Policy: "gang/1"}}},
			}},
		},
		"shared gang excludes the DependsOn-deferred member from minCount": {
			js: compositeJobSet(
				[]jobset.ReplicatedJob{
					func() jobset.ReplicatedJob {
						r := rjob("launcher", 1, 1)
						r.DependsOn = []jobset.DependsOn{ready("node")}
						return r
					}(),
					rjob("node", 1, 49),
				},
				&jobset.JobSetScheduling{
					SchedulingPolicy: compositeBasic(),
					ReplicatedJobs: []jobset.ReplicatedJobScheduling{
						{TargetReplicatedJobs: []string{"launcher", "node"}},
					},
				},
			),
			want: treeNode{Name: "js-cpg", Policy: "basic", Children: []treeNode{
				{Name: "0-launcher-node-cpg", Policy: "gang/1", Leaves: []treeNode{{Name: "0-launcher-node", Policy: "gang/49"}}},
			}},
		},
		"index prefix keeps [a, b] and [a-b] distinct": {
			js: compositeJobSet(
				[]jobset.ReplicatedJob{rjob("a", 1, 1), rjob("b", 1, 1), rjob("a-b", 1, 1)},
				&jobset.JobSetScheduling{ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{TargetReplicatedJobs: []string{"a", "b"}},
					{TargetReplicatedJobs: []string{"a-b"}},
				}},
			),
			want: treeNode{Name: "js-cpg", Policy: "basic", Children: []treeNode{
				{Name: "0-a-b-cpg", Policy: "gang/1", Leaves: []treeNode{{Name: "0-a-b", Policy: "gang/2"}}},
				{Name: "1-a-b-cpg", Policy: "gang/1", Leaves: []treeNode{{Name: "1-a-b", Policy: "gang/1"}}},
			}},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			wl, err := buildCompositeWorkload(tc.js)
			if err != nil {
				t.Fatalf("buildCompositeWorkload: %v", err)
			}
			if len(wl.Spec.PodGroupTemplates) != 0 {
				t.Errorf("expected no top-level PodGroupTemplates, got %d", len(wl.Spec.PodGroupTemplates))
			}
			if len(wl.Spec.CompositePodGroupTemplates) != 1 {
				t.Fatalf("expected exactly 1 root CompositePodGroupTemplate, got %d", len(wl.Spec.CompositePodGroupTemplates))
			}
			if diff := cmp.Diff(tc.want, describeComposite(wl.Spec.CompositePodGroupTemplates[0])); diff != "" {
				t.Errorf("unexpected tree (-want +got):\n%s", diff)
			}
		})
	}
}

// TestCompositeEntryDisruptionModeOnGroup verifies that an entry's
// disruptionMode configures its group CompositePodGroup (the per-group
// disruption use case), not the shared leaf beneath it.
func TestCompositeEntryDisruptionModeOnGroup(t *testing.T) {
	js := compositeJobSet(
		[]jobset.ReplicatedJob{rjob("leader", 1, 1), rjob("worker", 1, 4)},
		&jobset.JobSetScheduling{
			SchedulingPolicy: compositeBasic(),
			ReplicatedJobs: []jobset.ReplicatedJobScheduling{
				{TargetReplicatedJobs: []string{"leader"}, DisruptionMode: &schedulingv1alpha3.WorkloadCompositePodGroupDisruptionMode{Single: &schedulingv1alpha3.WorkloadCompositePodGroupSingleDisruptionMode{}}},
				{TargetReplicatedJobs: []string{"worker"}, DisruptionMode: &schedulingv1alpha3.WorkloadCompositePodGroupDisruptionMode{All: &schedulingv1alpha3.WorkloadCompositePodGroupAllDisruptionMode{}}},
			},
		},
	)
	wl, err := buildCompositeWorkload(js)
	if err != nil {
		t.Fatalf("buildCompositeWorkload: %v", err)
	}
	groups := wl.Spec.CompositePodGroupTemplates[0].CompositePodGroupTemplates
	if dm := groups[0].DisruptionMode; dm == nil || dm.Single == nil {
		t.Errorf("leader group disruptionMode = %+v, want Single", dm)
	}
	if dm := groups[1].DisruptionMode; dm == nil || dm.All == nil {
		t.Errorf("worker group disruptionMode = %+v, want All", dm)
	}
	for _, g := range groups {
		if dm := g.PodGroupTemplates[0].DisruptionMode; dm != nil {
			t.Errorf("leaf %s disruptionMode = %+v, want unset (it belongs to the group)", g.PodGroupTemplates[0].Name, dm)
		}
	}
}

// TestCompositeWorkloadDriftAndPatch verifies which composite changes are
// patched in place (leaf minCount) and which recreate the Workload (anything
// else, including a CompositePodGroup's minGroupCount, which upstream does not
// allow to change).
func TestCompositeWorkloadDriftAndPatch(t *testing.T) {
	build := func(t *testing.T, js *jobset.JobSet) *schedulingv1beta1.Workload {
		t.Helper()
		wl, err := buildCompositeWorkload(js)
		if err != nil {
			t.Fatalf("buildCompositeWorkload: %v", err)
		}
		return wl
	}
	sharedGroup := func(parallelism int32) *jobset.JobSet {
		return compositeJobSet(
			[]jobset.ReplicatedJob{rjob("leader", 1, 1), rjob("workers", 2, parallelism)},
			&jobset.JobSetScheduling{
				SchedulingPolicy: compositeBasic(),
				ReplicatedJobs:   []jobset.ReplicatedJobScheduling{{TargetReplicatedJobs: []string{"workers"}}},
			},
		)
	}
	perJob := func(replicas int32) *jobset.JobSet {
		return compositeJobSet(
			[]jobset.ReplicatedJob{rjob("workers", replicas, 2)},
			&jobset.JobSetScheduling{
				SchedulingPolicy: compositeBasic(),
				ReplicatedJobs:   []jobset.ReplicatedJobScheduling{{TargetReplicatedJobs: []string{"workers"}, Job: jobGang()}},
			},
		)
	}

	t.Run("leaf minCount change is patched in place", func(t *testing.T) {
		existing, scaled := build(t, sharedGroup(2)), build(t, sharedGroup(4))
		if workloadNeedsRecreation(existing, scaled) {
			t.Fatalf("leaf minCount-only change should not need recreation")
		}
		patched, changed := workloadWithPatchedMinCounts(existing, scaled)
		if !changed {
			t.Fatalf("leaf minCount-only change should be detected as a patch")
		}
		if workloadNeedsRecreation(patched, scaled) {
			t.Errorf("patched Workload should match desired")
		}
	})

	t.Run("minGroupCount or template count change recreates", func(t *testing.T) {
		if !workloadNeedsRecreation(build(t, perJob(2)), build(t, perJob(3))) {
			t.Errorf("changing the replicas under job changes minGroupCount and the template count, so it must recreate")
		}
	})

	t.Run("minGroupCount change alone recreates", func(t *testing.T) {
		existing, desired := build(t, sharedGroup(2)), build(t, sharedGroup(2))
		existing.Spec.CompositePodGroupTemplates[0].SchedulingPolicy = schedulingv1beta1.CompositePodGroupSchedulingPolicy{
			Gang: &schedulingv1beta1.CompositeGangSchedulingPolicy{MinGroupCount: 1},
		}
		desired.Spec.CompositePodGroupTemplates[0].SchedulingPolicy = schedulingv1beta1.CompositePodGroupSchedulingPolicy{
			Gang: &schedulingv1beta1.CompositeGangSchedulingPolicy{MinGroupCount: 2},
		}
		if !workloadNeedsRecreation(existing, desired) {
			t.Errorf("a minGroupCount change must recreate, since upstream does not allow patching it")
		}
		if _, changed := workloadWithPatchedMinCounts(existing, desired); changed {
			t.Errorf("minGroupCount must never be patched in place")
		}
	})

	t.Run("constraints dropped by the API server are not drift", func(t *testing.T) {
		js := sharedGroup(2)
		js.Spec.Scheduling.ReplicatedJobs[0].SchedulingConstraints = &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingConstraints{
			Topology: []schedulingv1alpha3.TopologyConstraint{{Key: "example.com/rack"}},
		}
		desired := build(t, js)
		existing := desired.DeepCopy()
		existing.Spec.CompositePodGroupTemplates[0].CompositePodGroupTemplates[0].SchedulingConstraints = nil
		if workloadNeedsRecreation(existing, desired) {
			t.Errorf("constraints stripped by a disabled upstream gate must not cause a recreate loop")
		}
	})

	t.Run("identical trees need nothing", func(t *testing.T) {
		existing, desired := build(t, sharedGroup(2)), build(t, sharedGroup(2))
		if workloadNeedsRecreation(existing, desired) {
			t.Errorf("identical trees should not need recreation")
		}
		if _, changed := workloadWithPatchedMinCounts(existing, desired); changed {
			t.Errorf("identical trees should not be patched")
		}
	})
}

// TestCompositeFanoutCap verifies the per-node 8-template cap.
func TestCompositeFanoutCap(t *testing.T) {
	cases := map[string]struct {
		js       *jobset.JobSet
		rejected bool
	}{
		"9 per-Job leaves under one group": {
			js: compositeJobSet(
				[]jobset.ReplicatedJob{rjob("workers", 9, 1)},
				&jobset.JobSetScheduling{
					SchedulingPolicy: compositeBasic(),
					ReplicatedJobs:   []jobset.ReplicatedJobScheduling{{TargetReplicatedJobs: []string{"workers"}, Job: jobGang()}},
				},
			),
			rejected: true,
		},
		"8 entries plus a fallback group is 9 groups under the root": {
			js: func() *jobset.JobSet {
				var rjobs []jobset.ReplicatedJob
				var entries []jobset.ReplicatedJobScheduling
				for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
					rjobs = append(rjobs, rjob(n, 1, 1))
					entries = append(entries, jobset.ReplicatedJobScheduling{TargetReplicatedJobs: []string{n}})
				}
				rjobs = append(rjobs, rjob("untargeted", 1, 1))
				return compositeJobSet(rjobs, &jobset.JobSetScheduling{SchedulingPolicy: compositeBasic(), ReplicatedJobs: entries})
			}(),
			rejected: true,
		},
		"groups and leaves within the cap": {
			js: compositeJobSet(
				[]jobset.ReplicatedJob{rjob("leader", 1, 1), rjob("a", 1, 1), rjob("b", 8, 1)},
				&jobset.JobSetScheduling{
					SchedulingPolicy: compositeBasic(),
					ReplicatedJobs: []jobset.ReplicatedJobScheduling{
						{TargetReplicatedJobs: []string{"a"}},
						{TargetReplicatedJobs: []string{"b"}, Job: jobGang()},
					},
				},
			),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			errs := validateCompositeWorkload(context.Background(), tc.js, schedulingRootPath)
			if tc.rejected && len(errs) == 0 {
				t.Errorf("expected rejection")
			}
			if !tc.rejected && len(errs) != 0 {
				t.Errorf("expected acceptance, got %v", errs)
			}
		})
	}
}

// TestCompositeJobMapping verifies that, with the gate on, every child Job is
// mapped to a leaf template and PodGroup object that the compiled tree actually
// materializes, so Pods never reference a PodGroup that does not exist.
func TestCompositeJobMapping(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.JobSetWorkloadAwareSchedulingAPI, true)
	features.SetFeatureGateDuringTest(t, features.JobSetGangOfGangs, true)

	js := compositeJobSet(
		[]jobset.ReplicatedJob{rjob("leader", 1, 1), rjob("workers", 3, 2), rjob("ps", 2, 1)},
		&jobset.JobSetScheduling{
			SchedulingPolicy: compositeGang(),
			ReplicatedJobs: []jobset.ReplicatedJobScheduling{
				{TargetReplicatedJobs: []string{"workers"}, Job: jobGang()},
				{TargetReplicatedJobs: []string{"ps"}},
			},
		},
	)
	if !UseGangOfGangs(js) {
		t.Fatalf("UseGangOfGangs should be true with the gate on and spec.scheduling set")
	}
	wl, err := buildWorkload(js)
	if err != nil {
		t.Fatalf("buildWorkload: %v", err)
	}

	leaves := map[string]bool{}
	var walk func(tmpls []schedulingv1beta1.CompositePodGroupTemplate)
	walk = func(tmpls []schedulingv1beta1.CompositePodGroupTemplate) {
		for _, c := range tmpls {
			for _, l := range c.PodGroupTemplates {
				leaves[l.Name] = true
			}
			walk(c.CompositePodGroupTemplates)
		}
	}
	walk(wl.Spec.CompositePodGroupTemplates)

	want := map[string]string{
		"leader/0":  "2-leader",
		"workers/0": "workers-0",
		"workers/1": "workers-1",
		"workers/2": "workers-2",
		"ps/0":      "1-ps",
		"ps/1":      "1-ps",
	}
	for _, rj := range js.Spec.ReplicatedJobs {
		for idx := 0; idx < int(rj.Replicas); idx++ {
			key := rj.Name + "/" + strconv.Itoa(idx)
			tmpl := schedulingGroupTemplateName(js, rj.Name, idx)
			if tmpl != want[key] {
				t.Errorf("%s: template = %q, want %q", key, tmpl, want[key])
			}
			if !leaves[tmpl] {
				t.Errorf("%s: template %q is not a leaf of the compiled tree", key, tmpl)
			}
			if got, wantObj := schedulingPodGroupName(js, tmpl), compositeObjectName(js, tmpl); got != wantObj {
				t.Errorf("%s: PodGroup name = %q, want the materialized leaf name %q", key, got, wantObj)
			}
		}
	}
}

// TestUseGangOfGangs verifies the uniform-tree selection: gate on and
// spec.scheduling set, whatever levels are configured.
func TestUseGangOfGangs(t *testing.T) {
	topLevelOnly := compositeJobSet([]jobset.ReplicatedJob{rjob("a", 1, 1)}, &jobset.JobSetScheduling{SchedulingPolicy: compositeGang()})
	noScheduling := compositeJobSet([]jobset.ReplicatedJob{rjob("a", 1, 1)}, nil)

	t.Run("gate off", func(t *testing.T) {
		features.SetFeatureGateDuringTest(t, features.JobSetGangOfGangs, false)
		if UseGangOfGangs(topLevelOnly) {
			t.Errorf("gate off must keep the flat models")
		}
	})
	t.Run("gate on", func(t *testing.T) {
		features.SetFeatureGateDuringTest(t, features.JobSetWorkloadAwareSchedulingAPI, true)
		features.SetFeatureGateDuringTest(t, features.JobSetGangOfGangs, true)
		if !UseGangOfGangs(topLevelOnly) {
			t.Errorf("gate on must use the tree even for top-level-only scheduling")
		}
		if UseGangOfGangs(noScheduling) {
			t.Errorf("a JobSet without spec.scheduling must create nothing")
		}
	})
}
