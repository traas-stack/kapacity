/*
 Copyright 2023 The Kapacity Authors.
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

package prometheus

import (
	"context"
	"fmt"
	"math"
	"time"

	prommodel "github.com/prometheus/common/model"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	promadapterclient "sigs.k8s.io/prometheus-adapter/pkg/client"
	"sigs.k8s.io/prometheus-adapter/pkg/naming"

	"github.com/traas-stack/kapacity/pkg/util"
)

var metricsListerLog = ctrl.Log.WithName("prometheus_metrics_lister")

type selectorSeries struct {
	Selector promadapterclient.Selector
	Series   []promadapterclient.Series
}

func (p *MetricProvider) Start(ctx context.Context) error {
	metricsListerLog.Info("starting metrics lister")

	wait.UntilWithContext(ctx, func(ctx context.Context) {
		if err := util.ExponentialBackoffWithContext(ctx, wait.Backoff{
			Duration: time.Second,
			Factor:   2,
			Steps:    math.MaxInt32,
			Cap:      p.metricsRelistInterval,
		}, func(ctx context.Context) (bool, error) {
			metricsListerLog.V(2).Info("start to relist metrics")
			if err := p.updateObjectSeries(ctx); err != nil {
				metricsListerLog.Error(err, "failed to update object series")
				return false, nil
			}
			if err := p.updateExternalSeries(ctx); err != nil {
				metricsListerLog.Error(err, "failed to update external series")
				return false, nil
			}
			return true, nil
		}); err != nil {
			metricsListerLog.Error(err, "backoff stopped with unexpected error")
		}
	}, p.metricsRelistInterval)

	return nil
}

func (*MetricProvider) NeedLeaderElection() bool {
	return false
}

func (p *MetricProvider) updateObjectSeries(ctx context.Context) error {
	newSeries, err := p.listAllMetrics(ctx, p.objectMetricNamers)
	if err != nil {
		return err
	}
	metricsListerLog.V(10).Info("set available object metric list from Prometheus", "newSeries", newSeries)
	return p.objectSeriesRegistry.SetSeries(newSeries, p.objectMetricNamers)
}

func (p *MetricProvider) updateExternalSeries(ctx context.Context) error {
	newSeries, err := p.listAllMetrics(ctx, p.externalMetricNamers)
	if err != nil {
		return err
	}
	metricsListerLog.V(10).Info("set available external metric list from Prometheus", "newSeries", newSeries)
	return p.externalSeriesRegistry.SetSeries(newSeries, p.externalMetricNamers)
}

func (p *MetricProvider) listAllMetrics(ctx context.Context, namers []naming.MetricNamer) ([][]promadapterclient.Series, error) {
	if len(namers) == 0 {
		return nil, nil
	}

	now := time.Now()
	startTime := now.Add(-1 * p.metricsMaxAge)

	// these can take a while on large clusters, so launch in parallel
	// and don't duplicate
	selectors := make(map[promadapterclient.Selector]struct{})
	selectorSeriesChan := make(chan selectorSeries, len(namers))
	errs := make(chan error, len(namers))
	for _, namer := range namers {
		sel := namer.Selector()
		if _, ok := selectors[sel]; ok {
			errs <- nil
			selectorSeriesChan <- selectorSeries{}
			continue
		}
		selectors[sel] = struct{}{}
		go func() {
			matches := []string{string(sel)}
			labelSets, warns, err := p.promAPI.Series(ctx, matches, startTime, now)
			if len(warns) > 0 {
				metricsListerLog.Info("got warnings from prom series", "warnings", warns, "matches", matches,
					"start", startTime, "end", now)
			}
			if err != nil {
				errs <- fmt.Errorf("unable to fetch metrics for query %q: %v", sel, err)
				return
			}
			series := make([]promadapterclient.Series, len(labelSets))
			for i := range labelSets {
				series[i] = convertLabelSetToSeries(labelSets[i])
			}
			errs <- nil
			// push into the channel: "this selector produced these series"
			selectorSeriesChan <- selectorSeries{
				Selector: sel,
				Series:   series,
			}
		}()
	}

	// don't do duplicate queries when it's just the matchers that change
	seriesCacheByQuery := make(map[promadapterclient.Selector][]promadapterclient.Series)

	// iterate through, blocking until we've got all results
	// We know that, from above, we should have pushed one item into the channel
	// for each namer. So here, we'll assume that we should receive one item per namer.
	for range namers {
		if err := <-errs; err != nil {
			return nil, fmt.Errorf("unable to update list of all metrics: %v", err)
		}
		// receive from the channel: "this selector produced these series"
		// We stuff that into this map so that we can collect the data as it arrives
		// and then, once we've received it all, we can process it below.
		if ss := <-selectorSeriesChan; ss.Series != nil {
			seriesCacheByQuery[ss.Selector] = ss.Series
		}
	}
	close(errs)

	newSeries := make([][]promadapterclient.Series, len(namers))
	for i, namer := range namers {
		series, cached := seriesCacheByQuery[namer.Selector()]
		if !cached {
			return nil, fmt.Errorf("unable to update list of all metrics: no metrics retrieved for query %q", namer.Selector())
		}
		newSeries[i] = namer.FilterSeries(series)
	}
	return newSeries, nil
}

func convertLabelSetToSeries(labelSet prommodel.LabelSet) promadapterclient.Series {
	s := promadapterclient.Series{}
	if name, ok := labelSet[prommodel.MetricNameLabel]; ok {
		s.Name = string(name)
		delete(labelSet, prommodel.MetricNameLabel)
	}
	s.Labels = labelSet
	return s
}
