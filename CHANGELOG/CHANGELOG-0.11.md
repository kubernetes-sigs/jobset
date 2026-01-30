## v0.11.0

Changes since `v0.10.0`.

  New Features

  - KEP-572: VolumeClaimPolicies API for Stateful JobSet (#1062, #1098)
    - New API for managing persistent volume claims with JobSets
  - KEP-467: Fast failure recovery with in-place restarts (#1083, #1096, #1099)
    - Add InPlaceRestart feature gate as alpha
    - Implement in-place restart logic for faster failure recovery
  - Add JobSet PodDisruptionBudget (#1112)
    - New PDB support for JobSet workloads
  - Emit event on job creation failure (#1076)
    - Controller now emits events when job creation fails

  Bug Fixes

  - Add jobset.sigs.k8s.io/priority to all child Pods to fix bug in exclusive placement (#1077)
  - Make Pod admission webhook fail if the Node of the leader Pod is not found (#1089)
  - Validate coordinator label value (#1079)

  Security

  - Restrict controller-manager Secrets access to jobset install namespace (#1063)
  - Remove pod create permission from controller (#1074)

  Helm/Deployment

  - Set namespace to the webhook service in charts (#1067)
  - Copy CRD to helm chart on manifest (#1047)
  - Use explicit bash image path instead of ambiguous shortname (#1065)

  Documentation

  - Guide for the VolumeClaimPolicies API (#1118)
  - Add agents markdown (#1080)
  - Fix incorrect code comments (#1072)
  - Remove 1.31 from readme (#1087)

  Build/CI Improvements

  - Update Kubernetes dependencies to 0.35 (#1105)
  - Update Golang to 1.25 (#1066)
  - Enable KAL linter in jobset (#1046)
  - Enable nobools for future API calls (#1069)
  - Ignore kubeapilinter violations in existing API fields (#1070)
  - Use setup-envtest@release-0.22 instead of the latest version (#1071)
  - Remove hardcoded ENVTEST_K8S_VERSION (#1075)
  - Fix make verify (#1100)

  Dependency Updates

  - Bump github.com/onsi/gomega from 1.38.3 to 1.39.0 (#1116)
  - Bump github.com/onsi/ginkgo/v2 from 2.25.3 to 2.27.4 (#1059, #1078, #1082, #1104, #1115)
  - Bump github.com/open-policy-agent/cert-controller from 0.14.0 to 0.15.0 (#1093)
  - Bump sigs.k8s.io/controller-runtime (multiple updates) (#1064, #1085)
  - Bump the kubernetes group (multiple updates) (#1092, #1102)
