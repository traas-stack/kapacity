# Cron Horizontal Portrait

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

### Create Cron Horizontal Portrait

```
kubectl apply -f examples/autoscaling/cron-portrait-sample.yaml
```

ihpa cr was created

```
kubectl get ihpa

NAME                   AGE
cron-portrait-sample   13s
```

The cron portrait sample is defined as follows

```yaml
apiVersion: autoscaling.kapacity.traas.io/v1alpha1
kind: IntelligentHorizontalPodAutoscaler
metadata:
  name: cron-portrait-sample
spec:
  paused: false
  minReplicas: 0
  maxReplicas: 10
  portraitProviders:
    - type: Cron
      priority: 0
      cron:
        crons:
          - name: "cron-1"
            start: "0 * * * *"
            end: "10 * * * *"
            replicas: 1
          - name: "cron-2"
            start: "10 * * * *"
            end: "20 * * * *"
            replicas: 2
          - name: "cron-3"
            start: "20 * * * *"
            end: "30 * * * *"
            replicas: 3
          - name: "cron-4"
            start: "30 * * * *"
            end: "40 * * * *"
            replicas: 4
          - name: "cron-5"
            start: "40 * * * *"
            end: "50 * * * *"
            replicas: 5
  scaleTargetRef:
    kind: StatefulSet
    name: nginx
    apiVersion: apps/v1
```

Kapacity will expand/shrink through the cron expression. The cron portrait sample above will update the replica every 10
minutes.

```
kubectl describe ihpa cron-portrait-sample

Events:
  Type     Reason                Age                From             Message
  ----     ------                ----               ----             -------
  Normal   CreateReplicaProfile  38m                ihpa_controller  create ReplicaProfile with onlineReplcas: 3, cutoffReplicas: 0, standbyReplicas: 0
  Normal   UpdateReplicaProfile  33m (x2 over 33m)  ihpa_controller  update ReplicaProfile with onlineReplcas: 3 -> 4, cutoffReplicas: 0 -> 0, standbyReplicas: 0 -> 0
  Normal   UpdateReplicaProfile  23m                ihpa_controller  update ReplicaProfile with onlineReplcas: 4 -> 5, cutoffReplicas: 0 -> 0, standbyReplicas: 0 -> 0
  Warning  NoValidPortraitValue  13m                ihpa_controller  no valid portrait value for now
  Normal   UpdateReplicaProfile  3m15s              ihpa_controller  update ReplicaProfile with onlineReplcas: 5 -> 1, cutoffReplicas: 0 -> 0, standbyReplicas: 0 -> 0
```

```
kubectl get po

NAME      READY   STATUS    RESTARTS   AGE
nginx-0   1/1     Running   0          40m
```

## Cleanup resources

```
kubectl delete -f examples/autoscaling/cron-portrait-sample.yaml 
kubectl delete -f examples/nginx-statefulset.yaml 
```