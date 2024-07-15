# JobsetV1alpha2Coordinator

Coordinator defines which pod can be marked as the coordinator for the JobSet workload.
## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**job_index** | **int** | JobIndex is the index of Job which contains the coordinator pod (i.e., for a ReplicatedJob with N replicas, there are Job indexes 0 to N-1). Defaults to 0 if unset. | [optional] 
**pod_index** | **int** | PodIndex is the Job completion index of the coordinator pod. Defaults to 0 if unset. | [optional] 
**replicated_job** | **str** | ReplicatedJob is the name of the ReplicatedJob which contains the coordinator pod. | [default to '']

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


