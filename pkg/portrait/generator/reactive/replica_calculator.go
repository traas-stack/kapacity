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
	"math"
	"time"

	k8sautoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
	"github.com/traas-stack/kapacity/pkg/util"
)

// replicaCalculator bundles all needed information to calculate the target amount of replicas.
type replicaCalculator struct {
	Client                        client.Client
	MetricsClient                 metricsClient
	Tolerance                     float64
	CPUInitializationPeriod       time.Duration
	DelayOfInitialReadinessStatus time.Duration
}

// ComputeReplicasForMetric computes the desired number of replicas for a specific metric specification.
func (c *replicaCalculator) ComputeReplicasForMetric(ctx context.Context, currentReplicas int32, metric autoscalingv1alpha1.MetricSpec, namespace string, selector labels.Selector) (int32, error) {
	switch metric.Type {
	case k8sautoscalingv2.ResourceMetricSourceType:
		return c.ComputeReplicasForResourceMetric(ctx, currentReplicas, metric.Resource.Target, metric.Resource.Name, namespace, "", selector)
	case k8sautoscalingv2.ContainerResourceMetricSourceType:
		return c.ComputeReplicasForResourceMetric(ctx, currentReplicas, metric.ContainerResource.Target, metric.ContainerResource.Name, namespace, metric.ContainerResource.Container, selector)
	// TODO(zqzten): support more metric types
	default:
		return 0, fmt.Errorf("unsupported metric source type %q", metric.Type)
	}
}

// ComputeReplicasForResourceMetric computes the desired number of replicas for a specific resource metric specification.
func (c *replicaCalculator) ComputeReplicasForResourceMetric(ctx context.Context, currentReplicas int32, target k8sautoscalingv2.MetricTarget,
	resourceName corev1.ResourceName, namespace string, container string, selector labels.Selector) (int32, error) {
	if target.AverageValue != nil {
		replicaCountProposal, err := c.getRawResourceReplicas(ctx, currentReplicas, target.AverageValue.MilliValue(), resourceName, namespace, selector, container)
		if err != nil {
			return 0, fmt.Errorf("failed to get %s usage: %v", resourceName, err)
		}
		return replicaCountProposal, nil
	}

	if target.AverageUtilization == nil {
		return 0, fmt.Errorf("invalid resource metric source: neither an average utilization target nor an average value (usage) target was set")
	}

	targetUtilization := *target.AverageUtilization
	replicaCountProposal, err := c.getResourceReplicas(ctx, currentReplicas, targetUtilization, resourceName, namespace, selector, container)
	if err != nil {
		return 0, fmt.Errorf("failed to get %s utilization: %v", resourceName, err)
	}
	return replicaCountProposal, nil
}

// getResourceReplicas calculates the desired replica count based on a target resource utilization percentage
// of the given resource for pods matching the given selector in the given namespace, and the current replica count.
func (c *replicaCalculator) getResourceReplicas(ctx context.Context, currentReplicas int32, targetUtilization int32, resource corev1.ResourceName, namespace string, selector labels.Selector, container string) (replicaCount int32, err error) {
	metrics, err := c.MetricsClient.GetResourceMetric(ctx, resource, namespace, selector, container)
	if err != nil {
		return 0, fmt.Errorf("unable to get metrics for resource %s: %v", resource, err)
	}

	podList := &corev1.PodList{}
	if err := c.Client.List(ctx, podList, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return 0, fmt.Errorf("unable to get pods while calculating replica count: %v", err)
	}
	if len(podList.Items) == 0 {
		return 0, fmt.Errorf("no pods returned by selector while calculating replica count")
	}

	readyPodCount, unreadyPods, missingPods, ignoredPods := groupPods(podList.Items, metrics, resource, c.CPUInitializationPeriod, c.DelayOfInitialReadinessStatus)
	removeMetricsForPods(metrics, ignoredPods)
	removeMetricsForPods(metrics, unreadyPods)
	if len(metrics) == 0 {
		return 0, fmt.Errorf("did not receive metrics for any ready pods")
	}

	requests, err := calculatePodRequests(podList.Items, container, resource)
	if err != nil {
		return 0, err
	}

	usageRatio, err := getResourceUtilizationRatio(metrics, requests, targetUtilization)
	if err != nil {
		return 0, err
	}

	scaleUpWithUnready := len(unreadyPods) > 0 && usageRatio > 1.0
	if !scaleUpWithUnready && len(missingPods) == 0 {
		if math.Abs(1.0-usageRatio) <= c.Tolerance {
			// return the current replicas if the change would be too small
			return currentReplicas, nil
		}

		// if we don't have any unready or missing pods, we can calculate the new replica count now
		return int32(math.Ceil(usageRatio * float64(readyPodCount))), nil
	}

	if len(missingPods) > 0 {
		if usageRatio < 1.0 {
			// on a scale-down, treat missing pods as using 100% (all) of the resource request
			// or the utilization target for targets higher than 100%
			fallbackUtilization := int64(util.MaxInt32(100, targetUtilization))
			for podName := range missingPods {
				metrics[podName] = &podMetric{Value: requests[podName] * fallbackUtilization / 100}
			}
		} else if usageRatio > 1.0 {
			// on a scale-up, treat missing pods as using 0% of the resource request
			for podName := range missingPods {
				metrics[podName] = &podMetric{Value: 0}
			}
		}
	}

	if scaleUpWithUnready {
		// on a scale-up, treat unready pods as using 0% of the resource request
		for podName := range unreadyPods {
			metrics[podName] = &podMetric{Value: 0}
		}
	}

	// re-run the utilization calculation with our new numbers
	newUsageRatio, err := getResourceUtilizationRatio(metrics, requests, targetUtilization)
	if err != nil {
		return 0, err
	}

	if math.Abs(1.0-newUsageRatio) <= c.Tolerance || (usageRatio < 1.0 && newUsageRatio > 1.0) || (usageRatio > 1.0 && newUsageRatio < 1.0) {
		// return the current replicas if the change would be too small,
		// or if the new usage ratio would cause a change in scale direction
		return currentReplicas, nil
	}

	newReplicas := int32(math.Ceil(newUsageRatio * float64(len(metrics))))
	if (newUsageRatio < 1.0 && newReplicas > currentReplicas) || (newUsageRatio > 1.0 && newReplicas < currentReplicas) {
		// return the current replicas if the change of metrics length would cause a change in scale direction
		return currentReplicas, nil
	}

	// return the result, where the number of replicas considered is
	// however many replicas factored into our calculation
	return newReplicas, nil
}

// getRawResourceReplicas calculates the desired replica count based on a target resource usage (as a raw milli-value)
// for pods matching the given selector in the given namespace, and the current replica count.
func (c *replicaCalculator) getRawResourceReplicas(ctx context.Context, currentReplicas int32, targetUsage int64, resource corev1.ResourceName, namespace string, selector labels.Selector, container string) (replicaCount int32, err error) {
	metrics, err := c.MetricsClient.GetResourceMetric(ctx, resource, namespace, selector, container)
	if err != nil {
		return 0, fmt.Errorf("failed to get metrics for resource %q: %v", resource, err)
	}
	return c.calcPlainMetricReplicas(ctx, metrics, currentReplicas, targetUsage, namespace, selector, resource)
}

// calcPlainMetricReplicas calculates the desired replicas for plain (i.e. non-utilization percentage) metrics.
func (c *replicaCalculator) calcPlainMetricReplicas(ctx context.Context, metrics podMetricsInfo, currentReplicas int32, targetUsage int64, namespace string, selector labels.Selector, resource corev1.ResourceName) (replicaCount int32, err error) {
	podList := &corev1.PodList{}
	if err := c.Client.List(ctx, podList, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return 0, fmt.Errorf("unable to get pods while calculating replica count: %v", err)
	}
	if len(podList.Items) == 0 {
		return 0, fmt.Errorf("no pods returned by selector while calculating replica count")
	}

	readyPodCount, unreadyPods, missingPods, ignoredPods := groupPods(podList.Items, metrics, resource, c.CPUInitializationPeriod, c.DelayOfInitialReadinessStatus)
	removeMetricsForPods(metrics, ignoredPods)
	removeMetricsForPods(metrics, unreadyPods)

	if len(metrics) == 0 {
		return 0, fmt.Errorf("did not receive metrics for any ready pods")
	}

	usageRatio := getMetricUsageRatio(metrics, targetUsage)

	scaleUpWithUnready := len(unreadyPods) > 0 && usageRatio > 1.0

	if !scaleUpWithUnready && len(missingPods) == 0 {
		if math.Abs(1.0-usageRatio) <= c.Tolerance {
			// return the current replicas if the change would be too small
			return currentReplicas, nil
		}

		// if we don't have any unready or missing pods, we can calculate the new replica count now
		return int32(math.Ceil(usageRatio * float64(readyPodCount))), nil
	}

	if len(missingPods) > 0 {
		if usageRatio < 1.0 {
			// on a scale-down, treat missing pods as using exactly the target amount
			for podName := range missingPods {
				metrics[podName] = &podMetric{Value: targetUsage}
			}
		} else {
			// on a scale-up, treat missing pods as using 0% of the resource request
			for podName := range missingPods {
				metrics[podName] = &podMetric{Value: 0}
			}
		}
	}

	if scaleUpWithUnready {
		// on a scale-up, treat unready pods as using 0% of the resource request
		for podName := range unreadyPods {
			metrics[podName] = &podMetric{Value: 0}
		}
	}

	// re-run the usage calculation with our new numbers
	newUsageRatio := getMetricUsageRatio(metrics, targetUsage)

	if math.Abs(1.0-newUsageRatio) <= c.Tolerance || (usageRatio < 1.0 && newUsageRatio > 1.0) || (usageRatio > 1.0 && newUsageRatio < 1.0) {
		// return the current replicas if the change would be too small,
		// or if the new usage ratio would cause a change in scale direction
		return currentReplicas, nil
	}

	newReplicas := int32(math.Ceil(newUsageRatio * float64(len(metrics))))
	if (newUsageRatio < 1.0 && newReplicas > currentReplicas) || (newUsageRatio > 1.0 && newReplicas < currentReplicas) {
		// return the current replicas if the change of metrics length would cause a change in scale direction
		return currentReplicas, nil
	}

	// return the result, where the number of replicas considered is
	// however many replicas factored into our calculation
	return newReplicas, nil
}

func groupPods(pods []corev1.Pod, metrics podMetricsInfo, resource corev1.ResourceName, cpuInitializationPeriod, delayOfInitialReadinessStatus time.Duration) (readyPodCount int, unreadyPods, missingPods, ignoredPods sets.String) {
	missingPods = sets.NewString()
	unreadyPods = sets.NewString()
	ignoredPods = sets.NewString()
	for _, pod := range pods {
		if pod.DeletionTimestamp != nil || pod.Status.Phase == corev1.PodFailed {
			ignoredPods.Insert(pod.Name)
			continue
		}
		// Pending pods are ignored.
		if pod.Status.Phase == corev1.PodPending {
			unreadyPods.Insert(pod.Name)
			continue
		}
		// Pods missing metrics.
		metric, found := metrics[pod.Name]
		if !found {
			missingPods.Insert(pod.Name)
			continue
		}
		// Unready pods are ignored.
		if resource == corev1.ResourceCPU {
			var unready bool
			_, condition := util.GetPodCondition(&pod.Status, corev1.PodReady)
			if condition == nil || pod.Status.StartTime == nil {
				unready = true
			} else {
				// Pod still within possible initialisation period.
				if pod.Status.StartTime.Add(cpuInitializationPeriod).After(time.Now()) {
					// Ignore sample if pod is unready or one window of metric wasn't collected since last state transition.
					unready = condition.Status == corev1.ConditionFalse || metric.Timestamp.Before(condition.LastTransitionTime.Time.Add(metric.Window))
				} else {
					// Ignore metric if pod is unready and it has never been ready.
					unready = condition.Status == corev1.ConditionFalse && pod.Status.StartTime.Add(delayOfInitialReadinessStatus).After(condition.LastTransitionTime.Time)
				}
			}
			if unready {
				unreadyPods.Insert(pod.Name)
				continue
			}
		}
		readyPodCount++
	}
	return
}

func calculatePodRequests(pods []corev1.Pod, container string, resource corev1.ResourceName) (map[string]int64, error) {
	requests := make(map[string]int64, len(pods))
	for _, pod := range pods {
		podSum := int64(0)
		for _, c := range pod.Spec.Containers {
			if container == "" || container == c.Name {
				if containerRequest, ok := c.Resources.Requests[resource]; ok {
					podSum += containerRequest.MilliValue()
				} else {
					return nil, fmt.Errorf("missing request for %s in container %q of Pod %q", resource, c.Name, pod.Name)
				}
			}
		}
		requests[pod.Name] = podSum
	}
	return requests, nil
}

func removeMetricsForPods(metrics podMetricsInfo, pods sets.String) {
	for _, pod := range pods.UnsortedList() {
		delete(metrics, pod)
	}
}
