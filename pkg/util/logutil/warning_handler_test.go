/*
Copyright 2025 The Kubernetes Authors.

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

package logutil

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func TestFilteringWarningHandler(t *testing.T) {
	testCases := []struct {
		name                   string
		inputMessages          []string
		filter                 FilterWarningFunc
		expectedOutputMessages []string
	}{
		{
			name: "message is dropped on filter returning false",
			inputMessages: []string{
				"pass",
				"drop this",
				"pass",
				"drop this also",
			},
			filter: func(ctx context.Context, code int, agent string, text string) bool {
				return text == "pass"
			},
			expectedOutputMessages: []string{
				"pass",
				"pass",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var next mockWarningHandler
			handler := NewFilteringWarningHandler(&next, tc.filter)
			for _, msg := range tc.inputMessages {
				handler.HandleWarningHeaderWithContext(context.TODO(), 0, "", msg)
			}
			if !cmp.Equal(next.loggedMessages, tc.expectedOutputMessages) {
				t.Errorf("Logged messages do not match:\n\n%s", cmp.Diff(tc.expectedOutputMessages, next.loggedMessages))
			}
		})
	}
}

func TestDefaultWarningHandler(t *testing.T) {
	testCases := []struct {
		name                       string
		inputMessages              []string
		expectedOutputBufferString string
	}{
		{
			name: "unknown creationTimestamp field log entry is dropped",
			inputMessages: []string{
				"pass",
				`unknown field "spec.replicatedJobs[0].template.metadata.creationTimestamp"`,
				"pass",
				`unknown field "spec.replicatedJobs[1].template.metadata.creationTimestamp"`,
				`unknown field "spec.replicatedJobs[1].template.metadata.whatever"`,
				`whatever "spec.replicatedJobs[1].template.metadata.creationTimestamp"`,
			},
			expectedOutputBufferString: `INFO pass
INFO pass
INFO unknown field "spec.replicatedJobs[1].template.metadata.whatever"
INFO whatever "spec.replicatedJobs[1].template.metadata.creationTimestamp"
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			logger := newBufferLogger(&b)
			ctx := log.IntoContext(context.Background(), logger)
			handler := NewDefaultWarningHandler()
			for _, msg := range tc.inputMessages {
				handler.HandleWarningHeaderWithContext(ctx, 299, "", msg)
			}
			if got := b.String(); !cmp.Equal(got, tc.expectedOutputBufferString) {
				t.Errorf("Logging buffers do not match:\n\n%s", cmp.Diff(tc.expectedOutputBufferString, got))
			}
		})
	}
}

type mockWarningHandler struct {
	loggedMessages []string
}

func (handler *mockWarningHandler) HandleWarningHeaderWithContext(ctx context.Context, code int, agent string, text string) {
	handler.loggedMessages = append(handler.loggedMessages, text)
}
