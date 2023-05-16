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

package metricsapi

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

	"github.com/traas-stack/kapacity/pkg/metric"
)

const (
	testPod          = "test-pod"
	testNamespace    = "test-namespace"
	timeStepInMinute = time.Minute
)

type metricsTestCase struct {
	resourceName corev1.ResourceName
	// key is timestamps, values is containers metrics
	timeMetrics map[int64][]int64
	//query info
	query *metric.Query
	//sample result
	timeResultMap map[int64]float64
}

func TestQueryLatest(t *testing.T) {
	now := time.Now()
	ls, _ := labels.Parse("foo=bar")
	testCases := []*metricsTestCase{
		{
			//podResourceType
			resourceName: corev1.ResourceCPU,
			timeMetrics: map[int64][]int64{
				now.Add(-1 * timeStepInMinute).Unix(): {100, 500},
				now.Add(-2 * timeStepInMinute).Unix(): {150, 600},
				now.Add(-3 * timeStepInMinute).Unix(): {200, 700},
			},
			query: &metric.Query{
				Type: metric.PodResourceQueryType,
				PodResource: &metric.PodResourceQuery{
					Namespace:    testNamespace,
					Selector:     ls,
					ResourceName: corev1.ResourceCPU,
				},
			},
			timeResultMap: map[int64]float64{
				now.Add(-1 * timeStepInMinute).Unix(): 0.6,
				now.Add(-2 * timeStepInMinute).Unix(): 0.75,
				now.Add(-3 * timeStepInMinute).Unix(): 0.9,
			},
		},
		{
			//containerResourceType
			resourceName: corev1.ResourceCPU,
			timeMetrics: map[int64][]int64{
				now.Add(-1 * timeStepInMinute).Unix(): {100, 500},
				now.Add(-2 * timeStepInMinute).Unix(): {150, 600},
				now.Add(-3 * timeStepInMinute).Unix(): {200, 700},
			},
			query: &metric.Query{
				Type: metric.ContainerResourceQueryType,
				ContainerResource: &metric.ContainerResourceQuery{
					PodResourceQuery: metric.PodResourceQuery{
						Namespace:    testNamespace,
						Selector:     ls,
						ResourceName: corev1.ResourceCPU,
					},
					ContainerName: fmt.Sprintf("%s-container-%v", testPod, 0),
				},
			},
			timeResultMap: map[int64]float64{
				now.Add(-1 * timeStepInMinute).Unix(): 0.1,
				now.Add(-2 * timeStepInMinute).Unix(): 0.15,
				now.Add(-3 * timeStepInMinute).Unix(): 0.2,
			},
		},
	}

	for _, testCase := range testCases {
		fakeMetricsClient := prepareFakeMetricsClient(testPod, testCase.resourceName, testCase.timeMetrics)
		metricsProvider := NewMetricProvider(fakeMetricsClient.MetricsV1beta1())

		samples, err := metricsProvider.QueryLatest(context.TODO(), testCase.query)
		assert.Nil(t, err, "failed to query latest metrics")
		for _, sample := range samples {
			assert.Equal(t, testCase.timeResultMap[sample.Timestamp.Unix()], sample.Value, "unexpected result")
		}
	}
}

func TestQuery(t *testing.T) {
	now := time.Now()
	ls, _ := labels.Parse("foo=bar")
	testCases := []*metricsTestCase{
		{
			resourceName: corev1.ResourceCPU,
			query: &metric.Query{
				Type: metric.PodResourceQueryType,
				PodResource: &metric.PodResourceQuery{
					Namespace:    testNamespace,
					Selector:     ls,
					ResourceName: corev1.ResourceCPU,
				},
			},
		},
	}

	startTime := now.Add(-1 * timeStepInMinute)
	endTime := now.Add(-2 * timeStepInMinute)

	for _, testCase := range testCases {
		fakeMetricsClient := prepareFakeMetricsClient(testPod, testCase.resourceName, testCase.timeMetrics)
		metricsProvider := NewMetricProvider(fakeMetricsClient.MetricsV1beta1())

		timeSeries, err := metricsProvider.Query(context.TODO(), testCase.query, startTime, endTime, timeStepInMinute)

		assert.NotNil(t, err, "MetricsAPI does not support query operation")
		assert.Nil(t, timeSeries, "MetricsAPI does not support query operation")
	}

}

func prepareFakeMetricsClient(podName string, resourceName corev1.ResourceName, containerTimeMetrics map[int64][]int64) *metricsfake.Clientset {
	fakeMetricsClient := &metricsfake.Clientset{}
	fakeMetricsClient.AddReactor("list", "pods", func(action coretesting.Action) (handled bool, ret runtime.Object, err error) {
		metrics := &v1beta1.PodMetricsList{}

		for timestamp, containerMetrics := range containerTimeMetrics {
			podMetric := v1beta1.PodMetrics{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podName,
					Namespace: testNamespace,
					Labels:    map[string]string{"foo": "bar"},
				},
				Timestamp:  metav1.Unix(timestamp, 0),
				Window:     metav1.Duration{Duration: time.Minute},
				Containers: make([]v1beta1.ContainerMetrics, len(containerMetrics)),
			}

			for i, m := range containerMetrics {
				podMetric.Containers[i] = v1beta1.ContainerMetrics{
					Name: fmt.Sprintf("%s-container-%v", podName, i),
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
