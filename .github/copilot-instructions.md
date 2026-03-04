# GitHub Copilot Code Review Instructions

## Review Philosophy

- Only comment when you have HIGH CONFIDENCE (>80%) that an issue exists
- Be concise: one sentence per comment when possible
- Focus on actionable feedback, not observations
- When reviewing text, only comment on clarity issues if the text is genuinely confusing or could lead to errors.
  "Could be clearer" is not the same as "is confusing" - stay silent unless HIGH confidence it will cause problems

## Priority Areas (Review These)

### Security & Safety

- Unsafe code blocks without justification
- Command injection risks (shell commands, user input)
- Path traversal vulnerabilities
- Credential exposure or hardcoded secrets
- Missing input validation on external data
- Improper error handling that could leak sensitive info

### Correctness Issues

- Logic errors that could cause panics or incorrect behavior
- Resource leaks (files, connections, memory)
- Off-by-one errors or boundary conditions
- Optional types that don't need to be optional
- Booleans that should default to false but are set as optional
- Overly defensive code that adds unnecessary checks
- Unnecessary comments that just restate what the code already shows (remove them)

### Architecture & Patterns

- Code that violates existing patterns in the codebase
- Missing error handling
- Code that is not following [Effective Go](https://go.dev/doc/effective_go)
- Violating [Kubernetes API guidelines](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api_changes.md)

## Project-Specific Context

- See [AGENTS.md](../AGENTS.md) in the root directory for project guidelines and architecture decisions.

## CI Pipeline Context

**Important**: You review PRs immediately, before CI completes. For the categories listed under
**Skip These** below, assume CI will catch them and do not comment on them.

JobSet presubmit tests are defined here: https://github.com/kubernetes/test-infra/blob/master/config/jobs/kubernetes-sigs/jobset/jobset-presubmit-main.yaml

### What Our CI Checks

- `pull-jobset-verify-main` - Code Quality Checks
- `pull-jobset-test-unit-main` - Code generation and Go unit tests
- `pull-jobset-test-integration-main` - Integration tests
- `pull-jobset-test-e2e-main-1-**` - E2E tests for the supported Kubernetes versions

## Skip These (IMPORTANT)

Do not comment on:

- **Auto generated code** - CI handles this (`make generate`)
- **Python Code** - Python API is auto-generated
- **Style/formatting** - CI handles this (gofmt)
- **Test failures** - CI handles this (full test suite)
- **Missing dependencies** - CI handles this

## Response Format

When you identify an issue:

1. **State the problem** (1 sentence)
2. **Why it matters** (1 sentence, only if not obvious)
3. **Suggested fix** (code snippet or specific action)

## When to Stay Silent

If you're uncertain whether something is an issue, don't comment. False positives create noise and reduce trust in the review process.
