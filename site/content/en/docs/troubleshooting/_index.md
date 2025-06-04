---

title: "Troubleshooting"
linkTitle: "Troubleshooting"
weight: 10
date: 2022-02-14
description: >
  JobSet troubleshooting tips.
no_list: true
---

## Troubleshooting Common Issues

## 1. "Webhook not available" error when attempting to create a JobSet

Example error: `failed calling webhook "mjobset.kb.io": failed to call webhook: Post "https://jobset-webhook-service.jobset-system.svc:443/mutate-jobset-x-k8s-io-v1alpha1-jobset?timeout=10s": no endpoints available for service "jobset-webhook-service"`

**Cause**: Usually this means the JobSet controller manager Deployment pods are unschedulable for some reason.

**Solution**: Check if jobset-controller-manager deployment pods are running (`kubectl get pods -n jobset-system`).
If they are in a `Pending` state, describe the pod to see why (`kubectl describe pod <pod> -n jobset-system`), you
should see a message in the pod Events indicating why they are unschedulable. The solution will depend on why the pods
are unschedulable. For example, if they unschedulable due to insufficient CPU/memory, the solution is to scale up your CPU node pools or turn on autoscaling.

## 2. JobSet is created but child jobs and/or pods are not being created 

Check the jobset controller logs to see why the jobs are not being created:

- `kubectl get pods -n jobset-system`
- `kubectl logs <pod> -n jobset-system`

Inspect the logs to look for one of the following issues:

1. Error message indicating an index does not exist (example: ` "error": "Index with name field:.metadata.controller does not exist"`)

**Cause**: In older versions of JobSet (older than v0.2.1) if the indexes could not be built for some reason, the JobSet controller would log the error and launch anyway. This resulted in confusing behavior later when trying to create JobSets, where the controller would encounter this "index not found" error and not be able to create any jobs. This bug was fixed
in v0.2.1 so the JobSet controller now fails fast and exits with an error if indexes cannot be built.

**Solution**: Uninstall JobSet and re-install using the latest release (or at minimum, JobSet v0.2.1). See [installation guide](/docs/setup/install.md) for the commands to do this.

2. Validation error creating Jobs and/or Services, indicating the Job/Service name is invalid.

**Cause**: Generated child job names or headless services names (which are derived from the JobSet name and ReplicatedJob names) are not valid. 

**Solution**: Validation has been added to fail the JobSet creation if the generated job/service names will be invalid, but the fix is not included in a release yet. For now, to resolve this simply delete/recreate the JobSet with a name such that:

* The generated Job names (format: `<jobset-name>-<replicatedJobName>-<jobIndex>-<podIndex>.<subdomain>`) will be valid DNS labels as defined in RFC 1035.
* The subdomain name (manually specified in `js.Spec.Network.Subdomain` or defaulted to the JobSet name if unspecified) is both [RFC 1123](https://datatracker.ietf.org/doc/html/rfc1123) compliant and [RFC 1035](https://datatracker.ietf.org/doc/html/rfc1035) compliant.


## 3. Using JobSet + Kueue, preempted workloads never resume

Look at the JobSet controller logs and you'll probably see an error like this:

```
 ERROR   resuming jobset {"controller": "jobset", "controllerGroup": "jobset.x-k8s.io", "controllerKind": "JobSet", "JobSet": {"name":"js","namespace":"default"}, "namespace": "default", "name": "js", "reconcileID": "e1ab5e21-586c-496e-96b7-8629cd702f3b", "jobset": {"name":"js","namespace":"default"}, "error": "jobs.batch \"js-slice-job-1\" is forbidden: User \"system:serviceaccount:jobset-system:jobset-controller-manager\" cannot update resource \"jobs/status\" in API group \"batch\" in the namespace \"default\""}
 ```

**Cause**: This could be due to a known bug in an older version of JobSet, or a known bug in an older version of Kueue. JobSet and Kueue integration requires JobSet v0.2.3+ and Kueue v0.4.1+.

**Solution**: If you're using JobSet version less than v0.2.3, uninstall and re-install using a versoin >= v0.2.3 (see the JobSet [installation guide](https://jobset.sigs.k8s.io/docs/installation/) for the commands to do this). If you're using a Kueue version less than v0.4.1, uninstall and re-install using a v0.4.1 (see the Kueue [installation guide](https://kueue.sigs.k8s.io/docs/installation/) for the commands to do this).

## 4. Troubleshooting network communication between different Pods

**Cause**: The network communication between different Pods might be blocked by the network policy, or caused by unstable cluster environment

**Solution**: You can follow the following debugging steps to troubleshoot. First, you can deploy the example by running `kubectl apply -f jobset-network.yaml` [example](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/simple/jobset-with-network.yaml) and then check if the pods and services of the JobSet are running correctly. Also, you can use the exec command to enter the container. By checking the /etc/hosts file within the container, you can observe the presence of a domain name, such as network-jobset-leader-0-0.example This domain name allows other containers to access the current pod. Similarly, you can also utilize the domain names of other pods for network communication.
```bash
root@VM-0-4-ubuntu:/home/ubuntu# vi jobset-network.yaml
root@VM-0-4-ubuntu:/home/ubuntu# kubectl apply -f jobset-network.yaml
jobset.jobset.x-k8s.io/network-jobset created
root@VM-0-4-ubuntu:/home/ubuntu# kubectl get pods
NAME                               READY   STATUS    RESTARTS   AGE   IP          NODE               NOMINATED NODE   READINESS GATES
network-jobset-leader-0-0-5xnzz    1/1     Running   0          17m   10.6.2.27   cluster1-worker    <none>           <none>
network-jobset-workers-0-0-78k9j   1/1     Running   0          17m   10.6.1.16   cluster1-worker2   <none>           <none>
network-jobset-workers-0-1-rmw42   1/1     Running   0          17m   10.6.2.28   cluster1-worker    <none>           <none>
root@VM-0-4-ubuntu:/home/ubuntu# kubectl get svc
NAME         TYPE        CLUSTER-IP   EXTERNAL-IP   PORT(S)   AGE
example      ClusterIP   None         <none>        <none>    19s
kubernetes   ClusterIP   10.96.0.1    <none>        443/TCP   2d1h
```

```bash
root@VM-0-4-ubuntu:/home/ubuntu# kubectl exec -it network-jobset-leader-0-0-5xnzz -- sh
/ # cat /etc/hosts
# Kubernetes-managed hosts file.
127.0.0.1	localhost
...
10.6.2.27	network-jobset-leader-0-0.example.default.svc.cluster.local	network-jobset-leader-0-0
/ # ping network-jobset-workers-0-0.example
PING network-jobset-workers-0-0.example (10.6.1.16): 56 data bytes
64 bytes from 10.6.1.16: seq=0 ttl=62 time=0.121 ms
64 bytes from 10.6.1.16: seq=1 ttl=62 time=0.093 ms
64 bytes from 10.6.1.16: seq=2 ttl=62 time=0.094 ms
64 bytes from 10.6.1.16: seq=3 ttl=62 time=0.103 ms
--- network-jobset-workers-0-0.example ping statistics ---
4 packets transmitted, 4 packets received, 0% packet loss
round-trip min/avg/max = 0.093/0.102/0.121 ms
/ # ping network-jobset-workers-0-1.example
PING network-jobset-workers-0-1.example (10.6.2.28): 56 data bytes
64 bytes from 10.6.2.28: seq=0 ttl=63 time=0.068 ms
64 bytes from 10.6.2.28: seq=1 ttl=63 time=0.072 ms
64 bytes from 10.6.2.28: seq=2 ttl=63 time=0.079 ms
--- network-jobset-workers-0-1.example ping statistics ---
```