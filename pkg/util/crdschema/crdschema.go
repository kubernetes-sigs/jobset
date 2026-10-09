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

// Package crdschema post-processes the CRD manifests emitted by controller-gen.
//
// JobSet transitively embeds batch/v1.JobSpec. Upstream Kubernetes types bound
// their lists with the declarative validation marker +k8s:maxItems, but
// controller-gen does not implement that marker, so the bound is silently
// dropped from the generated schema. It does implement +k8s:immutable, leaving
// a `self == oldSelf` CEL rule on an unbounded list. The API server estimates
// the cost of that rule from the declared bounds and rejects the CRD.
package crdschema

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/install"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/validation"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"
)

var docSeparator = []byte("---\n")

// missingArrayBounds contains bounds declared by upstream Kubernetes types with
// markers that controller-gen does not currently implement. Remove entries once
// controller-gen emits the bounds itself.
var missingArrayBounds = map[string]int64{
	// batch/v1.JobSchedulingConfiguration.ResourceClaims.
	"resourceClaims": 4,
}

var scheme = runtime.NewScheme()

func init() {
	install.Install(scheme)
}

// ApplyBounds restores dropped array bounds in every version of the CRD and
// returns the number of bounds added.
func ApplyBounds(crd *apiextensionsv1.CustomResourceDefinition) int {
	added := 0
	for i := range crd.Spec.Versions {
		if schema := crd.Spec.Versions[i].Schema; schema != nil {
			added += applyBounds(schema.OpenAPIV3Schema)
		}
	}
	return added
}

func applyBounds(schema *apiextensionsv1.JSONSchemaProps) int {
	if schema == nil {
		return 0
	}
	added := 0
	for name, property := range schema.Properties {
		if maxItems, found := missingArrayBounds[name]; found &&
			property.Type == "array" && property.MaxItems == nil && len(property.XValidations) > 0 {
			property.MaxItems = ptr.To(maxItems)
			added++
		}
		added += applyBounds(&property)
		schema.Properties[name] = property
	}
	if schema.Items != nil {
		added += applyBounds(schema.Items.Schema)
		for i := range schema.Items.JSONSchemas {
			added += applyBounds(&schema.Items.JSONSchemas[i])
		}
	}
	if schema.AdditionalProperties != nil {
		added += applyBounds(schema.AdditionalProperties.Schema)
	}
	return added
}

// Validate reports whether the API server would accept the CRD.
func Validate(crd *apiextensionsv1.CustomResourceDefinition) error {
	internal := &apiextensions.CustomResourceDefinition{}
	if err := scheme.Convert(crd, internal, nil); err != nil {
		return fmt.Errorf("converting %q to the internal version: %w", crd.Name, err)
	}
	// Generated manifests have no status. Seed the fields expected when the API
	// server validates an existing CRD so validation reaches the schema.
	for _, version := range internal.Spec.Versions {
		if version.Storage {
			internal.Status.StoredVersions = append(internal.Status.StoredVersions, version.Name)
		}
	}
	internal.Status.AcceptedNames = internal.Spec.Names
	if errs := validation.ValidateCustomResourceDefinition(context.Background(), internal); len(errs) > 0 {
		return fmt.Errorf("%q would be rejected by the API server: %w", crd.Name, errs.ToAggregate())
	}
	return nil
}

// Patch restores missing bounds in a generated CRD manifest and validates the
// result. Manifests that need no bounds are returned byte for byte unchanged.
func Patch(manifest []byte) ([]byte, int, error) {
	preamble, body := split(manifest)
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(body, &crd); err != nil {
		return nil, 0, fmt.Errorf("parsing CRD manifest: %w", err)
	}
	added := ApplyBounds(&crd)
	if err := Validate(&crd); err != nil {
		return nil, 0, err
	}
	if added == 0 {
		return manifest, 0, nil
	}
	encoded, err := encode(&crd)
	if err != nil {
		return nil, 0, err
	}
	return append(preamble, encoded...), added, nil
}

func split(manifest []byte) (preamble, body []byte) {
	if i := bytes.Index(manifest, docSeparator); i >= 0 && (i == 0 || manifest[i-1] == '\n') {
		end := i + len(docSeparator)
		return bytes.Clone(manifest[:end]), manifest[end:]
	}
	return nil, manifest
}

// encode marshals the CRD the same way controller-gen does, without the empty
// status introduced by marshaling a typed object.
func encode(crd *apiextensionsv1.CustomResourceDefinition) ([]byte, error) {
	encoded, err := json.Marshal(crd)
	if err != nil {
		return nil, fmt.Errorf("encoding CRD %q: %w", crd.Name, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var object map[string]any
	if err = decoder.Decode(&object); err != nil {
		return nil, fmt.Errorf("decoding CRD %q: %w", crd.Name, err)
	}
	delete(object, "status")
	encoded, err = json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("re-encoding CRD %q: %w", crd.Name, err)
	}
	out, err := yaml.JSONToYAML(encoded)
	if err != nil {
		return nil, fmt.Errorf("converting CRD %q to YAML: %w", crd.Name, err)
	}
	return out, nil
}
