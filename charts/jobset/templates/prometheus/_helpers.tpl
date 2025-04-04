{{- /*
Copyright 2025 The Kubernetes authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/ -}}

{{/*
Create the name prefix of the resources associated with Prometheus metrics.
*/}}
{{- define "jobset.metrics.name" -}}
{{ include "jobset.fullname" . }}-metrics
{{- end -}}

{{/*
Create the name of the jobset Prometheus metrics service.
*/}}
{{- define "jobset.metrics.service.name" -}}
{{ include "jobset.metrics.name" . }}-service
{{- end -}}

{{/*
Create the name of the jobset Prometheus service monitor.
*/}}
{{- define "jobset.metrics.serviceMonitor.name" -}}
{{ include "jobset.metrics.name" . }}-service-monitor
{{- end -}}

{{/*
Create the name of the jobset serving metrics TLS secret.
*/}}
{{- define "jobset.metrics.secret.name" -}}
{{ include "jobset.fullname" . }}-metrics-server-cert
{{- end -}}
