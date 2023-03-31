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
	"text/template"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/traas-stack/kapacity/pkg/metric"
	"github.com/traas-stack/kapacity/pkg/util"
)

const (
	defaultPodCPUUsageQueryTemplate = `sum by (namespace, pod) (irate(container_cpu_usage_seconds_total{namespace="<<.Namespace>>",pod="<<.PodName>>",container!="",container!="POD"}[<<.Window>>]))`

	defaultContainerCPUUsageQueryTemplate = `irate(container_cpu_usage_seconds_total{namespace="<<.Namespace>>",pod="<<.PodName>>",container="<<.ContainerName>>"}[<<.Window>>])`

	defaultWorkloadCPUUsageQueryTemplate = `sum by (namespace) (irate(container_cpu_usage_seconds_total{namespace="<<.Namespace>>",pod=~"<<.PodNameRegex>>",container!="",container!="POD"}[<<.Window>>]))`
)

type queryTemplateArgs struct {
	Window        time.Duration
	Namespace     string
	PodName       string
	PodNameRegex  string
	ContainerName string
}

func (p *MetricProvider) buildPromQueries(ctx context.Context, query *metric.Query) ([]string, error) {
	switch query.Type {
	case metric.PodResourceQueryType:
		return p.buildPodOrContainerResourcePromQueries(ctx, query.PodResource, "")
	case metric.ContainerResourceQueryType:
		return p.buildPodOrContainerResourcePromQueries(ctx, &query.ContainerResource.PodResourceQuery, query.ContainerResource.ContainerName)
	case metric.WorkloadResourceQueryType:
		return p.buildWorkloadResourcePromQueries(query.WorkloadResource)
	// TODO(zqzten): support more query types
	default:
		return nil, fmt.Errorf("unsupported query type %q", query.Type)
	}
}

func (p *MetricProvider) buildPodOrContainerResourcePromQueries(ctx context.Context, prq *metric.PodResourceQuery, containerName string) ([]string, error) {
	var (
		queryTemplate *template.Template
		ok            bool
	)
	if containerName != "" {
		queryTemplate, ok = p.containerResourceUsageQueryTemplates[prq.ResourceName]
		if !ok {
			return nil, fmt.Errorf("unsupported container resource %q", prq.ResourceName)
		}
	} else {
		queryTemplate, ok = p.podResourceUsageQueryTemplates[prq.ResourceName]
		if !ok {
			return nil, fmt.Errorf("unsupported pod resource %q", prq.ResourceName)
		}
	}

	if prq.Name != "" {
		promQuery, err := util.ExecuteTemplate(queryTemplate, &queryTemplateArgs{
			Window:        p.window,
			Namespace:     prq.Namespace,
			PodName:       prq.Name,
			ContainerName: containerName,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to build query from template %q: %v", queryTemplate.Name(), err)
		}
		return []string{promQuery}, nil
	}

	podList := &corev1.PodList{}
	if err := p.client.List(ctx, podList, client.InNamespace(prq.Namespace), client.MatchingLabelsSelector{Selector: prq.Selector}); err != nil {
		return nil, fmt.Errorf("failed to list pods in namespace %q with label selector %q: %v", prq.Namespace, prq.Selector, err)
	}
	result := make([]string, 0, len(podList.Items))
	for _, pod := range podList.Items {
		promQuery, err := util.ExecuteTemplate(queryTemplate, &queryTemplateArgs{
			Window:        p.window,
			Namespace:     pod.Namespace,
			PodName:       pod.Name,
			ContainerName: containerName,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to build query from template %q: %v", queryTemplate.Name(), err)
		}
		result = append(result, promQuery)
	}
	return result, nil
}

func (p *MetricProvider) buildWorkloadResourcePromQueries(wrq *metric.WorkloadResourceQuery) ([]string, error) {
	queryTemplate, ok := p.workloadResourceUsageQueryTemplates[wrq.ResourceName]
	if !ok {
		return nil, fmt.Errorf("unsupported workload resource %q", wrq.ResourceName)
	}
	// FIXME(zqzten): build more accurate pod name regex based on workload type
	podNameRegex := fmt.Sprintf("^%s-.+$", wrq.Name)
	promQuery, err := util.ExecuteTemplate(queryTemplate, &queryTemplateArgs{
		Window:       p.window,
		Namespace:    wrq.Namespace,
		PodNameRegex: podNameRegex,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build query from template %q: %v", queryTemplate.Name(), err)
	}
	return []string{promQuery}, nil
}
