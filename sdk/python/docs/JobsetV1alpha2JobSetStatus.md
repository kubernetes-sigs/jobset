# JobsetV1alpha2JobSetStatus

JobSetStatus defines the observed state of JobSet
## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**replicated_jobs_status** | [**list[JobsetV1alpha2ReplicatedJobStatus]**](JobsetV1alpha2ReplicatedJobStatus.md) | ReplicatedJobsStatus track the number of JobsReady for each replicatedJob. | [optional] 
**conditions** | [**list[V1Condition]**](V1Condition.md) |  | [optional] 
**restarts** | **int** | Restarts tracks the number of times the JobSet has restarted (i.e. recreated in case of RecreateAll policy). | [optional] 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


