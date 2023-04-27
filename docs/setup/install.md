# Installation

## Before you begin

Make sure the following conditions are met:

- A Kubernetes cluster with version 1.21 or newer is running. Learn how to [install the Kubernetes tools](https://kubernetes.io/docs/tasks/tools/).
- The `SuspendJob` [feature gate][feature_gate] is enabled. In Kubernetes 1.22 or newer, the feature gate is enabled by default and reached stable in Kubernetes 1.24.
- The kubectl command-line tool has communication with your cluster.

<!-- Uncomment once jobset publishes metrics -->
<!-- JobSet publishes [metrics](/docs/reference/metrics) to monitor its operators. -->
<!-- You can scrape these metrics with Prometheus. -->
<!-- Use [kube-prometheus](https://github.com/prometheus-operator/kube-prometheus) -->
<!-- if you don't have your own monitoring system. -->

<!-- The webhook server in JobSet uses an internal cert management for provisioning certificates. If you want to use -->
<!--   a third-party one, e.g. [cert-manager](https://github.com/cert-manager/cert-manager), follow these steps: -->
<!--   1. Set `internalCertManagement.enable` to `false` in [config file](#install-a-custom-configured-released-version). -->
<!--   2. Comment out the `internalcert` folder in `config/default/kustomization.yaml`. -->
<!--   3. Enable `cert-manager` in `config/default/kustomization.yaml` and uncomment all sections with 'CERTMANAGER'. -->

[feature_gate]: https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/


<!-- Uncomment once we release the first version -->
<!-- ## Install a released version -->

<!-- To install a released version of Jobset in your cluster, run the following command: -->

<!-- ```shell -->
<!-- VERSION=v0.1.0 -->
<!-- kubectl apply -f https://github.com/kubernetes-sigs/jobset/releases/download/$VERSION/manifests.yaml -->
<!-- ``` -->

<!-- <\!-- Uncomment once we have a prometheus setup -\-> -->
<!-- <\!-- ### Add metrics scraping for prometheus-operator -\-> -->

<!-- <\!-- _Available in JobSet v0.2.1 and later_ -\-> -->

<!-- <\!-- To allow [prometheus-operator](https://github.com/prometheus-operator/prometheus-operator) -\-> -->
<!-- <\!-- to scrape metrics from jobset components, run the following command: -\-> -->

<!-- <\!-- ```shell -\-> -->
<!-- <\!-- kubectl apply -f https://github.com/kubernetes-sigs/jobset/releases/download/$VERSION/prometheus.yaml -\-> -->
<!-- ``` -->

<!-- ### Uninstall -->

<!-- To uninstall a released version of JobSet from your cluster, run the following command: -->

<!-- ```shell -->
<!-- VERSION=v0.1.0 -->
<!-- kubectl delete -f https://github.com/kubernetes-sigs/jobset/releases/download/$VERSION/manifests.yaml -->
<!-- ``` -->

<!-- <\!-- Uncomment once we have component config setup -\-> -->
<!-- <\!-- ## Install a custom-configured released version -\-> -->

<!-- <\!-- To install a custom-configured released version of JobSet in your cluster, execute the following steps: -\-> -->

<!-- <\!-- 1. Download the release's `manifests.yaml` file: -\-> -->

<!-- <\!-- ```shell -\-> -->
<!-- <\!-- VERSION=v0.1.0 -\-> -->
<!-- <\!-- wget https://github.com/kubernetes-sigs/jobset/releases/download/$VERSION/manifests.yaml -\-> -->
<!-- <\!-- ``` -\-> -->
<!-- <\!-- 2. With an editor of your preference, open `manifests.yaml`. -\-> -->
<!-- <\!-- 3. In the `jobset-manager-config` ConfigMap manifest, edit the -\-> -->
<!-- <\!-- `controller_manager_config.yaml` data entry. The entry represents -\-> -->
<!-- <\!-- the default JobSet Configuration -\-> -->
<!-- <\!-- struct ([v1alpha1@v0.1.0](https://pkg.go.dev/sigs.k8s.io/jobset@v0.1.0/apis/config/v1alpha1#Configuration)). -\-> -->
<!-- <\!-- The contents of the ConfigMap are similar to the following: -\-> -->


<!-- <\!-- ```yaml -\-> -->
<!-- <\!-- apiVersion: v1 -\-> -->
<!-- <\!-- kind: ConfigMap -\-> -->
<!-- <\!-- metadata: -\-> -->
<!-- <\!--   name: jobset-manager-config -\-> -->
<!-- <\!--   namespace: jobset-system -\-> -->
<!-- <\!-- data: -\-> -->
<!-- <\!--   controller_manager_config.yaml: | -\-> -->
<!-- <\!--     apiVersion: config.jobset.x-k8s.io/v1alpha1 -\-> -->
<!-- <\!--     kind: Configuration -\-> -->
<!-- <\!--     namespace: jobset-system -\-> -->
<!-- <\!--     health: -\-> -->
<!-- <\!--       healthProbeBindAddress: :8081 -\-> -->
<!-- <\!--     metrics: -\-> -->
<!-- <\!--       bindAddress: :8080 -\-> -->
<!-- <\!--     webhook: -\-> -->
<!-- <\!--       port: 9443 -\-> -->
<!-- <\!--     internalCertManagement: -\-> -->
<!-- <\!--       enable: true -\-> -->
<!-- <\!--       webhookServiceName: jobset-webhook-service -\-> -->
<!-- <\!--       webhookSecretName: jobset-webhook-server-cert -\-> -->
<!-- <\!-- ``` -\-> -->

<!-- <\!-- 3. Apply the customized manifests to the cluster: -\-> -->

<!-- <\!-- ```shell -\-> -->
<!-- <\!-- kubectl apply -f manifests.yaml -\-> -->
<!-- <\!-- ``` -\-> -->

## Install the latest development version

To install the latest development version of Jobset in your cluster, run the
following command:

```shell
kubectl apply --server-side -k github.com/kubernetes-sigs/jobset/config/default?ref=main
```

The controller runs in the `jobset-system` namespace.

### Uninstall

To uninstall JobSet, run the following command:

```shell
kubectl delete -k github.com/kubernetes-sigs/jobset/config/default
```

## Build and install from source

To build Jobset from source and install Jobset in your cluster, run the following
commands:

```sh
git clone https://github.com/kubernetes-sigs/jobset.git
cd jobset
IMAGE_REGISTRY=<registry>/<project> make image-push deploy
```

<!-- Uncomment once we have a prometheus setup -->
<!-- ### Add metrics scraping for prometheus-operator -->

<!-- To allow [prometheus-operator](https://github.com/prometheus-operator/prometheus-operator) -->
<!-- to scrape metrics from jobset components, run the following command: -->

<!-- ```shell -->
<!-- make prometheus -->
<!-- ``` -->

### Uninstall

To uninstall JobSet, run the following command:

```sh
make undeploy
```
