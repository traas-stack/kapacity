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
	"testing"

	"github.com/stretchr/testify/assert"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
)

var (
	staticHorizontalProvider = &autoscalingv1alpha1.HorizontalPortraitProvider{
		Type: autoscalingv1alpha1.StaticHorizontalPortraitProviderType,
		Static: &autoscalingv1alpha1.StaticHorizontalPortraitProvider{
			Replicas: targetReplicas,
		},
	}
)

func TestStaticHorizontal_GetPortraitIdentifier(t *testing.T) {
	staticHorizontal := NewStaticHorizontal()
	portraitIdentifier := staticHorizontal.GetPortraitIdentifier(ihpa, staticHorizontalProvider)
	assert.Equal(t, string(staticHorizontalProvider.Type), portraitIdentifier)
}

func TestStaticHorizontal_UpdatePortraitSpec(t *testing.T) {
	staticHorizontal := NewStaticHorizontal()
	err := staticHorizontal.UpdatePortraitSpec(ctx, ihpa, staticHorizontalProvider)
	assert.Nil(t, err)
}

func TestStaticHorizontal_FetchPortraitValue(t *testing.T) {
	staticHorizontal := NewStaticHorizontal()
	err := staticHorizontal.UpdatePortraitSpec(ctx, ihpa, staticHorizontalProvider)
	assert.Nil(t, err)

	portraitValue, err := staticHorizontal.FetchPortraitValue(ctx, ihpa, staticHorizontalProvider)
	assert.Nil(t, err)
	assert.Equal(t, portraitValue.Provider, string(staticHorizontalProvider.Type))
	assert.Equal(t, portraitValue.Replicas, staticHorizontalProvider.Static.Replicas)
}

func TestStaticHorizontal_CleanupPortrait(t *testing.T) {
	staticHorizontal := NewStaticHorizontal()
	err := staticHorizontal.UpdatePortraitSpec(ctx, ihpa, staticHorizontalProvider)
	assert.Nil(t, err)

	err = staticHorizontal.CleanupPortrait(ctx, ihpa, string(staticHorizontalProvider.Type))
	assert.Nil(t, err)
}
