# IoK8sApiCoreV1ClusterTrustBundleProjection

ClusterTrustBundleProjection describes how to select a set of ClusterTrustBundle objects and project their contents into the pod filesystem.

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**label_selector** | [**IoK8sApimachineryPkgApisMetaV1LabelSelector**](IoK8sApimachineryPkgApisMetaV1LabelSelector.md) |  | [optional] 
**name** | **str** | Select a single ClusterTrustBundle by object name.  Mutually-exclusive with signerName and labelSelector. | [optional] 
**optional** | **bool** | If true, don&#39;t block pod startup if the referenced ClusterTrustBundle(s) aren&#39;t available.  If using name, then the named ClusterTrustBundle is allowed not to exist.  If using signerName, then the combination of signerName and labelSelector is allowed to match zero ClusterTrustBundles. | [optional] 
**path** | **str** | Relative path from the volume root to write the bundle. | 
**signer_name** | **str** | Select all ClusterTrustBundles that match this signer name. Mutually-exclusive with name.  The contents of all selected ClusterTrustBundles will be unified and deduplicated. | [optional] 

## Example

```python
from jobset.models.io_k8s_api_core_v1_cluster_trust_bundle_projection import IoK8sApiCoreV1ClusterTrustBundleProjection

# TODO update the JSON string below
json = "{}"
# create an instance of IoK8sApiCoreV1ClusterTrustBundleProjection from a JSON string
io_k8s_api_core_v1_cluster_trust_bundle_projection_instance = IoK8sApiCoreV1ClusterTrustBundleProjection.from_json(json)
# print the JSON string representation of the object
print(IoK8sApiCoreV1ClusterTrustBundleProjection.to_json())

# convert the object into a dict
io_k8s_api_core_v1_cluster_trust_bundle_projection_dict = io_k8s_api_core_v1_cluster_trust_bundle_projection_instance.to_dict()
# create an instance of IoK8sApiCoreV1ClusterTrustBundleProjection from a dict
io_k8s_api_core_v1_cluster_trust_bundle_projection_from_dict = IoK8sApiCoreV1ClusterTrustBundleProjection.from_dict(io_k8s_api_core_v1_cluster_trust_bundle_projection_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


