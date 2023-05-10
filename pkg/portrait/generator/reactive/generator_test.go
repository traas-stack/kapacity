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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	appsv1beta2 "k8s.io/api/apps/v1beta2"
	k8sautoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	fakedisco "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/dynamic"
	fakerest "k8s.io/client-go/rest/fake"
	"k8s.io/client-go/restmapper"
	corescale "k8s.io/client-go/scale"
	coretesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
	"github.com/traas-stack/kapacity/pkg/metric/provider/metricsapi"
	"github.com/traas-stack/kapacity/pkg/scale"
)

var (
	codecs = serializer.NewCodecFactory(corescale.NewScaleConverter().Scheme())
)

type generateHorizontalTestCase struct {
	metricsTestCase
	targetReplicas int32
}

func TestGenerateHorizontal(t *testing.T) {
	testCases := []*generateHorizontalTestCase{
		{
			metricsTestCase: metricsTestCase{
				resourceName: corev1.ResourceCPU,
				podMetricsMap: map[string][]int64{
					buildPodName(1): {300, 500, 700},
				},
				timestamp: time.Now(),
			},
			targetReplicas: 5,
		},
	}
	scaleTargetRef := k8sautoscalingv2.CrossVersionObjectReference{Kind: "Deployment", APIVersion: "apps/v1beta2", Name: "foo"}
	fakeClient := fake.NewClientBuilder().WithObjects(preparePod(1)).Build()
	algorithm := autoscalingv1alpha1.PortraitAlgorithm{Type: autoscalingv1alpha1.KubeHPAPortraitAlgorithmType}
	metricSpecs := prepareMetricSpec(corev1.ResourceCPU, 30)
	fakeScaler := prepareScaleClient(t)

	for _, testCase := range testCases {
		fakeMetricProvider := metricsapi.NewMetricProvider(
			prepareFakeMetricsClient(testCase.resourceName, testCase.podMetricsMap, testCase.timestamp).MetricsV1beta1(),
		)

		portraitGenerator := NewPortraitGenerator(fakeClient, fakeMetricProvider, fakeScaler)
		assert.NotNil(t, portraitGenerator)

		portraitData, _, _ := portraitGenerator.GenerateHorizontal(context.TODO(), testNamespace, scaleTargetRef, metricSpecs, algorithm)
		assert.NotNil(t, portraitData)
		assert.NotNil(t, portraitData.Static)
		assert.Equal(t, testCase.targetReplicas, portraitData.Static.Replicas)

	}
}

func prepareScaleClient(t *testing.T) *scale.Scaler {
	fakeDiscoveryClient := &fakedisco.FakeDiscovery{Fake: &coretesting.Fake{}}
	fakeDiscoveryClient.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: appsv1beta2.SchemeGroupVersion.String(),
			APIResources: []metav1.APIResource{
				{Name: "deployments", Namespaced: true, Kind: "Deployment"},
				{Name: "deployments/scale", Namespaced: true, Kind: "Scale", Group: "apps", Version: "v1beta2"},
			},
		},
	}

	restMapperRes, err := restmapper.GetAPIGroupResources(fakeDiscoveryClient)
	if err != nil {
		t.Fatalf("unexpected error while constructing resource list from fake discovery client: %v", err)
	}
	restMapper := restmapper.NewDiscoveryRESTMapper(restMapperRes)

	appsV1beta2Scale := &appsv1beta2.Scale{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Scale",
			APIVersion: appsv1beta2.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
		Spec: appsv1beta2.ScaleSpec{Replicas: 10},
		Status: appsv1beta2.ScaleStatus{
			Replicas:       10,
			TargetSelector: fmt.Sprintf("name=%s", podNamePrefix),
		},
	}

	resourcePaths := map[string]runtime.Object{
		fmt.Sprintf("/apis/apps/v1beta2/namespaces/%s/deployments/foo/scale", testNamespace): appsV1beta2Scale,
	}

	defaultHeaders := http.Header{}
	defaultHeaders.Set("Content-Type", runtime.ContentTypeJSON)

	fakeReqHandler := func(req *http.Request) (*http.Response, error) {
		scale, isScalePath := resourcePaths[req.URL.Path]
		if !isScalePath {
			return nil, fmt.Errorf("unexpected request for URL %q with method %q", req.URL.String(), req.Method)
		}

		switch req.Method {
		case "GET":
			res, err := json.Marshal(scale)
			if err != nil {
				return nil, err
			}
			return &http.Response{StatusCode: http.StatusOK, Header: defaultHeaders, Body: ioutil.NopCloser(bytes.NewReader(res))}, nil
		default:
			return nil, fmt.Errorf("unexpected request for URL %q with method %q", req.URL.String(), req.Method)
		}
	}

	fakeClient := &fakerest.RESTClient{
		Client:               fakerest.CreateHTTPClient(fakeReqHandler),
		NegotiatedSerializer: codecs.WithoutConversion(),
		GroupVersion:         schema.GroupVersion{},
		VersionedAPIPath:     "/not/a/real/path",
	}
	resolver := corescale.NewDiscoveryScaleKindResolver(fakeDiscoveryClient)
	client := corescale.New(fakeClient, restMapper, dynamic.LegacyAPIPathResolverFunc, resolver)
	return scale.NewScaler(client, restMapper)
}

func preparePod(index int) *corev1.Pod {
	now := metav1.Now()
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      buildPodName(index),
			Labels: map[string]string{
				"name": podNamePrefix,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: buildContainerName(podNamePrefix, index),
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1.0"),
						},
					},
				},
			},
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

func prepareMetricSpec(name corev1.ResourceName, targetAverageUtilization int32) []autoscalingv1alpha1.MetricSpec {
	return []autoscalingv1alpha1.MetricSpec{
		{
			MetricSpec: k8sautoscalingv2.MetricSpec{
				Type: k8sautoscalingv2.ResourceMetricSourceType,
				Resource: &k8sautoscalingv2.ResourceMetricSource{
					Name: name,
					Target: k8sautoscalingv2.MetricTarget{
						Type:               k8sautoscalingv2.UtilizationMetricType,
						AverageUtilization: &targetAverageUtilization,
					},
				},
			},
		},
	}
}
