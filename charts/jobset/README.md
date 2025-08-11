# jobset

![Version: 0.8.1](https://img.shields.io/badge/Version-0.8.1-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)

A Helm chart for deploying JobSet controller and webhook on Kubernetes.

**Homepage:** <https://github.com/kubernetes-sigs/jobset>

## Introduction

This Helm chart installs the JobSet controller and webhook to your Kubernetes cluster. JobSet is a Kubernetes controller that manages groups of related Jobs as a single unit.

## Prerequisites

- Helm >= 3
- Kubernetes >= 1.27

## Usage

### Install from registry.k8s.io

You can obtain the helm chart from `registry.k8s.io`.

```shell
helm install oci://registry.k8s.io/jobset/charts/jobset --version v0.8.1
```

The version is necessary as there is not a latest tag in this repository.

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
| image.repository | string | `"us-central1-docker.pkg.dev/k8s-staging-images/jobset/jobset"` | Image repository. |
| image.pullPolicy | string | `"Always"` | Image pull policy. |
| image.pullSecrets | list | `[]` | Image pull secrets for private image registry. |
| image.tag | string | `"main"` |  |
| controller.replicas | int | `1` | Replicas of the jobset controller deployment. |
| controller.leaderElection.enable | bool | `true` | Whether to enable leader election for jobset controller. |
| controller.clientConnection.qps | int | `500` | QPS is the number of queries per second allowed for K8S api server connection. |
| controller.clientConnection.burst | int | `500` | Burst allows extra queries to accumulate when a client is exceeding its rate. |
| controller.env | list | `[]` | Environment variables of the jobset controller container. |
| controller.envFrom | list | `[]` | Environment variable sources of the jobset controller container. |
| controller.volumeMounts | list | `[]` | Volume mounts of the jobset controller container. |
| controller.resources | object | `{"limits":{"cpu":2,"memory":"4Gi"},"requests":{"cpu":"500m","memory":"128Mi"}}` | Resources of the jobset controller container. |
| controller.securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true}` | Security context of the jobset controller container. |
| controller.volumes | list | `[]` | Volumes of the jobset controller pods. |
| controller.nodeSelector | object | `{}` | Node selector of the jobset controller pods. |
| controller.affinity | object | `{}` | Affinity of the jobset controller pods. |
| controller.tolerations | list | `[]` | Tolerations of the jobset controller pods. |
| controller.podSecurityContext | object | `{"runAsNonRoot":true,"seccompProfile":{"type":"RuntimeDefault"}}` | Security context of all jobset controller containers. |
| controller.hostNetwork | bool | `false` | Run the controller/webhook Pods on the node’s network namespace instead of the overlay CNI. |
| certManager.enable | bool | `false` | Whether to use cert-manager to generate certificates for the jobset webhook. |
| certManager.issuerRef | object | `{}` | The reference to the issuer. If empty, self-signed issuer will be created and used. |
| prometheus.enable | bool | `false` | Whether to enable Prometheus metrics exporting. |
| prometheus.prometheusNamespace | string | `"monitoring"` |  |
| prometheus.prometheusClientCertSecretName | string | `"jobset-metrics-server-cert"` |  |

## Maintainers

| Name | Url |
| ---- | --- |
| ahg-g | <https://github.com/ahg-g> |
| danielvegamyhre | <https://github.com/danielvegamyhre> |
| kannon92 | <https://github.com/kannon92> |
