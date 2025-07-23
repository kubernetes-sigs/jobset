---
title: "Installation"
linkTitle: "Installation"
weight: 2
description: >
  Installing Jobset to a Kubernetes Cluster
---

<!-- toc -->
- [Before you begin](#before-you-begin)
- [Install a released version](#install-a-released-version)
  - [Uninstall](#uninstall)
- [Install the latest development version](#install-the-latest-development-version)
  - [Uninstall](#uninstall-1)
- [Build and install from source](#build-and-install-from-source)
  - [Uninstall](#uninstall-2)
- [Use Cert Manager instead of internal cert](#optional-use-cert-manager-instead-of-internal-cert)

<!-- /toc -->

## Before you begin

Make sure the following conditions are met:

- A Kubernetes cluster running one of the last 3 Kubernetes minor versions. Learn how to [install the Kubernetes tools](https://kubernetes.io/docs/tasks/tools/).
- Your cluster has at least 1 node with 2+ CPUs and 512+ MB of memory available for the JobSet controller manager Deployment to run on. **NOTE: On some cloud providers, the default node machine type will not have sufficient resources to run the JobSet controller manager and all the required kube-system pods, so you'll need to use a larger
machine type for your nodes.**
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
VERSION={{< param "version" >}}
kubectl apply --server-side -f https://github.com/kubernetes-sigs/jobset/releases/download/$VERSION/manifests.yaml
```

To install a released version of JobSet in your cluster using Helm, run the following command:

```shell
helm install jobset oci://registry.k8s.io/jobset/charts/jobset --version $VERSION --create-namespace --namespace=jobset-system
```

For more HELM configurations options, follow the [instructions](https://github.com/kubernetes-sigs/jobset/tree/main/charts/jobset).

### Optional: Add metrics scraping for prometheus-operator

If you are using [prometheus-operator](https://github.com/prometheus-operator/prometheus-operator)
to scrape metrics from jobset components, run the following command:

```shell
VERSION={{< param "version" >}}
kubectl apply -f https://github.com/kubernetes-sigs/jobset/releases/download/$VERSION/prometheus.yaml
```

If you are using [kube-prometheus](https://github.com/prometheus-operator/kube-prometheus), metrics
can be scraped without performing this step.

## Customize Your Installation

You can customize the installation according to your requirements using [kustomize](https://kustomize.io/).

For instance, if you need to modify the resource allocations for a specific deployment, follow these steps:

### Step 1: Set Up Your Kustomization Environment

Start by creating a directory for your Kustomize configuration and navigate into it:

```shell
mkdir kustomize-jobset
cd kustomize-jobset
```

### Step 2: Download the Remote Manifest

Retrieve the remote manifest file specifying the version you need:

```shell
VERSION={{< param "version" >}}
curl -LO https://github.com/kubernetes-sigs/jobset/releases/download/$VERSION/manifests.yaml
```

### Step 3: Create a kustomization.yaml File

Create a kustomization.yaml file that references the downloaded manifest:

```shell
resources:
  - manifests.yaml
```

### Step 4: Define Your Resource Adjustments

Create a resource_patch.yaml file to specify your desired resource adjustments for the deployment. For example, to update the jobset-controller-manager:

```shell
apiVersion: apps/v1
kind: Deployment
metadata:
  name: jobset-controller-manager
  namespace: jobset-system
spec:
  template:
    spec:
      containers:
      - name: manager
        resources:
          requests:
            cpu: "1"      
            memory: "256Mi"  
          limits:
            cpu: "4"      
            memory: "1Gi"  
```

### Step 5: Include the Patch in Your Kustomization


Add the resource_patch.yaml file to your kustomization.yaml to apply the patch:

```shell
resources:
  - manifests.yaml
patches:
  - path: resource_patch.yaml
```

### Step 5: Include the Patch in Your Kustomization

Apply the configuration to your Kubernetes cluster using Kustomize and kubectl:

```shell
kubectl apply -k .
```

## Uninstall

To uninstall a released version of JobSet from your cluster, run the following command:

```shell
VERSION={{< param "version" >}}
kubectl delete -f https://github.com/kubernetes-sigs/jobset/releases/download/$VERSION/manifests.yaml
```

To uninstall a released version of Kueue from your cluster by Helm, run the following command:

```shell
helm uninstall jobset --namespace jobset-system
```

<!-- <\!-- Uncomment once we have component config setup -\-> -->
<!-- <\!-- ## Install a custom-configured released version -\-> -->

<!-- <\!-- To install a custom-configured released version of JobSet in your cluster, execute the following steps: -\-> -->

<!-- <\!-- 1. Download the release's `manifests.yaml` file: -\-> -->

<!-- <\!-- ```shell -\-> -->
<!-- <\!-- VERSION=v0.5.0 -\-> -->
<!-- <\!-- wget https://github.com/kubernetes-sigs/jobset/releases/download/$VERSION/manifests.yaml -\-> -->
<!-- <\!-- ``` -\-> -->
<!-- <\!-- 2. With an editor of your preference, open `manifests.yaml`. -\-> -->
<!-- <\!-- 3. In the `jobset-manager-config` ConfigMap manifest, edit the -\-> -->
<!-- <\!-- `controller_manager_config.yaml` data entry. The entry represents -\-> -->
<!-- <\!-- the default JobSet Configuration -\-> -->
<!-- <\!-- struct ([v1alpha2@v0.5.0](https://pkg.go.dev/sigs.k8s.io/jobset@v0.5.0/apis/config/v1alpha2#Configuration)). -\-> -->
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

