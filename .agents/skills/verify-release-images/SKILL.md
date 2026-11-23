---
name: verify-release-images
description: Verify JobSet release container images and Helm charts in the staging registry, capture evidence for a release issue comment, and prepare the kubernetes/k8s.io promotion PR. Use for the release checklist's "Promote the release image and Helm chart" step or to verify artifacts after promotion.
---

# Verify release images and Helm charts

Follow the "Promote the release image and Helm chart" section in
[the release checklist](../../../.github/ISSUE_TEMPLATE/new-release.md).
Read that checklist and the repository's `AGENTS.md` before proceeding.
Resolve repository paths relative to this skill directory, not the caller's
working directory.

## Inputs and prerequisites

- Confirm the release version (`vMAJ.MIN.PATCH`) and the release issue URL or
  number. Do not infer the target from the latest tag or use the examples as
  the intended release. The issue is optional for local verification only.
- Docker CLI with `docker manifest inspect`, and Helm with OCI support. A
  Kubernetes cluster is not required.
- `gcloud` for digest discovery, or staging registry metadata supplied by the
  release owner that links each release tag to its digest.
- `gh` authenticated to GitHub only if posting an issue comment or creating a PR.
- A separate checkout/fork of `kubernetes/k8s.io` only if preparing a promotion PR.

Default to read-only registry checks and local drafts. Post comments, push a
branch, or submit a PR only when requested by the user; otherwise provide the
comment and patch for review. Never rebuild, retag, push, or delete registry
artifacts as part of verification. Do not change JobSet CI or release automation.
Disclose AI assistance in issue comments and PR descriptions, including under
`#### Special notes for your reviewer:` when that section exists. Do not add AI
commit trailers, `@mentions`, or issue-closing text to commit messages.

## 1. Resolve the staging release digests

Replace the version placeholder with the confirmed release. Run the following
blocks in the same Bash session (or explicitly carry their variables into each
invocation). Keep evidence outside the repository:

```bash
set -euo pipefail
VERSION='<vMAJ.MIN.PATCH>'
[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
  printf 'Expected a release version such as v0.11.1\n' >&2
  exit 1
}
CHART_VERSION="${VERSION#v}"
IMAGE_REPO='us-central1-docker.pkg.dev/k8s-staging-images/jobset/jobset'
CHART_REPO='us-central1-docker.pkg.dev/k8s-staging-images/jobset/charts/jobset'
EVIDENCE_DIR="$(mktemp -d -t jobset-release-verification.XXXXXX)"
printf 'Evidence directory: %s\n' "$EVIDENCE_DIR"
```

Look up the **top-level manifest/index digest** for the version tag, not a
platform-specific image digest. The Helm chart has its own OCI manifest digest;
never reuse the container image digest for it.

```bash
IMAGE_DIGEST="$(gcloud artifacts docker images describe "${IMAGE_REPO}:${VERSION}" \
  --format='value(image_summary.digest)')"
CHART_DIGEST="$(gcloud artifacts docker images describe "${CHART_REPO}:${CHART_VERSION}" \
  --format='value(image_summary.digest)')"
```

If `gcloud` is unavailable, obtain both digests from staging registry metadata
and set `IMAGE_DIGEST` and `CHART_DIGEST` explicitly. Record the metadata source
and confirm the tags refer to those digests. A successful digest-pinned pull
alone does not prove that the release tag points to that digest.

```bash
for digest in "$IMAGE_DIGEST" "$CHART_DIGEST"; do
  [[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]] || {
    printf 'Missing or invalid SHA-256 digest: %s\n' "$digest" >&2
    exit 1
  }
done
IMAGE_REF="${IMAGE_REPO}:${VERSION}@${IMAGE_DIGEST}"
CHART_REF="oci://${CHART_REPO}@${CHART_DIGEST}"
printf 'Release: %s\nImage: %s\nChart: %s\nChart version: %s\n' \
  "$VERSION" "$IMAGE_REF" "$CHART_REF" "$CHART_VERSION" \
  | tee "$EVIDENCE_DIR/references.txt"
```

Stop on missing artifacts, empty digests, tag/digest mismatches, authentication
errors, or network errors. Report the exact error; an access failure does not
prove the artifact is missing. The release tag triggers staging publication, so
an unfinished build may require waiting and retrying. Never substitute `main`,
`latest`, or a previous release.

## 2. Verify the staging container image

```bash
docker manifest inspect "$IMAGE_REF" 2>&1 \
  | tee "$EVIDENCE_DIR/staging-image-manifest.txt"
```

Require exit status zero and a valid manifest. Inspect the index's platforms
against the release branch's `PLATFORMS` in `Makefile` (currently `linux/amd64`,
`linux/arm64`, and `linux/s390x`). Do not treat `unknown/unknown` attestation
entries as runnable platforms. Report missing expected platforms as a blocker.
The promotion must use the verified top-level digest, not one of the child
manifest digests shown in this output.

Checklist example only:

```bash
docker manifest inspect us-central1-docker.pkg.dev/k8s-staging-images/jobset/jobset:v0.11.1@sha256:88154814bfd507042c5e7d3ea551c0a214a6d9b319e1fb2c7110532055c0b5c5
```

## 3. Verify the staging Helm chart

```bash
helm show chart "$CHART_REF" --version "$CHART_VERSION" 2>&1 \
  | tee "$EVIDENCE_DIR/staging-chart.txt"
```

Require exit status zero, `name: jobset`, and `version` equal to `CHART_VERSION`
(without `v`). Check `appVersion` against `VERSION`: release packaging sets it
with `--app-version "${GIT_TAG}"` in `hack/helm-chart-package.sh`. If Helm prints
an OCI digest, confirm it equals `CHART_DIGEST`. Stop on any mismatch.

Checklist example only:

```bash
helm show chart oci://us-central1-docker.pkg.dev/k8s-staging-images/jobset/charts/jobset@sha256:34ab84eb4046562d5e7764eef4947bcf938f8a74f19654dd7a4097482f710aef --version 0.11.1
```

`pipefail` is required for the `tee` commands above so that a failed registry
check cannot be reported as success. Preserve stdout, stderr, and each command's
exit status, including failures. These checks establish artifact availability
and metadata consistency, not signature verification or vulnerability scanning.

## 4. Share evidence in the release issue

Prepare a Markdown comment with the actual version, references, digest source,
UTC verification time, exact commands, exit statuses, image platforms, and full
manifest/chart output in fenced code blocks or collapsible `<details>` sections.
Use the following outline, replacing all placeholders with observed results:

```text
### Staging release verification: <version>

AI assistance was used to perform these checks and prepare this comment.

Checked at: <UTC timestamp>
Digest source: <registry metadata source linking release tags to digests>

Container image: <digest-pinned image reference>
Command: docker manifest inspect <digest-pinned image reference>
Exit status: <observed status>
Platforms: <observed platforms and whether they match the release Makefile>
<full observed manifest output>

Helm chart: <digest-pinned OCI chart reference>
Command: helm show chart <digest-pinned OCI chart reference> --version <version without v>
Exit status: <observed status>
Chart name/version/appVersion: <observed values>
<full observed Helm output>

Result: <verified, failed, or blocked; include reason>
PR: <actual promotion PR URL, or not submitted>
```

Save the completed comment to `$EVIDENCE_DIR/comment.md`. Review it for credentials
or tokens before sharing. Set `ISSUE_URL` to the confirmed release issue URL. When
posting is requested:

```bash
gh issue comment "$ISSUE_URL" --body-file "$EVIDENCE_DIR/comment.md"
```

Capture the returned comment URL. Never fabricate output, report unrun checks as
passed, or mark checklist items complete when they failed or were not performed.

## 5. Prepare and submit the promotion PR

Proceed only after both staging checks pass. In the `kubernetes/k8s.io` checkout:

1. Read its contribution instructions and inspect the current inventory at
   [`registry.k8s.io/images/k8s-staging-jobset/images.yaml`](https://github.com/kubernetes/k8s.io/blob/main/registry.k8s.io/images/k8s-staging-jobset/images.yaml).
2. Check for an existing promotion PR and version mappings before adding entries.
   If the release already maps to the same digests, report that it is already
   configured. If it maps to different digests, stop and ask the release owner;
   do not overwrite a published mapping.
3. Make only these additions, matching the existing YAML structure:
   - Under `name: jobset`, add `IMAGE_DIGEST` to `dmap` with `["vMAJ.MIN.PATCH"]`.
   - Under `name: charts/jobset`, add `CHART_DIGEST` to `dmap` with `["MAJ.MIN.PATCH"]`.
   - If a digest already exists, add the new tag to its list without removing
     existing tags. Preserve all other releases.
4. Review the diff and run the inventory validation required by that repository.
5. Prepare a PR titled `Publish JobSet vMAJ.MIN.PATCH`. Explain that it promotes
   the verified staging image and chart; include both digests, the release issue
   and verification comment links, and the required AI disclosure. Use a focused
   branch and submit against `kubernetes/k8s.io` only when authorized.
6. Record the actual PR URL as `PR: <URL>` in the release issue (or provide it to
   the user for the checklist). Example: https://github.com/kubernetes/k8s.io/pull/8453.

If GitHub access or the checkout is unavailable, return the proposed YAML
additions and PR description locally and clearly state that no PR was submitted.

## 6. Verify production after promotion (when requested)

A merged promotion PR is not proof that artifacts are already available. Wait
for propagation, then follow the checklist's separate production-verification
step using the same evidence/reporting rules:

```bash
docker manifest inspect "registry.k8s.io/jobset/jobset:${VERSION}" 2>&1 \
  | tee "$EVIDENCE_DIR/production-image-manifest.txt"
helm show chart oci://registry.k8s.io/jobset/charts/jobset --version "$CHART_VERSION" 2>&1 \
  | tee "$EVIDENCE_DIR/production-chart.txt"
```

Confirm the release's platforms and chart metadata again. Resolve production
manifest digests with registry metadata or a registry-aware CLI and compare them
to the verified staging digests. Do not hash the formatted JSON from
`docker manifest inspect` to derive the registry digest. Report production
verification separately; staging success alone does not complete this step.
