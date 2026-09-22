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

package crdschema

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var immutableRule = []apiextensionsv1.ValidationRule{{
	Rule:    "self == oldSelf",
	Message: "field is immutable",
}}

func unboundedClaims() apiextensionsv1.JSONSchemaProps {
	return apiextensionsv1.JSONSchemaProps{
		Type: "array",
		Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: &apiextensionsv1.JSONSchemaProps{
			Type: "object",
			Properties: map[string]apiextensionsv1.JSONSchemaProps{
				"name":                      {Type: "string"},
				"resourceClaimName":         {Type: "string"},
				"resourceClaimTemplateName": {Type: "string"},
			},
			Required: []string{"name"},
		}},
		XValidations: immutableRule,
	}
}

func crdWith(properties map[string]apiextensionsv1.JSONSchemaProps) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "tests.jobset.x-k8s.io"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "jobset.x-k8s.io",
			Scope: apiextensionsv1.NamespaceScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural: "tests", Singular: "test", Kind: "Test", ListKind: "TestList",
			},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name: "v1alpha2", Served: true, Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"spec": {Type: "object", Properties: properties},
					},
				}},
			}},
		},
	}
}

func TestApplyBounds(t *testing.T) {
	bounded := unboundedClaims()
	bounded.MaxItems = ptr.To[int64](8)
	tests := map[string]struct {
		property     apiextensionsv1.JSONSchemaProps
		wantAdded    int
		wantMaxItems *int64
	}{
		"unbounded list with CEL rule": {unboundedClaims(), 1, ptr.To[int64](4)},
		"existing bound":               {bounded, 0, ptr.To[int64](8)},
		"list without CEL rule":        {apiextensionsv1.JSONSchemaProps{Type: "array", Items: unboundedClaims().Items}, 0, nil},
		"non-list property":            {apiextensionsv1.JSONSchemaProps{Type: "object", XValidations: immutableRule}, 0, nil},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			crd := crdWith(map[string]apiextensionsv1.JSONSchemaProps{"resourceClaims": test.property})
			if got := ApplyBounds(crd); got != test.wantAdded {
				t.Errorf("ApplyBounds() = %d, want %d", got, test.wantAdded)
			}
			spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
			if diff := cmp.Diff(test.wantMaxItems, spec.Properties["resourceClaims"].MaxItems); diff != "" {
				t.Errorf("unexpected maxItems (-want +got):\n%s", diff)
			}
		})
	}
}

func replicatedJobsWith(properties map[string]apiextensionsv1.JSONSchemaProps) map[string]apiextensionsv1.JSONSchemaProps {
	return map[string]apiextensionsv1.JSONSchemaProps{
		"replicatedJobs": {
			Type: "array", XListType: ptr.To("map"), XListMapKeys: []string{"name"},
			Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: &apiextensionsv1.JSONSchemaProps{
				Type: "object", Required: []string{"name"},
				Properties: map[string]apiextensionsv1.JSONSchemaProps{
					"name":     {Type: "string"},
					"template": {Type: "object", Properties: properties},
				},
			}},
		},
	}
}

func TestValidateCELCost(t *testing.T) {
	crd := crdWith(replicatedJobsWith(map[string]apiextensionsv1.JSONSchemaProps{
		"resourceClaims": unboundedClaims(),
	}))
	if err := Validate(crd); err == nil || !strings.Contains(err.Error(), "cost") {
		t.Fatalf("Validate() = %v, want a CEL cost budget error", err)
	}
	if got := ApplyBounds(crd); got != 1 {
		t.Fatalf("ApplyBounds() = %d, want 1", got)
	}
	if err := Validate(crd); err != nil {
		t.Errorf("Validate() after ApplyBounds() = %v, want nil", err)
	}
}

func TestPatchGeneratedManifest(t *testing.T) {
	path := filepath.Join("..", "..", "..", "config", "components", "crd", "bases", "jobset.x-k8s.io_jobsets.yaml")
	manifest, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	patched, added, err := Patch(manifest)
	if err != nil {
		t.Fatalf("Patch() = %v, want the CRD to be installable", err)
	}
	if added != 0 {
		t.Errorf("Patch() restored %d bound(s); run `make manifests`", added)
	}
	if !bytes.Equal(manifest, patched) {
		t.Error("Patch() rewrote the generated manifest; run `make manifests`")
	}
}

func TestPatchPreservesPreamble(t *testing.T) {
	crd := crdWith(map[string]apiextensionsv1.JSONSchemaProps{"resourceClaims": unboundedClaims()})
	body, err := encode(crd)
	if err != nil {
		t.Fatal(err)
	}
	preamble := "# Copyright The Kubernetes Authors.\n\n---\n"
	patched, added, err := Patch(append([]byte(preamble), body...))
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Errorf("Patch() restored %d bound(s), want 1", added)
	}
	if !bytes.HasPrefix(patched, []byte(preamble)) {
		t.Error("Patch() dropped the manifest preamble")
	}
}
