# JobsetV1alpha2JobSet

JobSet is the Schema for the jobsets API

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**api_version** | **str** | APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources | [optional] 
**kind** | **str** | Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds | [optional] 
**metadata** | [**V1ObjectMeta**](V1ObjectMeta.md) |  | [optional] 
**spec** | [**JobsetV1alpha2JobSetSpec**](JobsetV1alpha2JobSetSpec.md) |  | [optional] 
**status** | [**JobsetV1alpha2JobSetStatus**](JobsetV1alpha2JobSetStatus.md) |  | [optional] 

## Example

```python
from jobset.models.jobset_v1alpha2_job_set import JobsetV1alpha2JobSet

# TODO update the JSON string below
json = "{}"
# create an instance of JobsetV1alpha2JobSet from a JSON string
jobset_v1alpha2_job_set_instance = JobsetV1alpha2JobSet.from_json(json)
# print the JSON string representation of the object
print(JobsetV1alpha2JobSet.to_json())

# convert the object into a dict
jobset_v1alpha2_job_set_dict = jobset_v1alpha2_job_set_instance.to_dict()
# create an instance of JobsetV1alpha2JobSet from a dict
jobset_v1alpha2_job_set_from_dict = JobsetV1alpha2JobSet.from_dict(jobset_v1alpha2_job_set_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


