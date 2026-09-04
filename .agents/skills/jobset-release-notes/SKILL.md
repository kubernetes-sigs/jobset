---
name: jobset-release-notes
description: Generates or updates JobSet release notes from every merged pull request since a prior stable release. Use when preparing a JobSet changelog or release notes for a new minor or patch release, auditing changelog coverage, or summarizing changes between a release tag and current main.
compatibility: Requires git and a checkout of the kubernetes-sigs/jobset repository. The GitHub CLI is optional for resolving pull-request authors.
---

# JobSet Release Notes

Generate accurate, reviewer-friendly release notes from the repository history. Treat `AGENTS.md`, git history, diffs, and existing files under `CHANGELOG/` as the sources of truth.

## Inputs

Determine these from the request, asking only when they cannot be inferred:

- **Base release:** the prior stable tag, such as `v0.12.0`. Do not use `-devel` tags.
- **Target release:** the release being documented, such as `v0.13.0`.
- **End revision:** default to `HEAD` for “current main.”
- **Output:** default to `CHANGELOG/CHANGELOG-<major>.<minor>.md`.

Record the exact base and end revisions used so the result can be audited.

## Workflow

### 1. Read project guidance and existing style

Read:

- `AGENTS.md`
- The target changelog, if it exists
- The two most recent changelogs under `CHANGELOG/`

Preserve the repository's heading, indentation, PR-number, and contributor-handle conventions. Update an existing target changelog rather than creating a competing file.

### 2. Establish the comparison range

Verify the requested refs and working tree:

```bash
git status --short
git tag --sort=-version:refname | head -20
git rev-parse <base-tag> HEAD
git merge-base --is-ancestor <base-tag> HEAD
```

Use the range `<base-tag>..<end-revision>`. If the stable release was produced on a release branch and later synchronized to main, retain the synchronization PR in the coverage audit.

### 3. Inventory every merged PR

Start with first-parent history to identify merged changes without expanding merge commits:

```bash
git log --first-parent --reverse \
  --format='%h%x09%an%x09%s' <base-tag>..<end-revision>
```

Also inspect the complete range and changed files:

```bash
git rev-list --count <base-tag>..<end-revision>
git diff --stat <base-tag>..<end-revision>
git diff --name-status <base-tag>..<end-revision>
```

Do not use a list of merely closed GitHub PRs as the change inventory; include only commits in the git range.

### 4. Understand changes instead of copying titles

For each nontrivial PR, inspect its commit message, file list, and relevant diff:

```bash
git show --stat <commit>
git show -s --format='%B' <commit>
git show <commit> -- <relevant-paths>
```

Pay particular attention to:

- `api/jobset/v1alpha2/` for API fields and validation
- `pkg/features/` for feature-gate stage and defaults
- `pkg/controllers/` and `pkg/webhooks/` for behavior and bug fixes
- `charts/` and `config/` for installation or upgrade effects
- `site/` and `keps/` for documentation and proposals
- `go.mod`, tool modules, and `site/package*.json` for final dependency versions
- Tests for intended semantics and edge cases

Do not describe a KEP as implemented unless controller/API code implements it. Do not describe generated artifacts as independent features. Call out feature gates, alpha/beta/GA status, new API fields, annotations, defaults, and upgrade-relevant behavior.

### 5. Resolve contributor handles

Prefer the GitHub PR author over names inferred from commit email. If `gh` and network access are available:

```bash
gh api repos/kubernetes-sigs/jobset/pulls/<pr-number> \
  --jq '[.number, .user.login, .title] | @tsv'
```

If unavailable, use known handles from commit metadata and neighboring changelogs. Do not guess a handle when it cannot be verified. Dependency-bot entries do not need an author handle.

### 6. Write concise categorized notes

Use applicable sections in this order:

1. `New Features`
2. `Bug Fixes`
3. `Security`
4. `Helm/Deployment`
5. `Documentation`
6. `Build/CI Improvements`
7. `Dependency Updates`

Guidelines:

- Lead with user-visible impact, then implementation detail.
- Use one bullet per coherent change; use sub-bullets for related API or behavior details.
- Include every associated PR number.
- Group repetitive dependency bumps, but preserve every PR number and report the final version at the end revision.
- Include internal/build-only PRs under Build/CI so every merged PR remains auditable.
- Avoid marketing language, speculation, and claims not supported by the diff.
- Avoid issue-closing phrases such as `Fixes #123`.

Example:

```markdown
## v0.13.0

Changes since `v0.12.0`.

  New Features

  - Add alpha execution-attempt tracking behind the `ExecutionAttemptsTracking` feature gate (#1283, @contributor)
    - Add `status.executionAttempts` and propagate the current attempt to child Jobs and Pod templates

  Bug Fixes

  - Avoid a nil-pointer panic when reconciling JobSets without `spec.network` (#1266, @contributor)
```

### 7. Audit completeness and correctness

Extract PR numbers from first-parent subjects and compare them with the generated changelog:

```bash
python3 - <<'PY'
import pathlib
import re
import subprocess

base = "<base-tag>"
end = "<end-revision>"
notes_path = pathlib.Path("<output-path>")

log = subprocess.check_output(
    ["git", "log", "--first-parent", "--format=%s", f"{base}..{end}"],
    text=True,
)
merged = set(map(int, re.findall(r"#(\d+)", log)))
noted = set(map(int, re.findall(r"#(\d+)", notes_path.read_text())))

print("Missing PRs:", sorted(merged - noted))
print("PRs outside range:", sorted(noted - merged))
PY
```

Investigate every mismatch. A PR outside the range may be intentional only when it is directly associated with an in-range feature, such as its KEP; make that relationship explicit. Ensure dependency versions match files at the end revision.

Finally run:

```bash
git diff --check
git diff -- <output-path>
git status --short
```

Release-note-only changes do not require code tests. Report that tests were not run because the change is documentation-only.

## Completion Report

State:

- The changelog path changed
- The exact comparison range
- The number of merged PRs audited
- Whether any PRs were missing or outside the range
- Validation performed (`git diff --check`)
- That tests were not run for documentation-only changes
