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
Create the name of the jobset webhook.
*/}}
{{- define "jobset.webhook.name" -}}
{{ include "jobset.fullname" . }}-webhook
{{- end -}}

{{/*
Common labels of the jobset webhook.
*/}}
{{- define "jobset.webhook.labels" -}}
{{ include "jobset.labels" . }}
app.kubernetes.io/component: webhook
{{- end -}}

{{/*
Create the name of the jobset webhook TLS secret.
*/}}
{{- define "jobset.webhook.secret.name" -}}
{{ include "jobset.webhook.name" . }}-server-cert
{{- end -}}

{{/*
Create the name of the jobset webhook service.
*/}}
{{- define "jobset.webhook.service.name" -}}
{{ include "jobset.webhook.name" . }}-service
{{- end -}}

{{/*
Create the name of the jobset mutating webhook configuration.
*/}}
{{- define "jobset.webhook.mutatingWebhookConfiguration.name" -}}
jobset-mutating-webhook-configuration
{{- end -}}

{{/*
Create the name of the jobset validating webhook configuration.
*/}}
{{- define "jobset.webhook.validatingWebhookConfiguration.name" -}}
jobset-validating-webhook-configuration
{{- end -}}
