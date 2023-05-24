# Installation

We provide several ways to install Kapacity in your Kubernetes clusters:

## Pre-requisites

- Go 1.19+
- Kubernetes 1.19+
- Cert-manager 1.11+
- Helm 3.1.0

## Steps

### Installing with Helm

#### Install

- Add Helm repo

```bash
helm repo add kapacity https://kapacity.github.io/charts
```

- Update Helm repo

```bash
helm repo update
```

- Install kapacity Helm chart

```bash
kubectl create namespace kapacity-system
helm install kapacity kapacity --namespace kapacity-system
```

#### Uninstall

```
helm uninstall kapacity -n kapacity-system
```

### Installing with kubectl

We provide CRD and related resource in [kapacity-deploy.yaml](../examples/kapacity-deploy.yaml), you can install Kapacity by downloading the
file and executing kubectl command.

#### Install

```
kubectl apply -f kapacity-deploy.yaml
```

#### Uninstall

```
kubectl delete -f kapacity-deploy.yaml
```