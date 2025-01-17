#!/usr/bin/env bash
#
# Adapted from original script: https://github.com/kubeflow/training-operator/blob/master/hack/python-sdk/gen-sdk.sh
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

set -o errexit
set -o nounset
set -o pipefail
set -x

repo_root="$(pwd)"

SWAGGER_CODEGEN_CONF="${repo_root}/hack/python-sdk/swagger_config.json"
SDK_OUTPUT_PATH="${repo_root}/sdk/python"
VERSION=0.1.4
SWAGGER_CODEGEN_FILE="${repo_root}/hack/python-sdk/swagger.json"

if [ -z "${GOPATH:-}" ]; then
  export GOPATH=$(go env GOPATH)
fi

echo "Generating OpenAPI specification ..."
echo "./hack/update-codegen.sh already help us generate openapi specs ..."

echo "Generating swagger file ..."
go run "${repo_root}"/hack/swagger/main.go ${VERSION} >"${SWAGGER_CODEGEN_FILE}"

echo "Removing previously generated files ..."
rm -rf "${SDK_OUTPUT_PATH}"/docs/V1*.md "${SDK_OUTPUT_PATH}"/jobset/models "${SDK_OUTPUT_PATH}"/test/test_*.py

echo "Generating Python SDK for JobSet..."

# Defaults the container engine to docker
CONTAINER_ENGINE=${CONTAINER_ENGINE:-docker}

# Install the sdk using docker, using the user that is running the container engine so that files can still be removed
${CONTAINER_ENGINE} run --user $(id -u):$(id -g) --rm \
  -v "${repo_root}":/local docker.io/openapitools/openapi-generator-cli generate \
  -i /local/hack/python-sdk/swagger.json \
  -g python \
  -o /local/sdk/python \
  -c local/hack/python-sdk/swagger_config.json

echo "Running post-generation script ..."
"${repo_root}"/hack/python-sdk/post_gen.py

echo "JobSet Python SDK is generated successfully to folder ${SDK_OUTPUT_PATH}/."

# Remove setup.py
rm "${SDK_OUTPUT_PATH}"/setup.py