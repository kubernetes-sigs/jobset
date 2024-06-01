/*
Copyright 2023 The Kubernetes Authors.
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
package collections

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/exp/slices"
)

func TestConcat(t *testing.T) {
	type testCase struct {
		name   string
		slices [][]int
		want   []int
	}

	testCases := []testCase{
		{
			name:   "Two empty slices",
			slices: [][]int{{}, {}},
			want:   []int{},
		},
		{
			name:   "One empty slice, one non-empty slice",
			slices: [][]int{{1, 2, 3}, {}},
			want:   []int{1, 2, 3},
		},
		{
			name:   "Two non-empty slices",
			slices: [][]int{{1, 2, 3}, {4, 5, 6}},
			want:   []int{1, 2, 3, 4, 5, 6},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := Concat(tc.slices...)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("unexpected diff (-want/+got): %s", diff)
			}
		})
	}
}

func TestCloneMap(t *testing.T) {
	type testCase struct {
		name  string
		input map[string]string
		want  map[string]string
	}

	testCases := []testCase{
		{
			name:  "Empty map",
			input: map[string]string{},
			want:  map[string]string{},
		},
		{
			name:  "Non-empty map",
			input: map[string]string{"foo": "bar", "baz": "qux"},
			want:  map[string]string{"foo": "bar", "baz": "qux"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := CloneMap(tc.input)
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("cloned map values do not match input map. input: %s, output: %s", fmt.Sprintf("%v", tc.input), fmt.Sprintf("%v", got))
			}
			// Confirm these maps have different pointers.
			if &tc.want == &got {
				t.Errorf("input map reference and output map reference point to the same object")
			}
		})
	}
}

func TestContains(t *testing.T) {
	type testCase struct {
		name    string
		slice   []string
		element string
		want    bool
	}

	testCases := []testCase{
		{
			name:    "Empty slice",
			slice:   []string{},
			element: "foo",
			want:    false,
		},
		{
			name:    "Slice with one element, contains element",
			slice:   []string{"foo"},
			element: "foo",
			want:    true,
		},
		{
			name:    "Slice with one element, does not element",
			slice:   []string{"foo"},
			element: "bar",
			want:    false,
		},
		{
			name:    "Slice with two elements, contains element",
			slice:   []string{"foo", "bar"},
			element: "bar",
			want:    true,
		},
		{
			name:    "Slice with two elements, does not contain element",
			slice:   []string{"foo", "bar"},
			element: "baz",
			want:    false,
		},
		{
			name:    "Slice with three elements, contains element",
			slice:   []string{"foo", "bar", "baz"},
			element: "bar",
			want:    true,
		},
		{
			name:    "Slice with two elements, does not contain element",
			slice:   []string{"foo", "bar", "baz"},
			element: "missing",
			want:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Contains(tc.slice, tc.element); got != tc.want {
				t.Errorf("got: %t, want %t", got, tc.want)
			}
		})
	}
}

func TestMergeMaps(t *testing.T) {
	testCases := []struct {
		name     string
		m1       map[string]int
		m2       map[string]int
		expected map[string]int
	}{
		{
			name:     "Basic merge",
			m1:       map[string]int{"a": 1, "b": 2},
			m2:       map[string]int{"c": 3, "d": 4},
			expected: map[string]int{"a": 1, "b": 2, "c": 3, "d": 4},
		},
		{
			name:     "Overlapping keys",
			m1:       map[string]int{"a": 1, "b": 2},
			m2:       map[string]int{"b": 3, "c": 4},
			expected: map[string]int{"a": 1, "b": 3, "c": 4}, // m2 value for 'b' overwrites
		},
		{
			name:     "Empty maps",
			m1:       map[string]int{},
			m2:       map[string]int{},
			expected: map[string]int{},
		},
		{
			name:     "One empty map",
			m1:       map[string]int{"a": 1, "b": 2},
			m2:       map[string]int{},
			expected: map[string]int{"a": 1, "b": 2},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			merged := MergeMaps(tc.m1, tc.m2)

			if !reflect.DeepEqual(merged, tc.expected) {
				t.Errorf("expected %v, got %v", tc.expected, merged)
			}
		})
	}
}

func TestMergeSlices(t *testing.T) {
	testCases := []struct {
		name     string
		s1       []int
		s2       []int
		expected []int
	}{
		{
			name:     "merge with overlapping elements should not result in duplicates",
			s1:       []int{1, 2, 3},
			s2:       []int{3, 4, 5},
			expected: []int{1, 2, 3, 4, 5},
		},
		{
			name:     "empty slices",
			s1:       []int{},
			s2:       []int{},
			expected: []int{},
		},
		{
			name:     "one empty slice",
			s1:       []int{1, 2},
			s2:       []int{},
			expected: []int{1, 2},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			merged := MergeSlices(tc.s1, tc.s2)

			// Sort before comparison so slices with the same elements
			// should be the same.
			slices.Sort(merged)
			slices.Sort(tc.expected)

			if !reflect.DeepEqual(merged, tc.expected) {
				t.Errorf("Expected %v, got %v", tc.expected, merged)
			}
		})
	}
}
