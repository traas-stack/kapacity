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

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
)

// Horizontal provides methods to manage horizontal portraits and fetch value from them.
type Horizontal interface {
	// GetPortraitIdentifier returns a string identifier of the portrait managed by given IHPA and provider config.
	GetPortraitIdentifier(ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler, cfg *autoscalingv1alpha1.HorizontalPortraitProvider) string
	// UpdatePortraitSpec creates or updates the portrait backend managed by given IHPA and provider config.
	UpdatePortraitSpec(ctx context.Context, ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler, cfg *autoscalingv1alpha1.HorizontalPortraitProvider) error
	// FetchPortraitValue fetches the current value from the data of portrait managed by given IHPA and provider config.
	FetchPortraitValue(ctx context.Context, ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler, cfg *autoscalingv1alpha1.HorizontalPortraitProvider) (*autoscalingv1alpha1.HorizontalPortraitValue, error)
	// CleanupPortrait does clean up works for the identified portrait managed by given IHPA.
	CleanupPortrait(ctx context.Context, ihpa *autoscalingv1alpha1.IntelligentHorizontalPodAutoscaler, identifier string) error
}
