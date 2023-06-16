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

PYTHON_PATH=""

if [ -z `which python3` ]; then
    cd /tmp
    curl -O https://www.python.org/ftp/python/3.10.0/Python-3.10.0.tgz
    tar -xf Python-3.10.0.tgz

    cd Python-3.10.0
    ./configure --enable-optimizations
    make -j 4
    make altinstall

    PYTHON_PATH="/usr/local/bin/python3.10"
    echo "Python 3.10 is located at: ${PYTHON_PATH}"
    export PATH=/usr/local/bin:$PATH
else
    PYTHON_PATH=$(which python3.10)
fi

if [ -z `which pip` ]; then
    curl https://bootstrap.pypa.io/get-pip.py -o get-pip.py
    "${PYTHON_PATH}" get-pip.py
fi

repo_root="$(dirname "${BASH_SOURCE}")/../.."

cd "${repo_root}/sdk/python"

# install test requirements
"${PYTHON_PATH}" -m pip install -r test-requirements.txt

# run unit tests
"${PYTHON_PATH}" -m pytest test/
