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

GO_CMD=${1:-go}
JOBSET_ROOT=$(realpath $(dirname ${BASH_SOURCE[0]})/..)
CODEGEN_PKG=$($GO_CMD list -m -f "{{.Dir}}" k8s.io/code-generator)

cd $(dirname ${BASH_SOURCE[0]})/..

source "${CODEGEN_PKG}/kube_codegen.sh"

echo $JOBSET_ROOT
# TODO: remove the workaround when the issue is solved in the code-generator
# (https://github.com/kubernetes/code-generator/issues/165).
# Here, we create the soft link named "sigs.k8s.io" to the parent directory of
# Jobset to ensure the layout required by the kube_codegen.sh script.
ln -s .. sigs.k8s.io
trap "rm sigs.k8s.io" EXIT

echo "gen_helpers"
kube::codegen::gen_helpers \
    --input-pkg-root sigs.k8s.io/jobset/api \
    --output-base "${JOBSET_ROOT}" \
    --boilerplate "${JOBSET_ROOT}/hack/boilerplate.go.txt"


echo "gen_openapi"
kube::codegen::gen_openapi \
  --input-pkg-root sigs.k8s.io/jobset/api/jobset \
  --output-pkg-root sigs.k8s.io/jobset/api/jobset/v1alpha1 \
  --output-base "${JOBSET_ROOT}" \
  --update-report \
  --boilerplate "${JOBSET_ROOT}/hack/boilerplate.go.txt"

echo "gen_client"
kube::codegen::gen_client \
    --with-watch \
    --with-applyconfig \
    --input-pkg-root sigs.k8s.io/jobset/api \
    --output-base "$JOBSET_ROOT" \
    --output-pkg-root sigs.k8s.io/jobset/client-go \
    --boilerplate "${JOBSET_ROOT}/hack/boilerplate.go.txt"

echo "done"