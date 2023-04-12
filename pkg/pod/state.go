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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
	"github.com/traas-stack/kapacity/pkg/pod/sorter"
	"github.com/traas-stack/kapacity/pkg/util"
)

var (
	defaultStatesOrdered = []autoscalingv1alpha1.PodState{autoscalingv1alpha1.PodStateOnline, autoscalingv1alpha1.PodStateCutoff, autoscalingv1alpha1.PodStateStandby, autoscalingv1alpha1.PodStateDeleted}
	runningStates        = []autoscalingv1alpha1.PodState{autoscalingv1alpha1.PodStateOnline, autoscalingv1alpha1.PodStateCutoff, autoscalingv1alpha1.PodStateStandby}
)

// GetState get the current State of pod.
func GetState(pod *corev1.Pod) autoscalingv1alpha1.PodState {
	switch pod.Labels[LabelState] {
	case "":
		return autoscalingv1alpha1.PodStateOnline
	case string(autoscalingv1alpha1.PodStateCutoff):
		return autoscalingv1alpha1.PodStateCutoff
	case string(autoscalingv1alpha1.PodStateStandby):
		return autoscalingv1alpha1.PodStateStandby
	}
	// assume an unknown state to be online
	return autoscalingv1alpha1.PodStateOnline
}

// SetState set State to pod.
func SetState(pod *corev1.Pod, state autoscalingv1alpha1.PodState) {
	if state == autoscalingv1alpha1.PodStateOnline {
		delete(pod.Labels, LabelState)
		return
	}
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels[LabelState] = string(state)
}

// StateChanged reports whether the given pod's state has changed.
func StateChanged(old, new *corev1.Pod) bool {
	oldRunning := util.IsPodRunning(old)
	newRunning := util.IsPodRunning(new)
	if !oldRunning && !newRunning {
		return false
	}
	if (oldRunning && !newRunning) || (!oldRunning && newRunning) {
		return true
	}
	return old.Labels[LabelState] != new.Labels[LabelState]
}

// FilterAndClassifyByRunningState filter and classify given pods by their running states.
// It returns the classified result and the total number of running pods.
func FilterAndClassifyByRunningState(pods []corev1.Pod) (result map[autoscalingv1alpha1.PodState][]*corev1.Pod, total int) {
	result = make(map[autoscalingv1alpha1.PodState][]*corev1.Pod)
	for _, state := range runningStates {
		result[state] = make([]*corev1.Pod, 0)
	}
	for i := range pods {
		pod := &pods[i]
		if !util.IsPodRunning(pod) {
			continue
		}
		state := GetState(pod)
		result[state] = append(result[state], pod)
		total++
	}
	return
}

type stateInfo struct {
	DesiredReplicas   int
	CurrentPodNames   sets.String
	CandidatePodNames sets.String
}

func newStateInfo() *stateInfo {
	return &stateInfo{
		CurrentPodNames:   sets.String{},
		CandidatePodNames: sets.String{},
	}
}

// StateManager provides a method to calculate pod state change.
type StateManager struct {
	rp         *autoscalingv1alpha1.ReplicaProfile
	sorter     sorter.Interface
	statesInfo map[autoscalingv1alpha1.PodState]*stateInfo
	podNameMap map[string]*corev1.Pod
}

// NewStateManager build a state manager to calculate pod state change based on given spec and status.
func NewStateManager(rp *autoscalingv1alpha1.ReplicaProfile, sorter sorter.Interface, currentRunningPods map[autoscalingv1alpha1.PodState][]*corev1.Pod) *StateManager {
	sm := &StateManager{
		rp:         rp,
		sorter:     sorter,
		statesInfo: make(map[autoscalingv1alpha1.PodState]*stateInfo),
		podNameMap: make(map[string]*corev1.Pod),
	}
	for _, state := range defaultStatesOrdered {
		info := newStateInfo()
		switch state {
		case autoscalingv1alpha1.PodStateOnline:
			info.DesiredReplicas = int(rp.Spec.OnlineReplicas)
		case autoscalingv1alpha1.PodStateCutoff:
			info.DesiredReplicas = int(rp.Spec.CutoffReplicas)
		case autoscalingv1alpha1.PodStateStandby:
			info.DesiredReplicas = int(rp.Spec.StandbyReplicas)
		}
		for _, pod := range currentRunningPods[state] {
			info.CurrentPodNames.Insert(pod.Name)
			sm.podNameMap[pod.Name] = pod
		}
		sm.statesInfo[state] = info
	}
	return sm
}

// StateChange tells which pods should be changed to which State.
type StateChange struct {
	Online  []*corev1.Pod
	Cutoff  []*corev1.Pod
	Standby []*corev1.Pod
	Delete  []*corev1.Pod
}

// CalculateStateChange calculate pod state change based on the spec and status info in StateManager.
// State transitions are shown as below:
//
//	                                                   delete pods
//	    +-----------------------------------------------------------------------------------------------------+
//	    |                                                                                                     |
//	    |                                                                                                     |
//	    |                    turn off traffic       +-----------------+                                       |
//	    |       +----------------------------------->                 |        delete pods                    |
//	    |       |                                   |     cutoff      +----------------------------+          |
//	    |       |  +--------------------------------+                 |                            |          |
//	    |       |  |          turn on traffic       +--------+--------+                            |          |
//	    |       |  |                                         |                                     |          |
//	+---+-------+--v-+                                       |                                 +---v----------v---+
//	|                |                                       |                                 |                  |
//	|    online      |                                       | swap out memory                 |      deleted     |
//	+---+-------^----+                                       |                                 +---^----------+---+
//	    |       |                                            |                                     |          |
//	    |       |         swap in memory           +---------v---------+                           |          |
//	    |       |         turn on traffic          |                   |                           |          |
//	    |       +----------------------------------|      standby      |                           |          |
//	    |                                          |                   +---------------------------+          |
//	    |                                          +-------------------+        delete pods                   |
//	    |                                                                                                     |
//	    +-----------------------------------------------------------------------------------------------------+
//												 create new pods
func (sm *StateManager) CalculateStateChange(ctx context.Context) (*StateChange, error) {
	for i, state := range defaultStatesOrdered {
		info := sm.statesInfo[state]
		if diff := len(info.CurrentPodNames) + len(info.CandidatePodNames) - info.DesiredReplicas; diff < 0 {
			// Current state need more pods, transfer (scale up) from subsequent states
			// except for the last state (Deleted) because its CurrentPodNames is always empty.
			j := i + 1
			for j < len(defaultStatesOrdered)-1 && diff < 0 {
				neighborState := defaultStatesOrdered[j]
				neighborInfo := sm.statesInfo[neighborState]
				podsToTransfer, err := sm.selectPodsToTransfer(ctx, -diff, true, neighborInfo.CurrentPodNames)
				if err != nil {
					return nil, fmt.Errorf("failed to select pods to transfer from %s to %s: %v", neighborState, state, err)
				}
				for pod := range podsToTransfer {
					info.CandidatePodNames.Insert(pod)
					neighborInfo.CurrentPodNames.Delete(pod)
				}
				diff += len(podsToTransfer)
				j++
			}
		} else if diff > 0 && i < len(defaultStatesOrdered)-1 {
			// Current state need less pods, transfer (scale down) to subsequent state.
			// The diff of the last state (Deleted) will be ignored because its DesiredReplicas has no meaning.
			neighborState := defaultStatesOrdered[i+1]
			neighborInfo := sm.statesInfo[neighborState]
			podsToTransfer, err := sm.selectPodsToTransfer(ctx, diff, false, info.CurrentPodNames.Union(info.CandidatePodNames))
			if err != nil {
				return nil, fmt.Errorf("failed to select pods to transfer from %s to %s: %v", state, neighborState, err)
			}
			for pod := range podsToTransfer {
				neighborInfo.CandidatePodNames.Insert(pod)
				info.CurrentPodNames.Delete(pod)
				info.CandidatePodNames.Delete(pod)
			}
		}
	}

	result := &StateChange{}
	for state, info := range sm.statesInfo {
		candidatePods := sm.makePodListByNames(info.CandidatePodNames)
		switch state {
		case autoscalingv1alpha1.PodStateOnline:
			result.Online = candidatePods
		case autoscalingv1alpha1.PodStateCutoff:
			result.Cutoff = candidatePods
		case autoscalingv1alpha1.PodStateStandby:
			result.Standby = candidatePods
		case autoscalingv1alpha1.PodStateDeleted:
			result.Delete = candidatePods
		}
	}
	return result, nil
}

func (sm *StateManager) selectPodsToTransfer(ctx context.Context, targetCount int, scaleUp bool, candidates sets.String) (sets.String, error) {
	if targetCount <= 0 || candidates.Len() == 0 {
		return nil, nil
	}
	sortedCandidates, err := sm.sorter.Sort(ctx, sm.makePodListByNames(candidates))
	if err != nil {
		return nil, fmt.Errorf("failed to sort candidate pods: %v", err)
	}
	result := sets.String{}
	for i := range sortedCandidates {
		if scaleUp {
			// select in reverse order
			i = len(sortedCandidates) - 1 - i
		}
		result.Insert(sortedCandidates[i].Name)
		if result.Len() == targetCount {
			break
		}
	}
	return result, nil
}

func (sm *StateManager) makePodListByNames(names sets.String) []*corev1.Pod {
	result := make([]*corev1.Pod, 0, names.Len())
	for name := range names {
		result = append(result, sm.podNameMap[name])
	}
	return result
}
