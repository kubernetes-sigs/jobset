---
title: "Prometheus Metrics"
linkTitle: "Prometheus Metrics"
date: 2022-02-14
description: >
  Prometheus metrics exported by Jobset
---

## Prometheus Metrics

JobSet exposes [prometheus](https://prometheus.io) metrics to monitor the health
of the controller.

## Installation Examples

The following [example](https://github.com/kubernetes-sigs/jobset/tree/main/site/static/examples/prometheus-operator) show how to install the Prometheus Operator for JobSet system.

## JobSet controller health

Use the following metrics to monitor the health of the jobset controller:

| Metric name | Type | Description | Labels |
| ----------- | ---- | ----------- | ------ |
| `controller_runtime_reconcile_errors_total` | Counter | The total number of reconciliation errors encountered by each controller. | `controller`: name of controller (i.e. use value `jobset` to obtain metrics for jobset controller) |
| `controller_runtime_reconcile_time_seconds` | Histogram | The latency of a reconciliation attempt in seconds. | `controller`: name of controller (i.e. use value `jobset` to obtain metrics for jobset controller) |

## JobSet metrics

Use the following metrics to monitor the health of the jobsets created by the jobset controller:

| Metric name                                 | Type | Description                                                               | Labels                                                          |
|---------------------------------------------| ---- |---------------------------------------------------------------------------|-----------------------------------------------------------------|
| `jobset_failed_total`                       | Counter | The total number of failed JobSets. | `jobset_name`: name of jobset, `namespace`: namespace of jobset                                |
| `jobset_completed_total`                    | Counter | The total number of completed JobSets.                                    | `jobset_name`: name of jobset, `namespace`: namespace of jobset |
