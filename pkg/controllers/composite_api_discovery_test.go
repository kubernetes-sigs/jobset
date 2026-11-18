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
	"testing"

	schedulingv1alpha3 "k8s.io/api/scheduling/v1alpha3"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

func compositeTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	utilruntime.Must(schedulingv1alpha3.AddToScheme(s))
	return s
}

func compositeGVK(t *testing.T, s *runtime.Scheme) schema.GroupVersionKind {
	t.Helper()
	gvks, _, err := s.ObjectKinds(&schedulingv1alpha3.CompositePodGroup{})
	if err != nil {
		t.Fatalf("resolving CompositePodGroup GVK: %v", err)
	}
	return gvks[0]
}

func TestCompositePodGroupAPIServed(t *testing.T) {
	s := compositeTestScheme(t)
	gvk := compositeGVK(t, s)

	t.Run("served when the mapper has the mapping", func(t *testing.T) {
		mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{gvk.GroupVersion()})
		mapper.Add(gvk, apimeta.RESTScopeNamespace)
		served, err := compositePodGroupAPIServed(s, mapper)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !served {
			t.Fatalf("expected served=true when the mapping exists")
		}
	})

	t.Run("not served when the mapper has no mapping", func(t *testing.T) {
		// Empty mapper returns a no-match error, which must be treated as
		// "not served" rather than a hard failure.
		mapper := apimeta.NewDefaultRESTMapper(nil)
		served, err := compositePodGroupAPIServed(s, mapper)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if served {
			t.Fatalf("expected served=false when the mapping is absent")
		}
	})
}
