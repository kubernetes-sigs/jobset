package controllers

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/events"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/constants"
)

type benchmarkEventSink struct{}

func (s *benchmarkEventSink) Create(ctx context.Context, event *eventsv1.Event) (*eventsv1.Event, error) {
	return event, nil
}

func (s *benchmarkEventSink) Update(ctx context.Context, event *eventsv1.Event) (*eventsv1.Event, error) {
	return event, nil
}

func (s *benchmarkEventSink) Patch(ctx context.Context, event *eventsv1.Event, _ []byte) (*eventsv1.Event, error) {
	return event, nil
}

func BenchmarkEventRecorderV1(b *testing.B) {
	scheme := runtime.NewScheme()
	utilruntime.Must(jobset.AddToScheme(scheme))

	ctx, cancel := context.WithCancel(context.Background())
	b.Cleanup(cancel)

	broadcaster := events.NewBroadcaster(&benchmarkEventSink{})
	if err := broadcaster.StartRecordingToSinkWithContext(ctx); err != nil {
		b.Fatalf("start recording: %v", err)
	}
	b.Cleanup(broadcaster.Shutdown)

	recorder := broadcaster.NewRecorder(scheme, "jobset")
	js := &jobset.JobSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bench",
			Namespace: "default",
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recorder.Eventf(js, nil, corev1.EventTypeWarning, constants.JobCreationFailedReason, constants.JobCreationFailedReason, "benchmark")
	}
}
