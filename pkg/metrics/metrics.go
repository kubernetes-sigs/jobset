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
			Name:      "failed_total",
			Help:      `The total number of failed JobSets`,
		}, []string{"jobset_name"},
	)

	CompletedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: constants.JobSetSubsystemName,
			Name:      "completed_total",
			Help:      `The total number of completed JobSets`,
		}, []string{"jobset_name"},
	)

	JobSetTerminalState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: constants.JobSetSubsystemName,
			Name:      "terminal_state",
			Help:      `The current number of JobSets in terminal state (e.g. Completed, Failed)`,
		}, []string{"jobset_name", "terminal_state"},
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

// RecordJobSetTerminalState records the terminal state of a JobSet.
// It sets the value of the "terminal_state" gauge metric for the given jobSet, namespace, and phase.
// label values:
// - namespaceName: The namespace/name of the JobSet.
// - phase: The terminal state of the JobSet (e.g., "Completed" or "Failed").
// - value: The value to set for the gauge metric (typically 1.0 for active or 0.0 for inactive).
func RecordJobSetTerminalState(namespaceName, phase string, value float64) {
	JobSetTerminalState.WithLabelValues(namespaceName, phase).Set(value)
}

func Register() {
	metrics.Registry.MustRegister(
		FailedTotal,
		CompletedTotal,
		JobSetTerminalState,
	)
}
