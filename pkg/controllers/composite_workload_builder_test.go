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
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	schedulingv1alpha3 "k8s.io/api/scheduling/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

func rjob(name string, replicas, parallelism int32) jobset.ReplicatedJob {
	return jobset.ReplicatedJob{
		Name:     name,
		Replicas: replicas,
		Template: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Parallelism: ptr.To(parallelism)}},
	}
}

// TestBuildCompositeWorkload asserts the gang-of-gangs tree compiles into a
// Workload with the expected root/child CompositePodGroupTemplate hierarchy and
// leaf PodGroupTemplates.
func TestBuildCompositeWorkload(t *testing.T) {
	gang := func() *schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy {
		return &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{}}
	}
	basic := func() *schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy {
		return &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{Basic: &schedulingv1alpha3.WorkloadCompositePodGroupBasicSchedulingPolicy{}}
	}

	js := &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{Name: "js", Namespace: "ns"},
		Spec: jobset.JobSetSpec{
			ReplicatedJobs: []jobset.ReplicatedJob{
				rjob("leader", 1, 1),
				rjob("workers", 2, 4),
			},
			Scheduling: &jobset.JobSetScheduling{
				SchedulingPolicy: basic(),
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{
						TargetReplicatedJobs: []string{"workers"},
						SchedulingPolicy:     gang(),
						Job: &jobset.JobScheduling{
							SchedulingPolicy: &schedulingv1alpha3.WorkloadPodGroupSchedulingPolicy{Gang: &schedulingv1alpha3.WorkloadPodGroupGangSchedulingPolicy{}},
						},
					},
				},
			},
		},
	}

	wl, err := buildCompositeWorkload(js)
	if err != nil {
		t.Fatalf("buildCompositeWorkload: %v", err)
	}

	// Root CPG template, with one child CPG (the workers group).
	if got := len(wl.Spec.CompositePodGroupTemplates); got != 1 {
		t.Fatalf("expected 1 root CompositePodGroupTemplate, got %d", got)
	}
	root := wl.Spec.CompositePodGroupTemplates[0]
	if root.SchedulingPolicy.Basic == nil {
		t.Errorf("root policy = %+v, want Basic", root.SchedulingPolicy)
	}
	// Root has one child CPG (workers) and one leaf (untargeted leader).
	if got := len(root.CompositePodGroupTemplates); got != 1 {
		t.Errorf("expected 1 child CompositePodGroupTemplate under root, got %d", got)
	}
	if got := len(root.PodGroupTemplates); got != 1 {
		t.Errorf("expected 1 leaf PodGroupTemplate (untargeted leader) under root, got %d", got)
	}

	// The workers child CPG is gang and holds one leaf per replica (2).
	child := root.CompositePodGroupTemplates[0]
	if child.SchedulingPolicy.Gang == nil {
		t.Errorf("child policy = %+v, want Gang", child.SchedulingPolicy)
	}
	if got := len(child.PodGroupTemplates); got != 2 {
		t.Errorf("expected 2 per-Job leaf PodGroupTemplates under workers, got %d", got)
	}
}

// TestCompositeWorkloadDriftAndPatch verifies that an ElasticJobSet-style scale
// (parallelism change) is reconciled as an in-place min-count patch, not a
// recreation. spec.scheduling is immutable and replicas are fixed, so a
// composite tree never changes structurally; only gang min counts move.
func TestCompositeWorkloadDriftAndPatch(t *testing.T) {
	mk := func(policy *schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy, replicas int32) *jobset.JobSet {
		return &jobset.JobSet{
			ObjectMeta: metav1.ObjectMeta{Name: "js", Namespace: "ns"},
			Spec: jobset.JobSetSpec{
				ReplicatedJobs: []jobset.ReplicatedJob{rjob("leader", 1, 1), rjob("workers", replicas, 2)},
				Scheduling: &jobset.JobSetScheduling{
					SchedulingPolicy: policy,
					ReplicatedJobs: []jobset.ReplicatedJobScheduling{
						{TargetReplicatedJobs: []string{"workers"}, SchedulingPolicy: &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{}}},
					},
				},
			},
		}
	}
	basic := &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{Basic: &schedulingv1alpha3.WorkloadCompositePodGroupBasicSchedulingPolicy{}}

	existing, err := buildCompositeWorkload(mk(basic, 2))
	if err != nil {
		t.Fatalf("existing: %v", err)
	}

	// Scaling the workers gang's parallelism (here via a differently sized build)
	// changes only leaf minCount: patch in place, never recreate.
	scaled, err := buildCompositeWorkload(mk(basic, 4))
	if err != nil {
		t.Fatalf("scaled: %v", err)
	}
	if workloadNeedsRecreation(existing, scaled) {
		t.Errorf("min-count-only change should not need recreation")
	}
	if _, changed := workloadWithPatchedMinCounts(existing, scaled); !changed {
		t.Errorf("min-count-only change should be detected as a patch")
	}
}

// TestCompositeFanoutCap verifies the per-node 8-template cap: a single entry
// with job set and replicas > 8 produces too many per-Job leaves under one
// child and is rejected, while many leaves spread across children are allowed.
func TestCompositeFanoutCap(t *testing.T) {
	gang := func() *schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy {
		return &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{Gang: &schedulingv1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy{}}
	}
	basic := &schedulingv1alpha3.WorkloadCompositePodGroupSchedulingPolicy{Basic: &schedulingv1alpha3.WorkloadCompositePodGroupBasicSchedulingPolicy{}}

	// 9 per-Job leaves under one child CPG -> exceeds the cap.
	tooMany := &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{Name: "js", Namespace: "ns"},
		Spec: jobset.JobSetSpec{
			ReplicatedJobs: []jobset.ReplicatedJob{rjob("workers", 9, 1)},
			Scheduling: &jobset.JobSetScheduling{
				SchedulingPolicy: basic,
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{
						TargetReplicatedJobs: []string{"workers"},
						SchedulingPolicy:     gang(),
						Job:                  &jobset.JobScheduling{SchedulingPolicy: &schedulingv1alpha3.WorkloadPodGroupSchedulingPolicy{Gang: &schedulingv1alpha3.WorkloadPodGroupGangSchedulingPolicy{}}},
					},
				},
			},
		},
	}
	if errs := validateCompositeWorkload(context.Background(), tooMany, schedulingRootPath); len(errs) == 0 {
		t.Errorf("expected rejection for 9 per-Job leaves under one child CPG")
	}

	// Two shared-leaf children + untargeted leader = 2 composite children and 1
	// root leaf, all within the per-node cap: must be accepted.
	ok := &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{Name: "js", Namespace: "ns"},
		Spec: jobset.JobSetSpec{
			ReplicatedJobs: []jobset.ReplicatedJob{rjob("leader", 1, 1), rjob("a", 1, 1), rjob("b", 1, 1)},
			Scheduling: &jobset.JobSetScheduling{
				SchedulingPolicy: basic,
				ReplicatedJobs: []jobset.ReplicatedJobScheduling{
					{TargetReplicatedJobs: []string{"a"}, SchedulingPolicy: gang()},
					{TargetReplicatedJobs: []string{"b"}, SchedulingPolicy: gang()},
				},
			},
		},
	}
	if errs := validateCompositeWorkload(context.Background(), ok, schedulingRootPath); len(errs) != 0 {
		t.Errorf("expected acceptance, got %v", errs)
	}
}
