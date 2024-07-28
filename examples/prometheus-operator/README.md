# Install Prometheus-operator steps 

### Install the prometheus operator

Please follow the [documentation](https://github.com/prometheus-operator/prometheus-operator/blob/main/Documentation/user-guides/getting-started.md) to install 

```bash
# Installing the prometheus operator
root@VM-0-5-ubuntu:/home/ubuntu/jobset/examples/simple# kubectl get pods
NAME                                   READY   STATUS    RESTARTS   AGE
prometheus-operator-76469b7f8c-5wb8x   1/1     Running   0          12h
```

### Install the ServiceMonitor CR for JobSet System
  
Please follow the [documentation](https://jobset.sigs.k8s.io/docs/installation/#optional-add-metrics-scraping-for-prometheus-operator) or use `make prometheus` to install ServiceMonitor CR 

```bash
root@VM-0-5-ubuntu:/home/ubuntu/jobset# make prometheus
kubectl apply --server-side -k config/prometheus
role.rbac.authorization.k8s.io/prometheus-k8s serverside-applied
rolebinding.rbac.authorization.k8s.io/prometheus-k8s serverside-applied
servicemonitor.monitoring.coreos.com/controller-manager-metrics-monitor serverside-applied
```

```bash
root@VM-0-5-ubuntu:/home/ubuntu/jobset# kubectl get ServiceMonitor -njobset-system
NAME                                 AGE
controller-manager-metrics-monitor   7d11h
```

### Install the Prometheus CR for JobSet System

```bash
root@VM-0-5-ubuntu:/home/ubuntu# kubectl apply -f rbac.yaml
serviceaccount/prometheus-jobset created
clusterrole.rbac.authorization.k8s.io/prometheus-jobset created
clusterrolebinding.rbac.authorization.k8s.io/prometheus-jobset created

root@VM-0-5-ubuntu:/home/ubuntu# kubectl apply -f prometheus.yaml
prometheus.monitoring.coreos.com/jobset-metrics created
service/jobset-metrics created
```

```bash
root@VM-0-5-ubuntu:/home/ubuntu# kubectl get pods -n jobset-system
NAME                                         READY   STATUS    RESTARTS   AGE
jobset-controller-manager-76767b599b-v8wcc   2/2     Running   0          6d22h
prometheus-jobset-metrics-0                  2/2     Running   0          17s
root@VM-0-5-ubuntu:/home/ubuntu# kubectl get svc -njobset-system
NAME                                        TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)          AGE
jobset-controller-manager-metrics-service   ClusterIP   10.96.187.196   <none>        8443/TCP         7d11h
jobset-metrics                              NodePort    10.96.217.176   <none>        9090:30900/TCP   28s
jobset-webhook-service                      ClusterIP   10.96.252.163   <none>        443/TCP          7d11h
prometheus-operated                         ClusterIP   None            <none>        9090/TCP         28s
root@VM-0-5-ubuntu:/home/ubuntu#
```

### View metrics using the prometheus UI

```bash
root@VM-0-5-ubuntu:/home/ubuntu# kubectl port-forward services/jobset-metrics  39090:9090 --address 0.0.0.0 -n jobset-system
Forwarding from 0.0.0.0:39090 -> 9090
```

If using kind, we can use port-forward, `kubectl port-forward services/jobset-metrics  39090:9090 --address 0.0.0.0 -n jobset-system`
This allows us to access prometheus using a browser: `http://{ecs public IP}:39090/graph`

![prometheus](./prometheus.png?raw=true)
