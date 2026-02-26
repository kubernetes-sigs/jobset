---
title: "TPU Multislice Training with JobSet"
linkTitle: "TPU Multislice"
weight: 30
description: >
  Running distributed training workloads across multiple TPU slices using JobSet and Kueue.
---

TPU Multislice allows you to scale training workloads beyond a single TPU pod (slice) by connecting multiple slices together. 

Understanding the hardware network is critical for these workloads:
*   **Intra-slice (ICI):** The communication between TPU chips within a slice happens over inter chip interconnects (ICI). This is a dedicated, ultra-high-bandwidth physical network.
*   **Inter-slice (DCN):** The communication between slices happens over the Data Center Network (DCN).

The Google Cloud blog post on [scaling AI workloads with Multislice](https://cloud.google.com/blog/products/compute/using-cloud-tpu-multislice-to-scale-ai-workloads) provides helpful diagrams that visualize the difference between the high-speed ICI network within a slice and the DCN used between slices.

Because ICI relies on physical wiring within a specific hardware boundary (represented in GKE as a Node Pool), it is crucial that Kubernetes does not fragment a single logical slice across multiple Node Pools. JobSet solves this exact problem using **Exclusive Topology**.

## Understanding Exclusive Topology

To ensure optimal performance and prevent Job crashes due to broken ICI links, we must guarantee that all Pods belonging to a single TPU slice are scheduled onto the exact same physical Node Pool.

The exclusive placement feature is enabled by creating the JobSet with the annotation `alpha.jobset.sigs.k8s.io/exclusive-topology: cloud.google.com/gke-nodepool`. This annotation configures Pod affinity to ensure all Pods are scheduled on the same slice.

It is commonly used to ensure a 1:1 map between child Jobs and GKE Node pools. That is, if two Pods are part of the same child Job, then they will run in Nodes in the same Node pool. Otherwise, they will run in Nodes from different Node pools.

### How JobSet Schedules the Pods (Leader/Follower)

When a Multislice JobSet is submitted, the JobSet controller actively manages the scheduling sequence:

1.  **Leader Scheduling:** The JobSet controller, through its Pod webhook and Pod controller, will ensure that only leader Pods (Pods with value 0 for the label `batch.kubernetes.io/job-completion-index`, there is one per child Job) will be scheduled first.
2.  **Follower Binding:** The follower Pods (Pods with value different than 0 for the label `batch.kubernetes.io/job-completion-index`) will be allowed to be scheduled only when their leader has been scheduled. The webhook intercepts the follower Pods and dynamically injects `nodeSelectors` to force them into the exact same Node Pool as their leader.

## Example: JAX on TPU Trillium (v6e) Multislice

Before you begin, ensure you have the following set up in your GKE cluster:

1.  **Install JobSet and Kueue:**
    Make sure both the [JobSet](https://jobset.sigs.k8s.io/docs/installation) and [Kueue](https://kueue.sigs.k8s.io/docs/installation) controllers are installed.

2.  **Create TPU Node Pools:**
    For this example multislice workload, you need at least two separate TPU node pools, one for each slice. This allows JobSet's exclusive placement to assign each `ReplicatedJob` (representing a slice) to its own dedicated node pool.

    Replace the placeholders and run the following commands to create two `ct6e-standard-4t` node pools with a `4x4` topology:
    
    ```bash
    # Set your cluster variables
    export PROJECT_ID=my-project-id            # Replace with your Google Cloud Project ID
    export CLUSTER_NAME=my-tpu-cluster         # Replace with your GKE cluster name
    export CONTROL_PLANE_LOCATION=us-central1  # Replace with your GKE control plane region
    export NODE_LOCATION=us-central1-a         # Replace with the zone for TPU creation

    # Create the first node pool
    gcloud container node-pools create tpu-slice-a \
        --location=$CONTROL_PLANE_LOCATION \
        --cluster=$CLUSTER_NAME \
        --node-locations=$NODE_LOCATION \
        --machine-type=ct6e-standard-4t \
        --tpu-topology=4x4 \
        --project=$PROJECT_ID
    
    # Create the second node pool
    gcloud container node-pools create tpu-slice-b \
        --location=$CONTROL_PLANE_LOCATION \
        --cluster=$CLUSTER_NAME \
        --node-locations=$NODE_LOCATION \
        --machine-type=ct6e-standard-4t \
        --tpu-topology=4x4 \
        --project=$PROJECT_ID
    ```

3.  **Configure Kueue:**

    Create the necessary Kueue and Kubernetes resources to manage the TPU workloads. This includes defining a `ResourceFlavor` for the TPUs and setting up queues.

    ```yaml
    # kueue-config.yaml
    apiVersion: kueue.x-k8s.io/v1beta1
    kind: ResourceFlavor
    metadata:
      name: v6e-4x4
    spec:
      nodeLabels:
        cloud.google.com/gke-tpu-accelerator: tpu-v6e-slice
        cloud.google.com/gke-tpu-topology: 4x4
    ---
    apiVersion: kueue.x-k8s.io/v1beta1
    kind: ClusterQueue
    metadata:
      name: multislice-cluster-queue
    spec:
      namespaceSelector: {}
      queueingStrategy: BestEffortFIFO
      resourceGroups:
      - coveredResources: ["google.com/tpu"]
        flavors:
        - name: "v6e-4x4"
          resources:
          - name: "google.com/tpu"
            nominalQuota: "32" # 2 slices * 16 chips/slice = 32
    ---
    apiVersion: kueue.x-k8s.io/v1beta1
    kind: LocalQueue
    metadata:
      namespace: default # Or the namespace where you will run your JobSet
      name: multislice-queue
    spec:
      clusterQueue: multislice-cluster-queue
    ```
    
    Apply the configurations:

    ```bash
    kubectl apply -f kueue-config.yaml
    ```

### Example JobSet

The following example runs a distributed JAX workload across **2 slices** of **TPU Trillium (v6e)**. It demonstrates how to integrate with Kueue's WorkloadPriorityClass and configure the specialized networking required for v6e machines.

```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: v6e-multislice
  labels:
    # Kueue integration: Routes the workload to a specific LocalQueue
    kueue.x-k8s.io/queue-name: multislice-queue
  annotations:
    # Ensures 1:1 mapping between child Jobs and physical TPU Node Pools
    alpha.jobset.sigs.k8s.io/exclusive-topology: cloud.google.com/gke-nodepool
spec:
  failurePolicy:
    # Synchronous JAX training requires JobSet to restart if any node fails
    maxRestarts: 4
  replicatedJobs:
    - name: slice
      # Provision 2 independent TPU v6e slices (multislice)
      replicas: 2
      template:
        spec:
          # A 4x4 v6e slice has 16 chips. With 4 chips per node, we need 4 nodes per slice.
          parallelism: 4
          completions: 4
          backoffLimit: 0
          template:
            spec:
              hostNetwork: true
              dnsPolicy: ClusterFirstWithHostNet
              nodeSelector:
                cloud.google.com/gke-tpu-accelerator: tpu-v6e-slice
                cloud.google.com/gke-tpu-topology: 4x4
              containers:
                - name: jax-tpu
                  image: us-docker.pkg.dev/cloud-tpu-images/jax-ai-image/tpu:latest
                  securityContext:
                    privileged: true
                  command:
                    - bash
                    - -c
                    - |
                      python3 -c 'import jax; print("Global device count:", jax.device_count())'
                      sleep 60
                  resources:
                    limits:
                      google.com/tpu: 4 # 4 Trillium chips per node
```

### Visualizing the Scheduling

If you create a JobSet with `replicas: 2` (2 Slices) and `parallelism: 4` (4 Nodes per Slice), JobSet creates two child Jobs:

```bash
$ kubectl get jobs -o wide
NAME                     COMPLETIONS   DURATION   AGE   CONTAINERS   IMAGES                 SELECTOR
v6e-multislice-slice-0   0/4           2m         2m    jax-tpu      us-docker.pkg.../tpu   controller-uid=1111...
v6e-multislice-slice-1   0/4           2m         2m    jax-tpu      us-docker.pkg.../tpu   controller-uid=2222...
```

When checking the Pods, you will see the 1:1 mapping in action:

```bash
$ kubectl get pods -o custom-columns="NAME:.metadata.name,STATUS:.status.phase,NODE:.spec.nodeName,NODEPOOL_SELECTOR:.spec.nodeSelector.cloud\.google\.com/gke-nodepool"

NAME                             STATUS    NODE                    NODEPOOL_SELECTOR
# --------------------------------------------------------------------------------------------------
# 4 Pods of v6e-multislice-slice-0 -> Exclusively bound to tpu-slice-a (ICI intact)
# --------------------------------------------------------------------------------------------------
v6e-multislice-slice-0-0-sk2b6   Running   gke-tpu-3335d306-dhnf   <none>
v6e-multislice-slice-0-1-g5kzv   Running   gke-tpu-3335d306-3ww8   tpu-slice-a
v6e-multislice-slice-0-2-rnp55   Running   gke-tpu-3335d306-gqwm   tpu-slice-a
v6e-multislice-slice-0-3-2dtbf   Running   gke-tpu-3335d306-ccp2   tpu-slice-a

# --------------------------------------------------------------------------------------------------
# 4 Pods of v6e-multislice-slice-1 -> Exclusively bound to tpu-slice-b (ICI intact)
# --------------------------------------------------------------------------------------------------
v6e-multislice-slice-1-0-t4xq6   Running   gke-tpu-08d18b59-rkb0   <none>
v6e-multislice-slice-1-1-nw5hp   Running   gke-tpu-08d18b59-zvvp   tpu-slice-b
v6e-multislice-slice-1-2-gjt79   Running   gke-tpu-08d18b59-9mqf   tpu-slice-b
v6e-multislice-slice-1-3-gzm7p   Running   gke-tpu-08d18b59-j1fb   tpu-slice-b
```

The output demonstrates JobSet's Leader/Follower scheduling mechanism for `exclusive-topology`. The first pod (Leader, index 0) is scheduled first without a specific nodepool restriction (showing `<none>`). Once it is placed on a node, JobSet intercepts the remaining follower pods and dynamically injects the Leader's selected nodepool into their node selectors. This guarantees that all follower pods are forced into the exact same physical nodepool as the leader, keeping the high-speed ICI network between the TPU chips intact.

With the pods correctly scheduled, the final step is to verify that the JAX application can communicate across both slices and utilize all available TPU chips. By inspecting the logs of any pod, you can confirm that the multislice setup is working:

```bash
$ kubectl logs pod/v6e-multislice-slice-0-0-sk2b6
Global device count: 32
```

The log output `Global device count: 32` confirms that the application sees all 32 chips (2 slices × 16 chips/slice). This demonstrates that JobSet has successfully orchestrated a multislice workload, enabling communication across the DCN and presenting a unified accelerator environment to the training job.

## Further Reading

For more detailed information and official tutorials on TPU Multislice and Kueue integration, please refer to the following resources:

*   **[Deploy TPU Multislices in GKE](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/tpu-multislice)**: Official Google Cloud documentation on TPU Multislice architecture and manual deployment.
*   **[Orchestrate TPU Multislice workloads using JobSet and Kueue](https://docs.cloud.google.com/kubernetes-engine/docs/tutorials/tpu-multislice-kueue)**: A comprehensive GKE tutorial on setting up queueing and admission for TPUs.
*   **[Run a Kueue scheduled JobSet](https://kueue.sigs.k8s.io/docs/tasks/run/jobsets/#example-jobset)**: Official Kueue documentation detailing queue selection and resource configuration for JobSets.