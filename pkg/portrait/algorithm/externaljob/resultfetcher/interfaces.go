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

	"sigs.k8s.io/controller-runtime/pkg/manager"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
)

// Horizontal provides method to fetch the algorithm result of external horizontal portrait algorithm jobs.
// It extends manager.Runnable and would be started with the manager to do some background works such as watching or polling.
type Horizontal interface {
	manager.Runnable
	// FetchResult fetches the latest algorithm result of the external horizontal portrait algorithm job managed by given HorizontalPortrait with given result source config.
	FetchResult(ctx context.Context, hp *autoscalingv1alpha1.HorizontalPortrait, cfg *autoscalingv1alpha1.PortraitAlgorithmResultSource) (*autoscalingv1alpha1.HorizontalPortraitData, error)
}
