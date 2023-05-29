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

/*
 This file contains code derived from and/or modified from Kubernetes
 which is licensed under below license:

 Copyright 2015 The Kubernetes Authors.

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
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestActivePodsWithRanks_Sort(t *testing.T) {
	now := metav1.Now()
	then1Month := metav1.Time{Time: now.AddDate(0, -1, 0)}
	then2Hours := metav1.Time{Time: now.Add(-2 * time.Hour)}
	then5Hours := metav1.Time{Time: now.Add(-5 * time.Hour)}
	then8Hours := metav1.Time{Time: now.Add(-8 * time.Hour)}
	zeroTime := metav1.Time{}
	pod := func(podName, nodeName string, phase corev1.PodPhase, ready bool, restarts int32, readySince metav1.Time, created metav1.Time, annotations map[string]string) *corev1.Pod {
		var conditions []corev1.PodCondition
		var containerStatuses []corev1.ContainerStatus
		if ready {
			conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: readySince}}
			containerStatuses = []corev1.ContainerStatus{{RestartCount: restarts}}
		}
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				CreationTimestamp: created,
				Name:              podName,
				Annotations:       annotations,
			},
			Spec: corev1.PodSpec{NodeName: nodeName},
			Status: corev1.PodStatus{
				Conditions:        conditions,
				ContainerStatuses: containerStatuses,
				Phase:             phase,
			},
		}
	}
	var (
		unscheduledPod                      = pod("unscheduled", "", corev1.PodPending, false, 0, zeroTime, zeroTime, nil)
		scheduledPendingPod                 = pod("pending", "node", corev1.PodPending, false, 0, zeroTime, zeroTime, nil)
		unknownPhasePod                     = pod("unknown-phase", "node", corev1.PodUnknown, false, 0, zeroTime, zeroTime, nil)
		runningNotReadyPod                  = pod("not-ready", "node", corev1.PodRunning, false, 0, zeroTime, zeroTime, nil)
		runningReadyNoLastTransitionTimePod = pod("ready-no-last-transition-time", "node", corev1.PodRunning, true, 0, zeroTime, zeroTime, nil)
		runningReadyNow                     = pod("ready-now", "node", corev1.PodRunning, true, 0, now, now, nil)
		runningReadyThen                    = pod("ready-then", "node", corev1.PodRunning, true, 0, then1Month, then1Month, nil)
		runningReadyNowHighRestarts         = pod("ready-high-restarts", "node", corev1.PodRunning, true, 9001, now, now, nil)
		runningReadyNowCreatedThen          = pod("ready-now-created-then", "node", corev1.PodRunning, true, 0, now, then1Month, nil)
		lowPodDeletionCost                  = pod("low-deletion-cost", "node", corev1.PodRunning, true, 0, now, then1Month, map[string]string{corev1.PodDeletionCost: "10"})
		highPodDeletionCost                 = pod("high-deletion-cost", "node", corev1.PodRunning, true, 0, now, then1Month, map[string]string{corev1.PodDeletionCost: "100"})
		unscheduled5Hours                   = pod("unscheduled-5-hours", "", corev1.PodPending, false, 0, then5Hours, then5Hours, nil)
		unscheduled8Hours                   = pod("unscheduled-10-hours", "", corev1.PodPending, false, 0, then8Hours, then8Hours, nil)
		ready2Hours                         = pod("ready-2-hours", "", corev1.PodRunning, true, 0, then2Hours, then1Month, nil)
		ready5Hours                         = pod("ready-5-hours", "", corev1.PodRunning, true, 0, then5Hours, then1Month, nil)
		ready10Hours                        = pod("ready-10-hours", "", corev1.PodRunning, true, 0, then8Hours, then1Month, nil)
	)
	equalityTests := []struct {
		p1                          *corev1.Pod
		p2                          *corev1.Pod
		disableLogarithmicScaleDown bool
	}{
		{p1: unscheduledPod},
		{p1: scheduledPendingPod},
		{p1: unknownPhasePod},
		{p1: runningNotReadyPod},
		{p1: runningReadyNowCreatedThen},
		{p1: runningReadyNow},
		{p1: runningReadyThen},
		{p1: runningReadyNowHighRestarts},
		{p1: runningReadyNowCreatedThen},
		{p1: unscheduled5Hours, p2: unscheduled8Hours},
		{p1: ready5Hours, p2: ready10Hours},
	}
	for _, tc := range equalityTests {
		logarithmicScaleDownEnabled = !tc.disableLogarithmicScaleDown
		if tc.p2 == nil {
			tc.p2 = tc.p1
		}
		podsWithRanks := activePodsWithRanks{
			Pods:  []*corev1.Pod{tc.p1, tc.p2},
			Ranks: []int{1, 1},
			Now:   now,
		}
		if podsWithRanks.Less(0, 1) || podsWithRanks.Less(1, 0) {
			t.Errorf("expected pod %q to be equivalent to %q", tc.p1.Name, tc.p2.Name)
		}
	}
	type podWithRank struct {
		pod  *corev1.Pod
		rank int
	}
	inequalityTests := []struct {
		lesser, greater             podWithRank
		disablePodDeletioncost      bool
		disableLogarithmicScaleDown bool
	}{
		{lesser: podWithRank{unscheduledPod, 1}, greater: podWithRank{scheduledPendingPod, 2}},
		{lesser: podWithRank{unscheduledPod, 2}, greater: podWithRank{scheduledPendingPod, 1}},
		{lesser: podWithRank{scheduledPendingPod, 1}, greater: podWithRank{unknownPhasePod, 2}},
		{lesser: podWithRank{unknownPhasePod, 1}, greater: podWithRank{runningNotReadyPod, 2}},
		{lesser: podWithRank{runningNotReadyPod, 1}, greater: podWithRank{runningReadyNoLastTransitionTimePod, 1}},
		{lesser: podWithRank{runningReadyNoLastTransitionTimePod, 1}, greater: podWithRank{runningReadyNow, 1}},
		{lesser: podWithRank{runningReadyNow, 2}, greater: podWithRank{runningReadyNoLastTransitionTimePod, 1}},
		{lesser: podWithRank{runningReadyNow, 1}, greater: podWithRank{runningReadyThen, 1}},
		{lesser: podWithRank{runningReadyNow, 2}, greater: podWithRank{runningReadyThen, 1}},
		{lesser: podWithRank{runningReadyNowHighRestarts, 1}, greater: podWithRank{runningReadyNow, 1}},
		{lesser: podWithRank{runningReadyNow, 2}, greater: podWithRank{runningReadyNowHighRestarts, 1}},
		{lesser: podWithRank{runningReadyNow, 1}, greater: podWithRank{runningReadyNowCreatedThen, 1}},
		{lesser: podWithRank{runningReadyNowCreatedThen, 2}, greater: podWithRank{runningReadyNow, 1}},
		{lesser: podWithRank{lowPodDeletionCost, 2}, greater: podWithRank{highPodDeletionCost, 1}},
		{lesser: podWithRank{highPodDeletionCost, 2}, greater: podWithRank{lowPodDeletionCost, 1}, disablePodDeletioncost: true},
		{lesser: podWithRank{ready2Hours, 1}, greater: podWithRank{ready5Hours, 1}},
	}
	for i, test := range inequalityTests {
		t.Run(fmt.Sprintf("test%d", i), func(t *testing.T) {
			podDeletionCostEnabled = !test.disablePodDeletioncost
			logarithmicScaleDownEnabled = !test.disableLogarithmicScaleDown

			podsWithRanks := activePodsWithRanks{
				Pods:  []*corev1.Pod{test.lesser.pod, test.greater.pod},
				Ranks: []int{test.lesser.rank, test.greater.rank},
				Now:   now,
			}
			if !podsWithRanks.Less(0, 1) {
				t.Errorf("expected pod %q with rank %v to be less than %q with rank %v", podsWithRanks.Pods[0].Name, podsWithRanks.Ranks[0], podsWithRanks.Pods[1].Name, podsWithRanks.Ranks[1])
			}
			if podsWithRanks.Less(1, 0) {
				t.Errorf("expected pod %q with rank %v not to be less than %v with rank %v", podsWithRanks.Pods[1].Name, podsWithRanks.Ranks[1], podsWithRanks.Pods[0].Name, podsWithRanks.Ranks[0])
			}
		})
	}
}
