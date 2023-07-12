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

package service

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
	k8sautoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/traas-stack/kapacity/pkg/metric"
	"github.com/traas-stack/kapacity/pkg/metric/provider"
	"github.com/traas-stack/kapacity/pkg/metric/service/api"
	"github.com/traas-stack/kapacity/pkg/util"
)

// ProviderServer is a gRPC server of provider.Interface.
type ProviderServer struct {
	metricProvider provider.Interface
	api.UnimplementedProviderServiceServer
}

// NewProviderServer create a new instance of ProviderServer
func NewProviderServer(metricProvider provider.Interface) *ProviderServer {
	return &ProviderServer{
		metricProvider: metricProvider,
	}
}

// RegisterTo register the service to the server
func (s *ProviderServer) RegisterTo(sr grpc.ServiceRegistrar) {
	api.RegisterProviderServiceServer(sr, s)
}

func (s *ProviderServer) QueryLatest(ctx context.Context, req *api.QueryLatestRequest) (*api.QueryLatestResponse, error) {
	query, err := convertAPIQueryToInternalQuery(req.GetQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to convert api query to internal query: %v", err)
	}

	samples, err := s.metricProvider.QueryLatest(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query latest metrics: %v", err)
	}

	apiSamples := make([]*api.Sample, 0, len(samples))
	for _, sample := range samples {
		apiSamples = append(apiSamples, convertInternalSampleToAPISample(sample))
	}
	return &api.QueryLatestResponse{
		Samples: apiSamples,
	}, nil
}

func (s *ProviderServer) Query(ctx context.Context, req *api.QueryRequest) (*api.QueryResponse, error) {
	query, err := convertAPIQueryToInternalQuery(req.GetQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to convert api query to internal query: %v", err)
	}

	start := req.GetStart().AsTime()
	end := req.GetEnd().AsTime()
	step := req.GetStep().AsDuration()
	series, err := s.metricProvider.Query(ctx, query, start, end, step)
	if err != nil {
		return nil, fmt.Errorf("failed to query metrics: %v", err)
	}

	apiSeries := make([]*api.Series, 0, len(series))
	for _, s := range series {
		apiSeries = append(apiSeries, convertInternalSeriesToAPISeries(s))
	}
	return &api.QueryResponse{
		Series: apiSeries,
	}, nil
}

func convertAPIQueryToInternalQuery(in *api.Query) (*metric.Query, error) {
	queryType, err := convertAPIQueryTypeToInternalQueryType(in.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed to convert api query type to internal query type: %v", err)
	}
	out := &metric.Query{
		Type: queryType,
	}
	switch out.Type {
	case metric.PodResourceQueryType:
		podResourceQuery := in.GetPodResource()
		var (
			ls  labels.Selector
			err error
		)
		if podResourceQuery.GetName() == "" {
			ls, err = labels.Parse(podResourceQuery.GetSelector())
			if err != nil {
				return nil, fmt.Errorf("failed to parse selector %q of pod resource query: %v", podResourceQuery.GetSelector(), err)
			}
		}
		out.PodResource = &metric.PodResourceQuery{
			Namespace:    podResourceQuery.GetNamespace(),
			Name:         podResourceQuery.GetName(),
			Selector:     ls,
			ResourceName: corev1.ResourceName(podResourceQuery.GetResourceName()),
		}
	case metric.ContainerResourceQueryType:
		containerResourceQuery := in.GetContainerResource()
		var (
			ls  labels.Selector
			err error
		)
		if containerResourceQuery.GetName() == "" {
			ls, err = labels.Parse(containerResourceQuery.GetSelector())
			if err != nil {
				return nil, fmt.Errorf("failed to parse selector %q of container resource query: %v", containerResourceQuery.GetSelector(), err)
			}
		}
		out.ContainerResource = &metric.ContainerResourceQuery{
			PodResourceQuery: metric.PodResourceQuery{
				Namespace:    containerResourceQuery.GetNamespace(),
				Name:         containerResourceQuery.GetName(),
				Selector:     ls,
				ResourceName: corev1.ResourceName(containerResourceQuery.GetResourceName()),
			},
			ContainerName: containerResourceQuery.GetContainerName(),
		}
	case metric.WorkloadResourceQueryType:
		workloadResourceQuery := in.GetWorkloadResource()
		out.WorkloadResource = &metric.WorkloadResourceQuery{
			GroupKind: schema.GroupKind{
				Group: workloadResourceQuery.GetGroupKind().GetGroup(),
				Kind:  workloadResourceQuery.GetGroupKind().GetKind(),
			},
			Namespace:    workloadResourceQuery.GetNamespace(),
			Name:         workloadResourceQuery.GetName(),
			ResourceName: corev1.ResourceName(workloadResourceQuery.GetResourceName()),
		}
	case metric.WorkloadContainerResourceQueryType:
		workloadContainerResourceQuery := in.GetWorkloadContainerResource()
		out.WorkloadContainerResource = &metric.WorkloadContainerResourceQuery{
			WorkloadResourceQuery: metric.WorkloadResourceQuery{
				GroupKind: schema.GroupKind{
					Group: workloadContainerResourceQuery.GetGroupKind().GetGroup(),
					Kind:  workloadContainerResourceQuery.GetGroupKind().GetKind(),
				},
				Namespace:    workloadContainerResourceQuery.GetNamespace(),
				Name:         workloadContainerResourceQuery.GetName(),
				ResourceName: corev1.ResourceName(workloadContainerResourceQuery.GetResourceName()),
			},
			ContainerName: workloadContainerResourceQuery.GetContainerName(),
		}
	case metric.ObjectQueryType:
		objectQuery := in.GetObject()
		var (
			ls       labels.Selector
			metricLs *metav1.LabelSelector
			err      error
		)
		if objectQuery.GetName() == "" {
			ls, err = labels.Parse(objectQuery.GetSelector())
			if err != nil {
				return nil, fmt.Errorf("failed to parse selector %q of object query: %v", objectQuery.GetSelector(), err)
			}
		}
		if objectQuery.GetMetric() != nil && objectQuery.GetMetric().GetSelector() != "" {
			metricLs, err = metav1.ParseToLabelSelector(objectQuery.GetMetric().GetSelector())
			if err != nil {
				return nil, fmt.Errorf("failed to parse metric selector %q of object query: %v", objectQuery.GetMetric().GetSelector(), err)
			}
		}
		out.Object = &metric.ObjectQuery{
			GroupKind: schema.GroupKind{
				Group: objectQuery.GetGroupKind().GetGroup(),
				Kind:  objectQuery.GetGroupKind().GetKind(),
			},
			Namespace: objectQuery.GetNamespace(),
			Name:      objectQuery.GetName(),
			Selector:  ls,
			Metric: k8sautoscalingv2.MetricIdentifier{
				Name:     objectQuery.GetMetric().GetName(),
				Selector: metricLs,
			},
		}
	case metric.ExternalQueryType:
		externalQuery := in.GetExternal()
		var (
			ls  *metav1.LabelSelector
			err error
		)
		if externalQuery.GetMetric() != nil && externalQuery.GetMetric().GetSelector() != "" {
			ls, err = metav1.ParseToLabelSelector(externalQuery.GetMetric().GetSelector())
			if err != nil {
				return nil, fmt.Errorf("failed to parse metric selector %q of external query: %v", externalQuery.GetMetric().GetSelector(), err)
			}
		}
		out.External = &metric.ExternalQuery{
			Namespace: externalQuery.GetNamespace(),
			Metric: k8sautoscalingv2.MetricIdentifier{
				Name:     externalQuery.GetMetric().GetName(),
				Selector: ls,
			},
		}
	}

	return out, nil
}

func convertAPIQueryTypeToInternalQueryType(in api.QueryType) (metric.QueryType, error) {
	var out metric.QueryType
	switch in {
	case api.QueryType_POD_RESOURCE:
		out = metric.PodResourceQueryType
	case api.QueryType_CONTAINER_RESOURCE:
		out = metric.ContainerResourceQueryType
	case api.QueryType_WORKLOAD_RESOURCE:
		out = metric.WorkloadResourceQueryType
	case api.QueryType_WORKLOAD_CONTAINER_RESOURCE:
		out = metric.WorkloadContainerResourceQueryType
	case api.QueryType_OBJECT:
		out = metric.ObjectQueryType
	case api.QueryType_EXTERNAL:
		out = metric.ExternalQueryType
	default:
		return "", fmt.Errorf("unknown query type %q", in.String())
	}
	return out, nil
}

func convertInternalSampleToAPISample(in *metric.Sample) *api.Sample {
	out := &api.Sample{
		Point: &api.Point{
			Timestamp: int64(in.Timestamp),
			Value:     in.Value,
		},
		Labels: util.ConvertPromLabelSetToMap(in.Labels),
	}
	if in.Window != nil {
		out.Window = durationpb.New(*in.Window)
	}
	return out
}

func convertInternalSeriesToAPISeries(in *metric.Series) *api.Series {
	points := make([]*api.Point, 0, len(in.Points))
	for _, p := range in.Points {
		points = append(points, &api.Point{
			Timestamp: int64(p.Timestamp),
			Value:     p.Value,
		})
	}
	out := &api.Series{
		Points: points,
		Labels: util.ConvertPromLabelSetToMap(in.Labels),
	}
	if in.Window != nil {
		out.Window = durationpb.New(*in.Window)
	}
	return out
}
