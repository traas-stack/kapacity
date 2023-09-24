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

package resultfetcher

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllertest"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
)

const (
	testHpNamespace = "test-ns"
	testHpName      = "test"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
}

func TestConfigMapHorizontal_FetchResult(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testHpNamespace,
			Name:      testHpName + configMapNameSuffix,
		},
		Data: map[string]string{
			configMapKeyType:       string(autoscalingv1alpha1.TimeSeriesHorizontalPortraitDataType),
			configMapKeyExpireTime: "2023-09-21T12:05:51Z",
			configMapKeyTimeSeries: `{"1695290760":3,"1695290880":6,"1695291000":9}`,
		},
	}).Build()

	configMapHorizontal := NewConfigMapHorizontal(fakeClient, nil, &controllertest.FakeInformer{})
	hpData, err := configMapHorizontal.FetchResult(context.Background(), &autoscalingv1alpha1.HorizontalPortrait{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testHpNamespace,
			Name:      testHpName,
		},
	}, &autoscalingv1alpha1.PortraitAlgorithmResultSource{})

	assert.Nil(t, err)
	assert.Equal(t, &autoscalingv1alpha1.HorizontalPortraitData{
		Type: autoscalingv1alpha1.TimeSeriesHorizontalPortraitDataType,
		TimeSeries: &autoscalingv1alpha1.TimeSeriesHorizontalPortraitData{
			TimeSeries: []autoscalingv1alpha1.ReplicaTimeSeriesPoint{
				{
					Timestamp: 1695290760,
					Replicas:  3,
				},
				{
					Timestamp: 1695290880,
					Replicas:  6,
				},
				{
					Timestamp: 1695291000,
					Replicas:  9,
				},
			},
		},
		ExpireTime: &metav1.Time{Time: time.Date(2023, 9, 21, 12, 5, 51, 0, time.UTC)},
	}, hpData)
}
