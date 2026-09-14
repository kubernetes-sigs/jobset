#!/usr/bin/env bash
# Fetch the raw Prow build-log.txt for a periodic/presubmit job from the
# kubernetes-ci-logs GCS bucket, so you can see the actual error instead of
# just testgrid's pass/fail grid.
#
# Usage:
#   ./fetch-build-log.sh <job-name>              # latest build
#   ./fetch-build-log.sh <job-name> <build-id>   # specific build
#   ./fetch-build-log.sh <job-name> list         # list recent build IDs
#
# Example:
#   ./fetch-build-log.sh periodic-jobset-test-e2e-release-0-11-1-36
set -euo pipefail

JOB="${1:?usage: fetch-build-log.sh <job-name> [build-id|list]}"
BUILD="${2:-latest}"
BASE="gs://kubernetes-ci-logs/logs/${JOB}"

if [[ "$BUILD" == "list" ]]; then
    gsutil ls "$BASE/" | sed "s#$BASE/##;s#/##" | sort -n
    exit 0
fi

if [[ "$BUILD" == "latest" ]]; then
    BUILD=$(gsutil ls "$BASE/" | sed "s#$BASE/##;s#/##" | sort -n | tail -1)
    echo "# Using latest build: $BUILD" >&2
fi

echo "# Job:   $JOB" >&2
echo "# Build: $BUILD" >&2
echo "# Prow link: https://prow.k8s.io/view/gs/kubernetes-ci-logs/logs/${JOB}/${BUILD}" >&2
echo "# ---" >&2

gsutil cat "${BASE}/${BUILD}/build-log.txt"
