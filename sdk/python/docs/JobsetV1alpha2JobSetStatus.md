# JobsetV1alpha2JobSetStatus

JobSetStatus defines the observed state of JobSet
## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**conditions** | [**list[V1Condition]**](V1Condition.md) |  | [optional] 
**replicated_jobs_status** | [**list[JobsetV1alpha2ReplicatedJobStatus]**](JobsetV1alpha2ReplicatedJobStatus.md) | ReplicatedJobsStatus track the number of JobsReady for each replicatedJob. | [optional] 
**restarts** | **int** | Restarts tracks the number of times the JobSet has restarted (i.e. recreated in case of RecreateAll policy). | [optional] 
**restarts_count_towards_max** | **int** | RestartsCountTowardsMax tracks the number of times the JobSet has restarted that counts towards the maximum allowed number of restarts. | [optional] 
**terminal_state** | **str** | TerminalState the state of the JobSet when it finishes execution. It can be either Complete or Failed. Otherwise, it is empty by default. | [optional] 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


