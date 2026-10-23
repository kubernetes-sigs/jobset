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
)

func TestCompositeNodeNameLength(t *testing.T) {
	cases := map[string]string{
		"short base":                  "driver-0123456789",
		"base at the 63-char limit":   strings.Repeat("a", maxPodGroupNameLength-11) + "-0123456789",
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
}

func cpg(name string, minGroup int32, leaves ...string) schedulingv1beta1.CompositePodGroupTemplate {
	t := schedulingv1beta1.CompositePodGroupTemplate{
		Name: name,
		SchedulingPolicy: schedulingv1beta1.CompositePodGroupSchedulingPolicy{
			Gang: &schedulingv1beta1.CompositeGangSchedulingPolicy{MinGroupCount: minGroup},
		},
	}
	for _, l := range leaves {
		t.PodGroupTemplates = append(t.PodGroupTemplates, schedulingv1beta1.PodGroupTemplate{Name: l})
	}
	return t
}

func TestCompositeTreeShapeDiffers(t *testing.T) {
	base := []schedulingv1beta1.CompositePodGroupTemplate{cpg("root", 2, "driver", "workers")}

	tests := map[string]struct {
		a, b []schedulingv1beta1.CompositePodGroupTemplate
		want bool
	}{
		"identical shape": {
			a:    []schedulingv1beta1.CompositePodGroupTemplate{cpg("root", 2, "driver", "workers")},
			b:    base,
			want: false,
		},
		"only min counts differ is same shape": {
			a:    []schedulingv1beta1.CompositePodGroupTemplate{cpg("root", 9, "driver", "workers")},
			b:    base,
			want: false,
		},
		"different leaf count": {
			a:    []schedulingv1beta1.CompositePodGroupTemplate{cpg("root", 2, "driver")},
			b:    base,
			want: true,
		},
		"different leaf name": {
			a:    []schedulingv1beta1.CompositePodGroupTemplate{cpg("root", 2, "driver", "others")},
			b:    base,
			want: true,
		},
		"different node count": {
			a:    []schedulingv1beta1.CompositePodGroupTemplate{cpg("root", 2, "driver", "workers"), cpg("extra", 1, "x")},
			b:    base,
			want: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := compositeTreeShapeDiffers(tc.a, tc.b); got != tc.want {
				t.Fatalf("compositeTreeShapeDiffers = %v, want %v", got, tc.want)
			}
		})
	}
}
