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
	"fmt"

	"github.com/go-logr/logr"
)

func newBufferLogger(b *bytes.Buffer) logr.Logger {
	return logr.New(newBufferLogSink(b))
}

// bufferLogSink implements logr.LogSink wrapping a buffer.
type bufferLogSink struct {
	b    *bytes.Buffer
	name string
	kv   []any
}

func newBufferLogSink(b *bytes.Buffer) *bufferLogSink {
	return &bufferLogSink{b: b}
}

var _ logr.LogSink = (*bufferLogSink)(nil)

func (sink *bufferLogSink) Init(info logr.RuntimeInfo) {}

func (sink *bufferLogSink) Enabled(level int) bool {
	return true
}

func (sink *bufferLogSink) Info(level int, msg string, keysAndValues ...any) {
	sink.write("INFO", msg, keysAndValues...)
}

func (sink *bufferLogSink) Error(err error, msg string, keysAndValues ...any) {
	sink.write("ERROR", msg, keysAndValues...)
}

func (sink *bufferLogSink) write(prefix string, msg string, keysAndValues ...any) {
	sink.b.WriteString(prefix)
	sink.b.WriteString(" ")
	if sink.name != "" {
		sink.b.WriteString("[")
		sink.b.WriteString(sink.name)
		sink.b.WriteString("] ")
	}
	sink.b.WriteString(msg)

	kv := append(sink.kv, keysAndValues...)
	pop := func() any {
		if len(kv) == 0 {
			return "!MISSING VALUE!"
		}
		v := kv[0]
		kv = kv[1:]
		return v
	}

	for len(kv) > 0 {
		k, v := pop(), pop()
		fmt.Fprintf(sink.b, " %v=%v\n", k, v)
	}
	sink.b.WriteString("\n")
}

func (sink *bufferLogSink) WithValues(keysAndValues ...any) logr.LogSink {
	return &bufferLogSink{
		b:    sink.b,
		name: sink.name,
		kv:   append(sink.kv, keysAndValues...),
	}
}

func (sink *bufferLogSink) WithName(name string) logr.LogSink {
	if sink.name != "" {
		name = "." + name
	}
	return &bufferLogSink{
		b:    sink.b,
		name: sink.name + name,
		kv:   sink.kv,
	}
}
