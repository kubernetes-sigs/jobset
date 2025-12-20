# values.yaml
replicaCount: 1

image:
  repository: nginx
  tag: stable
  pullPolicy: IfNotPresent

podDisruptionBudget:
  enabled: true
  minAvailable: 1
  maxUnavailable: 0