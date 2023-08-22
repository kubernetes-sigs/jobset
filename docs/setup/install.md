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


# Install a released version

To install a released version of Jobset in your cluster, run the following command:

```shell
VERSION=v0.2.1
kubectl apply --server-side -f https://github.com/kubernetes-sigs/jobset/releases/download/$VERSION/manifests.yaml
```

### Optional: Add metrics scraping for prometheus-operator

If you are using [prometheus-operator](https://github.com/prometheus-operator/prometheus-operator)
to scrape metrics from jobset components, run the following command:

```shell
VERSION=v0.2.1
kubectl apply -f https://github.com/kubernetes-sigs/jobset/releases/download/$VERSION/prometheus.yaml
```

If you are using [kube-prometheus](https://github.com/prometheus-operator/kube-prometheus), metrics
can be scraped without performing this step.


## Uninstall

To uninstall a released version of JobSet from your cluster, run the following command:

```shell
VERSION=v0.2.1
kubectl delete -f https://github.com/kubernetes-sigs/jobset/releases/download/$VERSION/manifests.yaml
```

<!-- <\!-- Uncomment once we have component config setup -\-> -->
<!-- <\!-- ## Install a custom-configured released version -\-> -->

<!-- <\!-- To install a custom-configured released version of JobSet in your cluster, execute the following steps: -\-> -->

<!-- <\!-- 1. Download the release's `manifests.yaml` file: -\-> -->

<!-- <\!-- ```shell -\-> -->
<!-- <\!-- VERSION=v0.2.1 -\-> -->
<!-- <\!-- wget https://github.com/kubernetes-sigs/jobset/releases/download/$VERSION/manifests.yaml -\-> -->
<!-- <\!-- ``` -\-> -->
<!-- <\!-- 2. With an editor of your preference, open `manifests.yaml`. -\-> -->
<!-- <\!-- 3. In the `jobset-manager-config` ConfigMap manifest, edit the -\-> -->
<!-- <\!-- `controller_manager_config.yaml` data entry. The entry represents -\-> -->
<!-- <\!-- the default JobSet Configuration -\-> -->
<!-- <\!-- struct ([v1alpha2@v0.2.1](https://pkg.go.dev/sigs.k8s.io/jobset@v0.2.1/apis/config/v1alpha2#Configuration)). -\-> -->
<!-- <\!-- The contents of the ConfigMap are similar to the following: -\-> -->


<!-- <\!-- ```yaml -\-> -->
<!-- <\!-- apiVersion: v1 -\-> -->
<!-- <\!-- kind: ConfigMap -\-> -->
<!-- <\!-- metadata: -\-> -->
<!-- <\!--   name: jobset-manager-config -\-> -->
<!-- <\!--   namespace: jobset-system -\-> -->
<!-- <\!-- data: -\-> -->
<!-- <\!--   controller_manager_config.yaml: | -\-> -->
<!-- <\!--     apiVersion: config.jobset.x-k8s.io/v1alpha2 -\-> -->
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

# Install the latest development version

To install the latest development version of Jobset in your cluster, run the
following command:

```shell
kubectl apply --server-side -k github.com/kubernetes-sigs/jobset/config/default?ref=main
```

The controller runs in the `jobset-system` namespace.

### Optional: Add metrics scraping for prometheus-operator

If you are using [prometheus-operator](https://github.com/prometheus-operator/prometheus-operator)
to scrape metrics from jobset components, you need to run the following command so it can access
the metrics:

```shell
kubectl apply --server-side -k github.com/kubernetes-sigs/jobset/config/prometheus?ref=main
```
If you are using [kube-prometheus](https://github.com/prometheus-operator/kube-prometheus), metrics
can be scraped without performing this step.

## Uninstall

To uninstall JobSet, run the following command:

```shell
kubectl delete -k github.com/kubernetes-sigs/jobset/config/default
```

# Build and install from source

To build Jobset from source and install Jobset in your cluster, run the following
commands:

```sh
git clone https://github.com/kubernetes-sigs/jobset.git
cd jobset
IMAGE_REGISTRY=<registry>/<project> make image-push deploy
```

### Optional: Add metrics scraping for prometheus-operator

If you are using [prometheus-operator](https://github.com/prometheus-operator/prometheus-operator)
to scrape metrics from jobset components, you need to run the following command so it has access
to the metrics:

```shell
make prometheus
```

If you are using [kube-prometheus](https://github.com/prometheus-operator/kube-prometheus), metrics
can be scraped without performing this step.

## Uninstall

To uninstall JobSet, run the following command:

```sh
make undeploy
```

# Optional: Use cert manager instead of internal cert
JobSet webhooks use an internal certificate by default. However, if you wish to use cert-manager (which
supports cert rotation), instead of internal cert, you can by performing the following steps. 

First, install cert-manager on your cluster by running the following command:

```shell
VERSION=v1.11.0
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/$VERSION/cert-manager.yaml
```

Next, in the file ``jobset/config/default/kustomization.yaml`` replace ``../components/internalcert`` with
``../components/certmanager`` then uncomment all the lines beginning with ``[CERTMANAGER]``.

Finally, apply these configurations to your cluster with ``kubectl apply --server-side -k config/default``.