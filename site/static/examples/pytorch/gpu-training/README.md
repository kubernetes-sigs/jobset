# Distributed GPU Training with JobSet and PyTorch

This example shows how to run a distributed PyTorch training workload on GPUs
using JobSet. It trains a [ResNet-18](https://arxiv.org/abs/1512.03385) model
on the CIFAR-10 dataset across **2 worker pods, each using 2 GPUs** (4 GPUs in
total) with [PyTorch DDP](https://pytorch.org/tutorials/intermediate/ddp_tutorial.html).

The example is cloud-agnostic: it only requires a Kubernetes cluster with GPU
nodes that expose `nvidia.com/gpu` as a resource.

## How it works

JobSet creates an [IndexedJob](https://kubernetes.io/docs/concepts/workloads/controllers/job/)
per worker and automatically configures a **headless service** so that each pod
has a stable hostname. Because pod IPs change on restart, PyTorch uses these
stable hostnames to let workers find each other and set up the distributed
training ring.

In this manifest, worker `i` resolves to
`pytorch-gpu-workers-<i>-0.pytorch-gpu`, and the pod at index `0` acts as the
coordinator (`MASTER_ADDR`). Each pod runs `torchrun` with its index as the
node rank and 2 GPUs per pod (`nproc_per_node=2`).

## Prerequisites

- A Kubernetes cluster with **GPU nodes** (e.g. NVIDIA A100, L4, or H100).
  See the cloud notes below.
- The [NVIDIA device plugin](https://github.com/NVIDIA/k8s-device-plugin)
  installed so pods can request `nvidia.com/gpu`.
- [JobSet](https://jobset.sigs.k8s.io/docs/installation/) installed in the
  cluster.

## Run the example

1. Build the training image and push it to a registry:

   ```bash
   docker build -t <REGISTRY>/<USERNAME>/pytorch-gpu-training:latest .
   docker push <REGISTRY>/<USERNAME>/pytorch-gpu-training:latest
   ```

2. Set the image in `gpu-training.yaml`:

   ```yaml
   image: <REGISTRY>/<USERNAME>/pytorch-gpu-training:latest
   ```

3. Submit the JobSet:

   ```bash
   kubectl apply -f gpu-training.yaml
   ```

4. Watch the training progress:

   ```bash
   kubectl get jobsets --watch
   kubectl logs -f pytorch-gpu-workers-0-0   # coordinator pod
   ```

   The logs show per-epoch loss and test accuracy:

   ```
   Epoch 0 step 0/352 loss=2.2974
   Epoch 0 finished in 12.3s
   Epoch 0 test accuracy: 32.45%
   ```

5. Clean up:

   ```bash
   kubectl delete jobset pytorch-gpu
   ```

## Cloud-specific notes

| Cloud | How to get GPU nodes |
| --- | --- |
| AWS EKS | Add an NVIDIA GPU [managed node group](https://docs.aws.amazon.com/eks/latest/userguide/eks-gpu-ami.html) (G4/ G5 / P4 / P5 instances) and install the NVIDIA device plugin add-on. |
| Azure AKS | Create an AKS cluster with a GPU [node pool](https://docs.microsoft.com/en-us/azure/aks/gpu-cluster) (e.g. Standard_NC* series). |
| Google GKE | Create a GKE cluster with GPU nodes via the [NVIDIA GPU support](https://cloud.google.com/kubernetes-engine/docs/how-to/gpus) guide (e.g. L4 or A100 machines). |

> **Troubleshooting**: if workers cannot reach each other, ask the cluster
> admin to check the node network (NCCL uses TCP by default). Set
> `NCCL_DEBUG=INFO` in the container env to get detailed NCCL logs.