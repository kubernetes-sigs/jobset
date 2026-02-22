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
package collections

import (
	"reflect"
	"slices"
	"testing"
)

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
