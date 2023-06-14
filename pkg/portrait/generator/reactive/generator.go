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

package reactive

import (
	"context"
	"fmt"
	"strconv"
	"time"

	k8sautoscalingv2 "k8s.io/api/autoscaling/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
	metricprovider "github.com/traas-stack/kapacity/pkg/metric/provider"
	portraitgenerator "github.com/traas-stack/kapacity/pkg/portrait/generator"
	pkgscale "github.com/traas-stack/kapacity/pkg/scale"
	"github.com/traas-stack/kapacity/pkg/util"
)

const (
	defaultSyncPeriod              = 15 * time.Second
	defaultTolerance               = 0.1
	defaultCPUInitializationPeriod = 5 * time.Minute
	defaultInitialReadinessDelay   = 30 * time.Second
)

// PortraitGenerator generates portraits reactively based on latest metrics.
type PortraitGenerator struct {
	client         client.Client
	metricProvider metricprovider.Interface
	scaler         *pkgscale.Scaler
}

// NewPortraitGenerator creates a new PortraitGenerator with the given Kubernetes client, metrics provider and scaler.
func NewPortraitGenerator(client client.Client, metricProvider metricprovider.Interface, scaler *pkgscale.Scaler) portraitgenerator.Interface {
	return &PortraitGenerator{
		client:         client,
		metricProvider: metricProvider,
		scaler:         scaler,
	}
}

func (g *PortraitGenerator) GenerateHorizontal(ctx context.Context, namespace string, scaleTargetRef k8sautoscalingv2.CrossVersionObjectReference, metrics []autoscalingv1alpha1.MetricSpec, algorithm autoscalingv1alpha1.PortraitAlgorithm) (*autoscalingv1alpha1.HorizontalPortraitData, time.Duration, error) {
	l := log.FromContext(ctx)

	switch algorithm.Type {
	case autoscalingv1alpha1.KubeHPAPortraitAlgorithmType:
	default:
		return nil, 0, fmt.Errorf("unsupported algorithm type %q", algorithm.Type)
	}

	var err error
	syncPeriod := defaultSyncPeriod
	tolerance := defaultTolerance
	cpuInitializationPeriod := defaultCPUInitializationPeriod
	delayOfInitialReadinessStatus := defaultInitialReadinessDelay
	cfg := algorithm.KubeHPA
	if cfg != nil {
		syncPeriod = cfg.SyncPeriod.Duration
		tolerance, err = strconv.ParseFloat(cfg.Tolerance, 64)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to parse tolerance of KubeHPA algorithm to float64: %v", err)
		}
		cpuInitializationPeriod = cfg.CPUInitializationPeriod.Duration
		delayOfInitialReadinessStatus = cfg.InitialReadinessDelay.Duration
	}

	scale, _, err := g.scaler.GetScale(ctx, namespace, scaleTargetRef)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get the target's current scale: %v", err)
	}
	specReplicas := scale.Spec.Replicas
	selector, err := util.ParseScaleSelector(scale.Status.Selector)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse label selector %q of target's current scale: %v", scale.Status.Selector, err)
	}
	replicaCalc := &replicaCalculator{
		Client: g.client,
		MetricsClient: metricsClient{
			MetricProvider: g.metricProvider,
		},
		Tolerance:                     tolerance,
		CPUInitializationPeriod:       cpuInitializationPeriod,
		DelayOfInitialReadinessStatus: delayOfInitialReadinessStatus,
	}

	var (
		replicas            int32 = 1
		invalidMetricsCount int
		invalidMetricError  error
	)
	for _, metric := range metrics {
		replicasProposal, err := replicaCalc.ComputeReplicasForMetric(ctx, specReplicas, metric, namespace, selector)
		if err != nil {
			l.Error(err, "failed to compute replicas for metric")
			if invalidMetricsCount == 0 {
				invalidMetricError = err
			}
			invalidMetricsCount++
			continue
		}
		if replicasProposal > replicas {
			replicas = replicasProposal
		}
	}
	// If all metrics are invalid or some are invalid and we would scale down, return error.
	if invalidMetricsCount == len(metrics) || (invalidMetricsCount > 0 && replicas < specReplicas) {
		return nil, 0, fmt.Errorf("invalid metrics (%d invalid out of %d), first error is: %v", invalidMetricsCount, len(metrics), invalidMetricError)
	}
	return &autoscalingv1alpha1.HorizontalPortraitData{
		Type: autoscalingv1alpha1.StaticHorizontalPortraitDataType,
		Static: &autoscalingv1alpha1.StaticHorizontalPortraitData{
			Replicas: replicas,
		},
	}, syncPeriod, nil
}
