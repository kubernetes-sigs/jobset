# Tasks

## PyTorch Examples

In [pytorch](examples/pytorch), there are two examples using pytorch

- [mnist](examples/pytorch/mnist.yaml)
- [resnet](examples/pytorch/resnet.yaml)

Each of these examples demonstrate how you use the JobSet API to run pytorch jobs.  

Machine Learning images can be quite large so it make take some time to pull the images.

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

This is an example of running tensorflow.
