# JobsetV1alpha1JobSetSpec

JobSetSpec defines the desired state of JobSet
## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**failure_policy** | [**JobsetV1alpha1FailurePolicy**](JobsetV1alpha1FailurePolicy.md) |  | [optional] 
**managed_by** | **str** | ManagedBy is used to indicate the controller or entity that manages a JobSet | [optional] 
**network** | [**JobsetV1alpha1Network**](JobsetV1alpha1Network.md) |  | [optional] 
**replicated_jobs** | [**list[JobsetV1alpha1ReplicatedJob]**](JobsetV1alpha1ReplicatedJob.md) | ReplicatedJobs is the group of jobs that will form the set. | [optional] 
**startup_policy** | [**JobsetV1alpha1StartupPolicy**](JobsetV1alpha1StartupPolicy.md) |  | [optional] 
**success_policy** | [**JobsetV1alpha1SuccessPolicy**](JobsetV1alpha1SuccessPolicy.md) |  | [optional] 
**suspend** | **bool** | Suspend suspends all running child Jobs when set to true. | [optional] 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


