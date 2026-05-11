## v0.12.0 

Changes since `v0.11.1`.

  New Features

  - Elastic JobSets: Pod-level scaling support (#1179, @aniket2405)
    - Implement elastic scaling at the Pod level for JobSets
  - Add restart actions `RestartJob` and `RestartJobAndIgnoreMaxRestarts` (#1177, #1191, @GiuseppeTT)
    - New restart action types for finer-grained failure recovery control
    - KEP: Add restart actions KEP (#1152, @GiuseppeTT)
  - Add condition `RestartingJobSet` (#1225, @GiuseppeTT)
    - New condition to track when a JobSet is actively restarting
  - Add support for custom annotations in JobSet controller (#1148, @dabico)
    - Allow users to configure custom annotations on JobSet resources
  - Add sidecar container mode to the in-place restart agent (#1140, @GiuseppeTT)
    - Alternative deployment mode for the restart agent as a sidecar
  - Enable `tlsMinVersion` and `tlsCipherSuites` configuration in JobSet configuration (#1127, @kannon92)
    - Configurable TLS settings for the controller manager
  - Add `patchStrategy` markers to JobSet API for SMP (#1185, @andreyvelich)
    - Server-side apply / Structured Merge Patch support for API fields

  Bug Fixes

  - Fix admission webhook to return `Invalid` instead of `Forbidden` (#1207, @JerryHu1994)
  - Trigger reconciliation if status update fails (#1194, @GiuseppeTT)
  - List child Jobs by JobSet UID instead of JobSet name + namespace (#1167, @GiuseppeTT)
  - Filter out Pods that do not have the same owner reference (#1154, @GiuseppeTT)
  - Fix Helm installation command in docsite (#1142, @janekmichalik)
  - Add timeout for waiting for PVC to fight flakes (#1134, @kannon92)
  - Fix test: Check JobSet name before listing Jobs (#1163, @andreyvelich)
  - Fix PR #1195 (#1199, @GiuseppeTT)

  Helm/Deployment

  - Auto-generate Helm templates from kustomize using yaml-processor (#1166, @IrvingMg)
  - Update Helm chart maintainers to match real maintainers (#1156, @kannon92)
  - Align JobSet Webhooks with Controller-Runtime Conventions (#1159, @andreyvelich)

  Documentation

  - Add troubleshooting section: follower pod node selector not set (#1189, @0xlen)
  - Add KEP for Elastic JobSets (#1147, @aniket2405)
  - Add deepwiki link for code overviews (#1214, @kannon92)
  - Add Copilot Instructions (#1161, @andreyvelich)
  - Create symlink for CLAUDE.md (#1160, @andreyvelich)
  - Fix TOC for KEP-262 (#1180, @GiuseppeTT)
  - Improve the issue template for releases (#1176, @GiuseppeTT)
  - Fix image (#1220, @GiuseppeTT)

  Build/CI Improvements

  - Update Kubernetes dependencies to 0.36.0 (#1212, @kannon92)
  - Update Golang to 1.26 (#1210, @kannon92)
  - Update to k8s 0.35.1 (#1151, @kannon92)
  - Bump version of golangci-lint (#1196, @GiuseppeTT)
  - Add more golangci-lint linters and fix misspellings (#1181, @kannon92)
  - Standardize copyright headers across all files (#1162, @IrvingMg)
  - Update release template test infra (#1131, @kannon92)

  Dependency Updates

  - Bump sigs.k8s.io/controller-runtime (multiple updates) (#1158 @andreyvelich, #1183)
  - Bump the kubernetes group (multiple updates) (#1174, #1209)
  - Bump github.com/onsi/ginkgo/v2 from 2.27.5 to 2.28.3 (#1143, #1222)
  - Bump github.com/onsi/gomega from 1.39.0 to 1.39.1 (#1144)
  - Bump github.com/open-policy-agent/cert-controller from 0.15.0 to 0.16.0 (#1190)
  - Bump go.opentelemetry.io/otel/sdk from 1.36.0 to 1.43.0 (#1173, #1206)
  - Bump google.golang.org/grpc from 1.72.2 to 1.79.3 (#1192)
  - Bump sigs.k8s.io/structured-merge-diff/v6 (#1164)
  - Bump postcss from 8.5.6 to 8.5.13 in /site (#1224)
  - Bump picomatch in /site (#1201)
