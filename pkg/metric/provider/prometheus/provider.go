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

	promapi "github.com/prometheus/client_golang/api"
	promapiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	prommodel "github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/traas-stack/kapacity/pkg/metric"
	metricprovider "github.com/traas-stack/kapacity/pkg/metric/provider"
)

// MetricProvider provides metrics from Prometheus.
type MetricProvider struct {
	client  client.Client
	promAPI promapiv1.API

	window time.Duration

	podResourceUsageQueryTemplates       map[corev1.ResourceName]*template.Template
	containerResourceUsageQueryTemplates map[corev1.ResourceName]*template.Template
	workloadResourceUsageQueryTemplates  map[corev1.ResourceName]*template.Template
}

// NewMetricProvider creates a new MetricProvider with the given Prometheus client and rate metrics calculating window.
func NewMetricProvider(client client.Client, promClient promapi.Client, window time.Duration) (metricprovider.Interface, error) {
	// TODO(zqzten): make below configurable
	podCPUUsageQueryTemplate, err := template.New("pod-cpu-usage-query").Delims("<<", ">>").Parse(defaultPodCPUUsageQueryTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pod cpu usage query template: %v", err)
	}
	containerCPUUsageQueryTemplate, err := template.New("container-cpu-usage-query").Delims("<<", ">>").Parse(defaultContainerCPUUsageQueryTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse container cpu usage query template: %v", err)
	}
	workloadCPUUsageQueryTemplate, err := template.New("workload-cpu-usage-query").Delims("<<", ">>").Parse(defaultWorkloadCPUUsageQueryTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse workload cpu usage query template: %v", err)
	}

	return &MetricProvider{
		client:  client,
		promAPI: promapiv1.NewAPI(promClient),
		window:  window,
		podResourceUsageQueryTemplates: map[corev1.ResourceName]*template.Template{
			corev1.ResourceCPU: podCPUUsageQueryTemplate,
		},
		containerResourceUsageQueryTemplates: map[corev1.ResourceName]*template.Template{
			corev1.ResourceCPU: containerCPUUsageQueryTemplate,
		},
		workloadResourceUsageQueryTemplates: map[corev1.ResourceName]*template.Template{
			corev1.ResourceCPU: workloadCPUUsageQueryTemplate,
		},
	}, nil
}

func (p *MetricProvider) QueryLatest(ctx context.Context, query *metric.Query) ([]*metric.Sample, error) {
	l := log.FromContext(ctx)

	promQueries, err := p.buildPromQueries(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to build prom queries: %v", err)
	}

	result := make([]*metric.Sample, 0)
	now := time.Now()
	for _, promQuery := range promQueries {
		res, warns, err := p.promAPI.Query(ctx, promQuery, now)
		if len(warns) > 0 {
			l.Info("got warnings from prom query", "warnings", warns, "query", promQuery, "time", now)
		}
		if err != nil {
			return nil, fmt.Errorf("prom query failed with query %q at %v: %v", promQuery, now, err)
		}
		samples, err := p.convertPromResultToSamples(res)
		if err != nil {
			return nil, fmt.Errorf("failed to convert prom result %q to metric samples: %v", res.String(), err)
		}
		result = append(result, samples...)
	}

	return result, nil
}

func (p *MetricProvider) Query(ctx context.Context, query *metric.Query, start, end time.Time, step time.Duration) ([]*metric.Series, error) {
	l := log.FromContext(ctx)

	promQueries, err := p.buildPromQueries(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to build prom queries: %v", err)
	}

	result := make([]*metric.Series, 0)
	for _, promQuery := range promQueries {
		// TODO(zqzten): consider query range by shards
		res, warns, err := p.promAPI.QueryRange(ctx, promQuery, promapiv1.Range{
			Start: start,
			End:   end,
			Step:  step,
		})
		if len(warns) > 0 {
			l.Info("got warnings from prom query range", "warnings", warns, "query", promQuery,
				"start", start, "end", end, "step", step)
		}
		if err != nil {
			return nil, fmt.Errorf("prom query range failed with query %q from %v to %v with step %v: %v",
				promQuery, start, end, step, err)
		}
		series, err := p.convertPromResultToSeries(res)
		if err != nil {
			return nil, fmt.Errorf("failed to convert prom result %q to metric series: %v", res.String(), err)
		}
		result = append(result, series...)
	}

	return result, nil
}

func (p *MetricProvider) convertPromResultToSamples(value prommodel.Value) ([]*metric.Sample, error) {
	if value.Type() != prommodel.ValVector {
		return nil, fmt.Errorf("cannot convert prom value of type %q to samples", value.Type())
	}
	vector, ok := value.(prommodel.Vector)
	if !ok {
		return nil, fmt.Errorf("failed to convert prom value to vector")
	}
	samples := make([]*metric.Sample, 0)
	for _, promSample := range vector {
		if promSample == nil {
			continue
		}
		samples = append(samples, p.convertPromSampleToSample(promSample))
	}
	return samples, nil
}

func (p *MetricProvider) convertPromResultToSeries(value prommodel.Value) ([]*metric.Series, error) {
	if value.Type() != prommodel.ValMatrix {
		return nil, fmt.Errorf("cannot convert prom value of type %q to series", value.Type())
	}
	matrix, ok := value.(prommodel.Matrix)
	if !ok {
		return nil, fmt.Errorf("failed to convert prom value to matrix")
	}
	series := make([]*metric.Series, 0)
	for _, promSampleStream := range matrix {
		if promSampleStream == nil {
			continue
		}
		series = append(series, p.convertPromSampleStreamToSeries(promSampleStream))
	}
	return series, nil
}

func (p *MetricProvider) convertPromSampleToSample(promSample *prommodel.Sample) *metric.Sample {
	return &metric.Sample{
		Point: metric.Point{
			Timestamp: promSample.Timestamp,
			Value:     float64(promSample.Value),
		},
		Window: p.window,
		Labels: convertPromMetricToLabels(promSample.Metric),
	}
}

func (p *MetricProvider) convertPromSampleStreamToSeries(promSampleStream *prommodel.SampleStream) *metric.Series {
	points := make([]metric.Point, 0, len(promSampleStream.Values))
	for _, promSamplePair := range promSampleStream.Values {
		points = append(points, metric.Point{
			Timestamp: promSamplePair.Timestamp,
			Value:     float64(promSamplePair.Value),
		})
	}
	return &metric.Series{
		Points: points,
		Window: p.window,
		Labels: convertPromMetricToLabels(promSampleStream.Metric),
	}
}

func convertPromMetricToLabels(promMetric prommodel.Metric) metric.Labels {
	labels := metric.Labels{}
	for k, v := range promMetric {
		labels[string(k)] = string(v)
	}
	return labels
}
