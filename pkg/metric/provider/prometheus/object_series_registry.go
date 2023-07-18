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

package prometheus

import (
	"fmt"
	"sync"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	cmaprovider "sigs.k8s.io/custom-metrics-apiserver/pkg/provider"
	promadapterclient "sigs.k8s.io/prometheus-adapter/pkg/client"
	"sigs.k8s.io/prometheus-adapter/pkg/naming"
)

// objectSeriesRegistry acts as a low-level converter for transforming Kubernetes object metrics queries
// into Prometheus queries.
type objectSeriesRegistry struct {
	Mapper apimeta.RESTMapper

	mu sync.RWMutex
	// info maps metric info to information about the corresponding series
	info map[cmaprovider.CustomMetricInfo]seriesInfo
}

type seriesInfo struct {
	// SeriesName is the name of the corresponding Prometheus series.
	SeriesName string

	// Namer is the naming.MetricNamer used to name this series.
	Namer naming.MetricNamer
}

func (r *objectSeriesRegistry) QueryForMetric(metricInfo cmaprovider.CustomMetricInfo, namespace string, metricSelector labels.Selector, resourceNames ...string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(resourceNames) == 0 {
		return "", fmt.Errorf("no resource names requested while producing a query for metric %v", metricInfo)
	}

	metricInfo, _, err := metricInfo.Normalized(r.Mapper)
	if err != nil {
		return "", fmt.Errorf("unable to normalize group resource while producing a query: %v", err)
	}

	info, infoFound := r.info[metricInfo]
	if !infoFound {
		return "", fmt.Errorf("metric %v not registered", metricInfo)
	}

	query, err := info.Namer.QueryForSeries(info.SeriesName, metricInfo.GroupResource, namespace, metricSelector, resourceNames...)
	return string(query), err
}

// SetSeries replaces the known series in registry.
// Each slice in series should correspond to a naming.MetricNamer in namers.
func (r *objectSeriesRegistry) SetSeries(newSeriesSlices [][]promadapterclient.Series, namers []naming.MetricNamer) error {
	if len(newSeriesSlices) != len(namers) {
		return fmt.Errorf("need one set of series per namer")
	}

	newInfo := make(map[cmaprovider.CustomMetricInfo]seriesInfo)
	for i, newSeries := range newSeriesSlices {
		namer := namers[i]
		for _, series := range newSeries {
			resources, namespaced := namer.ResourcesForSeries(series)
			name, err := namer.MetricNameForSeries(series)
			if err != nil {
				metricsListerLog.Error(err, "unable to name series, skipping", "series", series.String())
				continue
			}
			for _, resource := range resources {
				info := cmaprovider.CustomMetricInfo{
					GroupResource: resource,
					Namespaced:    namespaced,
					Metric:        name,
				}

				// some metrics aren't counted as namespaced
				if resource == naming.NsGroupResource || resource == naming.NodeGroupResource || resource == naming.PVGroupResource {
					info.Namespaced = false
				}

				// we don't need to re-normalize, because the metric namer should have already normalized for us
				newInfo[info] = seriesInfo{
					SeriesName: series.Name,
					Namer:      namer,
				}
			}
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.info = newInfo

	return nil
}
