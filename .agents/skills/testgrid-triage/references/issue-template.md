# CI failure issue template

Use this structure when filing a GitHub issue for a genuine CI/testgrid
failure (JobSet follows the Kubernetes AI Tool Usage Policy — see
`AGENTS.md` at the repo root).

```markdown
## What happened

<Which job(s)/tab(s) on which testgrid dashboard, current pass rate over
recent runs, and whether individual tests fail or the whole job fails
before tests even run (setup/infra failure).>

## Root cause

<Paste the relevant excerpt from the Prow build-log.txt / junit output that
pinpoints the actual error. Explain what it means.>

## Evidence / reproduction

<List specific build IDs / Prow links used as evidence, and note whether
the pattern is consistent across the recent history shown on testgrid.>

## Expected behavior

<What should happen instead.>

## Suggested fix

<Concrete, minimal suggestion — e.g. bump a pinned image tag, fix a flaky
wait condition, add a retry, etc. Point at the exact file/config if it
lives in this repo; note if it's owned by kubernetes/test-infra instead.>

#### Special notes for your reviewer:

This investigation (pulling and analyzing testgrid data and CI logs, and
drafting this issue) was assisted by an AI coding agent, per the
[Kubernetes AI Tool Usage Policy](https://www.kubernetes.dev/docs/guide/pull-requests/#ai-guidance).
All findings were reviewed before filing.
```

## Rules (from AGENTS.md / Kubernetes AI policy)

- Always disclose AI assistance in the issue body (the "Special notes for
  your reviewer" section above is the minimum bar).
- Never add AI co-author/assisted-by/co-developed commit trailers.
- Do not use `@mentions` or issue-closing keywords (`Fixes #123`) in commit
  messages tied to the fix — that linkage belongs in the PR template only.
- Only file an issue once you've confirmed root cause from raw logs, not
  just from the testgrid pass/fail grid — testgrid alone often can't
  distinguish "real regression" from "flaky infra" or "stale pinned
  version".

## Filing the issue

```bash
gh issue create --repo kubernetes-sigs/jobset \
  --title "<concise, specific title including affected job(s)>" \
  --body-file /tmp/issue_body.md
```
