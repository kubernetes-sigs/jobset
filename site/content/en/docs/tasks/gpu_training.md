---
title: "Distributed GPU Training with PyTorch"
linkTitle: "GPU Training"
weight: 35
description: >
  Running distributed PyTorch training on GPUs with JobSet.
---

This example shows how to run a distributed PyTorch training workload on GPUs
using JobSet. It trains a ResNet-18 model on the CIFAR-10 dataset across 2
worker pods, each using 2 GPUs (4 GPUs in total), with
[PyTorch DDP](https://pytorch.org/tutorials/intermediate/ddp_tutorial.html).

The full manifest, training script, Dockerfile and a step-by-step walkthrough
live in the
[`pytorch/gpu-training`](https://github.com/kubernetes-sigs/jobset/tree/main/site/static/examples/pytorch/gpu-training)
example folder.

## Why JobSet for GPU training

Distributed frameworks such as PyTorch require every worker to know about the
others before the training ring can start. JobSet provides this out of the box:

- **Stable hostnames**: JobSet configures a headless service so each pod gets a
  stable DNS name (`<jobset>-<replicatedJob>-<index>-<podIndex>`). Workers keep
  the same address across restarts, even when pod IPs change.
- **Atomic restarts**: if any worker fails, JobSet restarts the whole workload,
  so the collective communication group is always consistent.

## The manifest at a glance

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: pytorch-gpu
spec:
  network:
    enableDNSHostnames: true
  replicatedJobs:
  - name: workers
    template:
      spec:
        parallelism: 2
        completions: 2
        backoffLimit: 0
        template:
          spec:
            containers:
            - name: pytorch-gpu
              image: <REGISTRY>/<USERNAME>/pytorch-gpu-training:latest
              resources:
                requests:
                  nvidia.com/gpu: "2"
                limits:
                  nvidia.com/gpu: "2"
              command:
              - bash
              - -xc
              - |
                torchrun --nproc_per_node=2 --nnodes=2 \
                  --rdzv_backend=c10d \
                  --rdzv_endpoint=$MASTER_ADDR:$MASTER_PORT \
                  --rdzv_id=jobset-gpu \
                  --node_rank=$RANK \
                  train.py --epochs=10 --batch-size=128 --lr=0.1
```

Key points:

- `enableDNSHostnames: true` makes each pod addressable by hostname, which
  PyTorch uses to discover its peers.
- The `RANK` environment variable is derived from the
  `batch.kubernetes.io/job-completion-index` annotation and becomes the node
  rank for the training ring.
- Each pod requests `nvidia.com/gpu: 2`, so the job needs GPU nodes exposing
  the `nvidia.com/gpu` resource.

## Before you begin

- A Kubernetes cluster with **GPU nodes**. See the
  [example README](https://github.com/kubernetes-sigs/jobset/tree/main/site/static/examples/pytorch/gpu-training/README.md)
  for AWS EKS, Azure AKS and Google GKE options.
- The [NVIDIA device plugin](https://github.com/NVIDIA/k8s-device-plugin)
  installed.
- [JobSet installed]({{< ref "/docs/installation" >}}) in the cluster.

## Run it

```bash
kubectl apply -f gpu-training.yaml
kubectl get jobsets --watch
kubectl logs -f pytorch-gpu-workers-0-0
```

When finished, the coordinator pod prints the validation accuracy and saves a
checkpoint to `/checkpoints`.

```bash
kubectl delete jobset pytorch-gpu
```