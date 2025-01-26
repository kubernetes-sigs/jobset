# JobsetV1alpha2ReplicatedJobStatus

ReplicatedJobStatus defines the observed ReplicatedJobs Readiness.

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**active** | **int** | Active is the number of child Jobs with at least 1 pod in a running or pending state which are not marked for deletion. | [default to 0]
**failed** | **int** | Failed is the number of failed child Jobs. | [default to 0]
**name** | **str** | Name of the ReplicatedJob. | [default to '']
**ready** | **int** | Ready is the number of child Jobs where the number of ready pods and completed pods is greater than or equal to the total expected pod count for the Job (i.e., the minimum of job.spec.parallelism and job.spec.completions). | [default to 0]
**succeeded** | **int** | Succeeded is the number of successfully completed child Jobs. | [default to 0]
**suspended** | **int** | Suspended is the number of child Jobs which are in a suspended state. | [default to 0]

## Example

```python
from jobset.models.jobset_v1alpha2_replicated_job_status import JobsetV1alpha2ReplicatedJobStatus

# TODO update the JSON string below
json = "{}"
# create an instance of JobsetV1alpha2ReplicatedJobStatus from a JSON string
jobset_v1alpha2_replicated_job_status_instance = JobsetV1alpha2ReplicatedJobStatus.from_json(json)
# print the JSON string representation of the object
print(JobsetV1alpha2ReplicatedJobStatus.to_json())

# convert the object into a dict
jobset_v1alpha2_replicated_job_status_dict = jobset_v1alpha2_replicated_job_status_instance.to_dict()
# create an instance of JobsetV1alpha2ReplicatedJobStatus from a dict
jobset_v1alpha2_replicated_job_status_from_dict = JobsetV1alpha2ReplicatedJobStatus.from_dict(jobset_v1alpha2_replicated_job_status_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


