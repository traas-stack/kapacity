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

package server

import (
	"context"
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"
	k8sautoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/traas-stack/kapacity/pkg/metric"
	"github.com/traas-stack/kapacity/pkg/metric/provider"
	"github.com/traas-stack/kapacity/pkg/metric/server/pb"
)

type MetricServiceServer struct {
	address        string
	metricProvider provider.Interface
	pb.UnimplementedMetricServiceServer
}

// NewMetricsServiceServer create a new instance of MetricServiceServer
func NewMetricsServiceServer(address string, metricProvider provider.Interface) *MetricServiceServer {
	return &MetricServiceServer{
		address:        address,
		metricProvider: metricProvider,
	}
}

func (s *MetricServiceServer) QueryLatest(ctx context.Context, in *pb.QueryLatestRequest) (*pb.QueryLatestResponse, error) {
	query, err := convertRequestQueryToProviderQuery(in.Query)
	if err != nil {
		return nil, fmt.Errorf("error when convert request to provider query: %v", err)
	}

	samples, err := s.metricProvider.QueryLatest(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("error when querying latest metrics: %v", err)
	}

	return &pb.QueryLatestResponse{
		Samples: convertProviderSamplesToServerSamples(samples),
	}, nil
}

func (s *MetricServiceServer) Query(ctx context.Context, in *pb.QueryRequest) (*pb.QueryResponse, error) {
	query, err := convertRequestQueryToProviderQuery(in.Query)
	if err != nil {
		return nil, fmt.Errorf("error when convert request to provider query: %v", err)
	}

	start := time.Unix(in.Start, 0)
	end := time.Unix(in.End, 0)
	series, err := s.metricProvider.Query(ctx, query, start, end, time.Duration(in.Step))
	if err != nil {
		return nil, fmt.Errorf("error when querying metrics: %v", err)
	}

	return &pb.QueryResponse{
		Series: convertProviderSeriesToServerSeries(series),
	}, nil
}

// Start starts a new Metrics Service gRPC Server
func (s *MetricServiceServer) Start(ctx context.Context) error {
	l := log.FromContext(ctx)
	server := grpc.NewServer()
	pb.RegisterMetricServiceServer(server, s)

	errChan := make(chan error)
	go func() {
		lis, err := net.Listen("tcp", s.address)
		if err != nil {
			errChan <- err
			return
		}

		l.V(1).Info("Starting Metrics Service gRPC Server", "address", s.address)
		if err := server.Serve(lis); err != nil {
			errChan <- err
		}
	}()
	return nil
}

// NeedLeaderElection implements a controller-runtime interface,
// which knows if a Runnable needs to be run in the leader election mode
func (s *MetricServiceServer) NeedLeaderElection() bool {
	return true
}

func convertRequestQueryToProviderQuery(in *pb.MetricQuery) (*metric.Query, error) {
	query := &metric.Query{
		Type: metric.QueryType(in.GetType().String()),
	}
	switch query.Type {
	case metric.PodResourceQueryType:
		podResourceQuery := in.GetPodResource()
		ls, err := labels.Parse(podResourceQuery.Selector)
		if err != nil {
			return nil, err
		}
		query.PodResource = &metric.PodResourceQuery{
			Namespace:    podResourceQuery.Namespace,
			Name:         podResourceQuery.Name,
			Selector:     ls,
			ResourceName: corev1.ResourceName(podResourceQuery.ResourceName),
		}
	case metric.ContainerResourceQueryType:
		containerResourceQuery := in.GetContainerResource()
		ls, err := labels.Parse(containerResourceQuery.Selector)
		if err != nil {
			return nil, err
		}
		query.ContainerResource = &metric.ContainerResourceQuery{
			PodResourceQuery: metric.PodResourceQuery{
				Namespace:    containerResourceQuery.Namespace,
				Name:         containerResourceQuery.Name,
				Selector:     ls,
				ResourceName: corev1.ResourceName(containerResourceQuery.ResourceName),
			},
			ContainerName: containerResourceQuery.ContainerName,
		}
	case metric.WorkloadResourceQueryType:
		workloadResourceQuery := in.GetWorkloadResource()
		query.WorkloadResource = &metric.WorkloadResourceQuery{
			Namespace:    workloadResourceQuery.Namespace,
			Name:         workloadResourceQuery.Name,
			Kind:         workloadResourceQuery.Kind,
			APIVersion:   workloadResourceQuery.ApiVersion,
			ResourceName: corev1.ResourceName(workloadResourceQuery.ResourceName),
		}
	case metric.ObjectQueryType:
		objectQuery := in.GetObject()
		ls, err := labels.Parse(objectQuery.Selector)
		if err != nil {
			return nil, err
		}
		metricLs, err := metav1.ParseToLabelSelector(objectQuery.Metric.Selector)
		if err != nil {
			return nil, err
		}
		query.Object = &metric.ObjectQuery{
			GroupKind: schema.GroupKind{
				Group: objectQuery.GroupKind.Group,
				Kind:  objectQuery.GroupKind.Kind,
			},
			Namespace: objectQuery.Namespace,
			Name:      objectQuery.Name,
			Selector:  ls,
			Metric: k8sautoscalingv2.MetricIdentifier{
				Name:     objectQuery.Metric.Name,
				Selector: metricLs,
			},
		}
	case metric.ExternalQueryType:
		externalQuery := in.GetExternal()
		ls, err := metav1.ParseToLabelSelector(externalQuery.Metric.Selector)
		if err != nil {
			return nil, err
		}
		query.External = &metric.ExternalQuery{
			Namespace: externalQuery.Namespace,
			Metric: k8sautoscalingv2.MetricIdentifier{
				Name:     externalQuery.Metric.Name,
				Selector: ls,
			},
		}
	}

	return query, nil
}

func convertProviderSamplesToServerSamples(in []*metric.Sample) []*pb.Sample {
	samples := make([]*pb.Sample, 0, len(in))
	for _, s := range in {
		sample := &pb.Sample{
			Point: &pb.Point{
				Timestamp: int64(s.Timestamp),
				Value:     s.Value,
			},
			Labels: s.Labels,
			Window: int64(s.Window),
		}
		samples = append(samples, sample)
	}
	return samples
}

func convertProviderSeriesToServerSeries(in []*metric.Series) []*pb.Series {
	series := make([]*pb.Series, 0, len(in))
	for _, s := range in {
		points := make([]*pb.Point, 0, len(s.Points))
		for _, p := range s.Points {
			points = append(points, &pb.Point{
				Timestamp: int64(p.Timestamp),
				Value:     p.Value,
			})
		}
		series = append(series, &pb.Series{
			Points: points,
			Labels: s.Labels,
			Window: int64(s.Window),
		})
	}
	return series
}
