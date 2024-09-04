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

package features

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/util/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/featuregate"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
)

const (
// Example
// owner: <feature owner>
// kep: <KEP link>
// alpha: v0.5
// beta: v0.6
//
// Enables example.
// Example featuregate.Feature = "Example"
)

func init() {
	runtime.Must(utilfeature.DefaultMutableFeatureGate.Add(defaultFeatureGates))
}

// defaultFeatureGates consists of all known Jobset-specific feature keys.
// To add a new feature, define a key for it above and add it here. The features will be
// available throughout Jobset binaries.
//
// Entries are separated from each other with blank lines to avoid sweeping gofmt changes
// when adding or removing one entry.
var defaultFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	// Example: {Default: true, PreRelease: featuregate.Beta},
}

func SetFeatureGateDuringTest(tb testing.TB, f featuregate.Feature, value bool) {
	featuregatetesting.SetFeatureGateDuringTest(tb, utilfeature.DefaultFeatureGate, f, value)
}

// Enabled is helper for `utilfeature.DefaultFeatureGate.Enabled()`
func Enabled(f featuregate.Feature) bool {
	return utilfeature.DefaultFeatureGate.Enabled(f)
}

// SetEnable helper function that can be used to set the enabled value of a feature gate,
// it should only be used in integration test pending the merge of
// https://github.com/kubernetes/kubernetes/pull/118346
func SetEnable(f featuregate.Feature, v bool) error {
	return utilfeature.DefaultMutableFeatureGate.Set(fmt.Sprintf("%s=%v", f, v))
}
