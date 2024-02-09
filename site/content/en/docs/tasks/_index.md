---

title: "Tasks"
linkTitle: "Tasks"
weight: 6
date: 2022-02-14
description: >
  Doing common Jobset tasks
no_list: true
---

## PyTorch Examples

In [pytorch](examples/pytorch), there are two examples using pytorch

- [mnist](examples/pytorch/mnist.yaml)
- [resnet](examples/pytorch/resnet.yaml)

Each of these examples demonstrate how you use the JobSet API to run pytorch jobs.  

Machine Learning images can be quite large so it may take some time to pull the images.

## Simple Examples

In [simple](examples/simple), we have some examples demonstrating features for the JobSet.

- [success-policy](examples/simple/driver-worker-success-policy.yaml)
- [max-restarts](examples/simple/max-restarts.yaml)
- [paralleljobs](examples/simple/paralleljobs.yaml)

[Success Policy](examples/simple/driver-worker-success-policy.yaml) demonstrates an example of utilizing `successPolicy`.
Success Policy allows one to specify when to mark a JobSet as success.  
This example showcases an example of using the success policy to mark the JobSet as successful if the worker replicated job completes.

[Max Restarts](examples/simple/max-restarts.yaml) demonstrates an example of utilizing `failurePolicy`.
Failure Policy allows one to control how many restarts a JobSet can do before declaring the JobSet as failed.

[Parallel Jobs](examples/simple/paralleljobs.yaml) demonstates how we can submit multiple replicated jobs in a jobset.

## Tensorflow Examples

In [tensorflow](examples/tensorflow), we have some examples demonstrating how to use Tensorflow with a JobSet.

- [mnist](examples/tensorflow/mnist.yaml)

[mnist](examples/tensorflow/mnist.yaml) runs an example job for a single epoch.
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
