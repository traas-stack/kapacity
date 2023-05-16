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

package pod

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
	"github.com/traas-stack/kapacity/pkg/workload"
)

func TestGetState(t *testing.T) {
	// online pod
	onlinePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "pod-1",
			Labels: map[string]string{},
		},
	}
	assert.Equal(t, autoscalingv1alpha1.PodStateOnline, GetState(onlinePod), "pod state should be online for %s", onlinePod.Name)

	// cutoff pod
	cutoffPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-2",
			Labels: map[string]string{
				LabelState: string(autoscalingv1alpha1.PodStateCutoff),
			},
		},
	}
	assert.Equal(t, autoscalingv1alpha1.PodStateCutoff, GetState(cutoffPod), "pod state should be cutoff for %s", cutoffPod.Name)

	// standby pod
	standbyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-2",
			Labels: map[string]string{
				LabelState: string(autoscalingv1alpha1.PodStateStandby),
			},
		},
	}
	assert.Equal(t, autoscalingv1alpha1.PodStateStandby, GetState(standbyPod), "pod state should be standby for %s", standbyPod.Name)
}

func TestSetState(t *testing.T) {
	// online pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "pod-1",
			Labels: map[string]string{},
		},
	}

	// set online state
	SetState(pod, autoscalingv1alpha1.PodStateOnline)
	_, ok := pod.Labels[LabelState]
	assert.False(t, ok, "pod %s should have online state label", pod.Name)

	// set cutoff state
	SetState(pod, autoscalingv1alpha1.PodStateCutoff)
	assert.Equal(t, string(autoscalingv1alpha1.PodStateCutoff), pod.Labels[LabelState], "pod %s should have online state label", pod.Name)

	// set standby state
	SetState(pod, autoscalingv1alpha1.PodStateStandby)
	assert.Equal(t, string(autoscalingv1alpha1.PodStateStandby), pod.Labels[LabelState], "pod %s should have online state label", pod.Name)
}

func TestStateChanged(t *testing.T) {
	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-1",
			Labels: map[string]string{
				LabelState: string(autoscalingv1alpha1.PodStateCutoff),
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
	// same pod state
	cutoffPod := oldPod.DeepCopy()
	assert.False(t, StateChanged(oldPod, cutoffPod), "old pod and new pod have different state")

	// different pod state
	standbyPod := oldPod.DeepCopy()
	standbyPod.Labels[LabelState] = string(autoscalingv1alpha1.PodStateStandby)
	assert.True(t, StateChanged(oldPod, standbyPod), "old pod and new pod have same state")
}

func TestFilterAndClassifyByRunningState(t *testing.T) {
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "pod-1",
				Labels: map[string]string{},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod-2",
				Labels: map[string]string{
					LabelState: string(autoscalingv1alpha1.PodStateOnline),
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod-3",
				Labels: map[string]string{
					LabelState: string(autoscalingv1alpha1.PodStateCutoff),
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod-4",
				Labels: map[string]string{
					LabelState: string(autoscalingv1alpha1.PodStateStandby),
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		},
	}

	resultMap, total := FilterAndClassifyByRunningState(pods)
	assert.Equal(t, 4, total, "pod number is error")
	for state, podList := range resultMap {
		switch state {
		case autoscalingv1alpha1.PodStateOnline:
			assert.Equal(t, 2, len(podList))
		case autoscalingv1alpha1.PodStateCutoff:
			assert.Equal(t, 1, len(podList))
		case autoscalingv1alpha1.PodStateStandby:
			assert.Equal(t, 1, len(podList))
		}
	}
}

func TestCalculateStateChange(t *testing.T) {
	currentPodMap := map[autoscalingv1alpha1.PodState][]*corev1.Pod{
		autoscalingv1alpha1.PodStateOnline: {
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "pod-1",
					Labels: map[string]string{},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod-2",
					Labels: map[string]string{
						LabelState: string(autoscalingv1alpha1.PodStateOnline),
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			},
		},
		autoscalingv1alpha1.PodStateCutoff: {
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod-3",
					Labels: map[string]string{
						LabelState: string(autoscalingv1alpha1.PodStateCutoff),
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			},
		},
	}

	testCases := []struct {
		name            string
		rp              *autoscalingv1alpha1.ReplicaProfile
		toOnlinePodNum  int
		toCutoffPodNum  int
		toStandbyPodNum int
		toDeletePodNum  int
	}{
		//cutoff -> online
		{
			name: "cutoff -> online",
			rp: &autoscalingv1alpha1.ReplicaProfile{
				Spec: autoscalingv1alpha1.ReplicaProfileSpec{
					OnlineReplicas: 3,
					CutoffReplicas: 0,
				},
			},
			toOnlinePodNum: 1,
		},
		//online -> cutoff
		{
			name: "online -> cutoff",
			rp: &autoscalingv1alpha1.ReplicaProfile{
				Spec: autoscalingv1alpha1.ReplicaProfileSpec{
					OnlineReplicas: 1,
					CutoffReplicas: 2,
				},
			},
			toCutoffPodNum: 1,
		},
		//cutoff -> delete
		{
			name: "cutoff -> delete",
			rp: &autoscalingv1alpha1.ReplicaProfile{
				Spec: autoscalingv1alpha1.ReplicaProfileSpec{
					OnlineReplicas: 2,
					CutoffReplicas: 0,
				},
			},
			toDeletePodNum: 1,
		},
	}

	for _, testCase := range testCases {
		statefulSet := &workload.StatefulSet{}
		stateManager := NewStateManager(testCase.rp, statefulSet, currentPodMap)

		stateChange, err := stateManager.CalculateStateChange(context.TODO())
		assert.Nil(t, err)
		assert.Equal(t, testCase.toOnlinePodNum, len(stateChange.Online), "to online pod num is error for %s", testCase.name)
		assert.Equal(t, testCase.toCutoffPodNum, len(stateChange.Cutoff), "to cutoff pod num is error for %s", testCase.name)
		assert.Equal(t, testCase.toStandbyPodNum, len(stateChange.Standby), "to standby pod num is error for %s", testCase.name)
		assert.Equal(t, testCase.toDeletePodNum, len(stateChange.Delete), "to delete pod num is error for %s", testCase.name)
	}
}
