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

package reactive

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/traas-stack/kapacity/pkg/metric"
	metricprovider "github.com/traas-stack/kapacity/pkg/metric/provider"
)

// podMetric contains pod metric value (the metric values are expected to be the metric as a milli-value).
type podMetric struct {
	Timestamp time.Time
	Window    time.Duration
	Value     int64
}

// podMetricsInfo contains pod metrics as a map from pod names to podMetricsInfo.
type podMetricsInfo map[string]*podMetric

// metricsClient knows how to query a remote interface to retrieve container-level
// resource metrics as well as pod-level arbitrary metrics.
type metricsClient struct {
	MetricProvider metricprovider.Interface
}

// GetResourceMetric gets the given resource metric for all pods matching the specified selector in the given namespace.
func (c *metricsClient) GetResourceMetric(ctx context.Context, resource corev1.ResourceName, namespace string, selector labels.Selector, container string) (podMetricsInfo, error) {
	switch resource {
	case corev1.ResourceCPU:
	case corev1.ResourceMemory:
	default:
		return nil, fmt.Errorf("unsupported resource %q", resource)
	}

	prq := &metric.PodResourceQuery{
		Namespace:    namespace,
		Selector:     selector,
		ResourceName: resource,
	}
	if container != "" {
		metrics, err := c.MetricProvider.QueryLatest(ctx, &metric.Query{
			Type: metric.ContainerResourceQueryType,
			ContainerResource: &metric.ContainerResourceQuery{
				PodResourceQuery: *prq,
				ContainerName:    container,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to query lastest container resource metrics: %v", err)
		}
		if len(metrics) == 0 {
			return nil, fmt.Errorf("no metrics returned from lastest container resource metrics query")
		}
		return buildPodMetricsInfoFromSamples(ctx, metrics, resource), nil
	} else {
		metrics, err := c.MetricProvider.QueryLatest(ctx, &metric.Query{
			Type:        metric.PodResourceQueryType,
			PodResource: prq,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to query lastest pod resource metrics: %v", err)
		}
		if len(metrics) == 0 {
			return nil, fmt.Errorf("no metrics returned from lastest pod resource metrics query")
		}
		return buildPodMetricsInfoFromSamples(ctx, metrics, resource), nil
	}
}

func buildPodMetricsInfoFromSamples(ctx context.Context, samples []*metric.Sample, resource corev1.ResourceName) podMetricsInfo {
	l := log.FromContext(ctx)
	res := make(podMetricsInfo, len(samples))
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
		res[podName] = &podMetric{
			Timestamp: s.Timestamp.Time(),
			Window:    *s.Window,
			Value:     getResourceQuantityFromRawValue(resource, s.Value).MilliValue(),
		}
	}
	return res
}

func getResourceQuantityFromRawValue(name corev1.ResourceName, v float64) *resource.Quantity {
	switch name {
	case corev1.ResourceCPU:
		return resource.NewMilliQuantity(int64(v*1000.0), resource.DecimalSI)
	case corev1.ResourceMemory:
		return resource.NewMilliQuantity(int64(v*1000.0), resource.BinarySI)
	default:
		return nil
	}
}

// getResourceUtilizationRatio takes in a set of metrics, a set of matching requests,
// and a target utilization percentage, and calculates the ratio of desired to actual utilization.
func getResourceUtilizationRatio(metrics podMetricsInfo, requests map[string]int64, targetUtilization int32) (utilizationRatio float64, err error) {
	metricsTotal := int64(0)
	requestsTotal := int64(0)
	numEntries := 0

	for podName, m := range metrics {
		request, hasRequest := requests[podName]
		if !hasRequest {
			// we check for missing requests elsewhere, so assuming missing requests == extraneous metrics
			continue
		}

		metricsTotal += m.Value
		requestsTotal += request
		numEntries++
	}

	// if the set of requests is completely disjoint from the set of metrics,
	// then we could have an issue where the requests total is zero
	if requestsTotal == 0 {
		return 0, fmt.Errorf("no metrics returned matched known pods")
	}

	currentUtilization := int32((metricsTotal * 100) / requestsTotal)

	return float64(currentUtilization) / float64(targetUtilization), nil
}

// getMetricUsageRatio takes in a set of metrics and a target usage value,
// and calculates the ratio of desired to actual usage.
func getMetricUsageRatio(metrics podMetricsInfo, targetUsage int64) float64 {
	metricsTotal := int64(0)
	for _, m := range metrics {
		metricsTotal += m.Value
	}

	currentUsage := metricsTotal / int64(len(metrics))

	return float64(currentUsage) / float64(targetUsage)
}
