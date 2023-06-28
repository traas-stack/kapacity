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

package jobcontroller

import (
	"context"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
)

// Horizontal provides methods to manage external horizontal portrait algorithm jobs.
type Horizontal interface {
	// UpdateJob creates or updates the external algorithm job managed by given HorizontalPortrait and job config.
	UpdateJob(ctx context.Context, hp *autoscalingv1alpha1.HorizontalPortrait, cfg *autoscalingv1alpha1.PortraitAlgorithmJob) error
	// CleanupJob does clean up works for the external algorithm job managed by given HorizontalPortrait.
	CleanupJob(ctx context.Context, hp *autoscalingv1alpha1.HorizontalPortrait) error
}
