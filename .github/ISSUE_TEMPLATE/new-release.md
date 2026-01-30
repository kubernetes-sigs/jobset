---
name: New Release
about: Propose a new release
title: Release v0.x.0
labels: ''
assignees: ''

---

## Release Checklist
<!--
Please do not remove items from the checklist
-->
- [ ] All [OWNERS](https://github.com/kubernetes-sigs/jobset/blob/main/OWNERS) must LGTM the release proposal
- [ ] Verify that the changelog in this issue is up-to-date
- [ ] For major or minor releases (v$MAJ.$MIN.0), create a new release branch.
  - [ ] an OWNER creates a vanilla release branch with
        `git branch release-$MAJ.$MIN main`
  - [ ] An OWNER pushes the new release branch with
        `git push release-$MAJ.$MIN`
- [ ] When a new release branch is created, onboard that branch to prow by adding new jobs to test infra. <!-- example: https://github.com/kubernetes/test-infra/pull/36295 -->
- [ ] Update `BRANCH_NAME` in the `Makefile` and set `export VERSION=vX.Y.Z`.
   - [ ] Run `make prepare-release-branch` to update the assets.
   - [ ] Update the `CHANGELOG-X.Y`
   - [ ] Submit a PR with the changes against the release branch <!-- example: https://github.com/kubernetes-sigs/jobset/pull/1130 -->
- [ ] After the above PR is merged rebase your local branch and create a signed tag running:
     `git tag -s $VERSION`
      and inserts the changelog into the tag description.
      To perform this step, you need [a PGP key registered on github](https://docs.github.com/en/authentication/managing-commit-signature-verification/checking-for-existing-gpg-keys).
- [ ] An OWNER pushes the tag with
      `git push $VERSION`
  - Triggers prow to build and publish a staging container image
      `gcr.io/k8s-staging-jobset/jobset:$VERSION`
- [ ] An OWNER [prepares a draft release](https://github.com/kubernetes-sigs/jobset/releases)
  - [ ] Write the changelog into the draft release.
  - [ ] Run `make artifacts IMAGE_REGISTRY=registry.k8s.io/jobset`
      to generate the artifacts and upload the files in the `artifacts` folder
      to the draft release.
- [ ] Submit a PR against [k8s.io](https://github.com/kubernetes/k8s.io), 
      updating `k8s.gcr.io/images/k8s-staging-jobset/images.yaml` to
      [promote the container images](https://github.com/kubernetes/k8s.io/tree/main/k8s.gcr.io#image-promoter)
      to production: <!-- example https://github.com/kubernetes/k8s.io/pull/8453-->
- [ ] Wait for the PR to be merged and verify that the image `registry.k8s.io/jobset/jobset:$VERSION` is available.
- [ ] Wait for the PR to be merged and verify that the chart `registry.k8s.io/jobset/charts/jobset` is available. 
      Try `helm show chart oci://registry.k8s.io/jobset/charts/jobset --version ${VERSION/#v/}
- [ ] Publish the draft release prepared at the [Github releases page](https://github.com/kubernetes-sigs/jobset/releases).
- [ ] Add a link to the tagged release in this issue: <!-- example https://github.com/kubernetes-sigs/jobset/releases/tag/v0.1.0 -->
- [ ] Send an announcement email to `sig-apps@kubernetes.io`, `sig-scheduling@kubernetes.io` and `wg-batch@kubernetes.io` with the subject `[ANNOUNCE] JobSet $VERSION is released`
- [ ] Add a link to the release announcement in this issue: <!-- example https://groups.google.com/a/kubernetes.io/g/wg-batch/c/-gZOrSnwDV4 -->
- [ ] For a major or minor release, update `README.md` in `main` branch:  <!-- TODO (andreyvelich): Add example here>
- [ ] For a major or minor release, create an unannotated _devel_ tag in the
      `main` branch, on the first commit that gets merged after the release
       branch has been created (presumably the README update commit above), and, push the tag:
      `DEVEL=v0.$(($MAJ+1)).0-devel; git tag $DEVEL main && git push $DEVEL`
      This ensures that the devel builds on the `main` branch will have a meaningful version number.
- [ ] For a major or minor release, submit a PR against [test-infra](https://github.com/kubernetes/test-infra),
      updating `config/jobs/kubernetes-sigs/jobset` periodics to drop n - 1 release and add a periodic
      covering the new release branch. <!-- example kubernetes/test-infra#35420-->
- [ ] Close this issue


## Changelog
<!--
Describe changes since the last release here.
-->
