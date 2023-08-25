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

	promapi "github.com/prometheus/client_golang/api"
	promapiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	prommodel "github.com/prometheus/common/model"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/prometheus-adapter/pkg/naming"

	"github.com/traas-stack/kapacity/pkg/metric"
	metricprovider "github.com/traas-stack/kapacity/pkg/metric/provider"
)

// MetricProvider provides metrics from Prometheus.
type MetricProvider struct {
	client        client.Client
	dynamicClient dynamic.Interface
	promAPI       promapiv1.API

	workloadPodNamePatternMap map[schema.GroupKind]string

	cpuQuery, memQuery  *resourceQuery
	resourceQueryWindow time.Duration

	metricsRelistInterval time.Duration
	metricsMaxAge         time.Duration

	objectMetricNamers   []naming.MetricNamer
	objectSeriesRegistry *objectSeriesRegistry

	externalMetricNamers   []naming.MetricNamer
	externalSeriesRegistry *externalSeriesRegistry
}

// NewMetricProvider creates a new MetricProvider with the given Prometheus client and rate metrics calculating window.
func NewMetricProvider(client client.Client, dynamicClient dynamic.Interface, promClient promapi.Client, metricsConfig *MetricsDiscoveryConfig, metricsRelistInterval, metricsMaxAge time.Duration) (metricprovider.Interface, error) {
	workloadPodNamePatternMap := make(map[schema.GroupKind]string, len(metricsConfig.WorkloadPodNamePatterns))
	for _, p := range metricsConfig.WorkloadPodNamePatterns {
		workloadPodNamePatternMap[schema.GroupKind{Group: p.Group, Kind: p.Kind}] = p.Pattern
	}

	resourceRules := metricsConfig.ResourceRules
	if resourceRules == nil {
		return nil, fmt.Errorf("missing resource rules in metrics discovery config")
	}
	cpuQuery, err := newResourceQuery(resourceRules.CPU, client.RESTMapper())
	if err != nil {
		return nil, fmt.Errorf("unable to construct querier for CPU metrics: %v", err)
	}
	memQuery, err := newResourceQuery(resourceRules.Memory, client.RESTMapper())
	if err != nil {
		return nil, fmt.Errorf("unable to construct querier for memory metrics: %v", err)
	}

	objectMetricNamers, err := naming.NamersFromConfig(metricsConfig.Rules, client.RESTMapper())
	if err != nil {
		return nil, fmt.Errorf("unable to construct object metrics naming scheme from metrics rules: %v", err)
	}
	externalMetricNamers, err := naming.NamersFromConfig(metricsConfig.ExternalRules, client.RESTMapper())
	if err != nil {
		return nil, fmt.Errorf("unable to construct external metrics naming scheme from metrics rules: %v", err)
	}

	return &MetricProvider{
		client:                    client,
		dynamicClient:             dynamicClient,
		promAPI:                   promapiv1.NewAPI(promClient),
		workloadPodNamePatternMap: workloadPodNamePatternMap,
		cpuQuery:                  cpuQuery,
		memQuery:                  memQuery,
		resourceQueryWindow:       time.Duration(resourceRules.Window),
		metricsRelistInterval:     metricsRelistInterval,
		metricsMaxAge:             metricsMaxAge,
		objectMetricNamers:        objectMetricNamers,
		objectSeriesRegistry:      &objectSeriesRegistry{Mapper: client.RESTMapper()},
		externalMetricNamers:      externalMetricNamers,
		externalSeriesRegistry:    &externalSeriesRegistry{},
	}, nil
}

func (p *MetricProvider) QueryLatest(ctx context.Context, query *metric.Query) ([]*metric.Sample, error) {
	l := log.FromContext(ctx)

	promQuery, window, err := p.buildPromQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to build prom queries: %v", err)
	}

	now := time.Now()
	res, warns, err := p.promAPI.Query(ctx, promQuery, now)
	if len(warns) > 0 {
		l.Info("got warnings from prom query", "warnings", warns, "query", promQuery, "time", now)
	}
	if err != nil {
		return nil, fmt.Errorf("prom query failed with query %q at %v: %v", promQuery, now, err)
	}

	samples, err := p.convertPromResultToSamples(res, window)
	if err != nil {
		return nil, fmt.Errorf("failed to convert prom result %q to metric samples: %v", res.String(), err)
	}
	return samples, nil
}

func (p *MetricProvider) Query(ctx context.Context, query *metric.Query, start, end time.Time, step time.Duration) ([]*metric.Series, error) {
	l := log.FromContext(ctx)

	promQuery, window, err := p.buildPromQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to build prom queries: %v", err)
	}

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

	series, err := p.convertPromResultToSeries(res, window)
	if err != nil {
		return nil, fmt.Errorf("failed to convert prom result %q to metric series: %v", res.String(), err)
	}
	return series, nil
}

func (p *MetricProvider) convertPromResultToSamples(value prommodel.Value, window *time.Duration) ([]*metric.Sample, error) {
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
		samples = append(samples, p.convertPromSampleToSample(promSample, window))
	}
	return samples, nil
}

func (p *MetricProvider) convertPromResultToSeries(value prommodel.Value, window *time.Duration) ([]*metric.Series, error) {
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
		series = append(series, p.convertPromSampleStreamToSeries(promSampleStream, window))
	}
	return series, nil
}

func (p *MetricProvider) convertPromSampleToSample(promSample *prommodel.Sample, window *time.Duration) *metric.Sample {
	return &metric.Sample{
		Point: metric.Point{
			Timestamp: promSample.Timestamp,
			Value:     float64(promSample.Value),
		},
		Labels: prommodel.LabelSet(promSample.Metric),
		Window: window,
	}
}

func (p *MetricProvider) convertPromSampleStreamToSeries(promSampleStream *prommodel.SampleStream, window *time.Duration) *metric.Series {
	points := make([]metric.Point, 0, len(promSampleStream.Values))
	for _, promSamplePair := range promSampleStream.Values {
		points = append(points, metric.Point{
			Timestamp: promSamplePair.Timestamp,
			Value:     float64(promSamplePair.Value),
		})
	}
	return &metric.Series{
		Points: points,
		Labels: prommodel.LabelSet(promSampleStream.Metric),
		Window: window,
	}
}

	}
}
