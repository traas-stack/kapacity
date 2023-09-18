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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiv1pod "k8s.io/kubernetes/pkg/api/v1/pod"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ReadinessGateOnline = "kapacitystack.io/online"
)

// ReadinessGate controls pod traffic by setting a specific readiness gate to make the pod ready/unready
// so that it would be automatically added to/removed from the endpoints of Kubernetes services.
type ReadinessGate struct {
	client.Client
}

func (c *ReadinessGate) On(ctx context.Context, pods []*corev1.Pod) error {
	for _, pod := range pods {
		if err := c.setReadinessGateStatus(ctx, pod, corev1.ConditionTrue); err != nil {
			return fmt.Errorf("failed to set readiness gate of pod %q: %v", pod.Name, err)
		}
	}
	return nil
}

func (c *ReadinessGate) Off(ctx context.Context, pods []*corev1.Pod) error {
	for _, pod := range pods {
		if err := c.setReadinessGateStatus(ctx, pod, corev1.ConditionFalse); err != nil {
			return fmt.Errorf("failed to set readiness gate of pod %q: %v", pod.Name, err)
		}
	}
	return nil
}

func (c *ReadinessGate) setReadinessGateStatus(ctx context.Context, pod *corev1.Pod, status corev1.ConditionStatus) error {
	patch := client.MergeFrom(pod.DeepCopy())
	if apiv1pod.UpdatePodCondition(&pod.Status, &corev1.PodCondition{
		Type:   ReadinessGateOnline,
		Status: status,
	}) {
		return c.Status().Patch(ctx, pod, patch)
	}
	return nil
}
