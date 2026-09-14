---
name: testgrid-triage
description: Triages failing/flaky CI jobs on JobSet's testgrid dashboards (e.g. sig-apps-jobset-periodics, sig-apps-jobset-presubmits). Summarizes job health, drills into which specific tests are failing vs flaking, fetches raw Prow/GCS build logs to find true root cause, and files well-evidenced GitHub issues with the required AI-usage disclosure. Use whenever asked to check testgrid, summarize CI failures, investigate a flaky/red job, or file a bug report for a failing test.
---

# Testgrid Triage

Streamlines the workflow of: testgrid summary → drill into a job → find the
run-length-encoded pass/fail pattern per test → pull the raw build log for
root cause → (optionally) file a GitHub issue.

Requires: `curl`, `python3`, `jq` (optional), `gsutil` (for raw logs),
`gh` (authenticated, for filing issues — check with `gh auth status`).

## Step 1 — Get overall dashboard health

```bash
./scripts/summary.sh sig-apps-jobset-periodics
```

Prints every job/tab sorted worst-first (FAILING, then FLAKY, then PASSING)
with its recent pass-rate string. Use this to decide which job(s) deserve a
closer look. Common dashboards for this repo: `sig-apps-jobset-periodics`,
`sig-apps-jobset-presubmits` (check testgrid.k8s.io for exact names).

## Step 2 — Drill into a specific job

```bash
./scripts/failing-tests.sh sig-apps-jobset-periodics periodic-jobset-test-e2e-release-0-12-1-35
```

Saves the full table JSON to `/tmp/<job>.json` and prints, for every test
that failed or flaked recently, a run-length-encoded list of
`(status_value, count)` pairs (most recent run first). Status values:
`1`=PASS, `0`=NO_RESULT, `12`=FAIL, `13`=FLAKY.

Use this to distinguish patterns:
- **A cluster of unrelated tests fails together in the same 1-2 runs**
  (e.g. `(12,1)` sprinkled among long `(1,N)` runs) → likely a transient
  infra blip (DNS hiccup, resource contention), not a regression.
- **`overall-status` FAILING with 0% pass and no individual `[It]` test
  rows at all** (only `Overall`/`Pod`/`[BeforeSuite]` show up) → the job is
  failing before any test runs — a setup/infra/config problem, not a test
  bug. Go straight to Step 3 to find why setup fails.
- **One specific test fails a small, non-trivial fraction of runs while
  everything else is 100% green** → likely a real flaky test worth its own
  narrower investigation/issue.

## Step 3 — Get the real error from raw build logs

Testgrid's grid only tells you pass/fail, not *why*. Always confirm root
cause from the actual Prow log before concluding anything or filing an
issue:

```bash
./scripts/fetch-build-log.sh <job-name> list        # list recent build IDs
./scripts/fetch-build-log.sh <job-name>             # cat latest build's build-log.txt
./scripts/fetch-build-log.sh <job-name> <build-id>  # cat a specific build
```

Grep the output for the actual failure, e.g.:

```bash
./scripts/fetch-build-log.sh periodic-jobset-test-e2e-release-0-11-1-36 \
  | grep -E -B2 -A15 -i "error|fail|panic"
```

Compare the same failure across 2+ builds/jobs when possible — if the
identical error appears in every failing run of multiple jobs, that's
strong evidence of a systemic cause (e.g. a pinned dependency/image tag
that no longer resolves) rather than a one-off flake.

## Step 4 — File an issue (only after root cause is confirmed)

Follow [references/issue-template.md](references/issue-template.md) for
the required structure and the mandatory AI-disclosure note (per this
repo's `AGENTS.md` / the Kubernetes AI Tool Usage Policy). Then:

```bash
gh issue create --repo kubernetes-sigs/jobset \
  --title "<concise, specific title>" \
  --body-file /tmp/issue_body.md
```

Do **not** file an issue for a single-run transient blip — only for
patterns that are reproducible/consistent (e.g. 100% failure over the
visible history, or a clearly identified systemic root cause).

## Notes

- These scripts only read public testgrid/GCS data and don't need
  authentication, except `gh issue create` which needs `gh auth status` to
  show a logged-in account with `repo` scope.
- Prow job configs (`E2E_KIND_VERSION`, cluster version matrices, etc.) for
  periodics/presubmits typically live outside this repo (in
  `kubernetes/test-infra` or `kubernetes-sigs/jobset`'s own
  `.prow.yaml`/`config/jobs`, if present) — check both when a fix requires
  a job-config change rather than a source-code change.
