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

// TestCRDSchemaMatchesV1alpha2 makes sure v1 stays an exact copy of v1alpha2.
// The CRD has no conversion strategy yet, which is only safe while both
// schemas are the same. Field changes come with the conversion webhook.
func TestCRDSchemaMatchesV1alpha2(t *testing.T) {
	versions := loadCRDVersions(t)
	v1, v1alpha2 := versions["v1"], versions["v1alpha2"]

	for _, field := range []string{"schema", "subresources", "additionalPrinterColumns"} {
		assert.Equal(t, v1alpha2[field], v1[field], "v1 %s differs from v1alpha2", field)
	}
}
