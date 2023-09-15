/*
 Copyright 2023 The Kapacity Authors.
 Copyright 2014 The Kubernetes Authors.

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
	"fmt"
	"math"
	"sort"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/integer"

	"github.com/traas-stack/kapacity/pkg/util"
)

var (
	podPhaseToOrdinal = map[corev1.PodPhase]int{corev1.PodPending: 0, corev1.PodUnknown: 1, corev1.PodRunning: 2}

	// TODO(zqzten): make below configurable
	podDeletionCostEnabled      = true
	logarithmicScaleDownEnabled = true
)

// ActivePodsWithRanks sorts pods with a list of corresponding ranks which will be considered during sorting.
// The list's length must be equal to the length of pod list to be sorted.
// After sorting, the pods will be ordered as follows, applying each
// rule in turn until one matches:
//
//  1. If only one of the pods is assigned to a node, the pod that is not
//     assigned comes before the pod that is.
//  2. If the pods' phases differ, a pending pod comes before a pod whose phase
//     is unknown, and a pod whose phase is unknown comes before a running pod.
//  3. If exactly one of the pods is ready, the pod that is not ready comes
//     before the ready pod.
//  4. If controller.kubernetes.io/pod-deletion-cost annotation is set, then
//     the pod with the lower value will come first.
//  5. If the pods' ranks differ, the pod with greater rank comes before the pod
//     with lower rank.
//  6. If both pods are ready but have not been ready for the same amount of
//     time, the pod that has been ready for a shorter amount of time comes
//     before the pod that has been ready for longer.
//  7. If one pod has a container that has restarted more than any container in
//     the other pod, the pod with the container with more restarts comes
//     before the other pod.
//  8. If the pods' creation times differ, the pod that was created more recently
//     comes before the older pod.
//
// In 6 and 8, times are compared in a logarithmic scale. This allows a level
// of randomness among equivalent Pods when sorting. If two pods have the same
// logarithmic rank, they are sorted by UUID to provide a pseudorandom order.
//
// If none of these rules matches, the second pod comes before the first pod.
//
// This ordering is the same with the one used by Kubernetes ReplicaSet to choose pods
// that should be preferred for deletion first.
type ActivePodsWithRanks struct {
	// Rank is a ranking of pods.
	// This ranking is used during sorting when comparing two pods that are both scheduled,
	// in the same phase, and having the same ready status.
	Ranks []int

	// Now is a reference timestamp for doing logarithmic timestamp comparisons.
	// If zero, comparison happens without scaling.
	Now metav1.Time
}

func (s *ActivePodsWithRanks) Sort(_ context.Context, pods []*corev1.Pod) ([]*corev1.Pod, error) {
	sort.Sort(activePodsWithRanks{
		Pods:  pods,
		Ranks: s.Ranks,
		Now:   s.Now,
	})
	return pods, nil
}

type activePodsWithRanks struct {
	Pods  []*corev1.Pod
	Ranks []int
	Now   metav1.Time
}

func (s activePodsWithRanks) Len() int {
	return len(s.Pods)
}

func (s activePodsWithRanks) Swap(i, j int) {
	s.Pods[i], s.Pods[j] = s.Pods[j], s.Pods[i]
	s.Ranks[i], s.Ranks[j] = s.Ranks[j], s.Ranks[i]
}

func (s activePodsWithRanks) Less(i, j int) bool {
	// 1. Unassigned < assigned
	// If only one of the pods is unassigned, the unassigned one is smaller
	if s.Pods[i].Spec.NodeName != s.Pods[j].Spec.NodeName && (len(s.Pods[i].Spec.NodeName) == 0 || len(s.Pods[j].Spec.NodeName) == 0) {
		return len(s.Pods[i].Spec.NodeName) == 0
	}
	// 2. PodPending < PodUnknown < PodRunning
	if podPhaseToOrdinal[s.Pods[i].Status.Phase] != podPhaseToOrdinal[s.Pods[j].Status.Phase] {
		return podPhaseToOrdinal[s.Pods[i].Status.Phase] < podPhaseToOrdinal[s.Pods[j].Status.Phase]
	}
	// 3. Not ready < ready
	// If only one of the pods is not ready, the not ready one is smaller
	if util.IsPodReady(s.Pods[i]) != util.IsPodReady(s.Pods[j]) {
		return !util.IsPodReady(s.Pods[i])
	}

	// 4. lower pod-deletion-cost < higher pod-deletion cost
	if podDeletionCostEnabled {
		pi, _ := getDeletionCostFromPodAnnotations(s.Pods[i].Annotations)
		pj, _ := getDeletionCostFromPodAnnotations(s.Pods[j].Annotations)
		if pi != pj {
			return pi < pj
		}
	}

	// 5. Doubled up < not doubled up
	// If one of the two pods is on the same node as one or more additional
	// ready pods that belong to the same replicaset, whichever pod has more
	// colocated ready pods is less
	if s.Ranks[i] != s.Ranks[j] {
		return s.Ranks[i] > s.Ranks[j]
	}

	// 6. Been ready for empty time < less time < more time
	// If both pods are ready, the latest ready one is smaller
	if util.IsPodReady(s.Pods[i]) && util.IsPodReady(s.Pods[j]) {
		readyTime1 := podReadyTime(s.Pods[i])
		readyTime2 := podReadyTime(s.Pods[j])
		if !readyTime1.Equal(readyTime2) {
			if !logarithmicScaleDownEnabled {
				return afterOrZero(readyTime1, readyTime2)
			} else {
				if s.Now.IsZero() || readyTime1.IsZero() || readyTime2.IsZero() {
					return afterOrZero(readyTime1, readyTime2)
				}
				rankDiff := logarithmicRankDiff(*readyTime1, *readyTime2, s.Now)
				if rankDiff == 0 {
					return s.Pods[i].UID < s.Pods[j].UID
				}
				return rankDiff < 0
			}
		}
	}
	// 7. Pods with containers with higher restart counts < lower restart counts
	if maxContainerRestarts(s.Pods[i]) != maxContainerRestarts(s.Pods[j]) {
		return maxContainerRestarts(s.Pods[i]) > maxContainerRestarts(s.Pods[j])
	}
	// 8. Empty creation time pods < newer pods < older pods
	if !s.Pods[i].CreationTimestamp.Equal(&s.Pods[j].CreationTimestamp) {
		if !logarithmicScaleDownEnabled {
			return afterOrZero(&s.Pods[i].CreationTimestamp, &s.Pods[j].CreationTimestamp)
		} else {
			if s.Now.IsZero() || s.Pods[i].CreationTimestamp.IsZero() || s.Pods[j].CreationTimestamp.IsZero() {
				return afterOrZero(&s.Pods[i].CreationTimestamp, &s.Pods[j].CreationTimestamp)
			}
			rankDiff := logarithmicRankDiff(s.Pods[i].CreationTimestamp, s.Pods[j].CreationTimestamp, s.Now)
			if rankDiff == 0 {
				return s.Pods[i].UID < s.Pods[j].UID
			}
			return rankDiff < 0
		}
	}
	return false
}

// afterOrZero checks if time t1 is after time t2; if one of them is zero,
// the zero time is seen as after non-zero time.
func afterOrZero(t1, t2 *metav1.Time) bool {
	if t1.Time.IsZero() || t2.Time.IsZero() {
		return t1.Time.IsZero()
	}
	return t1.After(t2.Time)
}

// logarithmicRankDiff calculates the base-2 logarithmic ranks of 2 timestamps,
// compared to the current timestamp.
func logarithmicRankDiff(t1, t2, now metav1.Time) int64 {
	d1 := now.Sub(t1.Time)
	d2 := now.Sub(t2.Time)
	r1 := int64(-1)
	r2 := int64(-1)
	if d1 > 0 {
		r1 = int64(math.Log2(float64(d1)))
	}
	if d2 > 0 {
		r2 = int64(math.Log2(float64(d2)))
	}
	return r1 - r2
}

func podReadyTime(pod *corev1.Pod) *metav1.Time {
	if util.IsPodReady(pod) {
		for _, c := range pod.Status.Conditions {
			// we only care about pod ready conditions
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				return &c.LastTransitionTime
			}
		}
	}
	return &metav1.Time{}
}

func maxContainerRestarts(pod *corev1.Pod) int {
	maxRestarts := 0
	for _, c := range pod.Status.ContainerStatuses {
		maxRestarts = integer.IntMax(maxRestarts, int(c.RestartCount))
	}
	return maxRestarts
}

// getDeletionCostFromPodAnnotations returns the integer value of pod-deletion-cost.
// Returns 0 if not set or the value is invalid.
func getDeletionCostFromPodAnnotations(annotations map[string]string) (int32, error) {
	if value, exist := annotations[corev1.PodDeletionCost]; exist {
		// values that start with plus sign (e.g, "+10") or leading zeros (e.g., "008") are not valid.
		if !validFirstDigit(value) {
			return 0, fmt.Errorf("invalid value %q", value)
		}

		i, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			// make sure we default to 0 on error.
			return 0, err
		}
		return int32(i), nil
	}
	return 0, nil
}

func validFirstDigit(str string) bool {
	if len(str) == 0 {
		return false
	}
	return str[0] == '-' || (str[0] == '0' && str == "0") || (str[0] >= '1' && str[0] <= '9')
}
