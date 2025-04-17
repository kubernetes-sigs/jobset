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
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestJobSetFailed(t *testing.T) {
	prometheus.MustRegister(FailedTotal)
	FailedTotal.Reset()

	JobSetFailed("jobset-test1", "default")
	JobSetFailed("jobset-test2", "default")
	JobSetFailed("jobset-test1", "default")

	if count := testutil.CollectAndCount(FailedTotal); count != 2 {
		t.Errorf("Expecting %d metrics, got: %d", 2, count)
	}

	if count := testutil.ToFloat64(FailedTotal.WithLabelValues("jobset-test1", "default")); count != float64(2) {
		t.Errorf("Expecting %s to have value %d, but got %f", "default/jobset-test1", 2, count)
	}

	if count := testutil.ToFloat64(FailedTotal.WithLabelValues("jobset-test2", "default")); count != float64(1) {
		t.Errorf("Expecting %s to have value %d, but got %f", "default/jobset-test2", 1, count)
	}
}

func TestJobSetCompleted(t *testing.T) {
	prometheus.MustRegister(CompletedTotal)
	CompletedTotal.Reset()

	JobSetCompleted("jobset-test1", "default")
	JobSetCompleted("jobset-test2", "default")
	JobSetCompleted("jobset-test1", "default")

	if count := testutil.CollectAndCount(CompletedTotal); count != 2 {
		t.Errorf("Expecting %d metrics, got: %d", 2, count)
	}

	if count := testutil.ToFloat64(CompletedTotal.WithLabelValues("jobset-test1", "default")); count != float64(2) {
		t.Errorf("Expecting %s to have value %d, but got %f", "default/jobset-test1", 2, count)
	}

	if count := testutil.ToFloat64(CompletedTotal.WithLabelValues("jobset-test2", "default")); count != float64(1) {
		t.Errorf("Expecting %s to have value %d, but got %f", "default/jobset-test2", 1, count)
	}
}
