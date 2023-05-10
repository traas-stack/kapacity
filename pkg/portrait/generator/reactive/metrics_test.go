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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	coretesting "k8s.io/client-go/testing"
	"k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"

	"github.com/traas-stack/kapacity/pkg/metric/provider/metricsapi"
)

const (
	testNamespace = "test-namespace"
	podNamePrefix = "test-pod"
)

func TestGetResourceMetric_UnsupportedResource(t *testing.T) {
	metricsClient := metricsClient{}

	selector, _ := labels.Parse("foo=bar")
	_, err := metricsClient.GetResourceMetric(context.TODO(), corev1.ResourceStorage, testNamespace, selector, "test-container")
	assert.NotNil(t, err, "unsupported resource for %s", corev1.ResourceStorage)
}

func TestGetResourceMetric(t *testing.T) {
	testCases := []*metricsTestCase{
		{
			resourceName: corev1.ResourceCPU,
			podMetricsMap: map[string][]int64{
				fmt.Sprintf("%s-%d", podNamePrefix, 1): {300, 500, 700},
			},
			timestamp: time.Now(),
		},
	}

	for _, testCase := range testCases {
		fakeMetricsClient := prepareFakeMetricsClient(testCase.resourceName, testCase.podMetricsMap, testCase.timestamp)
		metricsClient := metricsClient{
			MetricProvider: metricsapi.NewMetricProvider(
				fakeMetricsClient.MetricsV1beta1(),
			),
		}

		selector, _ := labels.Parse("name=test-pod")
		// pod resources
		podMetrics, err := metricsClient.GetResourceMetric(context.TODO(), testCase.resourceName, testNamespace, selector, "")
		assert.Nil(t, err)

		for podName, resValues := range testCase.podMetricsMap {
			var expectedValue int64 = 0
			for index, containerValue := range resValues {
				expectedValue += containerValue

				// container resources
				containerName := buildContainerName(podName, index+1)
				containerMetrics, err := metricsClient.GetResourceMetric(context.TODO(), testCase.resourceName, testNamespace, selector, containerName)
				assert.Nil(t, err, "failed to get resource metrics")
				assert.NotNil(t, containerMetrics, "container metrics not found for %s", containerName)
				assert.Equal(t, containerValue, containerMetrics[podName].Value, "container metrics not expected for %s", containerName)
			}

			assert.Equal(t, expectedValue, podMetrics[podName].Value, "pod metrics not expected for %s", podName)
		}
	}
}

type metricsTestCase struct {
	resourceName corev1.ResourceName
	//key is pod name, value is container resource metric values
	podMetricsMap map[string][]int64
	timestamp     time.Time
}

func prepareFakeMetricsClient(resourceName corev1.ResourceName, podMetricsMap map[string][]int64, timestamp time.Time) *metricsfake.Clientset {
	fakeMetricsClient := &metricsfake.Clientset{}
	fakeMetricsClient.AddReactor("list", "pods", func(action coretesting.Action) (handled bool, ret runtime.Object, err error) {
		metrics := &v1beta1.PodMetricsList{}
		for podName, resValue := range podMetricsMap {
			podMetric := v1beta1.PodMetrics{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podName,
					Namespace: testNamespace,
					Labels:    map[string]string{"name": podNamePrefix},
				},
				Timestamp:  metav1.Time{Time: timestamp},
				Window:     metav1.Duration{Duration: time.Minute},
				Containers: make([]v1beta1.ContainerMetrics, len(resValue)),
			}

			for i, m := range resValue {
				podMetric.Containers[i] = v1beta1.ContainerMetrics{
					Name: buildContainerName(podName, i+1),
					Usage: corev1.ResourceList{
						resourceName: *resource.NewMilliQuantity(m, resource.DecimalSI),
					},
				}
			}
			metrics.Items = append(metrics.Items, podMetric)
		}
		return true, metrics, nil
	})
	return fakeMetricsClient
}

func buildContainerName(prefix string, index int) string {
	return fmt.Sprintf("%s-container-%v", prefix, index)
}

func buildPodName(index int) string {
	return fmt.Sprintf("%s-%v", podNamePrefix, index)
}
