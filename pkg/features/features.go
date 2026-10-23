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
	"fmt"
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
	// Graduated to GA; locked to its default (enabled).
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

	// owner: @jianqiaol
	// kep: https://github.com/kubernetes-sigs/jobset/blob/main/keps/1282-execution-attempts/README.md
	//
	// Enables tracking and propagating JobSet execution attempts as a monotonic counter.
	ExecutionAttemptsTracking featuregate.Feature = "ExecutionAttemptsTracking"

	// owner: @yindia
	// kep: https://github.com/kubernetes-sigs/jobset/blob/main/keps/1186-active-deadline-seconds/README.md
	//
	// Enables enforcement of the JobSet-level spec.activeDeadlineSeconds, which bounds the
	// continuous active runtime of a JobSet before the controller marks it Failed and deletes
	// its active Jobs. The .status.startTime field is only maintained while this gate is
	// enabled; when the gate is off the controller clears any existing value.
	JobSetActiveDeadlineSeconds featuregate.Feature = "JobSetActiveDeadlineSeconds"

	// owner: @kehannon
	// kep: https://github.com/kubernetes-sigs/jobset/blob/main/keps/969-WAS-integration/README.md
	//
	// JobSetWorkloadAwareSchedulingAPI enables integration with Workload-Aware Scheduling (WAS)
	// building blocks from scheduling.k8s.io, allowing users to configure gang scheduling,
	// topology constraints, and disruption policies on JobSets.
	JobSetWorkloadAwareSchedulingAPI featuregate.Feature = "JobSetWorkloadAwareSchedulingAPI"

	// owner: @yindia
	// kep: https://github.com/kubernetes-sigs/jobset/blob/main/keps/969-WAS-integration/README.md
	//
	// JobSetGangOfGangs enables the composite PodGroup (gang of gangs) scheduling model,
	// allowing a top-level JobSet scheduling policy and per-ReplicatedJob policies to be
	// combined into one CompositePodGroup tree instead of being mutually exclusive.
	//
	// When enabled, the webhook accepts a JobSet that sets both a top-level policy and
	// replicatedJobs entries, and the controller materializes the CompositePodGroup tree
	// (root and per-entry child CompositePodGroups plus leaf PodGroups, linked through
	// spec.parentCompositePodGroupName). With the gate off, that combination is rejected at
	// admission and behavior is unchanged.
	//
	// Requires JobSetWorkloadAwareSchedulingAPI: the gang-of-gangs model is reachable only
	// through spec.scheduling, which the webhook rejects unless that gate is also enabled.
	// Enabling JobSetGangOfGangs alone is therefore inert; see ValidateFeatureGates, which
	// rejects that misconfiguration at startup.
	JobSetGangOfGangs featuregate.Feature = "JobSetGangOfGangs"
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

	TLSOptions: {Default: true, PreRelease: featuregate.GA, LockToDefault: true},

	RestartJob: {Default: false, PreRelease: featuregate.Alpha},

	ElasticJobSet: {Default: false, PreRelease: featuregate.Alpha},

	ExecutionAttemptsTracking: {Default: false, PreRelease: featuregate.Alpha},

	JobSetActiveDeadlineSeconds: {Default: false, PreRelease: featuregate.Alpha},

	JobSetWorkloadAwareSchedulingAPI: {Default: false, PreRelease: featuregate.Alpha},

	JobSetGangOfGangs: {Default: false, PreRelease: featuregate.Alpha},
}

// ValidateFeatureGates rejects unsupported feature-gate combinations at startup. It is a
// fail-fast guard so an operator learns about a misconfiguration immediately rather than through
// silently inert behavior.
func ValidateFeatureGates() error {
	// JobSetGangOfGangs is only reachable through spec.scheduling, which the webhook rejects
	// unless JobSetWorkloadAwareSchedulingAPI is enabled. Enabling it alone is inert, so reject
	// the combination rather than let it look enabled but do nothing.
	if Enabled(JobSetGangOfGangs) && !Enabled(JobSetWorkloadAwareSchedulingAPI) {
		return fmt.Errorf("feature gate %s requires %s to be enabled", JobSetGangOfGangs, JobSetWorkloadAwareSchedulingAPI)
	}
	return nil
}

func SetFeatureGateDuringTest(tb testing.TB, f featuregate.Feature, value bool) {
	featuregatetesting.SetFeatureGateDuringTest(tb, utilfeature.DefaultFeatureGate, f, value)
}

// Enabled is helper for `utilfeature.DefaultFeatureGate.Enabled()`
func Enabled(f featuregate.Feature) bool {
	return utilfeature.DefaultFeatureGate.Enabled(f)
}
