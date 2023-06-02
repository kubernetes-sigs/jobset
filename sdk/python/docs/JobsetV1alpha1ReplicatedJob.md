# JobsetV1alpha1ReplicatedJob

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**name** | **str** | Name is the name of the entry and will be used as a suffix for the Job name. | [default to '']
**network** | [**JobsetV1alpha1Network**](JobsetV1alpha1Network.md) |  | [optional] 
**replicas** | **int** | Replicas is the number of jobs that will be created from this ReplicatedJob&#39;s template. Jobs names will be in the format: &lt;jobSet.name&gt;-&lt;spec.replicatedJob.name&gt;-&lt;job-index&gt; | [optional] 
**template** | [**K8sIoApiBatchV1JobTemplateSpec**](K8sIoApiBatchV1JobTemplateSpec.md) |  | 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


