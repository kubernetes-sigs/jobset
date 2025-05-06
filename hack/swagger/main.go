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
		if strings.HasPrefix(name, "k8s.io") {
			return spec.MustCreateRef(k8sOpenAPISpec + "#/definitions/" + swaggify(name))
		}
		return spec.MustCreateRef("#/definitions/" + swaggify(name))
	}

	for k, v := range jobset.GetOpenAPIDefinitions(refCallback) {
		oAPIDefs[k] = v
	}

	for defName, val := range oAPIDefs {
		defs[swaggify(defName)] = val.Schema
	}
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

func swaggify(name string) string {
	name = strings.ReplaceAll(name, "sigs.k8s.io/jobset/api/", "")
	name = strings.ReplaceAll(name, "k8s.io", "io.k8s")
	name = strings.ReplaceAll(name, "/", ".")
	return name
}
