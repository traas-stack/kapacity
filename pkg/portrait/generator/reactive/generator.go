/*
 Copyright 2023 The Kapacity Authors.
 Copyright 2016 The Kubernetes Authors.

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/kubernetes/pkg/controller/podautoscaler"
	podautoscalermetrics "k8s.io/kubernetes/pkg/controller/podautoscaler/metrics"
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
	metricsClient podautoscalermetrics.MetricsClient
	podLister     corev1listers.PodLister
	scaler        *pkgscale.Scaler
}

// NewPortraitGenerator creates a new PortraitGenerator with the given Kubernetes client, metrics provider and scaler.
func NewPortraitGenerator(metricProvider metricprovider.Interface, podLister corev1listers.PodLister, scaler *pkgscale.Scaler) portraitgenerator.Interface {
	return &PortraitGenerator{
		metricsClient: NewMetricsClient(metricProvider),
		podLister:     podLister,
		scaler:        scaler,
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
	statusReplicas := scale.Status.Replicas
	selector, err := util.ParseScaleSelector(scale.Status.Selector)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse label selector %q of target's current scale: %v", scale.Status.Selector, err)
	}
	replicaCalc := podautoscaler.NewReplicaCalculator(g.metricsClient, g.podLister, tolerance, cpuInitializationPeriod, delayOfInitialReadinessStatus)

	var (
		replicas            int32 = 1
		invalidMetricsCount int
		invalidMetricError  error
	)
	for _, metric := range metrics {
		replicasProposal, err := computeReplicasForMetric(ctx, replicaCalc, specReplicas, statusReplicas, metric, namespace, selector)
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

// computeReplicasForMetric computes the desired number of replicas for the specified metric.
func computeReplicasForMetric(ctx context.Context, replicaCalc *podautoscaler.ReplicaCalculator, specReplicas, statusReplicas int32, metric autoscalingv1alpha1.MetricSpec, namespace string, selector labels.Selector) (int32, error) {
	switch metric.Type {
	case k8sautoscalingv2.ResourceMetricSourceType:
		return computeReplicasForResourceMetric(ctx, replicaCalc, specReplicas, metric.Resource.Target, metric.Resource.Name, namespace, "", selector)
	case k8sautoscalingv2.ContainerResourceMetricSourceType:
		return computeReplicasForResourceMetric(ctx, replicaCalc, specReplicas, metric.ContainerResource.Target, metric.ContainerResource.Name, namespace, metric.ContainerResource.Container, selector)
	case k8sautoscalingv2.PodsMetricSourceType:
		return computeReplicasForPodsMetric(replicaCalc, specReplicas, metric.Pods.Target, metric.Pods.Metric, namespace, selector)
	case k8sautoscalingv2.ObjectMetricSourceType:
		return computeReplicasForObjectMetric(replicaCalc, specReplicas, statusReplicas, metric.Object.Target, metric.Object.Metric, metric.Object.DescribedObject, namespace, selector)
	case k8sautoscalingv2.ExternalMetricSourceType:
		return computeReplicasForExternalMetric(replicaCalc, specReplicas, statusReplicas, metric.External.Target, metric.External.Metric, namespace, selector)
	default:
		return 0, fmt.Errorf("unsupported metric source type %q", metric.Type)
	}
}

// computeReplicasForResourceMetric computes the desired number of replicas for the specified metric of type (Container)ResourceMetricSourceType.
func computeReplicasForResourceMetric(ctx context.Context, replicaCalc *podautoscaler.ReplicaCalculator, currentReplicas int32, target k8sautoscalingv2.MetricTarget,
	resourceName corev1.ResourceName, namespace string, container string, selector labels.Selector) (int32, error) {
	if target.AverageValue != nil {
		replicaCountProposal, _, _, err := replicaCalc.GetRawResourceReplicas(ctx, currentReplicas, target.AverageValue.MilliValue(), resourceName, namespace, selector, container)
		if err != nil {
			return 0, fmt.Errorf("failed to get %s usage: %v", resourceName, err)
		}
		return replicaCountProposal, nil
	}

	if target.AverageUtilization == nil {
		return 0, fmt.Errorf("invalid resource metric source: neither an average utilization target nor an average value (usage) target was set")
	}

	targetUtilization := *target.AverageUtilization
	replicaCountProposal, _, _, _, err := replicaCalc.GetResourceReplicas(ctx, currentReplicas, targetUtilization, resourceName, namespace, selector, container)
	if err != nil {
		return 0, fmt.Errorf("failed to get %s utilization: %v", resourceName, err)
	}
	return replicaCountProposal, nil
}

// computeReplicasForPodsMetric computes the desired number of replicas for the specified metric of type PodsMetricSourceType.
func computeReplicasForPodsMetric(replicaCalc *podautoscaler.ReplicaCalculator, currentReplicas int32, target k8sautoscalingv2.MetricTarget,
	metric k8sautoscalingv2.MetricIdentifier, namespace string, selector labels.Selector) (int32, error) {
	metricSelector, err := metav1.LabelSelectorAsSelector(metric.Selector)
	if err != nil {
		return 0, fmt.Errorf("failed to parse metric selector as label selector: %v", err)
	}

	replicaCountProposal, _, _, err := replicaCalc.GetMetricReplicas(currentReplicas, target.AverageValue.MilliValue(), metric.Name, namespace, selector, metricSelector)
	if err != nil {
		return 0, fmt.Errorf("failed to get pods metric: %v", err)
	}
	return replicaCountProposal, nil
}

// computeReplicasForObjectMetric computes the desired number of replicas for the specified metric of type ObjectMetricSourceType.
func computeReplicasForObjectMetric(replicaCalc *podautoscaler.ReplicaCalculator, specReplicas, statusReplicas int32, target k8sautoscalingv2.MetricTarget,
	metric k8sautoscalingv2.MetricIdentifier, describedObject k8sautoscalingv2.CrossVersionObjectReference, namespace string, selector labels.Selector) (int32, error) {
	metricSelector, err := metav1.LabelSelectorAsSelector(metric.Selector)
	if err != nil {
		return 0, fmt.Errorf("failed to parse metric selector as label selector: %v", err)
	}

	var replicaCountProposal int32
	if target.Type == k8sautoscalingv2.ValueMetricType {
		replicaCountProposal, _, _, err = replicaCalc.GetObjectMetricReplicas(specReplicas, target.Value.MilliValue(), metric.Name, namespace, &describedObject, selector, metricSelector)
	} else if target.Type == k8sautoscalingv2.AverageValueMetricType {
		replicaCountProposal, _, _, err = replicaCalc.GetObjectPerPodMetricReplicas(statusReplicas, target.AverageValue.MilliValue(), metric.Name, namespace, &describedObject, metricSelector)
	} else {
		return 0, fmt.Errorf("invalid object metric source: neither a value target nor an average value target was set")
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get object metric: %v", err)
	}
	return replicaCountProposal, nil
}

// computeReplicasForExternalMetric computes the desired number of replicas for the specified metric of type ExternalMetricSourceType.
func computeReplicasForExternalMetric(replicaCalc *podautoscaler.ReplicaCalculator, specReplicas, statusReplicas int32, target k8sautoscalingv2.MetricTarget,
	metric k8sautoscalingv2.MetricIdentifier, namespace string, selector labels.Selector) (int32, error) {
	var (
		replicaCountProposal int32
		err                  error
	)
	if target.AverageValue != nil {
		replicaCountProposal, _, _, err = replicaCalc.GetExternalPerPodMetricReplicas(statusReplicas, target.AverageValue.MilliValue(), metric.Name, namespace, metric.Selector)
	} else if target.Value != nil {
		replicaCountProposal, _, _, err = replicaCalc.GetExternalMetricReplicas(specReplicas, target.Value.MilliValue(), metric.Name, namespace, metric.Selector, selector)
	} else {
		return 0, fmt.Errorf("invalid external metric source: neither a value target nor an average value target was set")
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get external metric: %v", err)
	}
	return replicaCountProposal, nil
}
