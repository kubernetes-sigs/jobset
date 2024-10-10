# JobsetV1alpha2JobSetStatus

JobSetStatus defines the observed state of JobSet

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**conditions** | [**List[V1Condition]**](V1Condition.md) |  | [optional] 
**replicated_jobs_status** | [**List[JobsetV1alpha2ReplicatedJobStatus]**](JobsetV1alpha2ReplicatedJobStatus.md) | ReplicatedJobsStatus track the number of JobsReady for each replicatedJob. | [optional] 
**restarts** | **int** | Restarts tracks the number of times the JobSet has restarted (i.e. recreated in case of RecreateAll policy). | [optional] 
**restarts_count_towards_max** | **int** | RestartsCountTowardsMax tracks the number of times the JobSet has restarted that counts towards the maximum allowed number of restarts. | [optional] 
**terminal_state** | **str** | TerminalState the state of the JobSet when it finishes execution. It can be either Complete or Failed. Otherwise, it is empty by default. | [optional] 

## Example

```python
from jobset.models.jobset_v1alpha2_job_set_status import JobsetV1alpha2JobSetStatus

# TODO update the JSON string below
json = "{}"
# create an instance of JobsetV1alpha2JobSetStatus from a JSON string
jobset_v1alpha2_job_set_status_instance = JobsetV1alpha2JobSetStatus.from_json(json)
# print the JSON string representation of the object
print(JobsetV1alpha2JobSetStatus.to_json())

# convert the object into a dict
jobset_v1alpha2_job_set_status_dict = jobset_v1alpha2_job_set_status_instance.to_dict()
# create an instance of JobsetV1alpha2JobSetStatus from a dict
jobset_v1alpha2_job_set_status_from_dict = JobsetV1alpha2JobSetStatus.from_dict(jobset_v1alpha2_job_set_status_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


