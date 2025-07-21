/*
Copyright 2025.

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
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
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

type mockWarningHandler struct {
	loggedMessages []string
}

func (handler *mockWarningHandler) HandleWarningHeaderWithContext(ctx context.Context, code int, agent string, text string) {
	handler.loggedMessages = append(handler.loggedMessages, text)
}
