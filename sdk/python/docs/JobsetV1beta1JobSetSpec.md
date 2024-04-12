# Jobsetv1alpha1JobSetSpec

JobSetSpec defines the desired state of JobSet
## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**failure_policy** | [**Jobsetv1alpha1FailurePolicy**](Jobsetv1alpha1FailurePolicy.md) |  | [optional] 
**managed_by** | **str** | ManagedBy is used to indicate the controller or entity that manages a JobSet | [optional] 
**network** | [**Jobsetv1alpha1Network**](Jobsetv1alpha1Network.md) |  | [optional] 
**replicated_jobs** | [**list[Jobsetv1alpha1ReplicatedJob]**](Jobsetv1alpha1ReplicatedJob.md) | ReplicatedJobs is the group of jobs that will form the set. | [optional] 
**startup_policy** | [**Jobsetv1alpha1StartupPolicy**](Jobsetv1alpha1StartupPolicy.md) |  | [optional] 
**success_policy** | [**Jobsetv1alpha1SuccessPolicy**](Jobsetv1alpha1SuccessPolicy.md) |  | [optional] 
**suspend** | **bool** | Suspend suspends all running child Jobs when set to true. | [optional] 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


