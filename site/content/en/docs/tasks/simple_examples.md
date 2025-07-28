---
title: "Simple Examples"
linktitle: "Simple Examples"
weight: 7
date: 2025-07-23
description: >
    Simple examples of JobSet core features
no_list: true
---

Here we have some simple examples demonstrating core JobSet features.

- [Success Policy](https://github.com/kubernetes-sigs/jobset/tree/main/site/static/examples/simple/success-policy.yaml) demonstrates an example of utilizing `successPolicy`.
  Success Policy allows one to specify when to mark a JobSet as completed successfully.
  This example showcases how to use success policy to mark the JobSet as successful if the worker replicated job completes.

- [Exclusive Job Placement](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/simple/exclusive-placement.yaml)
  demonstrates how to configure a JobSet to have a 1:1 mapping between each child Job and a particular topology domain, such as a datacenter rack or zone. This means that all the pods belonging to a child job will be colocated in the same topology domain, while pods from other jobs will not be allowed to run within this domain. This gives the child job exclusive access to computer resources in this domain.

- [Parallel Jobs](https://github.com/kubernetes-sigs/jobset/tree/main/site/static/examples/simple/paralleljobs.yaml)
  demonstrates how to submit multiple replicated jobs in a jobset.

- [Depends On](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/depends-on/depends-on.yaml)
  demonstrates how to define dependencies between ReplicatedJobs, ensuring they are executed in
  the correct sequence. This is important for implementing the leader-worker paradigm in distributed
  ML training, where the workers need to wait for the leader to start first before they attempt to
  connect to it. You can also see the [example of multiple DependsOn items](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/depends-on/multiple-depends-on.yaml)
  that uses DependsOn API to wait for the completion of two previous ReplicatedJobs.

- [Startup Policy (DEPRECATED)](https://github.com/kubernetes-sigs/jobset/tree/main/site/static/examples/startup-policy/startup-driver-ready.yaml)
  demonstrates how to define a startup order for ReplicatedJobs in order to ensure a "leader"
  pod is running before the "workers" are created. **Note:** Startup Policy is deprecated, please use the DependsOn API.

- [TTL after finished](https://github.com/kubernetes-sigs/jobset/tree/main/site/static/examples/simple/ttl-after-finished.yaml)
  demonstrates how to configure a JobSet to be cleaned up automatically after a defined period of time has passed after the JobSet finishes.

- [Replicated jobs grouping](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/simple/group.yaml)
  demonstrates how to group replicated jobs to count and index the replicas job within each group with the labels/annotations `jobset.sigs.k8s.io/group-name`, `jobset.sigs.k8s.io/group-replicas` and `jobset.sigs.k8s.io/job-group-index`. This is similar to how `jobset.sigs.k8s.io/global-replicas` and `jobset.sigs.k8s.io/job-global-index` handle global replica counting and indexing
  