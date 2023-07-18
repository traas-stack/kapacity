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

package prometheus

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"
	cmaprovider "sigs.k8s.io/custom-metrics-apiserver/pkg/provider"
	"sigs.k8s.io/custom-metrics-apiserver/pkg/provider/helpers"

	"github.com/traas-stack/kapacity/pkg/metric"
)

func (p *MetricProvider) buildPromQuery(ctx context.Context, query *metric.Query) (string, *time.Duration, error) {
	switch query.Type {
	case metric.PodResourceQueryType:
		return p.buildPodResourcePromQuery(ctx, query.PodResource, "")
	case metric.ContainerResourceQueryType:
		return p.buildPodResourcePromQuery(ctx, &query.ContainerResource.PodResourceQuery, query.ContainerResource.ContainerName)
	case metric.WorkloadResourceQueryType:
		return p.buildWorkloadResourcePromQuery(query.WorkloadResource, "")
	case metric.WorkloadContainerResourceQueryType:
		return p.buildWorkloadResourcePromQuery(&query.WorkloadContainerResource.WorkloadResourceQuery, query.WorkloadContainerResource.ContainerName)
	case metric.ObjectQueryType:
		return p.buildObjectPromQuery(query.Object)
	case metric.ExternalQueryType:
		return p.buildExternalPromQuery(query.External)
	default:
		return "", nil, fmt.Errorf("unsupported query type %q", query.Type)
	}
}

func (p *MetricProvider) buildPodResourcePromQuery(ctx context.Context, prq *metric.PodResourceQuery, containerName string) (string, *time.Duration, error) {
	var rq *resourceQuery
	switch prq.ResourceName {
	case corev1.ResourceCPU:
		rq = p.cpuQuery
	case corev1.ResourceMemory:
		rq = p.memQuery
	default:
		return "", nil, fmt.Errorf("unsupported resource %q", prq.ResourceName)
	}

	var podNames []string
	if prq.Name != "" {
		podNames = []string{prq.Name}
	} else if prq.Selector != nil {
		podList := &corev1.PodList{}
		if err := p.client.List(ctx, podList, client.InNamespace(prq.Namespace), client.MatchingLabelsSelector{Selector: prq.Selector}); err != nil {
			return "", nil, fmt.Errorf("failed to list pods in namespace %q with label selector %q: %v",
				prq.Namespace, prq.Selector, err)
		}
		podNames = make([]string, 0, len(podList.Items))
		for _, pod := range podList.Items {
			podNames = append(podNames, pod.Name)
		}
	} else {
		return "", nil, fmt.Errorf("either name or selector should be specified in query")
	}

	var selector labels.Selector
	if containerName != "" {
		selector = labels.SelectorFromSet(labels.Set{rq.ContainerLabel: containerName})
	} else {
		selector = labels.Everything()
	}
	q, err := rq.ContainerQuery.Build("", schema.GroupResource{Resource: "pods"}, prq.Namespace, nil, selector, podNames...)
	return string(q), &p.resourceQueryWindow, err
}

func (p *MetricProvider) buildWorkloadResourcePromQuery(wrq *metric.WorkloadResourceQuery, containerName string) (string, *time.Duration, error) {
	var rq *resourceQuery
	switch wrq.ResourceName {
	case corev1.ResourceCPU:
		rq = p.cpuQuery
	case corev1.ResourceMemory:
		rq = p.memQuery
	default:
		return "", nil, fmt.Errorf("unsupported resource %q", wrq.ResourceName)
	}

	podNamePattern, ok := p.workloadPodNamePatternMap[wrq.GroupKind]
	if !ok {
		return "", nil, fmt.Errorf("unknown workload type %q", wrq.GroupKind)
	}
	podNamePattern = fmt.Sprintf(podNamePattern, wrq.Name)

	var selector labels.Selector
	if containerName != "" {
		selector = labels.SelectorFromSet(labels.Set{rq.ContainerLabel: containerName})
	} else {
		selector = labels.Everything()
	}
	// Ignore error here to skip validation because we are not building a real k8s label selector, but a pseudo one
	// to let prom adapter build a regex match promql selector.
	r, _ := labels.NewRequirement(metric.LabelPodName, selection.In, []string{podNamePattern})
	selector = selector.Add(*r)
	q, err := rq.ContainerQuery.BuildExternal("", wrq.Namespace, metric.LabelNamespace, nil, selector)
	return string(q), &p.resourceQueryWindow, err
}

func (p *MetricProvider) buildObjectPromQuery(oq *metric.ObjectQuery) (string, *time.Duration, error) {
	mapping, err := p.client.RESTMapper().RESTMapping(oq.GroupKind)
	if err != nil {
		return "", nil, fmt.Errorf("failed to map kind %q to resource: %v", oq.GroupKind, err)
	}
	info := cmaprovider.CustomMetricInfo{
		GroupResource: mapping.Resource.GroupResource(),
		Namespaced:    oq.Namespace != "",
		Metric:        oq.Metric.Name,
	}

	selector, err := metav1.LabelSelectorAsSelector(oq.Metric.Selector)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse metric selector as label selector: %v", err)
	}

	var objectNames []string
	if oq.Name != "" {
		objectNames = []string{oq.Name}
	} else if oq.Selector != nil {
		objectNames, err = helpers.ListObjectNames(p.client.RESTMapper(), p.dynamicClient, oq.Namespace, oq.Selector, info)
		if err != nil {
			return "", nil, fmt.Errorf("failed to list objects of kind %q in namespace %q with label selector %q: %v",
				oq.GroupKind, oq.Namespace, oq.Selector, err)
		}
	} else {
		return "", nil, fmt.Errorf("either name or selector should be specified in query")
	}

	q, err := p.objectSeriesRegistry.QueryForMetric(info, oq.Namespace, selector, objectNames...)
	return q, nil, err
}

func (p *MetricProvider) buildExternalPromQuery(eq *metric.ExternalQuery) (string, *time.Duration, error) {
	selector, err := metav1.LabelSelectorAsSelector(eq.Metric.Selector)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse metric selector as label selector: %v", err)
	}

	q, err := p.externalSeriesRegistry.QueryForMetric(eq.Namespace, eq.Metric.Name, selector)
	return q, nil, err
}
