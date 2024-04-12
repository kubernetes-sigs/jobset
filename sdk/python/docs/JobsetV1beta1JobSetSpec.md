# JobsetV1beta1JobSetSpec

JobSetSpec defines the desired state of JobSet
## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**failure_policy** | [**JobsetV1beta1FailurePolicy**](JobsetV1beta1FailurePolicy.md) |  | [optional] 
**managed_by** | **str** | ManagedBy is used to indicate the controller or entity that manages a JobSet | [optional] 
**network** | [**JobsetV1beta1Network**](JobsetV1beta1Network.md) |  | [optional] 
**replicated_jobs** | [**list[JobsetV1beta1ReplicatedJob]**](JobsetV1beta1ReplicatedJob.md) | ReplicatedJobs is the group of jobs that will form the set. | [optional] 
**startup_policy** | [**JobsetV1beta1StartupPolicy**](JobsetV1beta1StartupPolicy.md) |  | [optional] 
**success_policy** | [**JobsetV1beta1SuccessPolicy**](JobsetV1beta1SuccessPolicy.md) |  | [optional] 
**suspend** | **bool** | Suspend suspends all running child Jobs when set to true. | [optional] 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


