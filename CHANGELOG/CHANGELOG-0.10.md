## v0.10.1

### Bug Fixes

- update crd in helm chart([#1048](https://github.com/kubernetes-sigs/jobset/pull/1048))

## v0.10.0

Changes since `v0.9.x`.

### New features

- Add onJobFailureMessagePatterns to distinguish retriable from non retriable Pod failure policies ([#1033](https://github.com/kubernetes-sigs/jobset/pull/1033))

### Documentation

- Update failure policy KEP: distinguish retriable from non retriable Pod failure policies ([#1027](https://github.com/kubernetes-sigs/jobset/pull/1027))

### Development and tooling

- use release 0.9 instead of 0.8 in release branch ([#990](https://github.com/kubernetes-sigs/jobset/pull/990))
- add test-infra requirement for release ([#991](https://github.com/kubernetes-sigs/jobset/pull/991))
- [main] chore(docs): Changelog for JobSet v0.9.0 ([#997](https://github.com/kubernetes-sigs/jobset/pull/997))
- use chart version and app version to match kueue tagging ([#999](https://github.com/kubernetes-sigs/jobset/pull/999))
- write helm package to artifacts ([#1003](https://github.com/kubernetes-sigs/jobset/pull/1003))
- potentially fix date tag on helm push ([#1005](https://github.com/kubernetes-sigs/jobset/pull/1005))
- use hard coded variable for branch name ([#1008](https://github.com/kubernetes-sigs/jobset/pull/1008))
- match kueue and jobset cloud build ([#1010](https://github.com/kubernetes-sigs/jobset/pull/1010))
- use 1.34 for envtest and update prod page for 1.34 ([#1011](https://github.com/kubernetes-sigs/jobset/pull/1011))
- Makefile: docker: Add CGO_ENABLED build arg ([#1023](https://github.com/kubernetes-sigs/jobset/pull/1023))
- formalize our release process ([#1039](https://github.com/kubernetes-sigs/jobset/pull/1039))

### Dependencies

- Bump github.com/open-policy-agent/cert-controller from 0.13.0 to 0.14.0 ([#993](https://github.com/kubernetes-sigs/jobset/pull/993))
- Bump github.com/onsi/ginkgo/v2 from 2.25.1 to 2.25.2 ([#995](https://github.com/kubernetes-sigs/jobset/pull/995))
- Bump github.com/stretchr/testify from 1.11.0 to 1.11.1 ([#996](https://github.com/kubernetes-sigs/jobset/pull/996))
- Bump github.com/prometheus/client_golang from 1.23.0 to 1.23.2 ([#1014](https://github.com/kubernetes-sigs/jobset/pull/1014))
- Bump github.com/onsi/ginkgo/v2 from 2.25.2 to 2.25.3 ([#1015](https://github.com/kubernetes-sigs/jobset/pull/1015))
- Bump the kubernetes group with 8 updates ([#1020](https://github.com/kubernetes-sigs/jobset/pull/1020))
