# IoK8sApiCoreV1ServiceAccountTokenProjection

ServiceAccountTokenProjection represents a projected service account token volume. This projection can be used to insert a service account token into the pods runtime filesystem for use against APIs (Kubernetes API Server or otherwise).

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**audience** | **str** | audience is the intended audience of the token. A recipient of a token must identify itself with an identifier specified in the audience of the token, and otherwise should reject the token. The audience defaults to the identifier of the apiserver. | [optional] 
**expiration_seconds** | **int** | expirationSeconds is the requested duration of validity of the service account token. As the token approaches expiration, the kubelet volume plugin will proactively rotate the service account token. The kubelet will start trying to rotate the token if the token is older than 80 percent of its time to live or if the token is older than 24 hours.Defaults to 1 hour and must be at least 10 minutes. | [optional] 
**path** | **str** | path is the path relative to the mount point of the file to project the token into. | 

## Example

```python
from jobset.models.io_k8s_api_core_v1_service_account_token_projection import IoK8sApiCoreV1ServiceAccountTokenProjection

# TODO update the JSON string below
json = "{}"
# create an instance of IoK8sApiCoreV1ServiceAccountTokenProjection from a JSON string
io_k8s_api_core_v1_service_account_token_projection_instance = IoK8sApiCoreV1ServiceAccountTokenProjection.from_json(json)
# print the JSON string representation of the object
print(IoK8sApiCoreV1ServiceAccountTokenProjection.to_json())

# convert the object into a dict
io_k8s_api_core_v1_service_account_token_projection_dict = io_k8s_api_core_v1_service_account_token_projection_instance.to_dict()
# create an instance of IoK8sApiCoreV1ServiceAccountTokenProjection from a dict
io_k8s_api_core_v1_service_account_token_projection_from_dict = IoK8sApiCoreV1ServiceAccountTokenProjection.from_dict(io_k8s_api_core_v1_service_account_token_projection_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


