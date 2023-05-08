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

package provider

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
)

var (
	scheme                    = runtime.NewScheme()
	dynamicHorizontalProvider = &autoscalingv1alpha1.HorizontalPortraitProvider{
		Type: autoscalingv1alpha1.DynamicHorizontalPortraitProviderType,
		Dynamic: &autoscalingv1alpha1.DynamicHorizontalPortraitProvider{
			PortraitSpec: autoscalingv1alpha1.PortraitSpec{
				PortraitType: autoscalingv1alpha1.ReactivePortraitType,
			},
		},
	}
)

func init() {
	_ = autoscalingv1alpha1.AddToScheme(scheme)
}

func TestDynamicHorizontal_GetPortraitIdentifier(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects().Build()
	dynamicHorizontal := NewDynamicHorizontal(fakeClient, genericEvent)
	portraitIdentifier := dynamicHorizontal.GetPortraitIdentifier(ihpa, dynamicHorizontalProvider)

	expectedPortraitIdentifier := fmt.Sprintf("%s-%s", dynamicHorizontalProvider.Type, dynamicHorizontalProvider.Dynamic.PortraitType)
	assert.Equal(t, expectedPortraitIdentifier, portraitIdentifier)
}

func TestDynamicHorizontal_UpdatePortraitSpec(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects().Build()
	ctx := context.Background()
	hpName := buildHorizontalPortraitName(ihpa.Name, dynamicHorizontalProvider.Dynamic.PortraitType)
	hpNamespacedName := types.NamespacedName{
		Namespace: ihpa.Namespace,
		Name:      hpName,
	}

	// There was no portrait before the update
	hp := &autoscalingv1alpha1.HorizontalPortrait{}
	err := fakeClient.Get(ctx, hpNamespacedName, hp)
	assert.NotNil(t, err)
	assert.True(t, apierrors.IsNotFound(err))

	dynamicHorizontal := NewDynamicHorizontal(fakeClient, genericEvent)
	err = dynamicHorizontal.UpdatePortraitSpec(ctx, ihpa, dynamicHorizontalProvider)
	assert.Nil(t, err)

	// The update generates the portrait
	err = fakeClient.Get(ctx, hpNamespacedName, hp)
	assert.Nil(t, err)
	assert.Equal(t, ihpa.Namespace, hp.ObjectMeta.Namespace)
	assert.Equal(t, hpName, hp.ObjectMeta.Name)
	assert.Equal(t, dynamicHorizontalProvider.Dynamic.PortraitSpec, hp.Spec.PortraitSpec)
}

func TestDynamicHorizontal_FetchPortraitValue_StaticPortraitDataType(t *testing.T) {
	horizontalPortrait := &autoscalingv1alpha1.HorizontalPortrait{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespaceName.Namespace,
			Name:      buildHorizontalPortraitName(ihpa.Name, dynamicHorizontalProvider.Dynamic.PortraitType),
		},
		Status: autoscalingv1alpha1.HorizontalPortraitStatus{
			PortraitData: &autoscalingv1alpha1.HorizontalPortraitData{
				Type: autoscalingv1alpha1.StaticHorizontalPortraitDataType,
				Static: &autoscalingv1alpha1.StaticHorizontalPortraitData{
					Replicas: targetReplicas,
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(horizontalPortrait).Build()
	dynamicHorizontal := NewDynamicHorizontal(fakeClient, genericEvent)
	portraitValue, err := dynamicHorizontal.FetchPortraitValue(ctx, ihpa, dynamicHorizontalProvider)
	assert.Nil(t, err)
	assert.Equal(t, portraitValue.Replicas, targetReplicas)
}

func TestDynamicHorizontal_FetchPortraitValue_CronPortraitDataType(t *testing.T) {
	horizontalPortrait := &autoscalingv1alpha1.HorizontalPortrait{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespaceName.Namespace,
			Name:      buildHorizontalPortraitName(ihpa.Name, dynamicHorizontalProvider.Dynamic.PortraitType),
		},
		Status: autoscalingv1alpha1.HorizontalPortraitStatus{
			PortraitData: &autoscalingv1alpha1.HorizontalPortraitData{
				Type: autoscalingv1alpha1.CronHorizontalPortraitDataType,
				Cron: &autoscalingv1alpha1.CronHorizontalPortraitData{
					Crons: crons,
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(horizontalPortrait).Build()
	dynamicHorizontal := NewDynamicHorizontal(fakeClient, genericEvent)
	portraitValue, err := dynamicHorizontal.FetchPortraitValue(ctx, ihpa, dynamicHorizontalProvider)
	assert.Nil(t, err)
	assert.Equal(t, portraitValue.Replicas, targetReplicas)
}

func TestDynamicHorizontal_FetchPortraitValue_TimeSeriesPortraitDataType(t *testing.T) {
	now := time.Now()
	horizontalPortrait := &autoscalingv1alpha1.HorizontalPortrait{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespaceName.Namespace,
			Name:      buildHorizontalPortraitName(ihpa.Name, dynamicHorizontalProvider.Dynamic.PortraitType),
		},
		Status: autoscalingv1alpha1.HorizontalPortraitStatus{
			PortraitData: &autoscalingv1alpha1.HorizontalPortraitData{
				Type: autoscalingv1alpha1.TimeSeriesHorizontalPortraitDataType,
				TimeSeries: &autoscalingv1alpha1.TimeSeriesHorizontalPortraitData{
					TimeSeries: []autoscalingv1alpha1.ReplicaTimeSeriesPoint{
						{
							Timestamp: now.Add(-1 * time.Minute).Unix(),
							Replicas:  targetReplicas,
						},
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(horizontalPortrait).Build()
	dynamicHorizontal := NewDynamicHorizontal(fakeClient, genericEvent)
	portraitValue, err := dynamicHorizontal.FetchPortraitValue(ctx, ihpa, dynamicHorizontalProvider)
	assert.Nil(t, err)
	assert.Equal(t, portraitValue.Replicas, targetReplicas)
}

func TestDynamicHorizontal_CleanupPortrait(t *testing.T) {
	horizontalPortrait := &autoscalingv1alpha1.HorizontalPortrait{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespaceName.Namespace,
			Name:      buildHorizontalPortraitName(ihpa.Name, dynamicHorizontalProvider.Dynamic.PortraitType),
		},
		Status: autoscalingv1alpha1.HorizontalPortraitStatus{
			PortraitData: &autoscalingv1alpha1.HorizontalPortraitData{
				Type: autoscalingv1alpha1.StaticHorizontalPortraitDataType,
				Static: &autoscalingv1alpha1.StaticHorizontalPortraitData{
					Replicas: targetReplicas,
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(horizontalPortrait).Build()
	dynamicHorizontal := NewDynamicHorizontal(fakeClient, genericEvent)

	portraitIdentifier := dynamicHorizontal.GetPortraitIdentifier(ihpa, dynamicHorizontalProvider)
	err := dynamicHorizontal.CleanupPortrait(ctx, ihpa, portraitIdentifier)
	assert.Nil(t, err)

	portraitValue, _ := dynamicHorizontal.FetchPortraitValue(ctx, ihpa, dynamicHorizontalProvider)
	assert.Nil(t, portraitValue)
}
