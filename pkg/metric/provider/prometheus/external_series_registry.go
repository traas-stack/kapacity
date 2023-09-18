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
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/labels"
	promadapterclient "sigs.k8s.io/prometheus-adapter/pkg/client"
	"sigs.k8s.io/prometheus-adapter/pkg/naming"
)

// externalSeriesRegistry acts as a low-level converter for transforming external metrics queries
// into Prometheus queries.
type externalSeriesRegistry struct {
	mu sync.RWMutex
	// info maps metrics to information about the corresponding series
	info map[string]seriesInfo
}

func (r *externalSeriesRegistry) QueryForMetric(namespace string, metricName string, metricSelector labels.Selector) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, found := r.info[metricName]
	if !found {
		return "", fmt.Errorf("external metric %q not found", metricName)
	}

	query, err := info.Namer.QueryForExternalSeries(info.SeriesName, namespace, metricSelector)
	return string(query), err
}

// SetSeries replaces the known series in registry.
// Each slice in series should correspond to a naming.MetricNamer in namers.
func (r *externalSeriesRegistry) SetSeries(newSeriesSlices [][]promadapterclient.Series, namers []naming.MetricNamer) error {
	if len(newSeriesSlices) != len(namers) {
		return fmt.Errorf("need one set of series per namer")
	}

	newInfo := make(map[string]seriesInfo)
	for i, newSeries := range newSeriesSlices {
		namer := namers[i]
		for _, series := range newSeries {
			name, err := namer.MetricNameForSeries(series)
			if err != nil {
				metricsListerLog.Error(err, "unable to name series, skipping", "series", series.String())
				continue
			}
			newInfo[name] = seriesInfo{
				SeriesName: series.Name,
				Namer:      namer,
			}
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.info = newInfo

	return nil
}
