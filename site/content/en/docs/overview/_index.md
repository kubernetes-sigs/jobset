---
title: "JobSet"
linkTitle: "Overview"
weight: 1
menu:
  main:
    weight: 20
description: >
  An overview of JobSet
---

JobSet is a Kubernetes-native API for managing a group of [k8s Jobs](https://kubernetes.io/docs/concepts/workloads/controllers/job/) as a unit. It aims to offer a unified API for deploying HPC (e.g., MPI) and AI/ML training workloads (PyTorch, Jax, TensorFlow etc.) on Kubernetes.

Take a look at the [concepts](../concepts/) page for a brief description of how to use JobSet.

## Conceptual Diagram

<img src="../../images/jobset_diagram.png" alt="jobset diagram">

## Features Overview

- **Support for multi-template jobs**: JobSet models a distributed training workload as a group of K8s Jobs. This allows a user to easily specify different pod templates for different distinct groups of pods (e.g. a leader, workers, parameter servers, etc.), something which cannot be done by a single Job.

- **Automatic headless service configuration and lifecycle management**: ML and HPC frameworks require a stable network endpoint for each worker in the distributed workload, and since pod IPs are dynamically assigned and can change between restarts, stable pod hostnames are required for distributed training on k8s, By default, JobSet uses [IndexedJobs](https://kubernetes.io/blog/2021/04/19/introducing-indexed-jobs/) to establish stable pod hostnames, and does automatic configuration and lifecycle management of the headless service to trigger DNS record creations and establish network connectivity via pod hostnames. These networking configurations are defaulted automatically to enable stable network endpoints and pod-to-pod communication via hostnames; however, they can be customized in the JobSet spec: see this [example](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/simple/jobset-with-network.yaml) of using a custom subdomain your JobSet's network configuration.

- **Configurable failure policies**: JobSet has configurable failure policies which allow the user to specify a maximum number of times the JobSet should be restarted in the event of a failure. If any job is marked failed, the entire JobSet will be recreated, allowing the workload to resume from the last checkpoint. When no failure policy is specified, if any job fails, the JobSet simply fails. Using JobSet v0.6.0+, the [extended failure policy API](https://github.com/kubernetes-sigs/jobset/tree/main/keps/262-ConfigurableFailurePolicy) allows
users to configure different behavior for different error types, enabling them to use compute resources more
efficiently and improve ML training goodput.

- **Configurable success policies**: JobSet has [configurable success policies](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/simple/success-policy.yaml) which target specific ReplicatedJobs, with operators to target `Any` or `All` of their child jobs. For example, you can configure the JobSet to be marked complete if and only if all pods that are part of the “worker” ReplicatedJob are completed. This enables users to use their compute resources more efficiently, allowing a workload to be declared successful and release the resources for the next workload more quickly.

- **Exclusive Placement Per Topology Domain**: JobSet includes an [annotation](https://github.com/kubernetes-sigs/jobset/blob/1ae6c0c039c21d29083de38ae70d13c2c8ec613f/examples/simple/exclusive-placement.yaml#L6) which can be set by the user, specifying that there should be a 1:1 mapping between child job and a particular topology domain, such as a datacenter rack or zone. This means that all the pods belonging to a child job will be colocated in the same topology domain, while pods from other jobs will not be allowed to run within this domain. This gives the child job exclusive access to computer resources in this domain. You can run this [example](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/simple/exclusive-placement.yaml) yourself to see how exclusive placement works.

- **Fast failure recovery**: JobSet recovers from failures by recreating all the child Jobs. When scheduling constraints such as exclusive Job placement are used, fast failure recovery at scale can become challenging. As of JobSet v0.3.0, JobSet uses a designed such that it minimizes impact on scheduling throughput. We have benchmarked scheduling throughput during failure recovery at 290 pods/second at a 15k node scale.

- **Startup Sequencing**: As of JobSet v0.6.0 users can configure a [startup order](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/startup-policy/startup-driver-ready.yaml) for the ReplicatedJobs in a JobSet. This enables support for patterns like the “leader-worker” paradigm, where the leader must be running before the workers should start up and connect to it.

- **Integration with Kueue**: Use JobSet v0.2.3+ and [Kueue](https://kueue.sigs.k8s.io/) v0.6.0+ to oversubscribe your cluster with JobSet workloads, placing them in queue which supports multi-tenancy, resource sharing and more. See [Kueue documentation](https://kueue.sigs.k8s.io/) for more details on the benefits of managing JobSet workloads via Kueue.

## Production Readiness Status

- ✔️ API version: v1alpha2, respecting [Kubernetes Deprecation Policy](https://kubernetes.io/docs/reference/using-api/deprecation-policy/)
- ✔️ Maintains support for latest 3 Kubernetes minor versions.
- ✔️ Up-to-date [documentation](https://jobset.sigs.k8s.io/docs).
- ✔️ Test Coverage:
  - ✔️ Unit Test [testgrid](https://testgrid.k8s.io/sig-apps#pull-jobset-test-unit-main).
  - ✔️ Integration Test [testgrid](https://testgrid.k8s.io/sig-apps#pull-jobset-test-integration-main)
  - ✔️ E2E Tests for Kubernetes
    on Kind.
- ✔️ Monitoring via [metrics](https://jobset.sigs.k8s.io/docs/reference/metrics).
- ✔️ Security: RBAC based accessibility.
- ✔️ Stable release cycle(2-3 months) for new features, bugfixes, cleanups.

## Installation

**Requires a Kubernetes cluster running one of the last 3 Kubernetes minor versions.**

To install the latest release of JobSet in your cluster, run the following command:

```shell
kubectl apply --server-side -f https://github.com/kubernetes-sigs/jobset/releases/download/v0.8.0/manifests.yaml
```

The controller runs in the `jobset-system` namespace.

Read the [installation guide](https://jobset.sigs.k8s.io/docs/installation/) to learn more.

## Roadmap

See our github project for our [roadmap](https://github.com/orgs/kubernetes-sigs/projects/99/views/2)

## Troubleshooting Common Issues

See the [troubleshooting](https://jobset.sigs.k8s.io/docs/troubleshooting/) guide for help resolving common issues.


## Community, Discussion, Contribution, and Support

Learn how to engage with the Kubernetes community on the [community page](http://kubernetes.io/community/).

You can reach the maintainers of this project at:

- [Slack](https://kubernetes.slack.com/messages/wg-batch)
- [Mailing List](https://groups.google.com/a/kubernetes.io/g/wg-batch)

## Code of conduct

Participation in the Kubernetes community is governed by the [Kubernetes Code of Conduct](https://git.k8s.io/community/code-of-conduct.md).
