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
		}, []string{"jobset_name", "namespace"},
	)

	CompletedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: constants.JobSetSubsystemName,
			Name:      "completed_total",
			Help:      `The total number of completed JobSets`,
		}, []string{"jobset_name", "namespace"},
	)
)

// JobSetFailed records the failed case
// label values: name, namespace
func JobSetFailed(name, namespace string) {
	FailedTotal.WithLabelValues(name, namespace).Inc()
}

// JobSetCompleted records the completed case
// label values: name, namespace
func JobSetCompleted(name, namespace string) {
	CompletedTotal.WithLabelValues(name, namespace).Inc()
}

func Register() {
	metrics.Registry.MustRegister(
		FailedTotal,
		CompletedTotal,
	)
}
