# coding: utf-8

"""
    JobSet SDK

    Python SDK for the JobSet API

    The version of the OpenAPI document: v0.1.4
    Generated by OpenAPI Generator (https://openapi-generator.tech)

    Do not edit the class manually.
"""  # noqa: E501


import unittest

from jobset.models.io_k8s_api_core_v1_persistent_volume_claim_template import IoK8sApiCoreV1PersistentVolumeClaimTemplate

class TestIoK8sApiCoreV1PersistentVolumeClaimTemplate(unittest.TestCase):
    """IoK8sApiCoreV1PersistentVolumeClaimTemplate unit test stubs"""

    def setUp(self):
        pass

    def tearDown(self):
        pass

    def make_instance(self, include_optional) -> IoK8sApiCoreV1PersistentVolumeClaimTemplate:
        """Test IoK8sApiCoreV1PersistentVolumeClaimTemplate
            include_optional is a boolean, when False only required
            params are included, when True both required and
            optional params are included """
        # uncomment below to create an instance of `IoK8sApiCoreV1PersistentVolumeClaimTemplate`
        """
        model = IoK8sApiCoreV1PersistentVolumeClaimTemplate()
        if include_optional:
            return IoK8sApiCoreV1PersistentVolumeClaimTemplate(
                metadata = jobset.models.io/k8s/apimachinery/pkg/apis/meta/v1/object_meta.io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta(
                    annotations = {
                        'key' : ''
                        }, 
                    creation_timestamp = datetime.datetime.strptime('2013-10-20 19:20:30.00', '%Y-%m-%d %H:%M:%S.%f'), 
                    deletion_grace_period_seconds = 56, 
                    deletion_timestamp = datetime.datetime.strptime('2013-10-20 19:20:30.00', '%Y-%m-%d %H:%M:%S.%f'), 
                    finalizers = [
                        ''
                        ], 
                    generate_name = '', 
                    generation = 56, 
                    labels = {
                        'key' : ''
                        }, 
                    managed_fields = [
                        jobset.models.io/k8s/apimachinery/pkg/apis/meta/v1/managed_fields_entry.io.k8s.apimachinery.pkg.apis.meta.v1.ManagedFieldsEntry(
                            api_version = '', 
                            fields_type = '', 
                            fields_v1 = jobset.models.io/k8s/apimachinery/pkg/apis/meta/v1/fields_v1.io.k8s.apimachinery.pkg.apis.meta.v1.FieldsV1(), 
                            manager = '', 
                            operation = '', 
                            subresource = '', 
                            time = datetime.datetime.strptime('2013-10-20 19:20:30.00', '%Y-%m-%d %H:%M:%S.%f'), )
                        ], 
                    name = '', 
                    namespace = '', 
                    owner_references = [
                        jobset.models.io/k8s/apimachinery/pkg/apis/meta/v1/owner_reference.io.k8s.apimachinery.pkg.apis.meta.v1.OwnerReference(
                            api_version = '', 
                            block_owner_deletion = True, 
                            controller = True, 
                            kind = '', 
                            name = '', 
                            uid = '', )
                        ], 
                    resource_version = '', 
                    self_link = '', 
                    uid = '', ),
                spec = jobset.models.io/k8s/api/core/v1/persistent_volume_claim_spec.io.k8s.api.core.v1.PersistentVolumeClaimSpec(
                    access_modes = [
                        ''
                        ], 
                    data_source = jobset.models.io/k8s/api/core/v1/typed_local_object_reference.io.k8s.api.core.v1.TypedLocalObjectReference(
                        api_group = '', 
                        kind = '', 
                        name = '', ), 
                    data_source_ref = jobset.models.io/k8s/api/core/v1/typed_object_reference.io.k8s.api.core.v1.TypedObjectReference(
                        api_group = '', 
                        kind = '', 
                        name = '', 
                        namespace = '', ), 
                    resources = jobset.models.io/k8s/api/core/v1/volume_resource_requirements.io.k8s.api.core.v1.VolumeResourceRequirements(
                        limits = {
                            'key' : ''
                            }, 
                        requests = {
                            'key' : ''
                            }, ), 
                    selector = jobset.models.io/k8s/apimachinery/pkg/apis/meta/v1/label_selector.io.k8s.apimachinery.pkg.apis.meta.v1.LabelSelector(
                        match_expressions = [
                            jobset.models.io/k8s/apimachinery/pkg/apis/meta/v1/label_selector_requirement.io.k8s.apimachinery.pkg.apis.meta.v1.LabelSelectorRequirement(
                                key = '', 
                                operator = '', 
                                values = [
                                    ''
                                    ], )
                            ], 
                        match_labels = {
                            'key' : ''
                            }, ), 
                    storage_class_name = '', 
                    volume_attributes_class_name = '', 
                    volume_mode = '', 
                    volume_name = '', )
            )
        else:
            return IoK8sApiCoreV1PersistentVolumeClaimTemplate(
                spec = jobset.models.io/k8s/api/core/v1/persistent_volume_claim_spec.io.k8s.api.core.v1.PersistentVolumeClaimSpec(
                    access_modes = [
                        ''
                        ], 
                    data_source = jobset.models.io/k8s/api/core/v1/typed_local_object_reference.io.k8s.api.core.v1.TypedLocalObjectReference(
                        api_group = '', 
                        kind = '', 
                        name = '', ), 
                    data_source_ref = jobset.models.io/k8s/api/core/v1/typed_object_reference.io.k8s.api.core.v1.TypedObjectReference(
                        api_group = '', 
                        kind = '', 
                        name = '', 
                        namespace = '', ), 
                    resources = jobset.models.io/k8s/api/core/v1/volume_resource_requirements.io.k8s.api.core.v1.VolumeResourceRequirements(
                        limits = {
                            'key' : ''
                            }, 
                        requests = {
                            'key' : ''
                            }, ), 
                    selector = jobset.models.io/k8s/apimachinery/pkg/apis/meta/v1/label_selector.io.k8s.apimachinery.pkg.apis.meta.v1.LabelSelector(
                        match_expressions = [
                            jobset.models.io/k8s/apimachinery/pkg/apis/meta/v1/label_selector_requirement.io.k8s.apimachinery.pkg.apis.meta.v1.LabelSelectorRequirement(
                                key = '', 
                                operator = '', 
                                values = [
                                    ''
                                    ], )
                            ], 
                        match_labels = {
                            'key' : ''
                            }, ), 
                    storage_class_name = '', 
                    volume_attributes_class_name = '', 
                    volume_mode = '', 
                    volume_name = '', ),
        )
        """

    def testIoK8sApiCoreV1PersistentVolumeClaimTemplate(self):
        """Test IoK8sApiCoreV1PersistentVolumeClaimTemplate"""
        # inst_req_only = self.make_instance(include_optional=False)
        # inst_req_and_optional = self.make_instance(include_optional=True)

if __name__ == '__main__':
    unittest.main()
