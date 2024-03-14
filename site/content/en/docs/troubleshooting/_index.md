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

**Solution**: If you're using JobSet version less than v0.2.3, uninstall and re-install using a versoin >= v0.2.3 (see the JobSet [installation guide](/docs/setup/install.md) for the commands to do this). If you're using a Kueue version less than v0.4.1, uninstall and re-install using a v0.4.1 (see the Kueue [installation guide](https://kueue.sigs.k8s.io/docs/installation/) for the commands to do this).
