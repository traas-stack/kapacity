/*
 Copyright 2023 The Kapacity Authors.

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package metric

import (
	k8sautoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type QueryType string

const (
	// PodResourceQueryType is for resource metrics (such as those specified in requests and limits, e.g. CPU or memory)
	// known to Kubernetes describing each pod.
	PodResourceQueryType QueryType = "PodResource"
	// ContainerResourceQueryType is for resource metrics (such as those specified in requests and limits, e.g. CPU or memory)
	// known to Kubernetes describing a specific container in each pod.
	ContainerResourceQueryType QueryType = "ContainerResource"
	// WorkloadResourceQueryType is for resource metrics (such as those specified in requests and limits, e.g. CPU or memory)
	// known to Kubernetes describing each group of pods belonging to the same workload.
	WorkloadResourceQueryType QueryType = "WorkloadResource"
	// WorkloadContainerResourceQueryType is for resource metrics (such as those specified in requests and limits, e.g. CPU or memory)
	// known to Kubernetes describing a specific container in each group of pods belonging to the same workload.
	WorkloadContainerResourceQueryType QueryType = "WorkloadContainerResource"
	// ObjectQueryType is for metrics describing a single Kubernetes object
	// (e.g. hits-per-second on an Ingress object).
	ObjectQueryType QueryType = "Object"
	// ExternalQueryType is for global metrics that are not associated with any Kubernetes object
	// (e.g. length of queue in cloud messaging service or QPS from loadbalancer running outside of cluster).
	ExternalQueryType QueryType = "External"
	// WorkloadExternalQueryType is for global metrics describing each group of pods belonging to the same workload
	// (e.g. the total number of ready pods).
	WorkloadExternalQueryType QueryType = "WorkloadExternal"
)

// Query represents a query for a specific type of metrics.
type Query struct {
	Type                      QueryType
	PodResource               *PodResourceQuery
	ContainerResource         *ContainerResourceQuery
	WorkloadResource          *WorkloadResourceQuery
	WorkloadContainerResource *WorkloadContainerResourceQuery
	Object                    *ObjectQuery
	External                  *ExternalQuery
	WorkloadExternal          *WorkloadExternalQuery
}

type PodResourceQuery struct {
	Namespace    string
	Name         string
	Selector     labels.Selector
	ResourceName corev1.ResourceName
}

type ContainerResourceQuery struct {
	PodResourceQuery
	ContainerName string
}

type WorkloadResourceQuery struct {
	GroupKind    schema.GroupKind
	Namespace    string
	Name         string
	ResourceName corev1.ResourceName
}

type WorkloadContainerResourceQuery struct {
	WorkloadResourceQuery
	ContainerName string
}

type ObjectQuery struct {
	GroupKind schema.GroupKind
	Namespace string
	Name      string
	Selector  labels.Selector
	Metric    k8sautoscalingv2.MetricIdentifier
}

type ExternalQuery struct {
	Namespace string
	Metric    k8sautoscalingv2.MetricIdentifier
}

type WorkloadExternalQuery struct {
	GroupKind schema.GroupKind
	Namespace string
	Name      string
	Metric    k8sautoscalingv2.MetricIdentifier
}
