---
title: "Example Workloads"
linkTitle: "Example Workloads"
weight: 6
date: 2025-07-23
description: >
    Examples of PyTorch and TensorFlow workloads using JobSet
no_list: true
---


## PyTorch Example

- [Distributed Training of a CNN on the MNIST dataset using PyTorch and JobSet](https://github.com/kubernetes-sigs/jobset/tree/main/site/static/examples/pytorch/cnn-mnist/mnist.yaml)

**Note**: Machine learning container images can be quite large so it may take some time to pull the images.

## TensorFlow Example

- [Distributed Training of a Handwritten Digit Classifier on the MNIST dataset using TensorFlow and JobSet](https://github.com/kubernetes-sigs/jobset/tree/main/site/static/examples/tensorflow/mnist.yaml)

This example runs an example job for a single epoch.
You can view the progress of your jobs via `kubectl logs jobs/tensorflow-tensorflow-0`.

```
Train Epoch: 1 [0/60000 (0%)]   loss=2.3130, accuracy=12.5000
Train Epoch: 1 [6400/60000 (11%)]       loss=0.4624, accuracy=86.4171
Train Epoch: 1 [12800/60000 (21%)]      loss=0.3381, accuracy=90.0109
Train Epoch: 1 [19200/60000 (32%)]      loss=0.2724, accuracy=91.8916
Train Epoch: 1 [25600/60000 (43%)]      loss=0.2367, accuracy=92.9941
Train Epoch: 1 [32000/60000 (53%)]      loss=0.2111, accuracy=93.7063
Train Epoch: 1 [38400/60000 (64%)]      loss=0.1925, accuracy=94.2882
Train Epoch: 1 [44800/60000 (75%)]      loss=0.1796, accuracy=94.6416
Train Epoch: 1 [51200/60000 (85%)]      loss=0.1677, accuracy=94.9945
Train Epoch: 1 [57600/60000 (96%)]      loss=0.1565, accuracy=95.3229
Test Loss: 0.0635, Test Accuracy: 97.8400
```