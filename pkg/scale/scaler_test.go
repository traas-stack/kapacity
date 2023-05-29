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

package scale

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1beta1 "k8s.io/api/apps/v1beta1"
	appsv1beta2 "k8s.io/api/apps/v1beta2"
	k8sautoscalingv1 "k8s.io/api/autoscaling/v1"
	k8sautoscalingv2 "k8s.io/api/autoscaling/v2"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
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
)

var (
	codecs = serializer.NewCodecFactory(corescale.NewScaleConverter().Scheme())
)

func bytesBody(bodyBytes []byte) io.ReadCloser {
	return io.NopCloser(bytes.NewReader(bodyBytes))
}

func defaultHeaders() http.Header {
	header := http.Header{}
	header.Set("Content-Type", runtime.ContentTypeJSON)
	return header
}

func fakeScaleClient(t *testing.T) (corescale.ScalesGetter, apimeta.RESTMapper) {
	fakeDiscoveryClient := &fakedisco.FakeDiscovery{Fake: &coretesting.Fake{}}
	fakeDiscoveryClient.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: appsv1beta2.SchemeGroupVersion.String(),
			APIResources: []metav1.APIResource{
				{Name: "deployments", Namespaced: true, Kind: "Deployment"},
				{Name: "deployments/scale", Namespaced: true, Kind: "Scale", Group: "apps", Version: "v1beta2"},
			},
		},
		{
			GroupVersion: appsv1beta1.SchemeGroupVersion.String(),
			APIResources: []metav1.APIResource{
				{Name: "statefulsets", Namespaced: true, Kind: "StatefulSet"},
				{Name: "statefulsets/scale", Namespaced: true, Kind: "Scale", Group: "apps", Version: "v1beta1"},
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
			TargetSelector: "foo=bar",
		},
	}
	appsV1beta1Scale := &appsv1beta1.Scale{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Scale",
			APIVersion: appsv1beta1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
		Spec: appsv1beta1.ScaleSpec{Replicas: 10},
		Status: appsv1beta1.ScaleStatus{
			Replicas:       10,
			TargetSelector: "foo=bar",
		},
	}

	resourcePaths := map[string]runtime.Object{
		"/apis/apps/v1beta1/namespaces/default/statefulsets/foo/scale": appsV1beta1Scale,
		"/apis/apps/v1beta2/namespaces/default/deployments/foo/scale":  appsV1beta2Scale,
	}

	fakeReqHandler := func(req *http.Request) (*http.Response, error) {
		scale, isScalePath := resourcePaths[req.URL.Path]
		if !isScalePath {
			return nil, fmt.Errorf("unexpected request for URL %q with method %q", req.URL.String(), req.Method)
		}

		switch req.Method {
		case http.MethodGet:
			res, err := json.Marshal(scale)
			if err != nil {
				return nil, err
			}
			return &http.Response{StatusCode: http.StatusOK, Header: defaultHeaders(), Body: bytesBody(res)}, nil
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
	return client, restMapper
}

func TestGetScale(t *testing.T) {
	scaleClient := NewScaler(fakeScaleClient(t))
	scaleTargetRefs := []k8sautoscalingv2.CrossVersionObjectReference{
		{Kind: "StatefulSet", APIVersion: "apps/v1beta1", Name: "foo"},
		{Kind: "Deployment", APIVersion: "apps/v1beta2", Name: "foo"},
	}

	expectedGroupResources := []schema.GroupResource{
		{Group: appsv1beta1.GroupName, Resource: "statefulsets"},
		{Group: appsv1beta2.GroupName, Resource: "deployments"},
	}
	expectedScale := &k8sautoscalingv1.Scale{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Scale",
			APIVersion: k8sautoscalingv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
		Spec: k8sautoscalingv1.ScaleSpec{Replicas: 10},
		Status: k8sautoscalingv1.ScaleStatus{
			Replicas: 10,
			Selector: "foo=bar",
		},
	}
	for index, scaleTargetRef := range scaleTargetRefs {
		scale, groupResource, err := scaleClient.GetScale(context.Background(), "default", scaleTargetRef)
		assert.Nil(t, err)
		assert.NotNil(t, scale, "should have returned a non-nil scale for %s", scaleTargetRef.String())
		assert.Equal(t, expectedScale, scale, "should have returned the expected scale for %s", scaleTargetRef.String())
		assert.Equal(t, expectedGroupResources[index], groupResource, "should have returned the expected groupResource for %s", scaleTargetRef.String())
	}
}
