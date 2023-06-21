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

# We always need to install python3-venv, so makes sense to update once
apt update -y

if [ -z `which python3` ]; then
    # Default python for bookworm (golang 1.19/1.20) is 3.11
    # so this should already be installed
    apt-get install python3 -y
fi

if [ -z `which pip` ]; then
    apt-get install python3-pip -y
fi

repo_root="$(dirname "${BASH_SOURCE}")/../.."

cd "${repo_root}/sdk/python"

# We need to create a virtual environment for testing
apt-get install -y python3-venv
python3 -m venv env
source env/bin/activate

# install test requirements
pip install -r test-requirements.txt

# run unit tests
pytest test/
