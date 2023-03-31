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

package sorter

import (
	"context"

	corev1 "k8s.io/api/core/v1"
)

// Interface provides a method to sort pods.
// Check method comment for ordering requirement.
type Interface interface {
	// Sort pods in descending "scale down" priority order, which means
	// the higher priority to be scaled down the pod is, the smaller will its index be.
	Sort(context.Context, []*corev1.Pod) ([]*corev1.Pod, error)
}
