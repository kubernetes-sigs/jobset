#!/usr/bin/python3
'''
This script can be used to add certain labels and taints
to a given list of GKE node pool, in order to dramatically
speed up scheduling of pods for very large JobSets which are
using an exclusive placement strategy of one job per node pool.

usage: label_nodes.py [-h] jobset nodepools

positional arguments:
  jobset      jobset yaml config
  nodepools   comma separated list of nodepool names


The way it works is by generating the list of namespaced job names
that will be created by the JobSet, and creating a 1:1 mapping of
job to node pool. For every given node pool, it looks up the job
mapped to that node pool, and applies a namespaced job name node label
to every node in that node pool, allowing the job nodeSelectors injected
by the JobSet controller to select those nodes to schedule the job pods on.
It also adds a NoSchedule taint to the nodes (tolerated by the job pods), 
preventing any external workloads from running on the nodes. Together,
these allow for exclusive placement of 1 job per GKE node pool.
'''

import sys
import yaml
import argparse
import kubernetes.client
import kubernetes.config

NAMESPACED_JOB_KEY = "alpha.jobset.sigs.k8s.io/namespaced-job"
NODE_POOL_KEY = "cloud.google.com/gke-nodepool"
NO_SCHEDULE_TAINT_KEY = "alpha.jobset.sigs.k8s.io/no-schedule"

def main(node_pools: str, jobset_yaml: str) -> None:
    namespaced_jobs = generate_namespaced_jobs(jobset_yaml)

    # Get target set of node pools to label from user input.
    target_node_pools = parse_node_pools(node_pools)

    # Validate we have 1 job per node pool.
    if len(namespaced_jobs) != len(target_node_pools):
        raise ValueError(f"number of node pools ({len(target_node_pools)}) does not match number of child jobs ({len(namespaced_jobs)})")

    # Map each job to a node pool.
    job_mapping = generate_job_mapping(namespaced_jobs, target_node_pools)

    # Load the Kubernetes configuration from the default location.
    kubernetes.config.load_kube_config()

    # Create a new Kubernetes client.
    client = kubernetes.client.CoreV1Api()

    # Get a list of all the nodes in the cluster.
    nodes = client.list_node().items

    # Add the label to each node that is part of a target node pool.
    for node in nodes:
        node_pool = node.metadata.labels[NODE_POOL_KEY]
        if node_pool not in target_node_pools:
            continue

        # Look up the job this node pool is assigned and add it as a label.
        body = {
            "metadata": {
                "labels": {
                    NAMESPACED_JOB_KEY: job_mapping[node_pool],
                },
            },
            "spec": {
                "taints": [
                    {
                        "key": NO_SCHEDULE_TAINT_KEY,
                        "value": "true",
                        "effect": "NoSchedule"
                    }
                ],
            },
        }

        client.patch_node(node.metadata.name, body)
        print(f"Successfully added label {NAMESPACED_JOB_KEY}={job_mapping[node_pool]} to node {node.metadata.name}")
        print(f"Successfully added taint key={NO_SCHEDULE_TAINT_KEY}, value=true, effect=NoSchedule to node {node.metadata.name}")


def parse_node_pools(node_pools: str) -> set:
    '''Parse comma separated list of node pools into a set, and confirm with user before continuing.'''
    node_pools_set = set(node_pools.split(','))
    
    print("Adding namespaced job name labels to the following node pools:")
    for np in node_pools_set:
        print(np)    

    input('Once confirmed, hit any key to continue, or ctrl+C to exit.')
    return node_pools_set


def generate_namespaced_jobs(jobset_yaml: str) -> list:
    '''Generate list of namespaced jobs from the jobset config.'''
    with open(jobset_yaml, 'r') as f:
        jobset = yaml.safe_load(f.read())

    jobset_name = jobset['metadata']['name']
    namespace = jobset['metadata'].get('namespace', 'default') # default namespace if unspecified

    jobs = []
    for replicated_job in jobset['spec']['replicatedJobs']:
        replicas = int(replicated_job.get('replicas', '1')) # replicas defaults to 1 if unspecified
        for job_idx in range(replicas):
            jobs.append(f"{namespace}_{jobset_name}-{replicated_job['name']}-{job_idx}")
    return jobs


def generate_job_mapping(namespaced_jobs: list[str], node_pools: set) -> dict:
    '''Map each job to a node pool, and return this mapping in a dictionary.'''
    mapping = {}
    for np in node_pools:
        mapping[np] = namespaced_jobs.pop()
    return mapping


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("jobset", help="jobset yaml config")
    parser.add_argument("nodepools", help="comma separated list of nodepool names")
    args = parser.parse_args()
    main(args.nodepools, args.jobset)