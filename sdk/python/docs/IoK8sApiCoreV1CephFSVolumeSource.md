# IoK8sApiCoreV1CephFSVolumeSource

Represents a Ceph Filesystem mount that lasts the lifetime of a pod Cephfs volumes do not support ownership management or SELinux relabeling.

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**monitors** | **List[str]** | monitors is Required: Monitors is a collection of Ceph monitors More info: https://examples.k8s.io/volumes/cephfs/README.md#how-to-use-it | 
**path** | **str** | path is Optional: Used as the mounted root, rather than the full Ceph tree, default is / | [optional] 
**read_only** | **bool** | readOnly is Optional: Defaults to false (read/write). ReadOnly here will force the ReadOnly setting in VolumeMounts. More info: https://examples.k8s.io/volumes/cephfs/README.md#how-to-use-it | [optional] 
**secret_file** | **str** | secretFile is Optional: SecretFile is the path to key ring for User, default is /etc/ceph/user.secret More info: https://examples.k8s.io/volumes/cephfs/README.md#how-to-use-it | [optional] 
**secret_ref** | [**IoK8sApiCoreV1LocalObjectReference**](IoK8sApiCoreV1LocalObjectReference.md) |  | [optional] 
**user** | **str** | user is optional: User is the rados user name, default is admin More info: https://examples.k8s.io/volumes/cephfs/README.md#how-to-use-it | [optional] 

## Example

```python
from jobset.models.io_k8s_api_core_v1_ceph_fs_volume_source import IoK8sApiCoreV1CephFSVolumeSource

# TODO update the JSON string below
json = "{}"
# create an instance of IoK8sApiCoreV1CephFSVolumeSource from a JSON string
io_k8s_api_core_v1_ceph_fs_volume_source_instance = IoK8sApiCoreV1CephFSVolumeSource.from_json(json)
# print the JSON string representation of the object
print(IoK8sApiCoreV1CephFSVolumeSource.to_json())

# convert the object into a dict
io_k8s_api_core_v1_ceph_fs_volume_source_dict = io_k8s_api_core_v1_ceph_fs_volume_source_instance.to_dict()
# create an instance of IoK8sApiCoreV1CephFSVolumeSource from a dict
io_k8s_api_core_v1_ceph_fs_volume_source_from_dict = IoK8sApiCoreV1CephFSVolumeSource.from_dict(io_k8s_api_core_v1_ceph_fs_volume_source_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


