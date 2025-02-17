# jobset

![Version: 0.7.3](https://img.shields.io/badge/Version-0.7.3-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.7.3](https://img.shields.io/badge/AppVersion-0.7.3-informational?style=flat-square)

A Helm chart for deploying JobSet controller and webhook on Kubernetes.

**Homepage:** <https://github.com/kubernetes-sigs/jobset>

## Introduction

This Helm chart installs the JobSet controller and webhook to your Kubernetes cluster. JobSet is a Kubernetes controller that manages groups of related Jobs as a single unit.

## Prerequisites

- Helm >= 3
- Kubernetes >= 1.27

## Usage

### Install the chart

```shell
helm install [RELEASE_NAME] charts/jobset
```

For example, if you want to create a release with name `jobset` in the `jobset-system` namespace:

```shell
helm install jobset charts/jobset \
    --namespace jobset-system \
    --create-namespace
```

Note that by passing the `--create-namespace` flag to the `helm install` command, `helm` will create the release namespace if it does not exist.

See [helm install](https://helm.sh/docs/helm/helm_install) for command documentation.

### Upgrade the chart

```shell
helm upgrade [RELEASE_NAME] charts/jobset-system [flags]
```

See [helm upgrade](https://helm.sh/docs/helm/helm_upgrade) for command documentation.

### Uninstall the chart

```shell
helm uninstall [RELEASE_NAME]
```

This removes all the Kubernetes resources associated with the chart and deletes the release, except for the `crds`, those will have to be removed manually.

See [helm uninstall](https://helm.sh/docs/helm/helm_uninstall) for command documentation.

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| nameOverride | string | `""` | String to partially override release name. |
| fullnameOverride | string | `""` | String to fully override release name. |
| commonLabels | object | `{}` | Common labels to add to the jobset resources. |
| image.registry | string | `"registry.k8s.io"` | Image registry. |
| image.repository | string | `"jobset/jobset"` | Image repository. |
| image.tag | string | If not set, the chart appVersion will be used. | Image tag. |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy. |
| image.pullSecrets | list | `[]` | Image pull secrets for private image registry. |
| controller.replicas | int | `1` | Replicas of the jobset controller deployment. |
| controller.leaderElection.enable | bool | `true` | Whether to enable leader election for jobset controller. |
| controller.env | list | `[]` | Environment variables of the jobset controller container. |
| controller.envFrom | list | `[]` | Environment variable sources of the jobset controller container. |
| controller.volumeMounts | list | `[]` | Volume mounts of the jobset controller container. |
| controller.resources | object | `{"limits":{"cpu":2,"memory":"512Mi"},"requests":{"cpu":"500m","memory":"128Mi"}}` | Resources of the jobset controller container. |
| controller.securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]}}` | Security context of the jobset controller container. |
| controller.volumes | list | `[]` | Volumes of the jobset controller pods. |
| controller.nodeSelector | object | `{}` | Node selector of the jobset controller pods. |
| controller.affinity | object | `{}` | Affinity of the jobset controller pods. |
| controller.tolerations | list | `[]` | Tolerations of the jobset controller pods. |
| controller.podSecurityContext | object | `{"runAsNonRoot":true,"seccompProfile":{"type":"RuntimeDefault"}}` | Security context of the jobset controller pods. |
| controller.serviceAccount.create | bool | `true` | Whether to create a service account for the jobset controller.  |
| controller.serviceAccount.name | Optional | `""` | Specifies the name of the jobset controller service account. |
| controller.serviceAccount.annotations | object | `{}` | Extra annotations of the jobset controller service account. |
| controller.rbac.create | bool | `true` | Whether to create RBAC resources for the jobset controller. |
| webhook.enable | bool | `true` | Whether to enable the jobset webhook. |
| webhook.service.type | string | `"ClusterIP"` | Type of the jobset webhook service. |
| webhook.service.port | int | `443` | Port of the jobset webhook service. |
| webhook.mutate.timeoutSeconds | int | `10` | Timeout seconds of the jobset mutating webhook. |
| webhook.mutate.failurePolicy | string | `"Fail"` | Failure policy of the jobset mutating webhook. |
| webhook.validate.timeoutSeconds | int | `10` | Timeout seconds of the jobset validating webhook. |
| webhook.validate.failurePolicy | string | `"Fail"` | Failure policy of the jobset validating webhook. |
| webhook.certManager.enable | bool | `false` | Whether to use cert-manager to generate certificates for the jobset webhook. |
| webhook.certManager.issuerRef | object | `{}` | The reference to the issuer. |
| webhook.certManager.duration | string | `"8760h"` | The duration of the certificate validity. |
| webhook.certManager.renewBefore | string | `"720h"` | The duration before the certificate expiration to renew the certificate. |
| metrics.enable | bool | `true` | Whether to enable Prometheus metrics exporting. |
| metrics.service.type | string | `"ClusterIP"` | Type of the Prometheus metrics service. |
| metrics.service.port | int | `8443` | Port of the Prometheus metrics service. |
| metrics.serviceMonitor.enable | bool | `false` | Whether to create a Prometheus service monitor. |
| metrics.serviceMonitor.interval | string | `"30s"` | Interval at which metrics should be scraped. |
| metrics.serviceMonitor.scrapeTimeout | string | `"10s"` | Timeout after which the scrape is ended. |
| metrics.serviceMonitor.labels | object | `{}` | Labels for the Prometheus service monitor. |

## Maintainers

| Name | Url |
| ---- | --- |
| ahg-g | <https://github.com/ahg-g> |
| danielvegamyhre | <https://github.com/danielvegamyhre> |
| kannon92 | <https://github.com/kannon92> |
