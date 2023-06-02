#!/usr/bin/env python3
#
# Adapted from original script: https://github.com/kubeflow/training-operator/blob/4043955725ba2a6a777b65b1aee2672ebdc3597d/hack/python-sdk/post_gen.py
#
# Copyright 2021 The Kubeflow Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""
This script is used for updating generated SDK files.
"""

import os
import fileinput
import re
from pathlib import Path


__import_replacements = [
    ("jobset\.models\.jobset\/v1alpha2\/", "jobset.models.jobset_v1alpha2_"),
    (".jobset\.v1alpha2\.", ".JobsetV1alpha2")
]

__requirement_replacements = [
    ("pytest[~=<>].*", "pytest>=6.2.5") # https://github.com/pytest-dev/pytest/discussions/9195
]

sdk_dir = os.path.abspath(os.path.join(__file__, "../../..", "sdk/python"))

def main():
    fix_test_files()
    fix_requirements()
    add_imports()

def fix_test_files() -> None:
    """
    Fix invalid model imports in generated model tests.
    """
    test_folder_dir = os.path.join(sdk_dir, "test")
    test_files = [f for f in os.listdir(test_folder_dir) if f.endswith('.py')]
    for test_file in test_files:
        print(f"Precessing file {test_file}")
        if test_file.endswith(".py"):
            with fileinput.FileInput(
                os.path.join(test_folder_dir, test_file), inplace=True
            ) as file:
                for line in file:
                    print(_apply_regex(__import_replacements, line), end="")


    # Add batchv1.JobTemplateSpec to test file for replicatedJob
    for test_file in test_files:
        with open(os.path.join(test_folder_dir, test_file), "r") as fp:
            new_lines = []
            for line in fp.readlines():
                if "template = None" in line:
                    fixed = re.sub(r'\btemplate = None', "template = V1JobTemplateSpec()", line)
                    new_lines.append(fixed)
                    continue
                new_lines.append(line)
                if line.startswith("from __future__ import absolute_import"):
                    new_lines.append("\n# Kubernetes imports")
                    new_lines.append("\nfrom kubernetes.client.models.v1_job_template_spec import V1JobTemplateSpec")
        with open(os.path.join(test_folder_dir, test_file), "w") as f:
            f.writelines(new_lines) 

def fix_requirements() -> None:
    """
    Fix invalid packages and add required packages 
    in requirements.txt and test-requirements.txt
    """

    test_reqs = os.path.join(sdk_dir, "test-requirements.txt")
    with open(test_reqs, 'r') as f:
        lines = f.readlines()

    # add kubernetes client library to imports
    lines = ["kubernetes\n"] + lines
    new_lines = []
    for line in lines:
        new_lines.append(_apply_regex(__requirement_replacements, line))
    with open(test_reqs, 'w') as f:
        f.writelines(new_lines)

def add_imports() -> None:
    """
    Add necessary missing imports.
    """
    # Add Kubernetes models for proper deserialization of JobSet models.
    with open(os.path.join(sdk_dir, "jobset/models/__init__.py"), "r") as f:
        new_lines = []
        for line in f.readlines():
            new_lines.append(line)
            if line.startswith("from __future__ import absolute_import"):
                new_lines.append("\n")
                new_lines.append("# Import Kubernetes models.\n")
                new_lines.append("from kubernetes.client import *\n")
    with open(os.path.join(sdk_dir, "jobset/models/__init__.py"), "w") as f:
        f.writelines(new_lines)


def _apply_regex(replacements: list[tuple[str, str]], input_str: str) -> str:
    for pattern, replacement in replacements:
        input_str = re.sub(pattern, replacement, input_str)
    return input_str

if __name__ == "__main__":
    main()