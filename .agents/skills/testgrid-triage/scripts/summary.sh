#!/usr/bin/env bash
# Fetch and print a compact per-job status summary for a testgrid dashboard.
#
# Usage: ./summary.sh <dashboard-name>
# Example: ./summary.sh sig-apps-jobset-periodics
set -euo pipefail

DASHBOARD="${1:?usage: summary.sh <dashboard-name>}"
URL="https://testgrid.k8s.io/${DASHBOARD}/summary"

curl -fsS --max-time 20 "$URL" | python3 -c '
import json, sys
d = json.load(sys.stdin)
rows = []
for k, v in d.items():
    rows.append((v.get("overall_status", "?"), k, v.get("status", "")))
# sort worst-first: FAILING, FLAKY, then PASSING
order = {"FAILING": 0, "FLAKY": 1, "PASSING": 2}
rows.sort(key=lambda r: (order.get(r[0], 1), r[1]))
for status, name, detail in rows:
    print(f"{status:10s} {name:55s} {detail}")
'
