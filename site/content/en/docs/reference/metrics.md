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

The following [example](https://github.com/kubernetes-sigs/jobset/tree/main/examples/prometheus-operator) show how to install the Prometheus Operator for JobSet system.

## JobSet controller health

Use the following metrics to monitor the health of the jobset controller:

| Metric name | Type | Description | Labels |
| ----------- | ---- | ----------- | ------ |
| `controller_runtime_reconcile_errors_total` | Counter | The total number of reconciliation errors encountered by each controller. | `controller`: name of controller (i.e. use value `jobset` to obtain metrics for jobset controller) |
| `controller_runtime_reconcile_time_seconds` | Histogram | The latency of a reconciliation attempt in seconds. | `controller`: name of controller (i.e. use value `jobset` to obtain metrics for jobset controller) |

## JobSet metrics

Use the following metrics to monitor the health of the jobsets created by the jobset controller:

| Metric Name                                | Type    | Description                                                                                     | Labels                                                                                             |
|--------------------------------------------|---------|-------------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------|
| `jobset_failed_total`                      | Counter | The total number of failed JobSets.                                                            | `jobset_name`: Identifier for the JobSet in the format `<namespace>/<name>`, where `<namespace>` is the namespace and `<name>` is the name of the JobSet. |
| `jobset_completed_total`                   | Counter | The total number of completed JobSets.                                                         | `jobset_name`: Identifier for the JobSet in the format `<namespace>/<name>`, where `<namespace>` is the namespace and `<name>` is the name of the JobSet. |
| `jobset_terminal_state`                    | Gauge   | The current number of JobSets in a terminal state (e.g., Completed, Failed).                   | `jobset_name`: Identifier for the JobSet in the format `<namespace>/<name>`, where `<namespace>` is the namespace and `<name>` is the name of the JobSet.<br>`terminal_state`: Terminal state of the JobSet (e.g., "Completed", "Failed"). |
