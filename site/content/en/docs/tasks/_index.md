---

title: "Tasks"
linkTitle: "Tasks"
weight: 6
date: 2022-02-14
description: >
  Doing common Jobset tasks
no_list: true
---

## PyTorch Example

- [Distributed Training of a CNN on the MNIST dataset using PyTorch and JobSet](https://github.com/kubernetes-sigs/jobset/blob/1ae6c0c039c21d29083de38ae70d13c2c8ec613f/examples/pytorch/cnn-mnist/mnist.yaml)

**Note**: machine Learning images can be quite large so it may take some time to pull the images.

## Simple Examples

Here we have some simple examples demonstrating core JobSet features.

- [Success Policy](https://github.com/kubernetes-sigs/jobset/blob/release-0.5/examples/simple/success-policy.yaml) demonstrates an example of utilizing `successPolicy`.
Success Policy allows one to specify when to mark a JobSet as success.  
This example showcases an example of using the success policy to mark the JobSet as successful if the worker replicated job completes.

- [Failure Policy with Max Restarts](https://github.com/kubernetes-sigs/jobset/blob/release-0.5/examples/simple/max-restarts.yaml) demonstrates an example of utilizing `failurePolicy`. Failure Policy allows one to control how many restarts a JobSet can do before declaring the JobSet as failed.

- [Exclusive Job Placement](https://github.com/kubernetes-sigs/jobset/blob/release-0.5/examples/simple/exclusive-placement.yaml) demonstrates how you can configure a JobSet to have a 1:1 mapping between each child Job and a particular topology domain, such as a datacenter rack or zone. This means that all the pods belonging to a child job will be colocated in the same topology domain, while pods from other jobs will not be allowed to run within this domain. This gives the child job exclusive access to computer resources in this domain.

- [Parallel Jobs](https://github.com/kubernetes-sigs/jobset/blob/release-0.5/examples/simple/paralleljobs.yaml) demonstrates how we can submit multiple replicated jobs in a jobset.

- [Startup Policy](https://github.com/kubernetes-sigs/jobset/blob/release-0.5/examples/startup-policy/startup-driver-ready.yaml) demonstrates how we can define a startup order for ReplicatedJobs in order to ensure a "leader"
pod is running before the "workers" are created. This is important for enabling the leader-worker paradigm in distributed ML training, where the workers will attempt to register with the leader as soon as they spawn.

- [TTL after finished](https://github.com/kubernetes-sigs/jobset/blob/release-0.5/examples/simple/ttl-after-finished.yaml) demonstrates how you can configure a JobSet to be cleaned up automatically after a defined period of time has passed after the JobSet finishes.

## Tensorflow Example

- [Distributed Training of a Handwritten Digit Classifier on the MNIST dataset using Tensorflow and JobSet](https://github.com/kubernetes-sigs/jobset/blob/release-0.5/examples/tensorflow/mnist.yaml)

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
