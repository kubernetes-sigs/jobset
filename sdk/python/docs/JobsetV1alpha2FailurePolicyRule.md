# JobsetV1alpha2FailurePolicyRule

FailurePolicyRule defines a FailurePolicyAction to be executed if a child job fails due to a reason listed in OnJobFailureReasons.
## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**action** | **str** | The action to take if the rule is matched. | [default to '']
**on_job_failure_reasons** | **list[str]** | The requirement on the job failure reasons. The requirement is satisfied if at least one reason matches the list. | 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


