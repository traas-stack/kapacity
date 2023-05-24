# Static Horizontal Portrait

## Pre-requisites

- Kubernetes cluster
- Kapacity installed on the cluster

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

### Expand by Static Horizontal Portrait

```
kubectl apply -f examples/autoscaling/static-portrait-sample.yaml
```

Ihpa cr was created

```
kubectl get ihpa

NAME                     AGE
static-portrait-sample   11m
```

The static portrait sample is defined as follows

```yaml
apiVersion: autoscaling.kapacity.traas.io/v1alpha1
kind: IntelligentHorizontalPodAutoscaler
metadata:
  name: static-portrait-sample
spec:
  paused: false
  minReplicas: 0
  maxReplicas: 10
  portraitProviders:
    - type: Static
      priority: 0
      static:
        replicas: 2
  scaleTargetRef:
    kind: StatefulSet
    name: nginx
    apiVersion: apps/v1
```

You can find that the nginx service has been expanded to 2 pods.

```
kubectl get po

NAME      READY   STATUS    RESTARTS   AGE
nginx-0   1/1     Running   0          7m18s
nginx-1   1/1     Running   0          7s
```

And you can also modify the ***replicas*** by editing ihpa cr.

## Cleanup resources

You can use the following command to clean up the related resources when running the sample

```
kubectl delete -f examples/autoscaling/static-portrait-sample.yaml 
kubectl delete -f examples/nginx-statefulset.yaml 
```