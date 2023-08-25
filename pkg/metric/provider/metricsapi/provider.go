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

/*
 This file contains code derived from and/or modified from Kubernetes
 which is licensed under below license:

 Copyright 2017 The Kubernetes Authors.

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

package metricsapi

import (
	"context"
	"fmt"
	"time"

	prommodel "github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	resourceclient "k8s.io/metrics/pkg/client/clientset/versioned/typed/metrics/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/traas-stack/kapacity/pkg/metric"
	metricprovider "github.com/traas-stack/kapacity/pkg/metric/provider"
)

// MetricProvider provides metrics from the Kubernetes metrics API.
type MetricProvider struct {
	resourceMetricsClient resourceclient.MetricsV1beta1Interface
}

// NewMetricProvider creates a new MetricProvider with the given Kubernetes resource metrics client.
func NewMetricProvider(resourceMetricsClient resourceclient.MetricsV1beta1Interface) metricprovider.Interface {
	return &MetricProvider{
		resourceMetricsClient: resourceMetricsClient,
	}
}

func (p *MetricProvider) QueryLatest(ctx context.Context, query *metric.Query) ([]*metric.Sample, error) {
	switch query.Type {
	case metric.PodResourceQueryType:
		prq := query.PodResource
		metrics, err := p.getPodMetrics(ctx, prq.Namespace, prq.Name, prq.Selector)
		if err != nil {
			return nil, fmt.Errorf("failed to get pod metrics: %v", err)
		}
		return genPodResourceSamples(ctx, metrics, prq.ResourceName), nil
	case metric.ContainerResourceQueryType:
		crq := query.ContainerResource
		prq := &crq.PodResourceQuery
		metrics, err := p.getPodMetrics(ctx, prq.Namespace, prq.Name, prq.Selector)
		if err != nil {
			return nil, fmt.Errorf("failed to get pod metrics: %v", err)
		}
		return genContainerResourceSamples(metrics, prq.ResourceName, crq.ContainerName)
	// TODO(zqzten): support more query types
	default:
		return nil, fmt.Errorf("unsupported query type %q", query.Type)
	}
}

func (*MetricProvider) Query(context.Context, *metric.Query, time.Time, time.Time, time.Duration) ([]*metric.Series, error) {
	return nil, fmt.Errorf("MetricsAPI does not support this operation")
}

func (p *MetricProvider) getPodMetrics(ctx context.Context, namespace, name string, selector labels.Selector) ([]metricsv1beta1.PodMetrics, error) {
	if name != "" {
		metrics, err := p.resourceMetricsClient.PodMetricses(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return []metricsv1beta1.PodMetrics{*metrics}, nil
	}

	if selector == nil {
		return nil, fmt.Errorf("either name or selector should be specified")
	}

	metrics, err := p.resourceMetricsClient.PodMetricses(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}
	return metrics.Items, nil
}

func genPodResourceSamples(ctx context.Context, metrics []metricsv1beta1.PodMetrics, resource corev1.ResourceName) []*metric.Sample {
	l := log.FromContext(ctx)
	samples := make([]*metric.Sample, 0, len(metrics))
	for _, m := range metrics {
		podSum := int64(0)
		missing := len(m.Containers) == 0
		for _, c := range m.Containers {
			resValue, found := c.Usage[resource]
			if !found {
				missing = true
				l.V(1).Info("missing pod resource metric",
					"resource", resource, "podName", m.Name, "container", c.Name)
				break
			}
			podSum += resValue.MilliValue()
		}
		if !missing {
			samples = append(samples, buildSampleFromPodResourceMetric(m, podSum))
		}
	}
	return samples
}

func genContainerResourceSamples(metrics []metricsv1beta1.PodMetrics, resource corev1.ResourceName, container string) ([]*metric.Sample, error) {
	samples := make([]*metric.Sample, 0, len(metrics))
	for _, m := range metrics {
		containerFound := false
		for _, c := range m.Containers {
			if c.Name == container {
				containerFound = true
				if val, resFound := c.Usage[resource]; resFound {
					samples = append(samples, buildSampleFromPodResourceMetric(m, val.MilliValue()))
				}
				break
			}
		}
		if !containerFound {
			return nil, fmt.Errorf("container %q not present in metrics of pod \"%s/%s\"", container, m.Namespace, m.Name)
		}
	}
	return samples, nil
}

func buildSampleFromPodResourceMetric(m metricsv1beta1.PodMetrics, v int64) *metric.Sample {
	return &metric.Sample{
		Point: metric.Point{
			Timestamp: prommodel.Time(m.Timestamp.UnixMilli()),
			Value:     float64(v) / 1000.0,
		},
		Labels: prommodel.LabelSet{
			metric.LabelPodName: prommodel.LabelValue(m.Name),
		},
		Window: &m.Window.Duration,
	}
}
