---
title: JobSet API
content_type: tool-reference
package: jobset.x-k8s.io/v1alpha2
auto_generated: true
description: Generated API reference documentation for jobset.x-k8s.io/v1alpha2.
---


## Resource Types 


- [JobSet](#jobset-x-k8s-io-v1alpha2-JobSet)
  

## `JobSet`     {#jobset-x-k8s-io-v1alpha2-JobSet}
    

**Appears in:**



<p>JobSet is the Schema for the jobsets API</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
    
<tr><td><code>apiVersion</code><br/>string</td><td><code>jobset.x-k8s.io/v1alpha2</code></td></tr>
<tr><td><code>kind</code><br/>string</td><td><code>JobSet</code></td></tr>
    
  
<tr><td><code>spec</code> <B>[Required]</B><br/>
<a href="#jobset-x-k8s-io-v1alpha2-JobSetSpec"><code>JobSetSpec</code></a>
</td>
<td>
   <span class="text-muted">No description provided.</span></td>
</tr>
<tr><td><code>status</code> <B>[Required]</B><br/>
<a href="#jobset-x-k8s-io-v1alpha2-JobSetStatus"><code>JobSetStatus</code></a>
</td>
<td>
   <span class="text-muted">No description provided.</span></td>
</tr>
</tbody>
</table>

## `Coordinator`     {#jobset-x-k8s-io-v1alpha2-Coordinator}
    

**Appears in:**

- [JobSetSpec](#jobset-x-k8s-io-v1alpha2-JobSetSpec)


<p>Coordinator defines which pod can be marked as the coordinator for the JobSet workload.</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
    
  
<tr><td><code>replicatedJob</code> <B>[Required]</B><br/>
<code>string</code>
</td>
<td>
   <p>ReplicatedJob is the name of the ReplicatedJob which contains
the coordinator pod.</p>
</td>
</tr>
<tr><td><code>jobIndex</code> <B>[Required]</B><br/>
<code>int</code>
</td>
<td>
   <p>JobIndex is the index of Job which contains the coordinator pod
(i.e., for a ReplicatedJob with N replicas, there are Job indexes 0 to N-1).</p>
</td>
</tr>
<tr><td><code>podIndex</code> <B>[Required]</B><br/>
<code>int</code>
</td>
<td>
   <p>PodIndex is the Job completion index of the coordinator pod.</p>
</td>
</tr>
</tbody>
</table>

## `DependsOn`     {#jobset-x-k8s-io-v1alpha2-DependsOn}
    

**Appears in:**

- [ReplicatedJob](#jobset-x-k8s-io-v1alpha2-ReplicatedJob)


<p>DependsOn defines the dependency on the previous ReplicatedJob status.</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
    
  
<tr><td><code>name</code> <B>[Required]</B><br/>
<code>string</code>
</td>
<td>
   <p>Name of the previous ReplicatedJob.</p>
</td>
</tr>
<tr><td><code>status</code> <B>[Required]</B><br/>
<a href="#jobset-x-k8s-io-v1alpha2-DependsOnStatus"><code>DependsOnStatus</code></a>
</td>
<td>
   <p>Status defines the condition for the ReplicatedJob. Only Ready or Complete status can be set.</p>
</td>
</tr>
</tbody>
</table>

## `DependsOnStatus`     {#jobset-x-k8s-io-v1alpha2-DependsOnStatus}
    
(Alias of `string`)

**Appears in:**

- [DependsOn](#jobset-x-k8s-io-v1alpha2-DependsOn)





## `FailurePolicy`     {#jobset-x-k8s-io-v1alpha2-FailurePolicy}
    

**Appears in:**

- [JobSetSpec](#jobset-x-k8s-io-v1alpha2-JobSetSpec)



<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
    
  
<tr><td><code>maxRestarts</code> <B>[Required]</B><br/>
<code>int32</code>
</td>
<td>
   <p>MaxRestarts defines the limit on the number of JobSet restarts.
A restart is achieved by recreating all active child jobs.</p>
</td>
</tr>
<tr><td><code>restartStrategy</code><br/>
<a href="#jobset-x-k8s-io-v1alpha2-JobSetRestartStrategy"><code>JobSetRestartStrategy</code></a>
</td>
<td>
   <p>RestartStrategy defines the strategy to use when restarting the JobSet.
Defaults to Recreate.</p>
</td>
</tr>
<tr><td><code>rules</code> <B>[Required]</B><br/>
<a href="#jobset-x-k8s-io-v1alpha2-FailurePolicyRule"><code>[]FailurePolicyRule</code></a>
</td>
<td>
   <p>List of failure policy rules for this JobSet.
For a given Job failure, the rules will be evaluated in order,
and only the first matching rule will be executed.
If no matching rule is found, the RestartJobSet action is applied.</p>
</td>
</tr>
</tbody>
</table>

## `FailurePolicyAction`     {#jobset-x-k8s-io-v1alpha2-FailurePolicyAction}
    
(Alias of `string`)

**Appears in:**

- [FailurePolicyRule](#jobset-x-k8s-io-v1alpha2-FailurePolicyRule)


<p>FailurePolicyAction defines the action the JobSet controller will take for
a given FailurePolicyRule.</p>




## `FailurePolicyRule`     {#jobset-x-k8s-io-v1alpha2-FailurePolicyRule}
    

**Appears in:**

- [FailurePolicy](#jobset-x-k8s-io-v1alpha2-FailurePolicy)


<p>FailurePolicyRule defines a FailurePolicyAction to be executed if a child job
fails due to a reason listed in OnJobFailureReasons.</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
    
  
<tr><td><code>name</code> <B>[Required]</B><br/>
<code>string</code>
</td>
<td>
   <p>The name of the failure policy rule.
The name is defaulted to 'failurePolicyRuleN' where N is the index of the failure policy rule.
The name must match the regular expression &quot;^<a href="%5BA-Za-z0-9_,:%5D*%5BA-Za-z0-9_%5D">A-Za-z</a>?$&quot;.</p>
</td>
</tr>
<tr><td><code>action</code> <B>[Required]</B><br/>
<a href="#jobset-x-k8s-io-v1alpha2-FailurePolicyAction"><code>FailurePolicyAction</code></a>
</td>
<td>
   <p>The action to take if the rule is matched.</p>
</td>
</tr>
<tr><td><code>onJobFailureReasons</code> <B>[Required]</B><br/>
<code>[]string</code>
</td>
<td>
   <p>The requirement on the job failure reasons. The requirement
is satisfied if at least one reason matches the list.
The rules are evaluated in order, and the first matching
rule is executed.
An empty list applies the rule to any job failure reason.</p>
</td>
</tr>
<tr><td><code>targetReplicatedJobs</code><br/>
<code>[]string</code>
</td>
<td>
   <p>TargetReplicatedJobs are the names of the replicated jobs the operator applies to.
An empty list will apply to all replicatedJobs.</p>
</td>
</tr>
</tbody>
</table>

## `JobSetRestartStrategy`     {#jobset-x-k8s-io-v1alpha2-JobSetRestartStrategy}
    
(Alias of `string`)

**Appears in:**

- [FailurePolicy](#jobset-x-k8s-io-v1alpha2-FailurePolicy)





## `JobSetSpec`     {#jobset-x-k8s-io-v1alpha2-JobSetSpec}
    

**Appears in:**

- [JobSet](#jobset-x-k8s-io-v1alpha2-JobSet)


<p>JobSetSpec defines the desired state of JobSet</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
    
  
<tr><td><code>replicatedJobs</code> <B>[Required]</B><br/>
<a href="#jobset-x-k8s-io-v1alpha2-ReplicatedJob"><code>[]ReplicatedJob</code></a>
</td>
<td>
   <p>ReplicatedJobs is the group of jobs that will form the set.</p>
</td>
</tr>
<tr><td><code>network</code><br/>
<a href="#jobset-x-k8s-io-v1alpha2-Network"><code>Network</code></a>
</td>
<td>
   <p>Network defines the networking options for the jobset.</p>
</td>
</tr>
<tr><td><code>successPolicy</code> <B>[Required]</B><br/>
<a href="#jobset-x-k8s-io-v1alpha2-SuccessPolicy"><code>SuccessPolicy</code></a>
</td>
<td>
   <p>SuccessPolicy configures when to declare the JobSet as
succeeded.
The JobSet is always declared succeeded if all jobs in the set
finished with status complete.</p>
</td>
</tr>
<tr><td><code>failurePolicy</code> <B>[Required]</B><br/>
<a href="#jobset-x-k8s-io-v1alpha2-FailurePolicy"><code>FailurePolicy</code></a>
</td>
<td>
   <p>FailurePolicy, if set, configures when to declare the JobSet as
failed.
The JobSet is always declared failed if any job in the set
finished with status failed.</p>
</td>
</tr>
<tr><td><code>startupPolicy</code> <B>[Required]</B><br/>
<a href="#jobset-x-k8s-io-v1alpha2-StartupPolicy"><code>StartupPolicy</code></a>
</td>
<td>
   <p>StartupPolicy, if set, configures in what order jobs must be started
Deprecated: StartupPolicy is deprecated, please use the DependsOn API.</p>
</td>
</tr>
<tr><td><code>suspend</code> <B>[Required]</B><br/>
<code>bool</code>
</td>
<td>
   <p>Suspend suspends all running child Jobs when set to true.</p>
</td>
</tr>
<tr><td><code>coordinator</code><br/>
<a href="#jobset-x-k8s-io-v1alpha2-Coordinator"><code>Coordinator</code></a>
</td>
<td>
   <p>Coordinator can be used to assign a specific pod as the coordinator for
the JobSet. If defined, an annotation will be added to all Jobs and pods with
coordinator pod, which contains the stable network endpoint where the
coordinator pod can be reached.
jobset.sigs.k8s.io/coordinator=<!-- raw HTML omitted -->.<!-- raw HTML omitted --></p>
</td>
</tr>
<tr><td><code>managedBy</code><br/>
<code>string</code>
</td>
<td>
   <p>ManagedBy is used to indicate the controller or entity that manages a JobSet.
The built-in JobSet controller reconciles JobSets which don't have this
field at all or the field value is the reserved string
<code>jobset.sigs.k8s.io/jobset-controller</code>, but skips reconciling JobSets
with a custom value for this field.</p>
<p>The value must be a valid domain-prefixed path (e.g. acme.io/foo) -
all characters before the first &quot;/&quot; must be a valid subdomain as defined
by RFC 1123. All characters trailing the first &quot;/&quot; must be valid HTTP Path
characters as defined by RFC 3986. The value cannot exceed 63 characters.
The field is immutable.</p>
</td>
</tr>
<tr><td><code>ttlSecondsAfterFinished</code><br/>
<code>int32</code>
</td>
<td>
   <p>TTLSecondsAfterFinished limits the lifetime of a JobSet that has finished
execution (either Complete or Failed). If this field is set,
TTLSecondsAfterFinished after the JobSet finishes, it is eligible to be
automatically deleted. When the JobSet is being deleted, its lifecycle
guarantees (e.g. finalizers) will be honored. If this field is unset,
the JobSet won't be automatically deleted. If this field is set to zero,
the JobSet becomes eligible to be deleted immediately after it finishes.</p>
</td>
</tr>
</tbody>
</table>

## `JobSetStatus`     {#jobset-x-k8s-io-v1alpha2-JobSetStatus}
    

**Appears in:**

- [JobSet](#jobset-x-k8s-io-v1alpha2-JobSet)


<p>JobSetStatus defines the observed state of JobSet</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
    
  
<tr><td><code>conditions</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#condition-v1-meta"><code>[]k8s.io/apimachinery/pkg/apis/meta/v1.Condition</code></a>
</td>
<td>
   <span class="text-muted">No description provided.</span></td>
</tr>
<tr><td><code>restarts</code><br/>
<code>int32</code>
</td>
<td>
   <p>Restarts tracks the number of times the JobSet has restarted (i.e. recreated in case of RecreateAll policy).</p>
</td>
</tr>
<tr><td><code>restartsCountTowardsMax</code> <B>[Required]</B><br/>
<code>int32</code>
</td>
<td>
   <p>RestartsCountTowardsMax tracks the number of times the JobSet has restarted that counts towards the maximum allowed number of restarts.</p>
</td>
</tr>
<tr><td><code>terminalState</code> <B>[Required]</B><br/>
<code>string</code>
</td>
<td>
   <p>TerminalState the state of the JobSet when it finishes execution.
It can be either Completed or Failed. Otherwise, it is empty by default.</p>
</td>
</tr>
<tr><td><code>replicatedJobsStatus</code><br/>
<a href="#jobset-x-k8s-io-v1alpha2-ReplicatedJobStatus"><code>[]ReplicatedJobStatus</code></a>
</td>
<td>
   <p>ReplicatedJobsStatus track the number of JobsReady for each replicatedJob.</p>
</td>
</tr>
</tbody>
</table>

## `Network`     {#jobset-x-k8s-io-v1alpha2-Network}
    

**Appears in:**

- [JobSetSpec](#jobset-x-k8s-io-v1alpha2-JobSetSpec)



<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
    
  
<tr><td><code>enableDNSHostnames</code><br/>
<code>bool</code>
</td>
<td>
   <p>EnableDNSHostnames allows pods to be reached via their hostnames.
Pods will be reachable using the fully qualified pod hostname:
&lt;jobSet.name&gt;-&lt;spec.replicatedJob.name&gt;-<!-- raw HTML omitted -->-<!-- raw HTML omitted -->.<!-- raw HTML omitted --></p>
</td>
</tr>
<tr><td><code>subdomain</code><br/>
<code>string</code>
</td>
<td>
   <p>Subdomain is an explicit choice for a network subdomain name
When set, any replicated job in the set is added to this network.
Defaults to &lt;jobSet.name&gt; if not set.</p>
</td>
</tr>
<tr><td><code>publishNotReadyAddresses</code><br/>
<code>bool</code>
</td>
<td>
   <p>Indicates if DNS records of pods should be published before the pods are ready.
Defaults to True.</p>
</td>
</tr>
</tbody>
</table>

## `Operator`     {#jobset-x-k8s-io-v1alpha2-Operator}
    
(Alias of `string`)

**Appears in:**

- [SuccessPolicy](#jobset-x-k8s-io-v1alpha2-SuccessPolicy)


<p>Operator defines the target of a SuccessPolicy or FailurePolicy.</p>




## `ReplicatedJob`     {#jobset-x-k8s-io-v1alpha2-ReplicatedJob}
    

**Appears in:**

- [JobSetSpec](#jobset-x-k8s-io-v1alpha2-JobSetSpec)



<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
    
  
<tr><td><code>name</code> <B>[Required]</B><br/>
<code>string</code>
</td>
<td>
   <p>Name is the name of the entry and will be used as a suffix
for the Job name.</p>
</td>
</tr>
<tr><td><code>groupName</code> <B>[Required]</B><br/>
<code>string</code>
</td>
<td>
   <p>GroupName defines the name of the group this ReplicatedJob belongs to. Defaults to &quot;default&quot;</p>
</td>
</tr>
<tr><td><code>template</code> <B>[Required]</B><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#jobtemplatespec-v1-batch"><code>k8s.io/api/batch/v1.JobTemplateSpec</code></a>
</td>
<td>
   <p>Template defines the template of the Job that will be created.</p>
</td>
</tr>
<tr><td><code>replicas</code> <B>[Required]</B><br/>
<code>int32</code>
</td>
<td>
   <p>Replicas is the number of jobs that will be created from this ReplicatedJob's template.
Jobs names will be in the format: &lt;jobSet.name&gt;-&lt;spec.replicatedJob.name&gt;-<!-- raw HTML omitted --></p>
</td>
</tr>
<tr><td><code>dependsOn</code><br/>
<a href="#jobset-x-k8s-io-v1alpha2-DependsOn"><code>[]DependsOn</code></a>
</td>
<td>
   <p>DependsOn is an optional list that specifies the preceding ReplicatedJobs upon which
the current ReplicatedJob depends. If specified, the ReplicatedJob will be created
only after the referenced ReplicatedJobs reach their desired state.
The Order of ReplicatedJobs is defined by their enumeration in the slice.
Note, that the first ReplicatedJob in the slice cannot use the DependsOn API.
Currently, only a single item is supported in the DependsOn list.
If JobSet is suspended the all active ReplicatedJobs will be suspended. When JobSet is
resumed the Job sequence starts again.
This API is mutually exclusive with the StartupPolicy API.</p>
</td>
</tr>
</tbody>
</table>

## `ReplicatedJobStatus`     {#jobset-x-k8s-io-v1alpha2-ReplicatedJobStatus}
    

**Appears in:**

- [JobSetStatus](#jobset-x-k8s-io-v1alpha2-JobSetStatus)


<p>ReplicatedJobStatus defines the observed ReplicatedJobs Readiness.</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
    
  
<tr><td><code>name</code> <B>[Required]</B><br/>
<code>string</code>
</td>
<td>
   <p>Name of the ReplicatedJob.</p>
</td>
</tr>
<tr><td><code>ready</code> <B>[Required]</B><br/>
<code>int32</code>
</td>
<td>
   <p>Ready is the number of child Jobs where the number of ready pods and completed pods
is greater than or equal to the total expected pod count for the Job (i.e., the minimum
of job.spec.parallelism and job.spec.completions).</p>
</td>
</tr>
<tr><td><code>succeeded</code> <B>[Required]</B><br/>
<code>int32</code>
</td>
<td>
   <p>Succeeded is the number of successfully completed child Jobs.</p>
</td>
</tr>
<tr><td><code>failed</code> <B>[Required]</B><br/>
<code>int32</code>
</td>
<td>
   <p>Failed is the number of failed child Jobs.</p>
</td>
</tr>
<tr><td><code>active</code> <B>[Required]</B><br/>
<code>int32</code>
</td>
<td>
   <p>Active is the number of child Jobs with at least 1 pod in a running or pending state
which are not marked for deletion.</p>
</td>
</tr>
<tr><td><code>suspended</code> <B>[Required]</B><br/>
<code>int32</code>
</td>
<td>
   <p>Suspended is the number of child Jobs which are in a suspended state.</p>
</td>
</tr>
</tbody>
</table>

## `StartupPolicy`     {#jobset-x-k8s-io-v1alpha2-StartupPolicy}
    

**Appears in:**

- [JobSetSpec](#jobset-x-k8s-io-v1alpha2-JobSetSpec)



<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
    
  
<tr><td><code>startupPolicyOrder</code> <B>[Required]</B><br/>
<a href="#jobset-x-k8s-io-v1alpha2-StartupPolicyOptions"><code>StartupPolicyOptions</code></a>
</td>
<td>
   <p>StartupPolicyOrder determines the startup order of the ReplicatedJobs.
AnyOrder means to start replicated jobs in any order.
InOrder means to start them as they are listed in the JobSet. A ReplicatedJob is started only
when all the jobs of the previous one are ready.</p>
</td>
</tr>
</tbody>
</table>

## `StartupPolicyOptions`     {#jobset-x-k8s-io-v1alpha2-StartupPolicyOptions}
    
(Alias of `string`)

**Appears in:**

- [StartupPolicy](#jobset-x-k8s-io-v1alpha2-StartupPolicy)





## `SuccessPolicy`     {#jobset-x-k8s-io-v1alpha2-SuccessPolicy}
    

**Appears in:**

- [JobSetSpec](#jobset-x-k8s-io-v1alpha2-JobSetSpec)



<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
    
  
<tr><td><code>operator</code> <B>[Required]</B><br/>
<a href="#jobset-x-k8s-io-v1alpha2-Operator"><code>Operator</code></a>
</td>
<td>
   <p>Operator determines either All or Any of the selected jobs should succeed to consider the JobSet successful</p>
</td>
</tr>
<tr><td><code>targetReplicatedJobs</code><br/>
<code>[]string</code>
</td>
<td>
   <p>TargetReplicatedJobs are the names of the replicated jobs the operator will apply to.
A null or empty list will apply to all replicatedJobs.</p>
</td>
</tr>
</tbody>
</table>
  