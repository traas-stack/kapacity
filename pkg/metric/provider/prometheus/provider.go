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
	"sync"
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

const (
	maxPointsPerPromTimeSeries = 11000
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

type shardQueryResult struct {
	Index  int
	Series []*metric.Series
	Err    error
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

	samples, err := convertPromResultToSamples(res, window)
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

	// query range by shards to avoid exceeding maximum points limitation per timeseries of prometheus api
	shards := promQueryRangeSharding(start, end, step)
	shardResCh := make(chan *shardQueryResult, len(shards))
	var wg sync.WaitGroup
	for i := range shards {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			shardRes := &shardQueryResult{Index: index}

			res, warns, err := p.promAPI.QueryRange(ctx, promQuery, shards[index])
			if len(warns) > 0 {
				l.Info("got warnings from prom query range", "warnings", warns, "query", promQuery,
					"start", start, "end", end, "step", step)
			}
			if err != nil {
				shardRes.Err = fmt.Errorf("prom query range failed with query %q from %v to %v with step %v: %v",
					promQuery, start, end, step, err)
				shardResCh <- shardRes
				return
			}

			series, err := convertPromResultToSeries(res, window)
			if err != nil {
				shardRes.Err = fmt.Errorf("failed to convert prom result %q to metric series: %v", res.String(), err)
				shardResCh <- shardRes
				return
			}
			shardRes.Series = series
			shardResCh <- shardRes
		}(i)
	}
	wg.Wait()
	close(shardResCh)

	// merge series shards by their labels
	orderedShardRes := make([]*shardQueryResult, len(shards))
	for shardRes := range shardResCh {
		orderedShardRes[shardRes.Index] = shardRes
	}
	seriesMap := make(map[string]*metric.Series)
	errs := make([]error, 0)
	for _, shardRes := range orderedShardRes {
		if shardRes.Err != nil {
			errs = append(errs, shardRes.Err)
			continue
		}
		for _, series := range shardRes.Series {
			key := series.Labels.String()
			if prevSeries, ok := seriesMap[key]; ok {
				prevSeries.Points = append(prevSeries.Points, series.Points...)
			} else {
				seriesMap[key] = series
			}
		}
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("%v", errs)
	}
	series := make([]*metric.Series, 0, len(seriesMap))
	for _, s := range seriesMap {
		series = append(series, s)
	}
	return series, nil
}

func promQueryRangeSharding(start, end time.Time, step time.Duration) []promapiv1.Range {
	shards := make([]promapiv1.Range, 0)
	for shardStart, complete := start, false; !complete; {
		shardEnd := shardStart.Add(step * maxPointsPerPromTimeSeries)
		if !shardEnd.Before(end) {
			shardEnd = end
			complete = true
		}
		shards = append(shards, promapiv1.Range{
			Start: shardStart,
			End:   shardEnd,
			Step:  step,
		})
		shardStart = shardEnd.Add(step)
		if shardStart.After(end) {
			complete = true
		}
	}
	return shards
}

func convertPromResultToSamples(value prommodel.Value, window *time.Duration) ([]*metric.Sample, error) {
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
		samples = append(samples, convertPromSampleToSample(promSample, window))
	}
	return samples, nil
}

func convertPromResultToSeries(value prommodel.Value, window *time.Duration) ([]*metric.Series, error) {
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
		series = append(series, convertPromSampleStreamToSeries(promSampleStream, window))
	}
	return series, nil
}

func convertPromSampleToSample(promSample *prommodel.Sample, window *time.Duration) *metric.Sample {
	return &metric.Sample{
		Point: metric.Point{
			Timestamp: promSample.Timestamp,
			Value:     float64(promSample.Value),
		},
		Labels: prommodel.LabelSet(promSample.Metric),
		Window: window,
	}
}

func convertPromSampleStreamToSeries(promSampleStream *prommodel.SampleStream, window *time.Duration) *metric.Series {
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
