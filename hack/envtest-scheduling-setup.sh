#!/usr/bin/env bash
# Prepares KUBEBUILDER_ASSETS for running the WAS (Workload-Aware Scheduling)
# integration test suite (test/integration/scheduling/...).
#
# That suite relies on the scheduling.k8s.io/v1alpha3 GenericWorkload API,
# which has not shipped in any released Kubernetes minor version yet.
# `setup-envtest` only publishes etcd/kube-apiserver/kubectl binaries built
# from released Kubernetes minor versions, so its kube-apiserver does not
# know about this API group and fails to start when it is enabled via
# --runtime-config.
#
# To work around this, we:
#   1. Use `setup-envtest` to fetch a standard etcd/kubectl (these don't
#      need to know about the new API).
#   2. Download a kube-apiserver binary built from the exact Kubernetes
#      pre-release tag that matches our vendored k8s.io/api version (see
#      go.mod). The scheduling.k8s.io/v1alpha3 API is still actively
#      evolving upstream, so using this exact tag -- rather than the latest
#      Kubernetes `main` CI build -- keeps the on-the-wire API shape in sync
#      with the Go types our controller and tests are compiled against.
#      Using a newer/older build can silently corrupt requests (mismatched
#      field names get serialized as protobuf and decoded as garbage on the
#      other end) instead of failing loudly.
#   3. Assemble both into a single directory and print its path so it can be
#      used as KUBEBUILDER_ASSETS.
#
# Usage:
#   envtest-scheduling-setup.sh <setup-envtest-bin> <envtest-k8s-version> <bin-dir>
set -o errexit
set -o nounset
set -o pipefail

ENVTEST="${1:?usage: envtest-scheduling-setup.sh <setup-envtest-bin> <envtest-k8s-version> <bin-dir>}"
ENVTEST_K8S_VERSION="${2:?usage: envtest-scheduling-setup.sh <setup-envtest-bin> <envtest-k8s-version> <bin-dir>}"
BIN_DIR="${3:?usage: envtest-scheduling-setup.sh <setup-envtest-bin> <envtest-k8s-version> <bin-dir>}"

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

ARCH="$(go env GOARCH)"
OS="$(go env GOOS)"

# Step 1: standard envtest assets (etcd, kubectl).
base_assets_dir="$("$ENVTEST" use "$ENVTEST_K8S_VERSION" --bin-dir "$BIN_DIR" -p path)"

# Step 2: derive the Kubernetes release tag matching our vendored
# k8s.io/api version. k8s.io/api follows Kubernetes' own versioning scheme
# (k8s.io/api vX.Y.Z <-> Kubernetes v(X+1).Y.Z, including pre-release
# suffixes), so k8s.io/api v0.37.0-alpha.2 corresponds to Kubernetes
# v1.37.0-alpha.2.
# Read the required version rather than the replaced module version so
# pseudo-versions cannot produce an invalid Kubernetes download tag.
k8s_api_version="$(awk '$1 == "k8s.io/api" { print $2; exit }' "$PROJECT_DIR/go.mod")"
k8s_version="${k8s_api_version/#v0./v1.}"
sanitized_version="${k8s_version//+/-}"
scheduling_assets_dir="${BIN_DIR}/k8s-was/${sanitized_version}-${OS}-${ARCH}"

mkdir -p "$scheduling_assets_dir"

# Reuse the etcd/kubectl binaries from the standard envtest assets; only
# kube-apiserver needs to come from the matching Kubernetes pre-release.
cp -f "${base_assets_dir}/etcd" "${scheduling_assets_dir}/etcd"
cp -f "${base_assets_dir}/kubectl" "${scheduling_assets_dir}/kubectl"

if [[ ! -x "${scheduling_assets_dir}/kube-apiserver" ]]; then
    echo "==> Downloading kube-apiserver ${k8s_version} (matches k8s.io/api ${k8s_api_version}, ${OS}/${ARCH})..." >&2
    curl -sL -o "${scheduling_assets_dir}/kube-apiserver" \
        "https://dl.k8s.io/release/${k8s_version}/bin/${OS}/${ARCH}/kube-apiserver"
    chmod +x "${scheduling_assets_dir}/kube-apiserver"
fi

echo "$scheduling_assets_dir"
