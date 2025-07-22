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
	"context"
	"strings"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// FilterWarningFunc is a filter function for warning log entries.
type FilterWarningFunc func(ctx context.Context, code int, agent string, text string) bool

// FilteringWarningHandler is a warning handler that filters entries by the given filter function.
// A log entry is accepted when the given filter function returns true.
type FilteringWarningHandler struct {
	next   rest.WarningHandlerWithContext
	filter FilterWarningFunc
}

// NewFilteringWarningHandler creates a new filtering warning handler for the given filter function.
func NewFilteringWarningHandler(next rest.WarningHandlerWithContext, filter FilterWarningFunc) *FilteringWarningHandler {
	return &FilteringWarningHandler{
		next:   next,
		filter: filter,
	}
}

// HandleWarningHeaderWithContext implements rest.WarningHandlerWithContext.
func (handler FilteringWarningHandler) HandleWarningHeaderWithContext(ctx context.Context, code int, agent string, text string) {
	if !handler.filter(ctx, code, agent, text) {
		return
	}
	handler.next.HandleWarningHeaderWithContext(ctx, code, agent, text)
}

// NewDefaultWarningHandler returns the warning handler being used by JobSet.
//
// It is currently set up to ignore unknown creationTimestamp field log entries,
// otherwise it uses the same log configuration as the default warning handler.
func NewDefaultWarningHandler() rest.WarningHandlerWithContext {
	return NewFilteringWarningHandler(log.NewKubeAPIWarningLogger(
		log.KubeAPIWarningLoggerOptions{
			Deduplicate: false,
		},
	), func(ctx context.Context, code int, agent string, text string) bool {
		return !strings.HasPrefix(text, `unknown field`) || !strings.HasSuffix(text, `.metadata.creationTimestamp"`)
	})
}
