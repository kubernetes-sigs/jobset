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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"sigs.k8s.io/jobset/pkg/constants"
)

var (
	FailedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: constants.JobSetSubsystemName,
			Name:      "jobset_failed_total",
			Help:      `The total number of failed JobSets`,
		}, []string{"jobset_name"},
	)

	CompletedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: constants.JobSetSubsystemName,
			Name:      "jobset_completed_total",
			Help:      `The total number of completed JobSets`,
		}, []string{"jobset_name"},
	)
)

// JobSetFailed records the failed case
// label values: namespace/name
func JobSetFailed(namespaceName string) {
	FailedTotal.WithLabelValues(namespaceName).Inc()
}

// JobSetCompleted records the completed case
// label values: namespace/name
func JobSetCompleted(namespaceName string) {
	CompletedTotal.WithLabelValues(namespaceName).Inc()
}

func Register() {
	metrics.Registry.MustRegister(
		FailedTotal,
		CompletedTotal,
	)
}
