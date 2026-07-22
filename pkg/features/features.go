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

package features

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/featuregate"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
)

const (
	// owner: @GiuseppeTT
	// kep: https://github.com/kubernetes-sigs/jobset/blob/main/keps/467-InPlaceRestart/README.md
	//
	// Enables in-place restart, allowing JobSet workloads to restart much faster by restarting
	// healthy Pods in-place to skip the Pod deletion+schedule+creation process.
	InPlaceRestart featuregate.Feature = "InPlaceRestart"

	// owner: @kannon92
	//
	// Enables TLSOptions for TLSMinVersion and CipherSuites for JobSet servers.
	TLSOptions featuregate.Feature = "TLSOptions"

	// owner: @GiuseppeTT
	// kep: https://github.com/kubernetes-sigs/jobset/blob/main/keps/262-ConfigurableFailurePolicy/README.md
	//
	// Enables the restart actions `RestartJob` and `RestartJobAndIgnoreMaxRestarts`, allowing JobSet to
	// recover from failures (i.e., failed Jobs) by recreating only the failed Job instead of all Jobs
	// as in `RestartJobSet`.
	RestartJob featuregate.Feature = "RestartJob"

	// owner: @aniket2405
	// kep: https://github.com/kubernetes-sigs/jobset/blob/main/keps/463-ElasticJobsets/README.md
	//
	// ElasticJobSet enables the mutation of parallelism and completions for ReplicatedJobs
	// to support dynamic horizontal pod-level scaling.
	ElasticJobSet featuregate.Feature = "ElasticJobSet"

	// owner: @kannon92
	// kep: https://kep.k8s.io/5440
	//
	// SuspendedJobResourceMutation enables mutation of container/initContainer resource
	// requests/limits on a ReplicatedJob's pod template while the JobSet is suspended (or
	// getting suspended), so that integrators (e.g., Kueue/DWS) can right-size Pods before
	// the JobSet is resumed. This relies on the Kubernetes `MutablePodResourcesForSuspendedJobs`
	// feature gate, which was introduced as alpha (disabled by default) in Kubernetes 1.35.
	SuspendedJobResourceMutation featuregate.Feature = "SuspendedJobResourceMutation"
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
	InPlaceRestart: {Default: false, PreRelease: featuregate.Alpha},

	TLSOptions: {Default: true, PreRelease: featuregate.Beta},

	RestartJob: {Default: false, PreRelease: featuregate.Alpha},

	ElasticJobSet: {Default: false, PreRelease: featuregate.Alpha},

	SuspendedJobResourceMutation: {Default: false, PreRelease: featuregate.Alpha},
}

func SetFeatureGateDuringTest(tb testing.TB, f featuregate.Feature, value bool) {
	featuregatetesting.SetFeatureGateDuringTest(tb, utilfeature.DefaultFeatureGate, f, value)
}

// Enabled is helper for `utilfeature.DefaultFeatureGate.Enabled()`
func Enabled(f featuregate.Feature) bool {
	return utilfeature.DefaultFeatureGate.Enabled(f)
}
