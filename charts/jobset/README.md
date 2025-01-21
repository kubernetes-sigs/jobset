# JobSet Helm Chart
![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

This Helm chart installs the JobSet Controller in your Kubernetes cluster. JobSet is a Kubernetes controller that manages groups of related Jobs as a single unit.

## Installing the Chart

To install the chart with the release name `jobset`:

```bash
helm install jobset ./charts/jobset
```

## Configuration

| Key                                       | Type   | Default                      | Description |
|-------------------------------------------|--------|------------------------------|-------------|
| affinity                                  | object | `{}`                         |             |
| certManager.certificate.duration          | string | `"8760h"`                    |             |
| certManager.certificate.renewBefore       | string | `"720h"`                     |             |
| certManager.enabled                       | bool   | `true`                       |             |
| crds.enabled                              | bool   | `true`                       |             |
| crds.install                              | bool   | `true`                       |             |
| fullnameOverride                          | string | `""`                         |             |
| image.pullPolicy                          | string | `"IfNotPresent"`             |             |
| image.repository                          | string | `"jobset-controller"`        |             |
| image.tag                                 | string | `""`                         |             |
| imagePullSecrets                          | list   | `[]`                         |             |
| leaderElection.enabled                    | bool   | `true`                       |             |
| leaderElection.resourceName               | string | `"jobset-leader-election"`   |             |
| manager.healthProbe.livenessInitialDelay  | int    | `15`                         |             |
| manager.healthProbe.livenessPath          | string | `"/healthz"`                 |             |
| manager.healthProbe.livenessTimeout       | int    | `30`                         |             |
| manager.healthProbe.port                  | int    | `8081`                       |             |
| manager.healthProbe.readinessInitialDelay | int    | `5`                          |             |
| manager.healthProbe.readinessPath         | string | `"/readyz"`                  |             |
| manager.healthProbe.readinessTimeout      | int    | `30`                         |             |
| metrics.enabled                           | bool   | `true`                       |             |
| metrics.service.port                      | int    | `8443`                       |             |
| metrics.service.type                      | string | `"ClusterIP"`                |             |
| metrics.serviceMonitor.enabled            | bool   | `false`                      |             |
| metrics.serviceMonitor.interval           | string | `"30s"`                      |             |
| metrics.serviceMonitor.labels             | object | `{}`                         |             |
| metrics.serviceMonitor.scrapeTimeout      | string | `"10s"`                      |             |
| nameOverride                              | string | `""`                         |             |
| nodeSelector                              | object | `{}`                         |             |
| podAnnotations                            | object | `{}`                         |             |
| podSecurityContext.runAsNonRoot           | bool   | `true`                       |             |
| podSecurityContext.seccompProfile.type    | string | `"RuntimeDefault"`           |             |
| rbac.create                               | bool   | `true`                       |             |
| rbac.createAggregateRoles                 | bool   | `true`                       |             |
| replicaCount                              | int    | `1`                          |             |
| resources.limits.cpu                      | int    | `2`                          |             |
| resources.limits.memory                   | string | `"512Mi"`                    |             |
| resources.requests.cpu                    | string | `"500m"`                     |             |
| resources.requests.memory                 | string | `"128Mi"`                    |             |
| securityContext.allowPrivilegeEscalation  | bool   | `false`                      |             |
| securityContext.capabilities.drop[0]      | string | `"ALL"`                      |             |
| serviceAccount.annotations                | object | `{}`                         |             |
| serviceAccount.create                     | bool   | `true`                       |             |
| serviceAccount.name                       | string | `""`                         |             |
| tolerations                               | list   | `[]`                         |             |
| webhook.certManager.enabled               | bool   | `true`                       |             |
| webhook.certManager.issuerGroup           | string | `"cert-manager.io"`          |             |
| webhook.certManager.issuerKind            | string | `"Issuer"`                   |             |
| webhook.certManager.issuerName            | string | `"jobset-selfsigned-issuer"` |             |
| webhook.enabled                           | bool   | `true`                       |             |
| webhook.mutatingWebhook.failurePolicy     | string | `"Fail"`                     |             |
| webhook.mutatingWebhook.timeoutSeconds    | int    | `10`                         |             |
| webhook.port                              | int    | `9443`                       |             |
| webhook.service.port                      | int    | `443`                        |             |
| webhook.service.type                      | string | `"ClusterIP"`                |             |
| webhook.validatingWebhook.failurePolicy   | string | `"Fail"`                     |             |
| webhook.validatingWebhook.timeoutSeconds  | int    | `10`                         |             |
