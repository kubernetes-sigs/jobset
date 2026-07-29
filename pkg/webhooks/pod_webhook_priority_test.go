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

package webhooks

import (
	"context"
	"math"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
)

// TestPriorityLabelValueIsAValidLabel checks that every Pod priority produces a
// usable label value. PriorityClasses may be negative, and a label value must
// start and end with an alphanumeric character, so rendering a negative priority
// verbatim made the API server reject the Pod and left the JobSet with no Pods.
func TestPriorityLabelValueIsAValidLabel(t *testing.T) {
	cases := map[string]struct {
		priority int32
		want     string
	}{
		"zero":                {priority: 0, want: "0"},
		"positive":            {priority: 100, want: "100"},
		"negative":            {priority: -1, want: "n1"},
		"large negative":      {priority: -1000, want: "n1000"},
		"most negative int32": {priority: math.MinInt32, want: "n2147483648"},
		"largest positive":    {priority: math.MaxInt32, want: "2147483647"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := priorityLabelValue(tc.priority)
			if got != tc.want {
				t.Errorf("priorityLabelValue(%d) = %q, want %q", tc.priority, got, tc.want)
			}
			if errs := validation.IsValidLabelValue(got); len(errs) > 0 {
				t.Errorf("priorityLabelValue(%d) = %q is not a valid label value: %v", tc.priority, got, errs)
			}
		})
	}
}

// TestPriorityLabelValueIsInjective guards the encoding: two different priorities
// must never collapse onto the same label value, since the exclusive-placement
// anti-affinity term matches Pods by this label.
func TestPriorityLabelValueIsInjective(t *testing.T) {
	seen := map[string]int32{}
	for _, p := range []int32{math.MinInt32, -1000, -100, -2, -1, 0, 1, 2, 100, 1000, math.MaxInt32} {
		v := priorityLabelValue(p)
		if prev, ok := seen[v]; ok {
			t.Errorf("priorities %d and %d both render as %q", prev, p, v)
		}
		seen[v] = p
	}
}

// TestDefaultSetsValidPriorityLabelForNegativePriority exercises the webhook end
// to end for the reported case.
func TestDefaultSetsValidPriorityLabelForNegativePriority(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "workers-0-0-abcde",
			Namespace:   "default",
			Annotations: map[string]string{jobset.JobSetNameKey: "negative-priority-repro"},
		},
		Spec: corev1.PodSpec{Priority: ptr.To(int32(-1))},
	}

	webhook := &podWebhook{}
	if err := webhook.Default(context.Background(), pod); err != nil {
		t.Fatalf("Default returned an unexpected error: %v", err)
	}

	got := pod.Labels[constants.PriorityKey]
	if errs := validation.IsValidLabelValue(got); len(errs) > 0 {
		t.Errorf("priority label %q is not a valid label value: %v", got, errs)
	}
}
