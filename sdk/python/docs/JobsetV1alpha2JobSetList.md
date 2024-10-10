# JobsetV1alpha2JobSetList

JobSetList contains a list of JobSet

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**api_version** | **str** | APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources | [optional] 
**items** | [**List[JobsetV1alpha2JobSet]**](JobsetV1alpha2JobSet.md) |  | 
**kind** | **str** | Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds | [optional] 
**metadata** | [**V1ListMeta**](V1ListMeta.md) |  | [optional] 

## Example

```python
from jobset.models.jobset_v1alpha2_job_set_list import JobsetV1alpha2JobSetList

# TODO update the JSON string below
json = "{}"
# create an instance of JobsetV1alpha2JobSetList from a JSON string
jobset_v1alpha2_job_set_list_instance = JobsetV1alpha2JobSetList.from_json(json)
# print the JSON string representation of the object
print(JobsetV1alpha2JobSetList.to_json())

# convert the object into a dict
jobset_v1alpha2_job_set_list_dict = jobset_v1alpha2_job_set_list_instance.to_dict()
# create an instance of JobsetV1alpha2JobSetList from a dict
jobset_v1alpha2_job_set_list_from_dict = JobsetV1alpha2JobSetList.from_dict(jobset_v1alpha2_job_set_list_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


