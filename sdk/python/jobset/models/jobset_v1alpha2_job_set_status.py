# coding: utf-8

"""
    JobSet SDK

    Python SDK for the JobSet API

    The version of the OpenAPI document: v0.1.4
    Generated by OpenAPI Generator (https://openapi-generator.tech)

    Do not edit the class manually.
"""  # noqa: E501


from __future__ import annotations
import pprint
import re  # noqa: F401
import json

from pydantic import BaseModel, ConfigDict, Field, StrictInt, StrictStr
from typing import Any, ClassVar, Dict, List, Optional
from jobset.models.io_k8s_apimachinery_pkg_apis_meta_v1_condition import IoK8sApimachineryPkgApisMetaV1Condition
from jobset.models.jobset_v1alpha2_replicated_job_status import JobsetV1alpha2ReplicatedJobStatus
from typing import Optional, Set
from typing_extensions import Self

class JobsetV1alpha2JobSetStatus(BaseModel):
    """
    JobSetStatus defines the observed state of JobSet
    """ # noqa: E501
    conditions: Optional[List[IoK8sApimachineryPkgApisMetaV1Condition]] = None
    individual_job_recreates: Optional[Dict[str, StrictInt]] = Field(default=None, description="IndividualJobRecreates tracks the number of times an individual Job within the JobSet has been recreated (i.e. in case of RecreateJob failure policy).", alias="individualJobRecreates")
    replicated_jobs_status: Optional[List[JobsetV1alpha2ReplicatedJobStatus]] = Field(default=None, description="ReplicatedJobsStatus track the number of JobsReady for each replicatedJob.", alias="replicatedJobsStatus")
    restarts: Optional[StrictInt] = Field(default=0, description="Restarts tracks the number of times the JobSet has restarted (i.e. recreated in case of RecreateAll policy).")
    restarts_count_towards_max: Optional[StrictInt] = Field(default=None, description="RestartsCountTowardsMax tracks the number of times the JobSet has restarted that counts towards the maximum allowed number of restarts.", alias="restartsCountTowardsMax")
    terminal_state: Optional[StrictStr] = Field(default=None, description="TerminalState the state of the JobSet when it finishes execution. It can be either Completed or Failed. Otherwise, it is empty by default.", alias="terminalState")
    __properties: ClassVar[List[str]] = ["conditions", "individualJobRecreates", "replicatedJobsStatus", "restarts", "restartsCountTowardsMax", "terminalState"]

    model_config = ConfigDict(
        populate_by_name=True,
        validate_assignment=True,
        protected_namespaces=(),
    )


    def to_str(self) -> str:
        """Returns the string representation of the model using alias"""
        return pprint.pformat(self.model_dump(by_alias=True))

    def to_json(self) -> str:
        """Returns the JSON representation of the model using alias"""
        # TODO: pydantic v2: use .model_dump_json(by_alias=True, exclude_unset=True) instead
        return json.dumps(self.to_dict())

    @classmethod
    def from_json(cls, json_str: str) -> Optional[Self]:
        """Create an instance of JobsetV1alpha2JobSetStatus from a JSON string"""
        return cls.from_dict(json.loads(json_str))

    def to_dict(self) -> Dict[str, Any]:
        """Return the dictionary representation of the model using alias.

        This has the following differences from calling pydantic's
        `self.model_dump(by_alias=True)`:

        * `None` is only added to the output dict for nullable fields that
          were set at model initialization. Other fields with value `None`
          are ignored.
        """
        excluded_fields: Set[str] = set([
        ])

        _dict = self.model_dump(
            by_alias=True,
            exclude=excluded_fields,
            exclude_none=True,
        )
        # override the default output from pydantic by calling `to_dict()` of each item in conditions (list)
        _items = []
        if self.conditions:
            for _item_conditions in self.conditions:
                if _item_conditions:
                    _items.append(_item_conditions.to_dict())
            _dict['conditions'] = _items
        # override the default output from pydantic by calling `to_dict()` of each item in replicated_jobs_status (list)
        _items = []
        if self.replicated_jobs_status:
            for _item_replicated_jobs_status in self.replicated_jobs_status:
                if _item_replicated_jobs_status:
                    _items.append(_item_replicated_jobs_status.to_dict())
            _dict['replicatedJobsStatus'] = _items
        return _dict

    @classmethod
    def from_dict(cls, obj: Optional[Dict[str, Any]]) -> Optional[Self]:
        """Create an instance of JobsetV1alpha2JobSetStatus from a dict"""
        if obj is None:
            return None

        if not isinstance(obj, dict):
            return cls.model_validate(obj)

        _obj = cls.model_validate({
            "conditions": [IoK8sApimachineryPkgApisMetaV1Condition.from_dict(_item) for _item in obj["conditions"]] if obj.get("conditions") is not None else None,
            "individualJobRecreates": obj.get("individualJobRecreates"),
            "replicatedJobsStatus": [JobsetV1alpha2ReplicatedJobStatus.from_dict(_item) for _item in obj["replicatedJobsStatus"]] if obj.get("replicatedJobsStatus") is not None else None,
            "restarts": obj.get("restarts") if obj.get("restarts") is not None else 0,
            "restartsCountTowardsMax": obj.get("restartsCountTowardsMax"),
            "terminalState": obj.get("terminalState")
        })
        return _obj


