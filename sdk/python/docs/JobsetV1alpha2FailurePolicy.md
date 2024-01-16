# JobsetV1alpha2FailurePolicy

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**max_restarts** | **int** |  | [default to 0]
**rules** | [**list[JobsetV1alpha2FailurePolicyRule]**](JobsetV1alpha2FailurePolicyRule.md) | Evaluated in order on each failure. Only the first matched rule will be exeucted, the rest are ignored. If no rule matched, then the default behavior is executed: restart all child jobs and count the failure against maxRestarts. | [optional] 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


