## v0.13.0

Changes since `v0.12.0`.

<!-- Audited range: v0.12.0 (b16bcb1b7b23aa477b677ad8cfd9e984f7fd6fe1)..2c09c4e839a9e75b6c8b0ae8480c4353a59f1ec1. -->

  New Features

  - Add alpha Workload-Aware Scheduling integration behind the `JobSetWorkloadAwareSchedulingAPI` feature gate, disabled by default (#1250, @kannon92)
    - Add `spec.scheduling` for Basic or Gang scheduling across a whole JobSet, groups of ReplicatedJobs, or individual Job replicas, with topology constraints and disruption policies
    - Create and reconcile an owned Workload and its PodGroups, map child Pods through `schedulingGroup.podGroupName`, and annotate Jobs with `scheduling.k8s.io/group-template-name`
    - Support shared DRA resource claims through `spec.scheduling.replicatedJobs[].job.resourceClaims`; composite PodGroup hierarchies are not implemented in alpha
    - Delete scheduling objects on suspension or completion and recreate them on resume; allow scheduling reconfiguration after the controller observes suspension
    - Keep derived Gang `minCount` values in sync with Elastic JobSet scaling; sequenced startup requires per-ReplicatedJob scheduling rather than a whole-JobSet gang
    - Require the Kubernetes WAS APIs and corresponding cluster feature gates; JobSets without `spec.scheduling` retain their existing behavior
  - Add alpha JobSet active deadlines behind the `JobSetActiveDeadlineSeconds` feature gate, disabled by default (#1320, @yindia)
    - Add mutable `spec.activeDeadlineSeconds` and `status.startTime`; exceeding the deadline deletes active child Jobs and marks the JobSet Failed with reason `DeadlineExceeded` instead of restarting it
    - Reset the deadline timer on resume and global JobSet restarts, but not individual Job restarts; suspended time does not count toward the deadline
    - Maintain `status.startTime` only while the gate is enabled; externally managed JobSets remain responsible for their own deadline enforcement
    - Add the `jobset_active_deadline_exceeded_total` metric
  - Add alpha execution-attempt tracking behind the `ExecutionAttemptsTracking` feature gate, disabled by default (#1283, @jianqiaol)
    - Add `status.executionAttempts`, a monotonic counter covering the initial execution, failure-policy restarts, and suspend/resume cycles
    - Propagate the current attempt through the `jobset.sigs.k8s.io/execution-attempt` annotation on child Jobs and Pod templates
    - Add the Execution Attempts Tracking KEP (#1292, @jianqiaol)
  - Populate JobSet headless Service ports from container ports and keep owned Services in sync, enabling service meshes to route traffic to JobSet Pods (#1300, @yindia)
  - Apply `ttlSecondsAfterFinished` cleanup to externally managed JobSets after their controller records a terminal condition (#1315, @yindia)
  - Graduate the `TLSOptions` feature gate to GA; TLS minimum-version and cipher-suite configuration is now permanently enabled and the gate can no longer be disabled (#1297, @kannon92)

  Bug Fixes

  - Avoid a nil-pointer panic when reconciling JobSets without `spec.network` (#1266, @immanuwell)
  - Validate child resource names derived from `metadata.generateName` during JobSet creation (#1267, @immanuwell)
  - Ignore terminal leader Pods in the admission webhook so retried follower Pods do not deadlock after a leader failure (#1276, @jianqiaol)
  - Delete followers with malformed or mismatched topology placement so exclusive-placement reconciliation can repair them (#1269, @immanuwell)
  - Reject negative `replicatedJobs[].replicas` values through CRD validation (#1279, @immanuwell)
  - Avoid nil-pointer panics when `volumeClaimPolicies[].retentionPolicy.whenDeleted` is omitted in programmatically constructed JobSets (#1305, @Viswalahiri)
  - Allow Pods using negative PriorityClass values to pass admission by encoding the `jobset.sigs.k8s.io/priority` label as `n<absolute-value>`, while preserving non-negative priority labels (#1291, @SrikarChittemsetty)

  Helm/Deployment

  - Allow controller feature gates to be configured through the `controller.featureGates` Helm values map; the default empty map preserves existing configuration (#1345, @sanposhiho)
  - Stop declaring webhook certificate Secret `data` fields in the Helm chart, preventing Helm v4 server-side-apply ownership conflicts during upgrades (#1248, @gzb1128)
  - Bootstrap webhook certificates before starting the main manager and disable bootstrap-only metrics and health listeners, avoiding startup failures and port collisions (#1243, @kannon92)
  - Increase the controller liveness-probe initial delay to tolerate certificate bootstrap and avoid transient container restarts (#1278, @kannon92)

  Documentation

  - Add a TPU multi-slice training guide with Kueue and JAX workload examples (#1168, @0xlen)
  - Replace manual Workload-Aware Scheduling setup guides with declarative `spec.scheduling` guidance and examples for gang scheduling, topology constraints, sequenced startup, elastic scaling, and suspend/resume (#1241, #1344, #1250, @kannon92)
  - Add Dynamic Resource Allocation integration guidance for Workload-Aware Scheduling; manual setup examples are superseded by declarative scheduling examples (#1277, @sairameshv)
  - Add the Gang JobSets and Workload-Aware Scheduling integration KEP, align its API types with the Workload builder, and correct scheduling and naming examples (#1253, #1313, #1314, #1330, @kannon92)
  - Add a KEP for mutable resources on suspended Jobs (#1274, @kannon92)
  - Add a KEP and concept documentation for JobSet `activeDeadlineSeconds`, including externally managed JobSet behavior (#1306, #1319, #1320, @yindia)
  - Link to the community k8s-CronJobSet project for recurring JobSet workloads (#1326, @yindia)
  - Correct Python SDK example virtual-environment activation instructions (#1343, @git-jxj)
  - Add links to JobSet talks and presentations in the README and documentation overview (#1346, @YamunadeviShanmugam)
  - Correct installation documentation references (#1256, @immanuwell)
  - Remove the obsolete Go Report Card badge (#1270, @tenzen-y)

  Build/CI Improvements

  - Update the Go toolchain and build images to Go 1.27 (#1328, @kannon92)
  - Add dedicated E2E coverage and configuration for alpha features, including in-place restart and per-Job restart (#1200, @IrvingMg)
  - Add a dedicated Kind environment and E2E suite for Workload Aware Scheduling (#1252, @kannon92)
  - Build Kind node images locally instead of relying on published node images (#1234, @kannon92)
  - Update Kind to 0.32.0 (#1260, @kannon92)
  - Probe the webhook request path before starting E2E tests to avoid connection-refused flakes (#1261, @kannon92)
  - Use standard feature-gate toggling to avoid Elastic JobSet test flakes (#1268, @kannon92)
  - Update the Cloud Build image (#1227, @kannon92)
  - Sync main with the v0.12.0 release artifacts (#1230, @kannon92)
  - Add CodeRabbit configuration (#1244, @kannon92)
  - Document the Kubernetes AI contribution policy for coding agents (#1240, @Copilot)
  - Add jianqiaol to the project reviewers (#1295, @jianqiaol)

  Dependency Updates

  - Update Kubernetes dependencies from 0.36.0 to 0.37.1 and regenerate clients, CRDs, and the Python SDK (#1236, #1245, #1288, #1301, #1298, #1339, @kannon92)
  - Bump `sigs.k8s.io/controller-runtime` from 0.24.0 to 0.25.2 (#1236, #1325, #1336, #1342)
  - Bump `github.com/onsi/ginkgo/v2` from 2.28.3 to 2.33.0 (#1237, #1246, #1299, #1331, #1338)
  - Bump `github.com/onsi/gomega` from 1.40.0 to 1.44.0 (#1238, #1247, #1249, #1309, #1337, #1340)
  - Bump `sigs.k8s.io/structured-merge-diff/v6` from 6.4.0 to 6.4.2 (#1275)
  - Bump `google.golang.org/grpc` from 1.79.3 to 1.83.2 (#1285, #1312, #1329)
  - Bump OpenTelemetry dependencies, including the OTLP gRPC trace exporter, to 1.45.0 (#1334)
  - Bump `github.com/google/cel-go` from 0.26.0 to 0.29.2 (#1286, #1298)
  - Bump `github.com/prometheus/client_golang` from 1.23.2 to 1.24.1 (#1289)
  - Bump `github.com/go-logr/logr` from 1.4.3 to 1.4.4 (#1290)
  - Bump `github.com/stretchr/testify` from 1.11.1 to 1.12.1 (#1302)
  - Bump `golang.org/x/net` to 0.55.0 in the client-go example and Helm YAML processor modules (#1254, #1255)
  - Bump `postcss` from 8.5.13 to 8.5.23 in the documentation site (#1287)
  - Bump `browserslist` from 4.25.1 to 4.28.8 in the documentation site (#1311)
