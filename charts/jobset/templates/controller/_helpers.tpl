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
Create the name of the jobset controller.
*/}}
{{- define "jobset.controller.name" -}}
{{ include "jobset.fullname" . }}-controller
{{- end -}}

{{/*
Common labels of the jobset controller.
*/}}
{{- define "jobset.controller.labels" -}}
{{ include "jobset.labels" . }}
app.kubernetes.io/component: controller
{{- end -}}

{{/*
Selector labels of the jobset controller.
*/}}
{{- define "jobset.controller.selectorLabels" -}}
{{ include "jobset.selectorLabels" . }}
app.kubernetes.io/component: controller
{{- end -}}

{{/*
Create the name of the jobset controller service account.
*/}}
{{- define "jobset.controller.serviceAccount.name" -}}
{{- include "jobset.controller.name" . }}
{{- end }}


{{/*
Create the name of the jobset controller clusterrole.
*/}}
{{- define "jobset.controller.clusterRole.name" -}}
{{ include "jobset.controller.name" . }}
{{- end -}}

{{/*
Create the name of the jobset controller clusterrole binding.
*/}}
{{- define "jobset.controller.clusterRoleBinding.name" -}}
{{ include "jobset.controller.name" . }}
{{- end -}}

{{/*
Create the name of the jobset controller role.
*/}}
{{- define "jobset.controller.role.name" -}}
{{ include "jobset.controller.name" . }}
{{- end -}}

{{/*
Create the name of the jobset controller role binding.
*/}}
{{- define "jobset.controller.roleBinding.name" -}}
{{ include "jobset.controller.name" . }}
{{- end -}}

{{/*
Create the name of the jobset controller configmap.
*/}}
{{- define "jobset.controller.configMap.name" -}}
{{ include "jobset.controller.name" . }}-config
{{- end -}}

{{/*
Create the name of the jobset controller deployment.
*/}}
{{- define "jobset.controller.deployment.name" -}}
{{ include "jobset.controller.name" . }}
{{- end -}}


{{/*
Create the name of jobset controller image.
*/}}
{{- define "jobset.controller.image" -}}
{{ include "jobset.image" . }}
{{- end -}}
