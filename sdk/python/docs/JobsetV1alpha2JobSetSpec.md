# Jobsetv1JobSetSpec

JobSetSpec defines the desired state of JobSet
## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**failure_policy** | [**Jobsetv1FailurePolicy**](Jobsetv1FailurePolicy.md) |  | [optional] 
**managed_by** | **str** | ManagedBy is used to indicate the controller or entity that manages a JobSet | [optional] 
**network** | [**Jobsetv1Network**](Jobsetv1Network.md) |  | [optional] 
**replicated_jobs** | [**list[Jobsetv1ReplicatedJob]**](Jobsetv1ReplicatedJob.md) | ReplicatedJobs is the group of jobs that will form the set. | [optional] 
**startup_policy** | [**Jobsetv1StartupPolicy**](Jobsetv1StartupPolicy.md) |  | [optional] 
**success_policy** | [**Jobsetv1SuccessPolicy**](Jobsetv1SuccessPolicy.md) |  | [optional] 
**suspend** | **bool** | Suspend suspends all running child Jobs when set to true. | [optional] 
**ttl_seconds_after_finished** | **int** | TTLSecondsAfterFinished limits the lifetime of a JobSet that has finished execution (either Complete or Failed). If this field is set, TTLSecondsAfterFinished after the JobSet finishes, it is eligible to be automatically deleted. When the JobSet is being deleted, its lifecycle guarantees (e.g. finalizers) will be honored. If this field is unset, the JobSet won&#39;t be automatically deleted. If this field is set to zero, the JobSet becomes eligible to be deleted immediately after it finishes. | [optional] 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


