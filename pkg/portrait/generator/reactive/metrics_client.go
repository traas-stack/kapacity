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

package reactive

import (
	"context"
	"fmt"
	"math"
	"time"

	k8sautoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	podautoscalermetrics "k8s.io/kubernetes/pkg/controller/podautoscaler/metrics"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/traas-stack/kapacity/pkg/metric"
	metricprovider "github.com/traas-stack/kapacity/pkg/metric/provider"
)

const (
	defaultMetricWindow = time.Minute
)

// NewMetricsClient creates a podautoscalermetrics.MetricsClient backed by given metric provider.
func NewMetricsClient(metricProvider metricprovider.Interface) podautoscalermetrics.MetricsClient {
	return &metricsClient{
		metricProvider: metricProvider,
	}
}

type metricsClient struct {
	metricProvider metricprovider.Interface
}

func (c *metricsClient) GetResourceMetric(ctx context.Context, resource corev1.ResourceName, namespace string, selector labels.Selector, container string) (podautoscalermetrics.PodMetricsInfo, time.Time, error) {
	switch resource {
	case corev1.ResourceCPU:
	case corev1.ResourceMemory:
	default:
		return nil, time.Time{}, fmt.Errorf("unsupported resource %q", resource)
	}
	prq := &metric.PodResourceQuery{
		Namespace:    namespace,
		Selector:     selector,
		ResourceName: resource,
	}
	if container != "" {
		metrics, err := c.metricProvider.QueryLatest(ctx, &metric.Query{
			Type: metric.ContainerResourceQueryType,
			ContainerResource: &metric.ContainerResourceQuery{
				PodResourceQuery: *prq,
				ContainerName:    container,
			},
		})
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("failed to query lastest container resource metrics: %v", err)
		}
		if len(metrics) == 0 {
			return nil, time.Time{}, fmt.Errorf("no metrics returned from lastest container resource metrics query")
		}
		return buildResourcePodMetricsInfoFromSamples(ctx, metrics, resource), metrics[0].Timestamp.Time(), nil
	} else {
		metrics, err := c.metricProvider.QueryLatest(ctx, &metric.Query{
			Type:        metric.PodResourceQueryType,
			PodResource: prq,
		})
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("failed to query lastest pod resource metrics: %v", err)
		}
		if len(metrics) == 0 {
			return nil, time.Time{}, fmt.Errorf("no metrics returned from lastest pod resource metrics query")
		}
		return buildResourcePodMetricsInfoFromSamples(ctx, metrics, resource), metrics[0].Timestamp.Time(), nil
	}
}

func (c *metricsClient) GetRawMetric(metricName string, namespace string, selector labels.Selector, metricSelector labels.Selector) (podautoscalermetrics.PodMetricsInfo, time.Time, error) {
	metricLabelSelector, err := metav1.ParseToLabelSelector(metricSelector.String())
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to parse metric selector %q to label selector: %v", metricSelector, err)
	}
	metrics, err := c.metricProvider.QueryLatest(context.TODO(), &metric.Query{
		Type: metric.ObjectQueryType,
		Object: &metric.ObjectQuery{
			GroupKind: schema.GroupKind{Kind: "Pod"},
			Namespace: namespace,
			Selector:  selector,
			Metric: k8sautoscalingv2.MetricIdentifier{
				Name:     metricName,
				Selector: metricLabelSelector,
			},
		},
	})
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to query lastest pod metrics: %v", err)
	}
	if len(metrics) == 0 {
		return nil, time.Time{}, fmt.Errorf("no metrics returned from lastest pod metrics query")
	}
	return buildPodMetricsInfoFromSamples(metrics), metrics[0].Timestamp.Time(), nil
}

func (c *metricsClient) GetObjectMetric(metricName string, namespace string, objectRef *k8sautoscalingv2.CrossVersionObjectReference, metricSelector labels.Selector) (int64, time.Time, error) {
	gvk := schema.FromAPIVersionAndKind(objectRef.APIVersion, objectRef.Kind)
	metricLabelSelector, err := metav1.ParseToLabelSelector(metricSelector.String())
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("failed to parse metric selector %q to label selector: %v", metricSelector, err)
	}
	metrics, err := c.metricProvider.QueryLatest(context.TODO(), &metric.Query{
		Type: metric.ObjectQueryType,
		Object: &metric.ObjectQuery{
			GroupKind: schema.GroupKind{
				Group: gvk.Group,
				Kind:  gvk.Kind,
			},
			Namespace: namespace,
			Name:      objectRef.Name,
			Metric: k8sautoscalingv2.MetricIdentifier{
				Name:     metricName,
				Selector: metricLabelSelector,
			},
		},
	})
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("failed to query lastest object metrics: %v", err)
	}
	if len(metrics) == 0 {
		return 0, time.Time{}, fmt.Errorf("no metrics returned from lastest object metrics query")
	}
	return getQuantityFromRawValue(metrics[0].Value).MilliValue(), metrics[0].Timestamp.Time(), nil
}

func (c *metricsClient) GetExternalMetric(metricName string, namespace string, selector labels.Selector) ([]int64, time.Time, error) {
	metricLabelSelector, err := metav1.ParseToLabelSelector(selector.String())
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to parse metric selector %q to label selector: %v", selector, err)
	}
	metrics, err := c.metricProvider.QueryLatest(context.TODO(), &metric.Query{
		Type: metric.ExternalQueryType,
		External: &metric.ExternalQuery{
			Namespace: namespace,
			Metric: k8sautoscalingv2.MetricIdentifier{
				Name:     metricName,
				Selector: metricLabelSelector,
			},
		},
	})
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to query lastest external metrics: %v", err)
	}
	if len(metrics) == 0 {
		return nil, time.Time{}, fmt.Errorf("no metrics returned from lastest external metrics query")
	}
	res := make([]int64, 0, len(metrics))
	for _, m := range metrics {
		res = append(res, getQuantityFromRawValue(m.Value).MilliValue())
	}
	return res, metrics[0].Timestamp.Time(), nil
}

func buildResourcePodMetricsInfoFromSamples(ctx context.Context, samples []*metric.Sample, resource corev1.ResourceName) podautoscalermetrics.PodMetricsInfo {
	l := log.FromContext(ctx)
	res := make(podautoscalermetrics.PodMetricsInfo, len(samples))
	for _, s := range samples {
		podName := string(s.Labels[metric.LabelPodName])
		if podName == "" {
			l.Info("met invalid metric sample without pod name label", "sample", s)
			continue
		}
		if s.Window == nil {
			l.Info("met invalid metric sample without window", "sample", s)
			continue
		}
		res[podName] = podautoscalermetrics.PodMetric{
			Timestamp: s.Timestamp.Time(),
			Window:    *s.Window,
			Value:     getResourceQuantityFromRawValue(resource, s.Value).MilliValue(),
		}
	}
	return res
}

func buildPodMetricsInfoFromSamples(samples []*metric.Sample) podautoscalermetrics.PodMetricsInfo {
	res := make(podautoscalermetrics.PodMetricsInfo, len(samples))
	for _, s := range samples {
		podName := string(s.Labels[metric.LabelPodName])
		if podName == "" {
			continue
		}
		window := defaultMetricWindow
		if s.Window != nil {
			window = *s.Window
		}
		res[podName] = podautoscalermetrics.PodMetric{
			Timestamp: s.Timestamp.Time(),
			Window:    window,
			Value:     getQuantityFromRawValue(s.Value).MilliValue(),
		}
	}
	return res
}

func getResourceQuantityFromRawValue(name corev1.ResourceName, v float64) *resource.Quantity {
	if math.IsNaN(v) || v < 0 {
		v = 0
	}
	switch name {
	case corev1.ResourceCPU:
		return resource.NewMilliQuantity(int64(v*1000.0), resource.DecimalSI)
	case corev1.ResourceMemory:
		return resource.NewMilliQuantity(int64(v*1000.0), resource.BinarySI)
	default:
		return nil
	}
}

func getQuantityFromRawValue(v float64) *resource.Quantity {
	if math.IsNaN(v) {
		return resource.NewQuantity(0, resource.DecimalSI)
	}
	return resource.NewMilliQuantity(int64(v*1000.0), resource.DecimalSI)
}
