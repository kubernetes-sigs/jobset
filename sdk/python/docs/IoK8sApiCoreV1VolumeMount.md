# IoK8sApiCoreV1VolumeMount

VolumeMount describes a mounting of a Volume within a container.

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**mount_path** | **str** | Path within the container at which the volume should be mounted.  Must not contain &#39;:&#39;. | 
**mount_propagation** | **str** | mountPropagation determines how mounts are propagated from the host to container and the other way around. When not set, MountPropagationNone is used. This field is beta in 1.10. When RecursiveReadOnly is set to IfPossible or to Enabled, MountPropagation must be None or unspecified (which defaults to None). | [optional] 
**name** | **str** | This must match the Name of a Volume. | 
**read_only** | **bool** | Mounted read-only if true, read-write otherwise (false or unspecified). Defaults to false. | [optional] 
**recursive_read_only** | **str** | RecursiveReadOnly specifies whether read-only mounts should be handled recursively.  If ReadOnly is false, this field has no meaning and must be unspecified.  If ReadOnly is true, and this field is set to Disabled, the mount is not made recursively read-only.  If this field is set to IfPossible, the mount is made recursively read-only, if it is supported by the container runtime.  If this field is set to Enabled, the mount is made recursively read-only if it is supported by the container runtime, otherwise the pod will not be started and an error will be generated to indicate the reason.  If this field is set to IfPossible or Enabled, MountPropagation must be set to None (or be unspecified, which defaults to None).  If this field is not specified, it is treated as an equivalent of Disabled. | [optional] 
**sub_path** | **str** | Path within the volume from which the container&#39;s volume should be mounted. Defaults to \&quot;\&quot; (volume&#39;s root). | [optional] 
**sub_path_expr** | **str** | Expanded path within the volume from which the container&#39;s volume should be mounted. Behaves similarly to SubPath but environment variable references $(VAR_NAME) are expanded using the container&#39;s environment. Defaults to \&quot;\&quot; (volume&#39;s root). SubPathExpr and SubPath are mutually exclusive. | [optional] 

## Example

```python
from jobset.models.io_k8s_api_core_v1_volume_mount import IoK8sApiCoreV1VolumeMount

# TODO update the JSON string below
json = "{}"
# create an instance of IoK8sApiCoreV1VolumeMount from a JSON string
io_k8s_api_core_v1_volume_mount_instance = IoK8sApiCoreV1VolumeMount.from_json(json)
# print the JSON string representation of the object
print(IoK8sApiCoreV1VolumeMount.to_json())

# convert the object into a dict
io_k8s_api_core_v1_volume_mount_dict = io_k8s_api_core_v1_volume_mount_instance.to_dict()
# create an instance of IoK8sApiCoreV1VolumeMount from a dict
io_k8s_api_core_v1_volume_mount_from_dict = IoK8sApiCoreV1VolumeMount.from_dict(io_k8s_api_core_v1_volume_mount_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


