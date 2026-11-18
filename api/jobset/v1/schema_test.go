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

package v1

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// loadCRDVersions returns the versions of the generated JobSet CRD keyed by name.
func loadCRDVersions(t *testing.T) map[string]map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "config", "components", "crd", "bases", "jobset.x-k8s.io_jobsets.yaml"))
	require.NoError(t, err, "reading CRD")

	var crd map[string]any
	require.NoError(t, yaml.Unmarshal(data, &crd), "parsing CRD")

	spec, ok := crd["spec"].(map[string]any)
	require.True(t, ok, "CRD has no spec")
	rawVersions, ok := spec["versions"].([]any)
	require.True(t, ok, "CRD has no spec.versions list")

	versions := map[string]map[string]any{}
	for _, v := range rawVersions {
		version, ok := v.(map[string]any)
		require.True(t, ok, "CRD version entry is not an object")
		name, _ := version["name"].(string)
		versions[name] = version
	}
	require.Contains(t, versions, "v1")
	require.Contains(t, versions, "v1alpha2")
	return versions
}

// TestCRDVersions guards the v1 API until the conversion webhook exists: v1
// must stay unserved and v1alpha2 must stay the served and storage version.
func TestCRDVersions(t *testing.T) {
	versions := loadCRDVersions(t)
	v1, v1alpha2 := versions["v1"], versions["v1alpha2"]

	assert.Equal(t, false, v1["served"], "v1 must not be served before the conversion webhook exists")
	assert.Equal(t, false, v1["storage"], "v1 must not be the storage version")
	assert.Equal(t, true, v1alpha2["served"], "v1alpha2 must remain served")
	assert.Equal(t, true, v1alpha2["storage"], "v1alpha2 must remain the storage version")
}

// TestCRDSchemaV1Changes makes sure v1 differs from v1alpha2 only by the
// planned v1 API changes. It applies those changes to the v1alpha2 schema and
// expects the result to equal the v1 schema, so any other drift fails.
// Descriptions are ignored; CEL rules on spec are checked separately.
//
// The planned changes:
//   - spec.startupPolicy is removed in favor of dependsOn (#956).
//   - spec.replicatedJobs is required with at least one entry (#1380).
//   - spec.failurePolicy.restartStrategy is renamed to restartPolicy (#1090).
//   - status.replicatedJobsStatus[].succeeded is renamed to complete (#723).
//   - The Completed printer column becomes Complete (#723).
func TestCRDSchemaV1Changes(t *testing.T) {
	versions := loadCRDVersions(t)
	v1, v1alpha2 := versions["v1"], versions["v1alpha2"]

	want := stripDescriptions(v1alpha2["schema"]).(map[string]any)
	got := stripDescriptions(v1["schema"]).(map[string]any)

	wantSpec := child(t, want, "openAPIV3Schema", "properties", "spec")
	gotSpec := child(t, got, "openAPIV3Schema", "properties", "spec")
	wantSpecProps := child(t, wantSpec, "properties")
	delete(wantSpecProps, "startupPolicy")
	wantSpec["required"] = []any{"replicatedJobs"}
	child(t, wantSpecProps, "replicatedJobs")["minItems"] = float64(1)

	failurePolicyProps := child(t, wantSpecProps, "failurePolicy", "properties")
	failurePolicyProps["restartPolicy"] = failurePolicyProps["restartStrategy"]
	delete(failurePolicyProps, "restartStrategy")

	rjStatus := child(t, want, "openAPIV3Schema", "properties", "status", "properties", "replicatedJobsStatus", "items")
	rjStatusProps := child(t, rjStatus, "properties")
	rjStatusProps["complete"] = rjStatusProps["succeeded"]
	delete(rjStatusProps, "succeeded")
	rjStatus["required"] = []any{"active", "complete", "failed", "name", "ready", "suspended"}

	// CEL rules on spec are rewritten without startupPolicy; compare them on
	// their own so the rest of the schema can be compared as a whole.
	assert.Equal(t, []any{
		map[string]any{
			"fieldPath": ".scheduling",
			"message":   "top-level scheduling (spec.scheduling without per-ReplicatedJob replicatedJobs entries, including the defaulted top-level gang) cannot be set with DependsOn; configure per-ReplicatedJob scheduling under spec.scheduling.replicatedJobs instead",
			"rule":      "!(has(self.scheduling) && (!has(self.scheduling.replicatedJobs) || size(self.scheduling.replicatedJobs) == 0)) || !has(self.replicatedJobs) || self.replicatedJobs.all(rj, !has(rj.dependsOn) || size(rj.dependsOn) == 0)",
		},
	}, gotSpec["x-kubernetes-validations"], "v1 spec CEL rules")
	delete(wantSpec, "x-kubernetes-validations")
	delete(gotSpec, "x-kubernetes-validations")

	assert.Equal(t, want, got, "v1 schema differs from v1alpha2 beyond the planned v1 changes")
	assert.Equal(t, v1alpha2["subresources"], v1["subresources"], "v1 subresources differ from v1alpha2")

	wantColumns := stripDescriptions(v1alpha2["additionalPrinterColumns"]).([]any)
	for _, c := range wantColumns {
		column := c.(map[string]any)
		if column["name"] == "Completed" {
			column["name"] = "Complete"
			column["jsonPath"] = `.status.conditions[?(@.type=="Complete")].status`
		}
	}
	assert.Equal(t, wantColumns, stripDescriptions(v1["additionalPrinterColumns"]), "v1 printer columns")
}

// child walks a parsed CRD by keys and fails the test if a key is missing.
func child(t *testing.T, m map[string]any, keys ...string) map[string]any {
	t.Helper()
	for _, k := range keys {
		next, ok := m[k].(map[string]any)
		require.True(t, ok, "CRD has no object at %q", k)
		m = next
	}
	return m
}

// stripDescriptions returns a deep copy of v without "description" keys.
func stripDescriptions(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			if k != "description" {
				out[k] = stripDescriptions(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			out[i] = stripDescriptions(val)
		}
		return out
	default:
		return v
	}
}
