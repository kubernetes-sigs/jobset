---
title: "Volume Claim Policies"
linktitle: "Volume Claim Policies"
weight: 15
date: 2026-01-13
description: >
  Managing stateful JobSet with shared persistent volumes
no_list: true
---

JobSet provides the VolumeClaimPolicies API to automatically create and manage shared
PersistentVolumeClaims (PVCs) across multiple ReplicatedJobs within a JobSet.
This enables stateful JobSets that require persistent storage for datasets, models, checkpoints, or
intermediate results.

## Basic Usage

To use VolumeClaimPolicies, define them in the `volumeClaimPolicies` field of your JobSet spec.
Each policy can contain one or more PVC templates.

[This example](https://github.com/kubernetes-sigs/jobset/blob/main/site/static/examples/volume-claim-policy/single-pvc.yaml)
demonstrates creating shared PVCs with different retention policies:

In this example:

1. An `initializer` ReplicatedJob downloads a model to the `initializer` volume
1. A `node` ReplicatedJob reads the model and writes checkpoints which contain index of the ReplicatedJob
1. The PVCs are automatically created with the naming convention: `<claim-name>-<jobset-name>`
   - `initializer-volume-claim-trainjob` (deleted when JobSet is deleted)
   - `checkpoints-volume-claim-trainjob` (retained after JobSet is deleted)

{{< include file="/examples/volume-claim-policy/single-pvc.yaml" lang="yaml" >}}

## How Volumes Are Mounted

To mount a shared PVC in your pods:

1. Define a `volumeClaimPolicies` template with a specific name (e.g., `model-data`)
1. Add a `volumeMount` in your container with the **same name**
1. JobSet automatically injects the PVC `volume` into your pod spec and creates the appropriate PVC

## Retention Policies

VolumeClaimPolicies support retention policies to control what happens to PVCs when the JobSet is deleted.

### `Delete` (Default)

The PVC is automatically deleted when the JobSet is deleted. This is the default behavior when no
retention policy is specified.

```yaml
volumeClaimPolicies:
  - templates:
      - metadata:
          name: temporary-data
        spec:
          accessModes: ["ReadWriteOnce"]
          resources:
            requests:
              storage: 10Gi
    retentionPolicy:
      whenDeleted: Delete
```

### `Retain`

The PVC is kept after the JobSet is deleted, allowing you to access the data later or use it in
subsequent JobSets.

{{% alert title="Note" color="primary" %}}
If you are trying to use the existing volume in the VolumeClaimPolicies,
the spec must be equal to the existing PVC spec.
{{% /alert %}}

```yaml
volumeClaimPolicies:
  - templates:
      - metadata:
          name: checkpoints
        spec:
          accessModes: ["ReadWriteOnce"]
          resources:
            requests:
              storage: 50Gi
    retentionPolicy:
      whenDeleted: Retain # PVC survives JobSet deletion
```

When using `Retain`, you can access the persisted data by:

- Creating a new JobSet with a volumeMount referencing the existing PVC name
- Mounting the PVC directly in a debug pod
- Using the PVC in other workloads

## Custom Labels and Annotations

You can add custom labels and annotations to PVC templates for organization, monitoring,
or integration with other tools. These labels and annotations are preserved on the created PVCs
along with the automatically added `jobset.sigs.k8s.io/jobset-name` label.

```yaml
volumeClaimPolicies:
  - templates:
      - metadata:
          name: my-volume
          labels:
            team: ml-platform
            environment: production
            content-type: model
          annotations:
            backup-policy: "daily"
            retention-days: "30"
        spec:
          accessModes: ["ReadWriteMany"]
          resources:
            requests:
              storage: 100Gi
```

## Limitations

- Maximum of 50 volume claim templates per JobSet
- PVC templates cannot specify the `namespace` field (namespace is inherited from the JobSet)
- ReplicatedJob templates must not define volumes with the same name as VolumeClaimPolicy templates
- At least one container or initContainer in the ReplicatedJobs must have a volumeMount matching
  each volume claim template name
- When defining the existing volume in VolumeClaimPolicies the spec must be equal to
  the pre-created PVC
