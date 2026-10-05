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
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"k8s.io/klog"
	"k8s.io/kube-openapi/pkg/common"
	"k8s.io/kube-openapi/pkg/validation/spec"

	_ "k8s.io/code-generator"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

// Generate OpenAPI spec definitions for API resources
func main() {
	if len(os.Args) <= 1 {
		klog.Fatal("Supply a version")
	}
	version := os.Args[1]
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	var oAPIDefs = map[string]common.OpenAPIDefinition{}
	defs := spec.Definitions{}

	// Get Kubernetes version
	var k8sVersion string
	info, ok := debug.ReadBuildInfo()
	if !ok {
		fmt.Println("Failed to read build info")
		return
	}

	for _, dep := range info.Deps {
		if dep.Path == "k8s.io/api" {
			k8sVersion = strings.ReplaceAll(dep.Version, "v0.", "v1.")
		}
	}
	if k8sVersion == "" {
		fmt.Println("OpenAPI spec generation failed. Unable to get Kubernetes version")
		return
	}

	k8sOpenAPISpec := fmt.Sprintf("https://raw.githubusercontent.com/kubernetes/kubernetes/refs/tags/%s/api/openapi-spec/swagger.json", k8sVersion)
	refCallback := func(name string) spec.Ref {
		// Kubernetes' published swagger.json does not contain the composite
		// scheduling types, even though they are part of k8s.io/api. Keep those
		// definitions local so SDK generation can resolve them.
		switch name {
		case compositeSchedulingPolicy, compositeSchedulingConstraints, compositeDisruptionMode:
			return localRef(name)
		}
		if strings.HasPrefix(name, "k8s.io") {
			return spec.MustCreateRef(k8sOpenAPISpec + "#/definitions/" + swaggify(name))
		}
		return localRef(name)
	}

	for k, v := range jobset.GetOpenAPIDefinitions(refCallback) {
		oAPIDefs[k] = v
	}

	for defName, val := range oAPIDefs {
		defs[swaggify(defName)] = val.Schema
	}
	addCompositeSchedulingDefinitions(defs, k8sOpenAPISpec)
	swagger := spec.Swagger{
		SwaggerProps: spec.SwaggerProps{
			Swagger:     "2.0",
			Definitions: defs,
			Paths:       &spec.Paths{Paths: map[string]spec.PathItem{}},
			Info: &spec.Info{
				InfoProps: spec.InfoProps{
					Title:       "JobSet SDK",
					Description: "Python SDK for the JobSet API",
					Version:     version,
				},
			},
		},
	}
	jsonBytes, err := json.MarshalIndent(swagger, "", "  ")
	if err != nil {
		klog.Fatal(err.Error())
	}
	fmt.Println(string(jsonBytes))
}

const (
	compositeSchedulingPolicy      = "k8s.io/api/scheduling/v1alpha3.WorkloadCompositePodGroupSchedulingPolicy"
	compositeBasicSchedulingPolicy = "k8s.io/api/scheduling/v1alpha3.WorkloadCompositePodGroupBasicSchedulingPolicy"
	compositeGangSchedulingPolicy  = "k8s.io/api/scheduling/v1alpha3.WorkloadCompositePodGroupGangSchedulingPolicy"
	compositeSchedulingConstraints = "k8s.io/api/scheduling/v1alpha3.WorkloadCompositePodGroupSchedulingConstraints"
	compositeDisruptionMode        = "k8s.io/api/scheduling/v1alpha3.WorkloadCompositePodGroupDisruptionMode"
	compositeSingleDisruptionMode  = "k8s.io/api/scheduling/v1alpha3.WorkloadCompositePodGroupSingleDisruptionMode"
	compositeAllDisruptionMode     = "k8s.io/api/scheduling/v1alpha3.WorkloadCompositePodGroupAllDisruptionMode"
	topologyConstraint             = "k8s.io/api/scheduling/v1alpha3.TopologyConstraint"
)

func addCompositeSchedulingDefinitions(defs spec.Definitions, k8sOpenAPISpec string) {
	defs[swaggify(compositeSchedulingPolicy)] = spec.Schema{SchemaProps: spec.SchemaProps{
		Type: []string{"object"},
		Properties: map[string]spec.Schema{
			"basic": {SchemaProps: spec.SchemaProps{Ref: localRef(compositeBasicSchedulingPolicy)}},
			"gang":  {SchemaProps: spec.SchemaProps{Ref: localRef(compositeGangSchedulingPolicy)}},
		},
	}}
	defs[swaggify(compositeBasicSchedulingPolicy)] = spec.Schema{SchemaProps: spec.SchemaProps{
		Type: []string{"object"},
	}}
	minimum := float64(1)
	defs[swaggify(compositeGangSchedulingPolicy)] = spec.Schema{SchemaProps: spec.SchemaProps{
		Type: []string{"object"},
		Properties: map[string]spec.Schema{
			"minGroupCount": {SchemaProps: spec.SchemaProps{
				Type:    []string{"integer"},
				Format:  "int32",
				Minimum: &minimum,
			}},
		},
	}}
	maxItems := int64(1)
	defs[swaggify(compositeSchedulingConstraints)] = spec.Schema{SchemaProps: spec.SchemaProps{
		Type: []string{"object"},
		Properties: map[string]spec.Schema{
			"topology": {SchemaProps: spec.SchemaProps{
				Type:     []string{"array"},
				MaxItems: &maxItems,
				Items: &spec.SchemaOrArray{Schema: &spec.Schema{SchemaProps: spec.SchemaProps{
					Ref: spec.MustCreateRef(k8sOpenAPISpec + "#/definitions/" + swaggify(topologyConstraint)),
				}}},
			}},
		},
	}}
	defs[swaggify(compositeDisruptionMode)] = spec.Schema{SchemaProps: spec.SchemaProps{
		Type: []string{"object"},
		Properties: map[string]spec.Schema{
			"single": {SchemaProps: spec.SchemaProps{Ref: localRef(compositeSingleDisruptionMode)}},
			"all":    {SchemaProps: spec.SchemaProps{Ref: localRef(compositeAllDisruptionMode)}},
		},
	}}
	defs[swaggify(compositeSingleDisruptionMode)] = spec.Schema{SchemaProps: spec.SchemaProps{Type: []string{"object"}}}
	defs[swaggify(compositeAllDisruptionMode)] = spec.Schema{SchemaProps: spec.SchemaProps{Type: []string{"object"}}}
}

func localRef(name string) spec.Ref {
	return spec.MustCreateRef("#/definitions/" + swaggify(name))
}

func swaggify(name string) string {
	name = strings.ReplaceAll(name, "sigs.k8s.io/jobset/api/", "")
	name = strings.ReplaceAll(name, "k8s.io", "io.k8s")
	name = strings.ReplaceAll(name, "/", ".")
	return name
}
