# Dynamic Horizontal Portrait

## Pre-requisites

- Kubernetes cluster
- Kapacity installed on the cluster
- Promethues

## Setup

### Create Nginx Service

Create a nginx service with 1 pod

```
cd kapacity
kubectl apply -f examples/nginx-statefulset.yaml
```

### Wait for Nginx to Deploy

```
kubectl get po

NAME      READY   STATUS    RESTARTS   AGE
nginx-0   1/1     Running   0          5s
```