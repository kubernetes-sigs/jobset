#!/usr/bin/env bash
# Fetch a testgrid job's table and list tests that failed or flaked in recent
# runs, with a per-test breakdown of (status_value, count) runs so you can
# tell "failed once in 30 runs" (likely flake/infra blip) apart from
# "fails every run" (likely a real regression or broken job config).
#
# Status values (testgrid convention): 1=PASS, 0=NO_RESULT, 12=FAIL, 13=FLAKY
#
# Usage: ./failing-tests.sh <dashboard-name> <job-name> [out.json]
# Example: ./failing-tests.sh sig-apps-jobset-periodics periodic-jobset-test-e2e-main-1-36
set -euo pipefail

DASHBOARD="${1:?usage: failing-tests.sh <dashboard-name> <job-name> [out.json]}"
JOB="${2:?usage: failing-tests.sh <dashboard-name> <job-name> [out.json]}"
OUT="${3:-/tmp/${JOB}.json}"

curl -fsS --max-time 30 "https://testgrid.k8s.io/${DASHBOARD}/table?tab=${JOB}" -o "$OUT"

python3 -c '
import json
import sys

d = json.load(open(sys.argv[1]))
print("overall-status code:", d.get("overall-status"))
print("columns (most recent first):", d.get("column-header-names"))
print()
for t in d.get("tests", []):
    name = t["name"]
    statuses = t.get("statuses", [])
    if any(s.get("value") in (12, 13) for s in statuses):
        print(name)
        print("  run-length-encoded statuses (value, count):", [(s.get("value"), s.get("count")) for s in statuses])
' "$OUT"
echo
echo "Raw table saved to: $OUT"
