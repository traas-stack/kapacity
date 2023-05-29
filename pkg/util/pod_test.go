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
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetPodNames(t *testing.T) {
	pods := []*corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod-1",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod-2",
			},
		},
	}

	podNames := GetPodNames(pods)
	for i, pod := range pods {
		assert.Equal(t, pod.Name, podNames[i])
	}
}

func TestIsPodRunning(t *testing.T) {
	// running pod
	runningPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-1",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
	isRunning := IsPodRunning(runningPod)
	assert.True(t, isRunning)

	// pending pod
	pendingPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-2",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}
	isRunning = IsPodRunning(pendingPod)
	assert.False(t, isRunning)
}

func TestIsPodActive(t *testing.T) {
	// running pod
	runningPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-1",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
	assert.True(t, IsPodActive(runningPod))

	// failed pod
	failedPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-2",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
		},
	}
	assert.False(t, IsPodActive(failedPod))
}

func TestIsPodReady(t *testing.T) {
	// ready pod
	runningPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-1",
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
					Reason: "PodReady",
				},
			},
		},
	}
	assert.True(t, IsPodReady(runningPod))

	// not ready pod
	failedPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-2",
		},
	}
	assert.False(t, IsPodReady(failedPod))
}

func TestGetPodCondition(t *testing.T) {
	conditions := []corev1.PodCondition{
		{
			Type:   corev1.PodScheduled,
			Status: corev1.ConditionTrue,
			Reason: "PodScheduled",
		},
		{
			Type:   corev1.PodReady,
			Status: corev1.ConditionFalse,
			Reason: "PodReady",
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-1",
		},
		Status: corev1.PodStatus{
			Conditions: conditions,
		},
	}

	index, podReadyCondition := GetPodCondition(&pod.Status, corev1.PodReady)
	assert.Equal(t, 1, index)
	assert.Equal(t, corev1.ConditionFalse, podReadyCondition.Status)
}

func TestUpdatePodCondition(t *testing.T) {
	oldConditions := []corev1.PodCondition{
		{
			Type:   corev1.PodScheduled,
			Status: corev1.ConditionTrue,
			Reason: "PodScheduled",
		},
		{
			Type:   corev1.PodReady,
			Status: corev1.ConditionFalse,
			Reason: "PodReady",
		},
	}
	podStatus := &corev1.PodStatus{
		Conditions: oldConditions,
	}

	// add exist condition
	newCondition := &corev1.PodCondition{
		Type:   corev1.PodReady,
		Status: corev1.ConditionFalse,
		Reason: "PodReady",
	}
	isUpdate := UpdatePodCondition(podStatus, newCondition)
	assert.False(t, isUpdate)

	// update PodReady status to true
	newCondition = &corev1.PodCondition{
		Type:   corev1.PodReady,
		Status: corev1.ConditionTrue,
		Reason: "PodReady",
	}
	isUpdate = UpdatePodCondition(podStatus, newCondition)
	assert.True(t, isUpdate)

	// add new ContainersReady condition
	newCondition = &corev1.PodCondition{
		Type:   corev1.ContainersReady,
		Status: corev1.ConditionTrue,
		Reason: "ContainersReady",
	}
	isUpdate = UpdatePodCondition(podStatus, newCondition)
	assert.True(t, isUpdate)
}

func TestAddPodCondition(t *testing.T) {
	podStatus := &corev1.PodStatus{}
	condition := &corev1.PodCondition{
		Type:   corev1.PodReady,
		Status: corev1.ConditionFalse,
		Reason: "PodReady",
	}

	assert.True(t, AddPodCondition(podStatus, condition))
	assert.False(t, AddPodCondition(podStatus, condition))
}

func TestAddPodReadinessGate(t *testing.T) {
	podSpec := &corev1.PodSpec{}
	conditionType := corev1.PodReady

	assert.True(t, AddPodReadinessGate(podSpec, conditionType))
	assert.False(t, AddPodReadinessGate(podSpec, conditionType))
}
