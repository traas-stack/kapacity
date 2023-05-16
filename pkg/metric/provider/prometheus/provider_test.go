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

package prometheus

import (
	"context"
	"testing"
	"text/template"
	"time"

	promapiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	prommodel "github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/traas-stack/kapacity/pkg/metric"
	metricprovider "github.com/traas-stack/kapacity/pkg/metric/provider"
)

type fakePromAPI struct {
	timeSeries map[int64]float64
	warnings   promapiv1.Warnings
	err        error
}

func (f *fakePromAPI) Alerts(context.Context) (promapiv1.AlertsResult, error) {
	return promapiv1.AlertsResult{}, nil
}

func (f *fakePromAPI) AlertManagers(context.Context) (promapiv1.AlertManagersResult, error) {
	return promapiv1.AlertManagersResult{}, nil
}

func (f *fakePromAPI) CleanTombstones(context.Context) error {
	return nil
}

func (f *fakePromAPI) Config(context.Context) (promapiv1.ConfigResult, error) {
	return promapiv1.ConfigResult{}, nil
}

func (f *fakePromAPI) DeleteSeries(context.Context, []string, time.Time, time.Time) error {
	return nil
}

func (f *fakePromAPI) Flags(context.Context) (promapiv1.FlagsResult, error) {
	return promapiv1.FlagsResult{}, nil
}

func (f *fakePromAPI) LabelNames(context.Context, []string, time.Time, time.Time) ([]string, promapiv1.Warnings, error) {
	return nil, promapiv1.Warnings{}, nil
}

func (f *fakePromAPI) LabelValues(context.Context, string, []string, time.Time, time.Time) (model.LabelValues, promapiv1.Warnings, error) {
	return model.LabelValues{}, promapiv1.Warnings{}, nil
}

func (f *fakePromAPI) Query(context.Context, string, time.Time) (model.Value, promapiv1.Warnings, error) {
	vector := model.Vector{}
	for k, v := range f.timeSeries {
		sample := &model.Sample{
			Timestamp: prommodel.TimeFromUnix(k),
			Value:     prommodel.SampleValue(v),
		}
		vector = append(vector, sample)
	}
	return vector, f.warnings, f.err
}

func (f *fakePromAPI) QueryRange(_ context.Context, _ string, r promapiv1.Range) (model.Value, promapiv1.Warnings, error) {
	samplePairs := make([]model.SamplePair, 0, len(f.timeSeries))
	for k, v := range f.timeSeries {
		timestamp := time.Unix(k, 0)
		if timestamp.After(r.Start) && timestamp.Before(r.End) {
			sample := model.SamplePair{
				Timestamp: prommodel.TimeFromUnix(k),
				Value:     prommodel.SampleValue(v),
			}
			samplePairs = append(samplePairs, sample)
		}
	}
	return model.Matrix{
		&model.SampleStream{
			Values: samplePairs,
		},
	}, f.warnings, f.err
}

func (f *fakePromAPI) QueryExemplars(context.Context, string, time.Time, time.Time) ([]promapiv1.ExemplarQueryResult, error) {
	return nil, nil
}

func (f *fakePromAPI) Buildinfo(context.Context) (promapiv1.BuildinfoResult, error) {
	return promapiv1.BuildinfoResult{}, nil
}

func (f *fakePromAPI) Runtimeinfo(context.Context) (promapiv1.RuntimeinfoResult, error) {
	return promapiv1.RuntimeinfoResult{}, nil
}

func (f *fakePromAPI) Series(context.Context, []string, time.Time, time.Time) ([]model.LabelSet, promapiv1.Warnings, error) {
	return nil, promapiv1.Warnings{}, nil
}

func (f *fakePromAPI) Snapshot(context.Context, bool) (promapiv1.SnapshotResult, error) {
	return promapiv1.SnapshotResult{}, nil
}

func (f *fakePromAPI) Rules(context.Context) (promapiv1.RulesResult, error) {
	return promapiv1.RulesResult{}, nil
}

func (f *fakePromAPI) Targets(context.Context) (promapiv1.TargetsResult, error) {
	return promapiv1.TargetsResult{}, nil
}

func (f *fakePromAPI) TargetsMetadata(context.Context, string, string, string) ([]promapiv1.MetricMetadata, error) {
	return nil, nil
}

func (f *fakePromAPI) Metadata(context.Context, string, string) (map[string][]promapiv1.Metadata, error) {
	return nil, nil
}

func (f *fakePromAPI) TSDB(context.Context) (promapiv1.TSDBResult, error) {
	return promapiv1.TSDBResult{}, nil
}

func (f *fakePromAPI) WalReplay(context.Context) (promapiv1.WalReplayStatus, error) {
	return promapiv1.WalReplayStatus{}, nil
}

func newFakePromAPI(timeSeries map[int64]float64, warnings promapiv1.Warnings, err error) *fakePromAPI {
	return &fakePromAPI{
		timeSeries: timeSeries,
		warnings:   warnings,
		err:        err,
	}
}

type promTestCase struct {
	timeSeries map[int64]float64
	//query info
	query *metric.Query
	start time.Time
	end   time.Time
	//result map
	resultMap map[int64]float64
}

func TestQuery(t *testing.T) {
	now := time.Now()
	start := now.Add(-10 * time.Minute)
	ls, _ := labels.Parse("foo=bar")

	testCases := []promTestCase{
		{
			//podResourceType
			timeSeries: buildTimeSeries(start, 60, []float64{100.0, 200.0, 300.0, 400.0}),
			query: &metric.Query{
				Type: metric.PodResourceQueryType,
				PodResource: &metric.PodResourceQuery{
					ResourceName: corev1.ResourceCPU,
					Selector:     ls,
					Namespace:    "testNamespace",
				},
			},
			start:     start,
			end:       now,
			resultMap: buildTimeSeries(start, 60, []float64{100.0, 200.0, 300.0, 400.0}),
		},
	}

	fakeClient := fake.NewClientBuilder().WithObjects(preparePod(types.NamespacedName{
		Namespace: "testNamespace",
		Name:      "testPod",
	})).WithObjects().Build()
	for _, testCase := range testCases {
		fakeMetricClient := newFakeMetricProvider(fakeClient, testCase.timeSeries)
		series, err := fakeMetricClient.Query(context.TODO(), testCase.query, testCase.start, testCase.end, time.Minute)

		assert.Nil(t, err, "failed to query by prometheus for %v", testCase.query)
		assert.NotNil(t, series)
		for _, s := range series {
			for _, p := range s.Points {
				assert.Equal(t, testCase.resultMap[p.Timestamp.Unix()], p.Value, "unexpected result")
			}
		}
	}
}

func TestQueryLatest(t *testing.T) {
	now := time.Now()
	start := now.Add(-10 * time.Minute)
	ls, _ := labels.Parse("foo=bar")

	testCases := []promTestCase{
		{
			//podResourceType
			timeSeries: buildTimeSeries(start, 60, []float64{100.0, 200.0, 300.0, 400.0}),
			query: &metric.Query{
				Type: metric.PodResourceQueryType,
				PodResource: &metric.PodResourceQuery{
					ResourceName: corev1.ResourceCPU,
					Selector:     ls,
					Namespace:    "testNamespace",
				},
			},
			start:     start,
			end:       now,
			resultMap: buildTimeSeries(start, 60, []float64{100.0, 200.0, 300.0, 400.0}),
		},
	}

	fakeClient := fake.NewClientBuilder().WithObjects(preparePod(types.NamespacedName{
		Namespace: "testNamespace",
		Name:      "testPod",
	})).WithObjects().Build()
	for _, testCase := range testCases {
		fakeMetricClient := newFakeMetricProvider(fakeClient, testCase.timeSeries)
		samples, err := fakeMetricClient.QueryLatest(context.TODO(), testCase.query)

		assert.Nil(t, err, "failed to query latest by prometheus for %v", testCase.query)
		assert.NotNil(t, samples)
		for _, s := range samples {
			assert.Equal(t, testCase.resultMap[s.Timestamp.Unix()], s.Value, "unexpected result")
		}
	}
}

func preparePod(name types.NamespacedName) *corev1.Pod {
	now := metav1.Now()
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: name.Namespace,
			Name:      name.Name,
			Labels:    map[string]string{"foo": "bar"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
			StartTime: &now,
		},
	}
}

func newFakeMetricProvider(client client.Client, timeSeries map[int64]float64) metricprovider.Interface {
	fakePromAPI := newFakePromAPI(timeSeries, nil, nil)
	podCPUUsageQueryTemplate, _ := template.New("pod-cpu-usage-query").Delims("<<", ">>").Parse(defaultPodCPUUsageQueryTemplate)
	containerCPUUsageQueryTemplate, _ := template.New("container-cpu-usage-query").Delims("<<", ">>").Parse(defaultContainerCPUUsageQueryTemplate)
	workloadCPUUsageQueryTemplate, _ := template.New("workload-cpu-usage-query").Delims("<<", ">>").Parse(defaultWorkloadCPUUsageQueryTemplate)
	return &MetricProvider{
		client:  client,
		promAPI: fakePromAPI,
		window:  time.Minute,
		podResourceUsageQueryTemplates: map[corev1.ResourceName]*template.Template{
			corev1.ResourceCPU: podCPUUsageQueryTemplate,
		},
		containerResourceUsageQueryTemplates: map[corev1.ResourceName]*template.Template{
			corev1.ResourceCPU: containerCPUUsageQueryTemplate,
		},
		workloadResourceUsageQueryTemplates: map[corev1.ResourceName]*template.Template{
			corev1.ResourceCPU: workloadCPUUsageQueryTemplate,
		},
	}
}

func buildTimeSeries(start time.Time, stepInSecond int, values []float64) map[int64]float64 {
	timeSeries := make(map[int64]float64, 0)
	for i, v := range values {
		timestamp := start.Unix() + int64(i*stepInSecond)
		timeSeries[timestamp] = v
	}
	return timeSeries
}
