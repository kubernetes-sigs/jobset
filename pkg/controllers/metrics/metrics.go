package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// TTLAfterFinishedSubsystem - subsystem name used for this controller.
const TTLAfterFinishedSubsystem = "ttl_after_finished_controller"

var (
	// JobSetDeletionDurationSeconds tracks the time it took to delete the jobset since it
	// became eligible for deletion.
	JobSetDeletionDurationSeconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: TTLAfterFinishedSubsystem,
			Name:      "jobset_deletion_duration_seconds",
			Help:      "The time it took to delete the jobset since it became eligible for deletion",
			// Start with 100ms with the last bucket being [~27m, Inf).
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 14),
		},
	)
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(JobSetDeletionDurationSeconds)
}
