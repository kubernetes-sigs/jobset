#!/usr/bin/env bash

# Copyright 2023 The Kubernetes Authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#     http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

if [ -z `which python3` ]; then
    apt update -y
    apt-get install python3.10 -y
    apt-get install python3-pip -y
    apt-get install python3-venv -y
fi

repo_root="$(dirname "${BASH_SOURCE}")/../.."

cd "${repo_root}/sdk/python"

# remove existing virtual env (for local testing)
rm -rf venv

# make new virtual env
python3 -m venv venv

# install test requirements
venv/bin/pip3 install -r test-requirements.txt

# run unit tests
venv/bin/python3 -m pytest test/