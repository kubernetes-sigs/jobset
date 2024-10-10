# JobsetV1alpha2SuccessPolicy

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**operator** | **str** | Operator determines either All or Any of the selected jobs should succeed to consider the JobSet successful | [default to '']
**target_replicated_jobs** | **list[str]** | TargetReplicatedJobs are the names of the replicated jobs the operator will apply to. A null or empty list will apply to all replicatedJobs. | [optional] 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


