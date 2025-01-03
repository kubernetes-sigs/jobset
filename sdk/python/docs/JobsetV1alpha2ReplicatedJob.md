# JobsetV1alpha2ReplicatedJob

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**depends_on** | [**list[JobsetV1alpha2DependsOn]**](JobsetV1alpha2DependsOn.md) | DependsOn is an optional list that specifies the preceding ReplicatedJobs upon which the current ReplicatedJob depends. If specified, the ReplicatedJob will be created only after the referenced ReplicatedJobs reach their desired state. The Order of ReplicatedJobs is defined by their enumeration in the slice. Note, that the first ReplicatedJob in the slice cannot use the DependsOn API. This API is mutually exclusive with the StartupPolicy API. | [optional] 
**name** | **str** | Name is the name of the entry and will be used as a suffix for the Job name. | [default to '']
**replicas** | **int** | Replicas is the number of jobs that will be created from this ReplicatedJob&#39;s template. Jobs names will be in the format: &lt;jobSet.name&gt;-&lt;spec.replicatedJob.name&gt;-&lt;job-index&gt; | [optional] 
**template** | [**V1JobTemplateSpec**](V1JobTemplateSpec.md) |  | 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


