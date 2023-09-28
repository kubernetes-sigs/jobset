#!/usr/bin/python3
import sys
import argparse
import kubernetes.client
import kubernetes.config

JOBSET_NAME_KEY = "jobset.sigs.k8s.io/jobset-name"
NODE_POOL_KEY = "cloud.google.com/gke-nodepool"

def main(node_pools: str, jobset: str) -> None:
    target_node_pools = set(node_pools.split(','))
    print(f"Adding node label {JOBSET_NAME_KEY}={jobset} to the following node pools:")
    for np in target_node_pools:
        print(np)

    input('Once confirmed, hit any key to continue, or ctrl+C to exit.')

    # Load the Kubernetes configuration from the default location.
    kubernetes.config.load_kube_config()

    # Create a new Kubernetes client.
    client = kubernetes.client.CoreV1Api()

    # Get a list of all the nodes in the cluster.
    nodes = client.list_node().items

    body = {
        "metadata": {
            "labels": {
                JOBSET_NAME_KEY: jobset,
            }
        }
    }

    # Add the label to each node that is part of a target node pool.
    for node in nodes:
        if node.metadata.labels[NODE_POOL_KEY] not in target_node_pools:
            continue
        node.metadata.labels[JOBSET_NAME_KEY] = jobset
        client.patch_node(node.metadata.name, body)
        print(f"Successfully added label {JOBSET_NAME_KEY}={jobset} to node {node.metadata.name}")

if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("jobset", help="name of the jobset that should run on the given nodepools")
    parser.add_argument("nodepools", help="comma separated list of nodepool names")
    args = parser.parse_args()
    main(args.nodepools, args.jobset)