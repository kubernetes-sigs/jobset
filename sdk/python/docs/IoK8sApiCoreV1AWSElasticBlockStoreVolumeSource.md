# IoK8sApiCoreV1AWSElasticBlockStoreVolumeSource

Represents a Persistent Disk resource in AWS.  An AWS EBS disk must exist before mounting to a container. The disk must also be in the same AWS zone as the kubelet. An AWS EBS disk can only be mounted as read/write once. AWS EBS volumes support ownership management and SELinux relabeling.

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**fs_type** | **str** | fsType is the filesystem type of the volume that you want to mount. Tip: Ensure that the filesystem type is supported by the host operating system. Examples: \&quot;ext4\&quot;, \&quot;xfs\&quot;, \&quot;ntfs\&quot;. Implicitly inferred to be \&quot;ext4\&quot; if unspecified. More info: https://kubernetes.io/docs/concepts/storage/volumes#awselasticblockstore | [optional] 
**partition** | **int** | partition is the partition in the volume that you want to mount. If omitted, the default is to mount by volume name. Examples: For volume /dev/sda1, you specify the partition as \&quot;1\&quot;. Similarly, the volume partition for /dev/sda is \&quot;0\&quot; (or you can leave the property empty). | [optional] 
**read_only** | **bool** | readOnly value true will force the readOnly setting in VolumeMounts. More info: https://kubernetes.io/docs/concepts/storage/volumes#awselasticblockstore | [optional] 
**volume_id** | **str** | volumeID is unique ID of the persistent disk resource in AWS (Amazon EBS volume). More info: https://kubernetes.io/docs/concepts/storage/volumes#awselasticblockstore | 

## Example

```python
from jobset.models.io_k8s_api_core_v1_aws_elastic_block_store_volume_source import IoK8sApiCoreV1AWSElasticBlockStoreVolumeSource

# TODO update the JSON string below
json = "{}"
# create an instance of IoK8sApiCoreV1AWSElasticBlockStoreVolumeSource from a JSON string
io_k8s_api_core_v1_aws_elastic_block_store_volume_source_instance = IoK8sApiCoreV1AWSElasticBlockStoreVolumeSource.from_json(json)
# print the JSON string representation of the object
print(IoK8sApiCoreV1AWSElasticBlockStoreVolumeSource.to_json())

# convert the object into a dict
io_k8s_api_core_v1_aws_elastic_block_store_volume_source_dict = io_k8s_api_core_v1_aws_elastic_block_store_volume_source_instance.to_dict()
# create an instance of IoK8sApiCoreV1AWSElasticBlockStoreVolumeSource from a dict
io_k8s_api_core_v1_aws_elastic_block_store_volume_source_from_dict = IoK8sApiCoreV1AWSElasticBlockStoreVolumeSource.from_dict(io_k8s_api_core_v1_aws_elastic_block_store_volume_source_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


