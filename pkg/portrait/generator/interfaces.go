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

package generator

import (
	"context"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"

	"github.com/traas-stack/kapacity/api/v1alpha1"
)

// Interface provide methods to generate portrait data.
type Interface interface {
	// GenerateHorizontal portrait data for the scale target with specified metrics spec and algorithm configuration.
	// It returns the generated portrait data and an expected duration before next generating.
	GenerateHorizontal(ctx context.Context, namespace string, scaleTargetRef autoscalingv2.CrossVersionObjectReference, metrics []v1alpha1.MetricSpec, algorithm v1alpha1.PortraitAlgorithm) (*v1alpha1.HorizontalPortraitData, time.Duration, error)
}
