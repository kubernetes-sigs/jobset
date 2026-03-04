#!/usr/bin/env bash

# Copyright The Kubernetes Authors.
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

repo_root="$(dirname "${BASH_SOURCE}")/../.."

cd "${repo_root}/sdk/python"

# Allow one to use podman or docker for local testing.
CONTAINER_ENGINE=${CONTAINER_ENGINE:-docker}
## For CI we found that docker wasn't started.
## Should be a no-op if docker is up

## If non ubuntu machine, install docker in your path
${CONTAINER_ENGINE} buildx build -f Dockerfile -t python-unit .
${CONTAINER_ENGINE} run python-unit pytest test
