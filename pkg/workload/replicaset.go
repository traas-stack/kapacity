/*
 Copyright 2023 The Kapacity Authors.
 Copyright 2016 The Kubernetes Authors.

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

package workload

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/kubernetes/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/traas-stack/kapacity/pkg/util"
)

// ReplicaSet represents behaviors of a Kubernetes ReplicaSet.
type ReplicaSet struct {
	client.Client
	Namespace string
	Selector  labels.Selector
}

func (w *ReplicaSet) Sort(ctx context.Context, pods []*corev1.Pod) ([]*corev1.Pod, error) {
	// related pods are all pods which selected by the ReplicaSet
	relatedPods := &corev1.PodList{}
	if err := w.List(ctx, relatedPods, client.InNamespace(w.Namespace), client.MatchingLabelsSelector{Selector: w.Selector}); err != nil {
		return nil, fmt.Errorf("failed to list related pods: %v", err)
	}
	// build rank equal to the number of active pods in related pods that are colocated on the same node with the pod
	podsOnNode := make(map[string]int)
	for i := range relatedPods.Items {
		pod := &relatedPods.Items[i]
		if util.IsPodActive(pod) {
			podsOnNode[pod.Spec.NodeName]++
		}
	}
	ranks := make([]int, 0, len(pods))
	for i, pod := range pods {
		ranks[i] = podsOnNode[pod.Spec.NodeName]
	}
	sort.Sort(controller.ActivePodsWithRanks{
		Pods: pods,
		Rank: ranks,
		Now:  metav1.Now(),
	})
	return pods, nil
}

func (*ReplicaSet) CanSelectPodsToScaleDown(context.Context) bool {
	return false
}

func (*ReplicaSet) SelectPodsToScaleDown(context.Context, []*corev1.Pod) error {
	return fmt.Errorf("ReplicaSet does not support this operation")
}
