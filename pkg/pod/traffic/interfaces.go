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

package traffic

import (
	"context"

	corev1 "k8s.io/api/core/v1"
)

// Controller provide methods to control pod traffic.
type Controller interface {
	// On turns on the specified pods' traffic.
	On(context.Context, []*corev1.Pod) error
	// Off turns off the specified pods' traffic.
	Off(context.Context, []*corev1.Pod) error
}
