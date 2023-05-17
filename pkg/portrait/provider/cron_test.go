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

	"github.com/mitchellh/hashstructure/v2"
	"github.com/stretchr/testify/assert"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
)

var (
	cronHorizontalProvider = &autoscalingv1alpha1.HorizontalPortraitProvider{
		Type: autoscalingv1alpha1.CronHorizontalPortraitProviderType,
		Cron: &autoscalingv1alpha1.CronHorizontalPortraitProvider{
			Crons: crons,
		},
	}
)

func TestCronHorizontal_GetPortraitIdentifier(t *testing.T) {
	cronHorizontal := NewCronHorizontal(genericEvent)
	portraitIdentifier := cronHorizontal.GetPortraitIdentifier(ihpa, cronHorizontalProvider)
	assert.Equal(t, string(cronHorizontalProvider.Type), portraitIdentifier)
}

func TestCronHorizontal_UpdatePortraitSpec(t *testing.T) {
	horizontal := NewCronHorizontal(genericEvent)
	cronHorizontal := horizontal.(*CronHorizontal)
	assert.Nil(t, cronHorizontal.UpdatePortraitSpec(ctx, ihpa, cronHorizontalProvider))
	defer func() {
		assert.Nil(t, cronHorizontal.CleanupPortrait(ctx, ihpa, string(cronHorizontalProvider.Type)))
	}()

	taskV, ok := cronHorizontal.cronTaskTriggerManager.cronTaskMap.Load(namespaceName)
	assert.True(t, ok)

	hash, err := hashstructure.Hash(crons, hashstructure.FormatV2, nil)
	assert.Nil(t, err)

	task := taskV.(*cronTask)
	assert.True(t, task.Hash == hash)
}

func TestCronHorizontal_FetchPortraitValue(t *testing.T) {
	horizontal := NewCronHorizontal(genericEvent)
	cronHorizontal := horizontal.(*CronHorizontal)
	assert.Nil(t, cronHorizontal.UpdatePortraitSpec(ctx, ihpa, cronHorizontalProvider))
	defer func() {
		assert.Nil(t, cronHorizontal.CleanupPortrait(ctx, ihpa, string(cronHorizontalProvider.Type)))
	}()

	portraitValue, err := cronHorizontal.FetchPortraitValue(ctx, ihpa, cronHorizontalProvider)
	assert.Nil(t, err)
	assert.Equal(t, portraitValue.Replicas, cronHorizontalProvider.Cron.Crons[0].Replicas)
	assert.Equal(t, portraitValue.Provider, string(cronHorizontalProvider.Type))
}

func TestCronHorizontal_CleanupPortrait(t *testing.T) {
	horizontal := NewCronHorizontal(genericEvent)
	cronHorizontal := horizontal.(*CronHorizontal)
	assert.Nil(t, cronHorizontal.UpdatePortraitSpec(ctx, ihpa, cronHorizontalProvider))
	assert.Nil(t, cronHorizontal.CleanupPortrait(ctx, ihpa, string(cronHorizontalProvider.Type)))

	_, ok := cronHorizontal.cronTaskTriggerManager.cronTaskMap.Load(namespaceName)
	assert.False(t, ok)
}
