# coding: utf-8
# Copyright 2023 The Kubernetes Authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#    http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from setuptools import setup, find_packages  # noqa: H301



import setuptools

TESTS_REQUIRES = ["kubernetes", "pytest", "pytest-cov", "pytest-randomly==1.2.3"]

REQUIRES = [
"certifi >= 14.05.14",
"future; python_version<=\"2.7\"",
"six >= 1.10",
"python_dateutil >= 2.5.3",
"setuptools >= 21.0.0",
"urllib3 >= 1.15.1",
]

setuptools.setup(
    name="Jobset",
    version="0.1.4",
    author="Kubernetes Authors",
    author_email="",
    license="Apache License Version 2.0",
    url="https://github.com/kubernetes-sigs/jobset/sdk/python",
    description="JobSet CRD for multiple template jobs",
    long_description="JobSet CRD for multiple template jobs",
    zip_safe=False,
    packages=["jobset", "jobset.models", "jobset.api"],
    install_requires=REQUIRES,
    tests_require=TESTS_REQUIRES,
    extras_require={"test": TESTS_REQUIRES},
)