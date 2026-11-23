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

	schedulingv1beta1 "k8s.io/api/scheduling/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

func TestCompositeNodeNameLength(t *testing.T) {
	cases := map[string]string{
		"short base":                  "0-driver",
		"base at the 63-char limit":   strings.Repeat("a", maxPodGroupNameLength),
		"base already over the limit": strings.Repeat("b", 80),
	}
	for name, base := range cases {
		t.Run(name, func(t *testing.T) {
			got := compositeNodeName(base)
			if len(got) > maxPodGroupNameLength {
				t.Fatalf("compositeNodeName(%q) = %q (len %d) exceeds %d", base, got, len(got), maxPodGroupNameLength)
			}
			if !strings.HasSuffix(got, compositeNameSuffix) {
				t.Fatalf("compositeNodeName(%q) = %q missing %q suffix", base, got, compositeNameSuffix)
			}
			if got != compositeNodeName(base) {
				t.Fatalf("compositeNodeName(%q) is not deterministic", base)
			}
		})
	}
	if compositeNodeName("0-driver") != "0-driver-cpg" {
		t.Errorf("a short base must keep its readable form")
	}
}

func TestBoundedTemplateName(t *testing.T) {
	if got := boundedTemplateName("0-launcher-node"); got != "0-launcher-node" {
		t.Errorf("short name changed: %q", got)
	}
	long1 := "0-" + strings.Repeat("x", 70)
	long2 := "0-" + strings.Repeat("x", 69) + "y"
	got1, got2 := boundedTemplateName(long1), boundedTemplateName(long2)
	if len(got1) > maxPodGroupNameLength || len(got2) > maxPodGroupNameLength {
		t.Fatalf("truncated names exceed the limit: %q, %q", got1, got2)
	}
	if got1 == got2 {
		t.Errorf("names that differ only past the truncation point must stay distinct")
	}
	if got1 != boundedTemplateName(long1) {
		t.Errorf("boundedTemplateName is not deterministic")
	}
}

func TestCompositeObjectName(t *testing.T) {
	js := &jobset.JobSet{ObjectMeta: metav1.ObjectMeta{Name: "js", Namespace: "ns"}}
	wl := workloadName(js)

	root := compositeObjectName(js, compositeRootTemplateName(js))
	if !strings.HasPrefix(root, wl+"-") || strings.Contains(root, "cpg") {
		t.Errorf("root object %q should be <workload-name>-<hash> without the template segment", root)
	}
	leaf := compositeObjectName(js, "0-driver")
	if !strings.HasPrefix(leaf, wl+"-0-driver-") {
		t.Errorf("leaf object %q should be <workload-name>-<template-name>-<hash>", leaf)
	}
	if leaf == compositeObjectName(js, "0-driver-cpg") {
		t.Errorf("a group and its shared leaf must get distinct object names")
	}
	other := &jobset.JobSet{ObjectMeta: metav1.ObjectMeta{Name: "js", Namespace: "other"}}
	if compositeObjectName(other, "0-driver") == leaf {
		t.Errorf("the same template in another namespace must hash differently")
	}

	longJS := &jobset.JobSet{ObjectMeta: metav1.ObjectMeta{Name: strings.Repeat("j", 50), Namespace: "ns"}}
	for _, tmpl := range []string{compositeRootTemplateName(longJS), "0-" + strings.Repeat("w", 60), "workers-7"} {
		if got := compositeObjectName(longJS, tmpl); len(got) > maxPodGroupNameLength {
			t.Errorf("compositeObjectName(%q) = %q exceeds %d", tmpl, got, maxPodGroupNameLength)
		}
	}
}

func cpg(name string, minGroup int32, leaves ...string) schedulingv1beta1.CompositePodGroupTemplate {
	t := schedulingv1beta1.CompositePodGroupTemplate{
		Name: name,
		SchedulingPolicy: schedulingv1beta1.CompositePodGroupSchedulingPolicy{
			Gang: &schedulingv1beta1.CompositeGangSchedulingPolicy{MinGroupCount: minGroup},
		},
	}
	for _, l := range leaves {
		t.PodGroupTemplates = append(t.PodGroupTemplates, schedulingv1beta1.PodGroupTemplate{
			Name: l,
			SchedulingPolicy: schedulingv1beta1.PodGroupSchedulingPolicy{
				Gang: &schedulingv1beta1.GangSchedulingPolicy{MinCount: 1},
			},
		})
	}
	return t
}

func TestCompositeTreeNeedsRecreation(t *testing.T) {
	base := []schedulingv1beta1.CompositePodGroupTemplate{cpg("root", 2, "driver", "workers")}
	leafMinCount := func(n int32) []schedulingv1beta1.CompositePodGroupTemplate {
		tree := []schedulingv1beta1.CompositePodGroupTemplate{cpg("root", 2, "driver", "workers")}
		tree[0].PodGroupTemplates[1].SchedulingPolicy.Gang.MinCount = n
		return tree
	}

	tests := map[string]struct {
		existing []schedulingv1beta1.CompositePodGroupTemplate
		want     bool
	}{
		"identical":             {existing: []schedulingv1beta1.CompositePodGroupTemplate{cpg("root", 2, "driver", "workers")}, want: false},
		"only a leaf minCount":  {existing: leafMinCount(9), want: false},
		"minGroupCount differs": {existing: []schedulingv1beta1.CompositePodGroupTemplate{cpg("root", 1, "driver", "workers")}, want: true},
		"different leaf count":  {existing: []schedulingv1beta1.CompositePodGroupTemplate{cpg("root", 2, "driver")}, want: true},
		"different leaf name":   {existing: []schedulingv1beta1.CompositePodGroupTemplate{cpg("root", 2, "driver", "others")}, want: true},
		"different node name":   {existing: []schedulingv1beta1.CompositePodGroupTemplate{cpg("other", 2, "driver", "workers")}, want: true},
		"different node count":  {existing: []schedulingv1beta1.CompositePodGroupTemplate{cpg("root", 2, "driver", "workers"), cpg("extra", 1, "x")}, want: true},
		"nested child added": {existing: func() []schedulingv1beta1.CompositePodGroupTemplate {
			tr := []schedulingv1beta1.CompositePodGroupTemplate{cpg("root", 2, "driver", "workers")}
			tr[0].CompositePodGroupTemplates = []schedulingv1beta1.CompositePodGroupTemplate{cpg("c", 1, "x")}
			return tr
		}(), want: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := compositeTreeNeedsRecreation(tc.existing, base); got != tc.want {
				t.Fatalf("compositeTreeNeedsRecreation = %v, want %v", got, tc.want)
			}
		})
	}
}
