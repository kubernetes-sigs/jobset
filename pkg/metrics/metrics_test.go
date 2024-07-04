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
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestFailedCase(t *testing.T) {
	prometheus.MustRegister(FailedTotal)

	FailedCase(fmt.Sprintf("%s/%s", "default", "jobset-test1"))
	FailedCase(fmt.Sprintf("%s/%s", "default", "jobset-test2"))
	FailedCase(fmt.Sprintf("%s/%s", "default", "jobset-test1"))

	if count := testutil.CollectAndCount(FailedTotal); count != 2 {
		t.Errorf("Expecting %d metrics, got: %d", 2, count)
	}

	if count := testutil.ToFloat64(FailedTotal.WithLabelValues(fmt.Sprintf("%s/%s", "default", "jobset-test1"))); count != float64(2) {
		t.Errorf("Expecting %s to have value %d, but got %f", fmt.Sprintf("%s/%s", "default", "jobset-test1"), 2, count)
	}

	if count := testutil.ToFloat64(FailedTotal.WithLabelValues(fmt.Sprintf("%s/%s", "default", "jobset-test2"))); count != float64(1) {
		t.Errorf("Expecting %s to have value %d, but got %f", fmt.Sprintf("%s/%s", "default", "jobset-test2"), 1, count)
	}
}

func TestCompletedCase(t *testing.T) {
	prometheus.MustRegister(CompletedTotal)

	CompletedCase(fmt.Sprintf("%s/%s", "default", "jobset-test1"))
	CompletedCase(fmt.Sprintf("%s/%s", "default", "jobset-test2"))
	CompletedCase(fmt.Sprintf("%s/%s", "default", "jobset-test1"))

	if count := testutil.CollectAndCount(CompletedTotal); count != 2 {
		t.Errorf("Expecting %d metrics, got: %d", 2, count)
	}

	if count := testutil.ToFloat64(CompletedTotal.WithLabelValues(fmt.Sprintf("%s/%s", "default", "jobset-test1"))); count != float64(2) {
		t.Errorf("Expecting %s to have value %d, but got %f", "lws-sample-0", 2, count)
	}

	if count := testutil.ToFloat64(CompletedTotal.WithLabelValues(fmt.Sprintf("%s/%s", "default", "jobset-test2"))); count != float64(1) {
		t.Errorf("Expecting %s to have value %d, but got %f", "lws-sample-1", 1, count)
	}
}
