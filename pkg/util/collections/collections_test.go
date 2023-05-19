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
