# JobsetV1alpha2FailurePolicy

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**max_restarts** | **int** | MaxRestarts defines the limit on the number of JobSet restarts. A restart is achieved by recreating all active child jobs. | [optional] 
**restart_strategy** | **str** | RestartStrategy defines the strategy to use when restarting the JobSet. Defaults to Recreate. | [optional] 
**rules** | [**list[JobsetV1alpha2FailurePolicyRule]**](JobsetV1alpha2FailurePolicyRule.md) | List of failure policy rules for this JobSet. For a given Job failure, the rules will be evaluated in order, and only the first matching rule will be executed. If no matching rule is found, the RestartJobSet action is applied. | [optional] 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


