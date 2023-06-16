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

# store initial working directory
initial_pwd=$(pwd)

repo_root="$(dirname "${BASH_SOURCE}")/../.."

SWAGGER_JAR_URL="https://repo1.maven.org/maven2/org/openapitools/openapi-generator-cli/4.3.1/openapi-generator-cli-4.3.1.jar"
SWAGGER_CODEGEN_JAR="${repo_root}/hack/python-sdk/openapi-generator-cli.jar"
SWAGGER_CODEGEN_CONF="${repo_root}/hack/python-sdk/swagger_config.json"
SDK_OUTPUT_PATH="${repo_root}/sdk/python"
VERSION=0.1.4
SWAGGER_CODEGEN_FILE="${repo_root}/hack/python-sdk/swagger.json"

if [ -z "${GOPATH:-}" ]; then
  export GOPATH=$(go env GOPATH)
fi

echo "Generating OpenAPI specification ..."
echo "./hack/update-codegen.sh already help us generate openapi specs ..."

if [[ ! -f "$SWAGGER_CODEGEN_JAR" ]]; then
  echo "Downloading the swagger-codegen JAR package ..."
  wget -O "${SWAGGER_CODEGEN_JAR}" ${SWAGGER_JAR_URL}
fi

if [ -z `which java` ]; then
  echo "Installing OpenJDK 11"
  pwd
  echo ${repo_root}

  # download OpenJDK 11 source code
  cd /tmp
  wget https://github.com/adoptium/temurin11-binaries/releases/download/jdk-11.0.19%2B7/OpenJDK11U-jdk_x64_linux_hotspot_11.0.19_7.tar.gz
  tar xvf OpenJDK11U-jdk_x64_linux_hotspot_11.0.19_7.tar.gz

  mkdir -p $HOME/jvm
  mv jdk-11.0.19+7 $HOME/jvm/

  # export JAVA_HOME and add /usr/lib/jvm/openjdk-11/bin to PATH
  export JAVA_HOME=$HOME/jvm/jdk-11.0.19+7
  export PATH=$PATH:$JAVA_HOME/bin
fi

echo "Generating swagger file ..."
cd ${initial_pwd}
go run "${repo_root}"/hack/swagger/main.go ${VERSION} >"${SWAGGER_CODEGEN_FILE}"

echo "Removing previously generated files ..."
rm -rf "${SDK_OUTPUT_PATH}"/docs/V1*.md "${SDK_OUTPUT_PATH}"/jobset/models "${SDK_OUTPUT_PATH}"/test/test_*.py

echo "Generating Python SDK for JobSet..."
java -jar "${SWAGGER_CODEGEN_JAR}" generate -i ${SWAGGER_CODEGEN_FILE} -g python -o "${SDK_OUTPUT_PATH}" -c "${SWAGGER_CODEGEN_CONF}"

echo "Running post-generation script ..."
"${repo_root}"/hack/python-sdk/post_gen.py

echo "JobSet Python SDK is generated successfully to folder ${SDK_OUTPUT_PATH}/."
