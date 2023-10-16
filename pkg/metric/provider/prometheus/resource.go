/*
 Copyright 2023 The Kapacity Authors.
 Copyright 2018 The Kubernetes Authors.

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

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/prometheus-adapter/pkg/naming"
)

// resourceQuery represents query information for querying resource metrics for some resource, like CPU or memory.
type resourceQuery struct {
	ContainerQuery              naming.MetricsQuery
	ReadyPodsOnlyContainerQuery naming.MetricsQuery
	ContainerLabel              string
}

// newResourceQuery instantiates query information from the give configuration rule for querying
// resource metrics for some resource.
func newResourceQuery(cfg ResourceRule, mapper apimeta.RESTMapper) (*resourceQuery, error) {
	converter, err := naming.NewResourceConverter(cfg.Resources.Template, cfg.Resources.Overrides, mapper)
	if err != nil {
		return nil, fmt.Errorf("unable to construct label-resource converter: %v", err)
	}

	containerQuery, err := naming.NewMetricsQuery(cfg.ContainerQuery, converter)
	if err != nil {
		return nil, fmt.Errorf("unable to construct container metrics query: %v", err)
	}

	readyPodsOnlyContainerQuery, err := naming.NewMetricsQuery(cfg.ReadyPodsOnlyContainerQuery, converter)
	if err != nil {
		return nil, fmt.Errorf("unable to construct ready pods only container metrics query: %v", err)
	}

	return &resourceQuery{
		ContainerQuery:              containerQuery,
		ReadyPodsOnlyContainerQuery: readyPodsOnlyContainerQuery,
		ContainerLabel:              cfg.ContainerLabel,
	}, nil
}
