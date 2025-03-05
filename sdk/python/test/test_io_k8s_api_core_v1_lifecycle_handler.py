# coding: utf-8

"""
    JobSet SDK

    Python SDK for the JobSet API

    The version of the OpenAPI document: v0.1.4
    Generated by OpenAPI Generator (https://openapi-generator.tech)

    Do not edit the class manually.
"""  # noqa: E501


import unittest

from jobset.models.io_k8s_api_core_v1_lifecycle_handler import IoK8sApiCoreV1LifecycleHandler

class TestIoK8sApiCoreV1LifecycleHandler(unittest.TestCase):
    """IoK8sApiCoreV1LifecycleHandler unit test stubs"""

    def setUp(self):
        pass

    def tearDown(self):
        pass

    def make_instance(self, include_optional) -> IoK8sApiCoreV1LifecycleHandler:
        """Test IoK8sApiCoreV1LifecycleHandler
            include_optional is a boolean, when False only required
            params are included, when True both required and
            optional params are included """
        # uncomment below to create an instance of `IoK8sApiCoreV1LifecycleHandler`
        """
        model = IoK8sApiCoreV1LifecycleHandler()
        if include_optional:
            return IoK8sApiCoreV1LifecycleHandler(
                var_exec = jobset.models.io/k8s/api/core/v1/exec_action.io.k8s.api.core.v1.ExecAction(
                    command = [
                        ''
                        ], ),
                http_get = jobset.models.io/k8s/api/core/v1/http_get_action.io.k8s.api.core.v1.HTTPGetAction(
                    host = '', 
                    http_headers = [
                        jobset.models.io/k8s/api/core/v1/http_header.io.k8s.api.core.v1.HTTPHeader(
                            name = '', 
                            value = '', )
                        ], 
                    path = '', 
                    port = '', 
                    scheme = '', ),
                sleep = jobset.models.io/k8s/api/core/v1/sleep_action.io.k8s.api.core.v1.SleepAction(
                    seconds = 56, ),
                tcp_socket = jobset.models.io/k8s/api/core/v1/tcp_socket_action.io.k8s.api.core.v1.TCPSocketAction(
                    host = '', 
                    port = '', )
            )
        else:
            return IoK8sApiCoreV1LifecycleHandler(
        )
        """

    def testIoK8sApiCoreV1LifecycleHandler(self):
        """Test IoK8sApiCoreV1LifecycleHandler"""
        # inst_req_only = self.make_instance(include_optional=False)
        # inst_req_and_optional = self.make_instance(include_optional=True)

if __name__ == '__main__':
    unittest.main()
