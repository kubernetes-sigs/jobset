# coding: utf-8

"""
    JobSet SDK

    Python SDK for the JobSet API

    The version of the OpenAPI document: v0.1.4
    Generated by OpenAPI Generator (https://openapi-generator.tech)

    Do not edit the class manually.
"""  # noqa: E501


import unittest

from jobset.models.io_k8s_api_core_v1_image_volume_source import IoK8sApiCoreV1ImageVolumeSource

class TestIoK8sApiCoreV1ImageVolumeSource(unittest.TestCase):
    """IoK8sApiCoreV1ImageVolumeSource unit test stubs"""

    def setUp(self):
        pass

    def tearDown(self):
        pass

    def make_instance(self, include_optional) -> IoK8sApiCoreV1ImageVolumeSource:
        """Test IoK8sApiCoreV1ImageVolumeSource
            include_optional is a boolean, when False only required
            params are included, when True both required and
            optional params are included """
        # uncomment below to create an instance of `IoK8sApiCoreV1ImageVolumeSource`
        """
        model = IoK8sApiCoreV1ImageVolumeSource()
        if include_optional:
            return IoK8sApiCoreV1ImageVolumeSource(
                pull_policy = '',
                reference = ''
            )
        else:
            return IoK8sApiCoreV1ImageVolumeSource(
        )
        """

    def testIoK8sApiCoreV1ImageVolumeSource(self):
        """Test IoK8sApiCoreV1ImageVolumeSource"""
        # inst_req_only = self.make_instance(include_optional=False)
        # inst_req_and_optional = self.make_instance(include_optional=True)

if __name__ == '__main__':
    unittest.main()
