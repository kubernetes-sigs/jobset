#!/usr/bin/env bash

# Copyright 2023 The Kubernetes Authors.
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

cd "$(dirname "${0}")"
GO_CMD=${1:-go}
CODEGEN_PKG=${2:-../bin}
REPO_ROOT="$(git rev-parse --show-toplevel)"

echo "GOPATH=$GOPATH"

bash "${CODEGEN_PKG}/generate-groups.sh" \
  "all" \
  sigs.k8s.io/jobset/client-go \
  sigs.k8s.io/jobset/api \
  jobset:v1alpha2 \
  --go-header-file ./boilerplate.go.txt

# if client-go files were generated outside of repo root, attempt to move them to the repo root.
if [ ! -d "$REPO_ROOT/client-go" ]; then

  echo "$REPO_ROOT/client-go does not exist."

  CLIENT_GO=$(find $GOPATH -regextype sed -regex ".*jobset.*client-go")
  if [ -z "$CLIENT_GO" ]; then
    echo "WARNING: generated client-go files were not found."
  else
    echo "moving generated files from $CLIENT_GO to $REPO_ROOT/client-go"
    mv $CLIENT_GO $REPO_ROOT
  fi

fi

