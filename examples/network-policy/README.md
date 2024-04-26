## Usage example
### Purpose
This document provides an example of achieving communication between different pods using a headless service with JobSet.

- apply and check whether the jobset's pod and svc start normally
```bash
root@VM-0-4-ubuntu:/home/ubuntu# vi jobset-network.yaml
root@VM-0-4-ubuntu:/home/ubuntu# kubectl apply -f jobset-network.yaml
jobset.jobset.x-k8s.io/network-jobset created
root@VM-0-4-ubuntu:/home/ubuntu# kubectl get pods
NAME                               READY   STATUS    RESTARTS   AGE   IP          NODE               NOMINATED NODE   READINESS GATES
network-jobset-leader-0-0-5xnzz    1/1     Running   0          17m   10.6.2.27   cluster1-worker    <none>           <none>
network-jobset-workers-0-0-78k9j   1/1     Running   0          17m   10.6.1.16   cluster1-worker2   <none>           <none>
network-jobset-workers-0-1-rmw42   1/1     Running   0          17m   10.6.2.28   cluster1-worker    <none>           <none>
root@VM-0-4-ubuntu:/home/ubuntu# kubectl get svc
NAME         TYPE        CLUSTER-IP   EXTERNAL-IP   PORT(S)   AGE
example      ClusterIP   None         <none>        <none>    19s
kubernetes   ClusterIP   10.96.0.1    <none>        443/TCP   2d1h
```
- Use the exec command to enter the container
- We can first check the /etc/hosts file in the container. We can see that there is a domain name, such as: network-jobset-leader-0-0.example.default.svc.cluster.local
- Other containers can access the current pod through this domain name. In the same way, we can also access the domain names of other pods for network communication.
- For example: we can access the pods of network-jobset-workers-0-0-78k9j and network-jobset-workers-0-1-rmw42 respectively
```bash
root@VM-0-4-ubuntu:/home/ubuntu# kubectl exec -it network-jobset-leader-0-0-5xnzz -- sh
/ # cat /etc/hosts
# Kubernetes-managed hosts file.
127.0.0.1	localhost
...
10.6.2.27	network-jobset-leader-0-0.example.default.svc.cluster.local	network-jobset-leader-0-0
/ # ping network-jobset-workers-0-.example.default.svc.cluster.local
ping: bad address 'network-jobset-workers-0-.example.default.svc.cluster.local'
/ # ping network-jobset-workers-0-0.example.default.svc.cluster.local
PING network-jobset-workers-0-0.example.default.svc.cluster.local (10.6.1.16): 56 data bytes
64 bytes from 10.6.1.16: seq=0 ttl=62 time=0.121 ms
64 bytes from 10.6.1.16: seq=1 ttl=62 time=0.093 ms
64 bytes from 10.6.1.16: seq=2 ttl=62 time=0.094 ms
64 bytes from 10.6.1.16: seq=3 ttl=62 time=0.103 ms
--- network-jobset-workers-0-0.example.default.svc.cluster.local ping statistics ---
4 packets transmitted, 4 packets received, 0% packet loss
round-trip min/avg/max = 0.093/0.102/0.121 ms
/ # ping network-jobset-workers-0-1.example.default.svc.cluster.local
PING network-jobset-workers-0-1.example.default.svc.cluster.local (10.6.2.28): 56 data bytes
64 bytes from 10.6.2.28: seq=0 ttl=63 time=0.068 ms
64 bytes from 10.6.2.28: seq=1 ttl=63 time=0.072 ms
64 bytes from 10.6.2.28: seq=2 ttl=63 time=0.079 ms
--- network-jobset-workers-0-1.example.default.svc.cluster.local ping statistics ---
```