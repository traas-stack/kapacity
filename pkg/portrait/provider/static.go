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

	"github.com/traas-stack/kapacity/api/v1alpha1"
)

// StaticHorizontal provides horizontal portraits with static replicas values.
type StaticHorizontal struct{}

// NewStaticHorizontal creates a new StaticHorizontal.
func NewStaticHorizontal() Horizontal {
	return &StaticHorizontal{}
}

func (*StaticHorizontal) GetPortraitIdentifier(*v1alpha1.IntelligentHorizontalPodAutoscaler, *v1alpha1.HorizontalPortraitProvider) string {
	return string(v1alpha1.StaticHorizontalPortraitProviderType)
}

func (*StaticHorizontal) UpdatePortraitSpec(context.Context, *v1alpha1.IntelligentHorizontalPodAutoscaler, *v1alpha1.HorizontalPortraitProvider) error {
	// do nothing
	return nil
}

func (h *StaticHorizontal) FetchPortraitValue(_ context.Context, ihpa *v1alpha1.IntelligentHorizontalPodAutoscaler, cfg *v1alpha1.HorizontalPortraitProvider) (*v1alpha1.HorizontalPortraitValue, error) {
	return &v1alpha1.HorizontalPortraitValue{
		Provider: h.GetPortraitIdentifier(ihpa, cfg),
		Replicas: cfg.Static.Replicas,
	}, nil
}

func (*StaticHorizontal) CleanupPortrait(context.Context, *v1alpha1.IntelligentHorizontalPodAutoscaler, string) error {
	// do nothing
	return nil
}
