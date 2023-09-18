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

package util

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiv1pod "k8s.io/kubernetes/pkg/api/v1/pod"
)

// GetPodNames generate a list of pod names from a list of pod objects.
func GetPodNames(pods []*corev1.Pod) []string {
	result := make([]string, 0, len(pods))
	for _, pod := range pods {
		result = append(result, pod.Name)
	}
	return result
}

// IsPodRunning returns if the given pod's phase is running and is not being deleted.
func IsPodRunning(pod *corev1.Pod) bool {
	return pod.DeletionTimestamp.IsZero() && pod.Status.Phase == corev1.PodRunning
}

// IsPodActive returns if the given pod has not terminated.
func IsPodActive(pod *corev1.Pod) bool {
	return pod.Status.Phase != corev1.PodSucceeded &&
		pod.Status.Phase != corev1.PodFailed &&
		pod.DeletionTimestamp.IsZero()
}

// AddPodCondition adds a pod condition if not exists. Sets LastTransitionTime to now if not exists.
// Returns true if pod condition has been added.
func AddPodCondition(status *corev1.PodStatus, condition *corev1.PodCondition) bool {
	if _, oldCondition := apiv1pod.GetPodCondition(status, condition.Type); oldCondition != nil {
		return false
	}
	condition.LastTransitionTime = metav1.Now()
	status.Conditions = append(status.Conditions, *condition)
	return true
}

// AddPodReadinessGate adds the provided condition to the pod's readiness gates.
// Returns true if the readiness gate has been added.
func AddPodReadinessGate(spec *corev1.PodSpec, conditionType corev1.PodConditionType) bool {
	for _, rg := range spec.ReadinessGates {
		if rg.ConditionType == conditionType {
			return false
		}
	}
	spec.ReadinessGates = append(spec.ReadinessGates, corev1.PodReadinessGate{ConditionType: conditionType})
	return true
}
